package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"trove.local/phase0/contracts"
)

type Mutation struct {
	Tx        *sql.Tx
	Kernel    *Kernel
	Context   context.Context
	Execution contracts.ServiceExecutionContext
	Request   contracts.ServiceBatchRequest
	guards    map[string]string
	touched   map[string]contracts.ServiceRevisionChange
	before    map[string]json.RawMessage
}

var entityTables = map[string]string{"page": "pages", "block": "blocks", "collection": "collections", "property": "properties", "record": "records", "view": "views", "container": "containers", "file": "files", "meeting": "meetings", "memory": "memory_entries", "transcript_segment": "transcript_segments"}

func (m *Mutation) Require(kind, id string) error {
	key := guardKey(kind, id)
	expected, ok := m.guards[key]
	if !ok {
		return Fail("INVALID_REQUEST", "Missing revision guard for "+key)
	}
	if expected != "0" {
		return m.Kernel.authorizeEntity(m.Context, m.Tx, m.Execution, kind, id)
	}
	return nil
}
func (m *Mutation) Touch(kind, id string) error {
	if err := m.Require(kind, id); err != nil {
		return err
	}
	key := guardKey(kind, id)
	if _, ok := m.touched[key]; !ok {
		before := m.guards[key]
		after, err := revNext(before)
		if err != nil {
			return err
		}
		m.touched[key] = contracts.ServiceRevisionChange{Target: contracts.ServiceEntityRef{EntityType: kind, EntityId: contracts.DomainId(id)}, Before: before, After: contracts.DomainRevision(after)}
	}
	return nil
}
func (m *Mutation) Deleted(kind, id string) error {
	if err := m.Touch(kind, id); err != nil {
		return err
	}
	key := guardKey(kind, id)
	v := m.touched[key]
	v.Deleted = true
	m.touched[key] = v
	return nil
}
func (m *Mutation) CapturePage(id string) error {
	if _, ok := m.before[id]; ok {
		return nil
	}
	p, e := readPageTx(m.Context, m.Tx, id)
	if errors.Is(e, sql.ErrNoRows) {
		m.before[id] = json.RawMessage(`null`)
		return nil
	}
	if e != nil {
		return e
	}
	snapshot, err := captureRecoverySnapshot(m.Context, m.Tx, id, p)
	if err != nil {
		return err
	}
	m.before[id] = snapshot
	_, e = m.Tx.ExecContext(m.Context, "INSERT OR IGNORE INTO page_snapshots(snapshot_id,page_id,revision,snapshot_json,created_at) VALUES(?,?,?,?,?)", NewID(), id, p.Page.Revision, string(m.before[id]), now())
	return e
}
func (m *Mutation) TouchPage(id string) error {
	if err := m.CapturePage(id); err != nil {
		return err
	}
	if err := m.Touch("page", id); err != nil {
		return err
	}
	var record string
	err := m.Tx.QueryRowContext(m.Context, "SELECT id FROM records WHERE page_id=?", id).Scan(&record)
	if err == nil {
		return m.Touch("record", record)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}
func (m *Mutation) AuthorizePage(id string) error {
	return m.Kernel.authorizePage(m.Context, m.Tx, m.Execution, id)
}
func (m *Mutation) Now() string   { return now() }
func (m *Mutation) NewID() string { return NewID() }
func commandHash(req contracts.ServiceBatchRequest, proposal bool) (string, error) {
	r := req
	r.Mutation.ExpectedRevisions = append([]contracts.ServiceExpectedRevision(nil), req.Mutation.ExpectedRevisions...)
	sort.Slice(r.Mutation.ExpectedRevisions, func(i, j int) bool {
		return guardKey(r.Mutation.ExpectedRevisions[i].Target.EntityType, string(r.Mutation.ExpectedRevisions[i].Target.EntityId)) < guardKey(r.Mutation.ExpectedRevisions[j].Target.EntityType, string(r.Mutation.ExpectedRevisions[j].Target.EntityId))
	})
	if proposal {
		r.Mutation.ApprovalId = nil
	}
	b, e := json.Marshal(map[string]any{"workspaceId": r.WorkspaceId, "commands": r.Commands, "mutation": r.Mutation})
	if e != nil {
		return "", e
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if e = decoder.Decode(&value); e != nil {
		return "", e
	}
	b, e = canonicalJSON(value)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func (k *Kernel) Batch(ctx context.Context, e contracts.ServiceExecutionContext, req contracts.ServiceBatchRequest) contracts.ServiceBatchResponse {
	raw := k.batch(ctx, e, marshal(req))
	var out contracts.ServiceBatchResponse
	json.Unmarshal(raw, &out)
	return out
}
func (k *Kernel) batch(ctx context.Context, e contracts.ServiceExecutionContext, raw []byte) json.RawMessage {
	req, err := contracts.DecodeBatch(raw)
	if err != nil {
		return errorResponse("", Fail("INVALID_REQUEST", err.Error()))
	}
	if err = k.authorize(e); err != nil {
		return errorResponse(string(req.RequestId), err)
	}
	if string(req.WorkspaceId) != k.id || req.Mutation.ScopeGrantId != e.ScopeGrantId {
		return errorResponse(string(req.RequestId), Fail("SCOPE_DENIED", "Workspace or scope grant mismatch"))
	}
	hash, err := commandHash(req, false)
	if err != nil {
		return errorResponse(string(req.RequestId), err)
	}
	cancellationID := NewID()
	if req.CancellationId != nil {
		cancellationID = string(*req.CancellationId)
	}
	cancellationKey := string(e.Actor.ActorId) + ":" + cancellationID
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	k.mu.Lock()
	if old := k.cancellations[cancellationKey]; old != nil && old.cancelled && old.transaction == "" {
		cancel()
	}
	activeCancellation := &cancellation{cancel: cancel}
	k.cancellations[cancellationKey] = activeCancellation
	k.mu.Unlock()
	defer func() {
		k.mu.Lock()
		if k.cancellations[cancellationKey] == activeCancellation {
			delete(k.cancellations, cancellationKey)
		}
		k.mu.Unlock()
	}()
	jobUnit, hasJobUnit := ctx.Value(jobCommitKey{}).(jobCommit)
	ctx = context.WithValue(ctx, commitGrantKey{}, e)
	var result json.RawMessage
	var event json.RawMessage
	var jobEvent json.RawMessage
	err = k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorize(e); err != nil {
			return err
		}
		if hasJobUnit {
			replay, err := k.validateJobCommit(ctx, tx, e, &req, jobUnit)
			if err != nil {
				return err
			}
			if replay != nil {
				result = replay
				return nil
			}
			if jobUnit.RebaseTranscriptDestination {
				hash, err = commandHash(req, false)
				if err != nil {
					return err
				}
			}
		}
		var receiptHash, receipt string
		err := tx.QueryRowContext(ctx, "SELECT command_hash,response_json FROM idempotency_receipts WHERE workspace_id=? AND actor_id=? AND idempotency_key=?", k.id, e.Actor.ActorId, req.Mutation.IdempotencyKey).Scan(&receiptHash, &receipt)
		if err == nil {
			if receiptHash != hash {
				return Fail("IDEMPOTENCY_CONFLICT", "Idempotency key was used for another mutation")
			}
			var replay contracts.ServiceBatchResult
			json.Unmarshal([]byte(receipt), &replay)
			for _, r := range replay.Revisions {
				if (r.Target.EntityType == "block" || r.Target.EntityType == "property") && r.Deleted {
					continue
				}
				if err = k.authorizeEntity(ctx, tx, e, r.Target.EntityType, string(r.Target.EntityId)); err != nil {
					return err
				}
			}
			replay.Replayed = true
			replay.RequestId = req.RequestId
			result = success(replay)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var cancelReceipt sql.NullString
		cancelErr := tx.QueryRowContext(ctx, "SELECT transaction_id FROM cancellation_receipts WHERE actor_id=? AND cancellation_id=?", e.Actor.ActorId, cancellationID).Scan(&cancelReceipt)
		if cancelErr == nil {
			if cancelReceipt.Valid {
				return Fail("ALREADY_COMMITTED", "Cancellation ID already belongs to a committed mutation")
			}
			return Fail("CANCELLED", "Cancellation won before commit")
		}
		if !errors.Is(cancelErr, sql.ErrNoRows) {
			return cancelErr
		}
		m := &Mutation{Tx: tx, Kernel: k, Context: ctx, Execution: e, Request: req, guards: map[string]string{}, touched: map[string]contracts.ServiceRevisionChange{}, before: map[string]json.RawMessage{}}
		for _, g := range req.Mutation.ExpectedRevisions {
			kind, id := g.Target.EntityType, string(g.Target.EntityId)
			table, ok := entityTables[kind]
			if !ok {
				return Fail("INVALID_REQUEST", "Unsupported revision guard kind")
			}
			key := guardKey(kind, id)
			if _, ok = m.guards[key]; ok {
				return Fail("INVALID_REQUEST", "Duplicate revision guard")
			}
			var actual string
			err := tx.QueryRowContext(ctx, "SELECT revision FROM "+table+" WHERE id=?", id).Scan(&actual)
			if errors.Is(err, sql.ErrNoRows) {
				actual = "0"
				err = nil
			}
			if err != nil {
				return err
			}
			if actual != "0" {
				if err = k.authorizeEntity(ctx, tx, e, kind, id); err != nil {
					return err
				}
			}
			if actual != g.Revision {
				return &ServiceFailure{Code: "CONFLICT", Message: "Entity revision changed", Current: []contracts.ServiceExpectedRevision{required(kind, id, actual)}}
			}
			m.guards[key] = g.Revision
		}
		if e.Origin != "user" && !hasJobUnit {
			proposal, _ := commandHash(req, true)
			if req.Mutation.ApprovalId == nil {
				return &ServiceFailure{Code: "APPROVAL_REQUIRED", Message: "Exact proposal approval is required", ApprovalHash: proposal}
			}
			var actor, grant, policy, approvedHash, expires, state string
			err = tx.QueryRowContext(ctx, "SELECT actor_id,scope_grant_id,policy_revision,command_hash,expires_at,state FROM approvals WHERE approval_id=?", *req.Mutation.ApprovalId).Scan(&actor, &grant, &policy, &approvedHash, &expires, &state)
			if err != nil {
				return Fail("APPROVAL_REQUIRED", "Approval is absent")
			}
			expiry, _ := time.Parse(time.RFC3339Nano, expires)
			if actor != string(e.Actor.ActorId) || grant != string(e.ScopeGrantId) || policy != string(e.PolicyRevision) || approvedHash != proposal || state != "approved" || !expiry.After(time.Now()) {
				return Fail("APPROVAL_REQUIRED", "Approval is stale, changed, expired, or already used")
			}
			if _, err = tx.ExecContext(ctx, "UPDATE approvals SET state='consumed' WHERE approval_id=?", *req.Mutation.ApprovalId); err != nil {
				return err
			}
		}
		for _, command := range req.Commands {
			var c map[string]any
			json.Unmarshal(marshal(command), &c)
			h := commandHandlers[str(c["kind"])]
			if h == nil {
				return Fail("UNSUPPORTED_SCHEMA", "Command is not available: "+str(c["kind"]))
			}
			if err = h(m, c); err != nil {
				return err
			}
		}
		for _, validate := range commitValidators {
			if err = validate(m); err != nil {
				return err
			}
		}
		// Dependency invalidation is a registered system side effect; the caller does
		// not acquire permission to inspect or edit the hidden memory body.
		for page := range m.before {
			rows, err := tx.QueryContext(ctx, `SELECT id,page_id,revision FROM memory_entries WHERE status='active' AND id IN(SELECT memory_id FROM memory_sources WHERE page_id=?)`, page)
			if err != nil {
				return err
			}
			type dependent struct{ id, page, revision string }
			deps := []dependent{}
			for rows.Next() {
				var d dependent
				if err = rows.Scan(&d.id, &d.page, &d.revision); err != nil {
					rows.Close()
					return err
				}
				deps = append(deps, d)
			}
			rows.Close()
			for _, d := range deps {
				key := guardKey("memory", d.id)
				next, err := revNext(d.revision)
				if err != nil {
					return err
				}
				m.touched[key] = contracts.ServiceRevisionChange{Target: contracts.ServiceEntityRef{EntityType: "memory", EntityId: contracts.DomainId(d.id)}, Before: d.revision, After: contracts.DomainRevision(next)}
				if err = m.CapturePage(d.page); err != nil {
					return err
				}
				var prev string
				if err = tx.QueryRowContext(ctx, "SELECT revision FROM pages WHERE id=?", d.page).Scan(&prev); err != nil {
					return err
				}
				next, err = revNext(prev)
				if err != nil {
					return err
				}
				m.touched[guardKey("page", d.page)] = contracts.ServiceRevisionChange{Target: contracts.ServiceEntityRef{EntityType: "page", EntityId: contracts.DomainId(d.page)}, Before: prev, After: contracts.DomainRevision(next)}
				if _, err = tx.ExecContext(ctx, "UPDATE memory_entries SET status='stale' WHERE id=?", d.id); err != nil {
					return err
				}
			}
		}
		keys := make([]string, 0, len(m.touched))
		for key := range m.touched {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		changes := make([]contracts.ServiceRevisionChange, 0, len(keys))
		for _, key := range keys {
			c := m.touched[key]
			kind, id := c.Target.EntityType, string(c.Target.EntityId)
			if c.Deleted && (kind == "block" || kind == "transcript_segment" || kind == "property") {
				_, err = tx.ExecContext(ctx, "INSERT INTO entity_tombstones VALUES(?,?,?,?) ON CONFLICT(entity_type,entity_id) DO UPDATE SET revision=excluded.revision,deleted_at=excluded.deleted_at", kind, id, c.After, now())
			} else {
				_, err = tx.ExecContext(ctx, "UPDATE "+entityTables[kind]+" SET revision=? WHERE id=?", c.After, id)
			}
			if err != nil {
				return err
			}
			if kind == "page" {
				_, err = tx.ExecContext(ctx, "UPDATE pages SET updated_at=? WHERE id=?", now(), id)
				if err != nil {
					return err
				}
			}
			changes = append(changes, c)
		}
		for _, project := range commitProjections {
			if err = project(m); err != nil {
				return err
			}
		}
		after := map[string]json.RawMessage{}
		for page := range m.before {
			p, err := readPageTx(ctx, tx, page)
			if err != nil {
				return err
			}
			if err = tx.QueryRowContext(ctx, "SELECT CAST(COALESCE(MAX(cursor),0)+1 AS TEXT) FROM committed_events").Scan(&p.SnapshotCursor); err != nil {
				return err
			}
			snapshot, err := captureRecoverySnapshot(ctx, tx, page, p)
			if err != nil {
				return err
			}
			after[page] = snapshot
			if _, err = tx.ExecContext(ctx, "INSERT INTO page_snapshots(snapshot_id,page_id,revision,snapshot_json,created_at) VALUES(?,?,?,?,?) ON CONFLICT(page_id,revision) DO UPDATE SET snapshot_json=excluded.snapshot_json", NewID(), page, p.Page.Revision, string(after[page]), now()); err != nil {
				return err
			}
		}

		committedContext := e
		automationCount := 0
		for _, command := range req.Commands {
			if _, ok := command.Value.(contracts.ServiceRunAutomation); ok {
				automationCount++
			}
		}
		if automationCount > 0 {
			if len(req.Commands) != 1 {
				return Fail("INVALID_REQUEST", "Automation execution must be an isolated mutation batch")
			}
			committedContext.Origin = "automation"
		}
		if strings.HasPrefix(req.Mutation.IdempotencyKey, "ai-proposal-") {
			committedContext.Origin = "ai"
		}
		restoreCount := 0
		for _, command := range req.Commands {
			if _, ok := command.Value.(contracts.ServiceRestoreRevision); ok {
				restoreCount++
			}
		}
		if restoreCount > 0 {
			if restoreCount != len(req.Commands) {
				return Fail("INVALID_REQUEST", "Restoration must be an isolated mutation batch")
			}
			if e.Origin == "user" {
				committedContext.Origin = "restore"
			}
		}
		transaction, history, eventID := NewID(), NewID(), NewID()
		committed := now()
		_, err = tx.ExecContext(ctx, `INSERT INTO mutation_history VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, history, transaction, req.RequestId, e.Actor.ActorId, committedContext.Origin, string(marshal(e.Scope)), e.PolicyRevision, req.Mutation.ApprovalId, hash, string(marshal(m.before)), string(marshal(after)), committed)
		if err != nil {
			return err
		}
		var cursor int64
		if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(cursor),0)+1 FROM committed_events").Scan(&cursor); err != nil {
			return err
		}
		ev := contracts.ServiceCommittedChangeEvent{ContractVersion: 1, EventId: contracts.DomainId(eventID), WorkspaceId: contracts.DomainId(k.id), TransactionId: contracts.DomainId(transaction), Cursor: contracts.DomainRevision(strconv.FormatInt(cursor, 10)), CommittedAt: contracts.DomainTimestamp(committed), Context: committedContext, Revisions: changes, MutationHistoryId: contracts.DomainId(history), IdempotencyKey: req.Mutation.IdempotencyKey}
		event = marshal(ev)
		if _, err = tx.ExecContext(ctx, "INSERT INTO committed_events(cursor,event_id,transaction_id,payload_json) VALUES(?,?,?,?)", cursor, eventID, transaction, string(event)); err != nil {
			return err
		}
		res := contracts.ServiceBatchResult{RequestId: req.RequestId, TransactionId: contracts.DomainId(transaction), EventCursor: ev.Cursor, Revisions: changes, Replayed: false}
		if _, err = tx.ExecContext(ctx, "INSERT INTO idempotency_receipts VALUES(?,?,?,?,?,?)", k.id, e.Actor.ActorId, req.Mutation.IdempotencyKey, hash, transaction, string(marshal(res))); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO cancellation_receipts VALUES(?,?,?)", e.Actor.ActorId, cancellationID, transaction); err != nil {
			return err
		}
		if hasJobUnit {
			if _, err = tx.ExecContext(ctx, "INSERT INTO job_results VALUES(?,?,?,?)", jobUnit.JobID, jobUnit.Unit, transaction, jobUnit.Hash); err != nil {
				return err
			}
			for _, change := range changes {
				if _, err = tx.ExecContext(ctx, "UPDATE job_sources SET revision=? WHERE job_id=? AND entity_type=? AND entity_id=?", change.After, jobUnit.JobID, change.Target.EntityType, change.Target.EntityId); err != nil {
					return err
				}
			}
		}
		if hasJobUnit {
			for _, command := range req.Commands {
				if c, ok := command.Value.(contracts.ServiceFinalizeMeeting); ok && c.Kind == "meeting.finalize" {
					if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state='succeeded',revision=revision+1,lease_token=NULL,lease_expires_at=NULL,updated_at=? WHERE job_id=?", now(), jobUnit.JobID); err != nil {
						return err
					}
					job, err := k.job(ctx, tx, jobUnit.JobID)
					if err != nil {
						return err
					}
					progress := contracts.JobsProgress{Sequence: 0, Completed: 1, Unit: "steps"}
					jobEvent = marshal(contracts.JobsJobProgressEvent{ContractVersion: 1, JobId: job.JobId, JobRevision: job.Revision, Attempt: job.Attempt, State: job.State, Progress: progress})
					if _, err = tx.ExecContext(ctx, "INSERT INTO job_events(job_id,payload_json) VALUES(?,?)", jobUnit.JobID, string(jobEvent)); err != nil {
						return err
					}
				}
			}
		}

		result = success(res)
		return nil
	})
	if err != nil {
		return errorResponse(string(req.RequestId), err)
	}
	k.mu.Lock()
	if c := k.cancellations[cancellationKey]; c != nil {
		var res struct {
			Result contracts.ServiceBatchResult `json:"result"`
		}
		json.Unmarshal(result, &res)
		c.transaction = string(res.Result.TransactionId)
		c.cancel = nil
	}
	k.mu.Unlock()
	if event != nil {
		k.publish(event)
	}
	if jobEvent != nil {
		k.publish(jobEvent)
	}
	return result
}
func timeAfterMinutes(n int) string {
	return time.Now().UTC().Add(time.Duration(n) * time.Minute).Format(time.RFC3339Nano)
}
func (m *Mutation) Exec(query string, args ...any) (sql.Result, error) {
	return m.Tx.ExecContext(m.Context, query, args...)
}
func (m *Mutation) GuardRevision(kind, id string) string { return m.guards[guardKey(kind, id)] }
func (m *Mutation) String() string                       { return fmt.Sprintf("mutation %s", m.Request.RequestId) }
