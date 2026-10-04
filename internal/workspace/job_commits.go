package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"trove.local/phase0/contracts"
)

type jobCommitKey struct{}
type jobCommit struct {
	JobID, Lease, Unit, Hash    string
	RebaseTranscriptDestination bool
}

func (k *Kernel) initServiceRuntime(ctx context.Context) error {
	return k.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS cancellation_receipts(actor_id TEXT NOT NULL,cancellation_id TEXT NOT NULL,transaction_id TEXT REFERENCES mutation_history(transaction_id),PRIMARY KEY(actor_id,cancellation_id));
CREATE TABLE IF NOT EXISTS job_authorizations(job_id TEXT PRIMARY KEY REFERENCES jobs(job_id),actor_id TEXT NOT NULL,grant_id TEXT NOT NULL,policy_revision TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN('approved','revoked')));
CREATE TABLE IF NOT EXISTS job_sources(job_id TEXT NOT NULL REFERENCES jobs(job_id),entity_type TEXT NOT NULL,entity_id TEXT NOT NULL,revision TEXT NOT NULL,PRIMARY KEY(job_id,entity_type,entity_id));
CREATE TABLE IF NOT EXISTS job_units(job_id TEXT NOT NULL REFERENCES jobs(job_id),unit_key TEXT NOT NULL,identities_json TEXT NOT NULL CHECK(json_valid(identities_json)),PRIMARY KEY(job_id,unit_key));
CREATE TABLE IF NOT EXISTS backup_destinations(id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,grant_id TEXT NOT NULL,path TEXT NOT NULL,expires_at TEXT NOT NULL,job_id TEXT REFERENCES jobs(job_id));
CREATE TABLE IF NOT EXISTS service_runtime_migrations(version INTEGER PRIMARY KEY,applied_at TEXT NOT NULL);
INSERT OR IGNORE INTO service_runtime_migrations VALUES(1,strftime('%Y-%m-%dT%H:%M:%fZ','now'));
`)
		if err != nil {
			return err
		}
		var exists int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('page_metadata') WHERE name='archived_at'").Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err = tx.ExecContext(ctx, "ALTER TABLE page_metadata ADD COLUMN archived_at TEXT"); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE page_metadata SET archived_at=(SELECT updated_at FROM pages WHERE id=page_id) WHERE archived=1"); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO service_runtime_migrations(version,applied_at) VALUES(2,?)", now())
		return err
	})
}
func (k *Kernel) cancelMutation(ctx context.Context, e contracts.ServiceExecutionContext, id string) (any, error) {
	key := string(e.Actor.ActorId) + ":" + id
	k.mu.Lock()
	c := k.cancellations[key]
	if c == nil {
		c = &cancellation{}
		k.cancellations[key] = c
	}
	c.cancelled = true
	if c.cancel != nil {
		c.cancel()
	}
	k.mu.Unlock()
	out := map[string]any{"cancellationId": id, "state": "cancelled"}
	err := k.write(ctx, func(tx *sql.Tx) error {
		var transaction sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT transaction_id FROM cancellation_receipts WHERE actor_id=? AND cancellation_id=?", e.Actor.ActorId, id).Scan(&transaction)
		if err == nil && transaction.Valid {
			out["state"] = "already_committed"
			out["transactionId"] = transaction.String
			return nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO cancellation_receipts(actor_id,cancellation_id) VALUES(?,?)", e.Actor.ActorId, id)
		return err
	})
	k.mu.Lock()
	if k.cancellations[key] == c && c.cancel == nil {
		delete(k.cancellations, key)
	}
	k.mu.Unlock()
	return out, err
}

// JobExecution reconstructs only the original approved input scope. It grants no
// general write approval; CommitJobBatch additionally checks lease and operation.
func (k *Kernel) JobExecution(ctx context.Context, jobID string) (contracts.ServiceExecutionContext, error) {
	var scopeJSON string
	if err := k.db.QueryRowContext(ctx, "SELECT c.scope_json FROM job_context c JOIN job_authorizations a USING(job_id) WHERE c.job_id=? AND a.state='approved' AND a.policy_revision='1'", jobID).Scan(&scopeJSON); err != nil {
		return contracts.ServiceExecutionContext{}, err
	}
	var scope contracts.DomainScope
	if err := json.Unmarshal([]byte(scopeJSON), &scope); err != nil {
		return contracts.ServiceExecutionContext{}, err
	}
	run := contracts.DomainId(jobID)
	return k.Grant(contracts.ServiceActor{Kind: "job", ActorId: run, RunId: &run}, "system", scope), nil
}

// CommitJobBatch commits a bounded registered result unit and its receipt in the
// same transaction as content/history/outbox. It is a trusted supervisor API.
func (k *Kernel) CommitJobBatch(ctx context.Context, e contracts.ServiceExecutionContext, jobID, lease, unit string, r contracts.ServiceBatchRequest) contracts.ServiceBatchResponse {
	return k.commitJobBatch(ctx, e, jobID, lease, unit, r, false)
}

// CommitTranscriptJobBatch admits only the leased meeting append/finalize
// operations. Its destination page revision is resolved inside the same writer
// transaction as the append, so typing cannot invalidate completed inference.
// Recording and meeting input revisions remain strictly fenced.
func (k *Kernel) CommitTranscriptJobBatch(ctx context.Context, e contracts.ServiceExecutionContext, jobID, lease, unit string, r contracts.ServiceBatchRequest) contracts.ServiceBatchResponse {
	return k.commitJobBatch(ctx, e, jobID, lease, unit, r, true)
}
func (k *Kernel) commitJobBatch(ctx context.Context, e contracts.ServiceExecutionContext, jobID, lease, unit string, r contracts.ServiceBatchRequest, rebaseTranscript bool) contracts.ServiceBatchResponse {
	if len(unit) == 0 || len(unit) > 128 {
		var out contracts.ServiceBatchResponse
		json.Unmarshal(errorResponse(string(r.RequestId), Fail("INVALID_REQUEST", "Invalid result unit key")), &out)
		return out
	}
	raw := marshal(r.Commands)
	hash := sha256.Sum256(raw)
	j := jobCommit{JobID: jobID, Lease: lease, Unit: unit, Hash: hex.EncodeToString(hash[:]), RebaseTranscriptDestination: rebaseTranscript}
	return k.Batch(context.WithValue(ctx, jobCommitKey{}, j), e, r)
}
func (k *Kernel) validateJobCommit(ctx context.Context, tx *sql.Tx, e contracts.ServiceExecutionContext, r *contracts.ServiceBatchRequest, j jobCommit) (json.RawMessage, error) {
	if e.Actor.Kind != "job" || string(e.Actor.ActorId) != j.JobID {
		return nil, Fail("SCOPE_DENIED", "Job execution principal mismatch")
	}
	var oldHash, response string
	err := tx.QueryRowContext(ctx, "SELECT u.result_hash,r.response_json FROM job_results u JOIN idempotency_receipts r ON r.transaction_id=u.transaction_id WHERE u.job_id=? AND u.unit_key=?", j.JobID, j.Unit).Scan(&oldHash, &response)
	if err == nil {
		if oldHash != j.Hash {
			return nil, Fail("IDEMPOTENCY_CONFLICT", "Job unit already committed different content")
		}
		var result contracts.ServiceBatchResult
		if err = json.Unmarshal([]byte(response), &result); err != nil {
			return nil, err
		}
		for _, change := range result.Revisions {
			if err = k.authorizeEntity(ctx, tx, e, change.Target.EntityType, string(change.Target.EntityId)); err != nil {
				return nil, err
			}
		}
		result.Replayed = true
		result.RequestId = r.RequestId
		return success(result), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var approved int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM job_authorizations WHERE job_id=? AND state='approved' AND policy_revision='1'", j.JobID).Scan(&approved); err != nil {
		return nil, err
	}
	if approved != 1 {
		return nil, Fail("SCOPE_DENIED", "Job delegation has been revoked")
	}
	job, err := k.checkLease(ctx, tx, j.JobID, j.Lease, false)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	json.Unmarshal(marshal(job.Payload), &payload)
	for _, command := range r.Commands {
		var c map[string]any
		json.Unmarshal(marshal(command), &c)
		switch string(job.Kind) {
		case "IMPORT_NOTION":
			if str(c["kind"]) != "import.applyPlan" || c["planId"] != payload["stagingId"] || c["planHash"] != payload["sourceRevision"] {
				return nil, Fail("SCOPE_DENIED", "Import job can only commit its reviewed plan and source revision")
			}
		case "TRANSCRIBE_AUDIO":
			if (str(c["kind"]) != "meeting.appendSegments" && str(c["kind"]) != "meeting.finalize") || c["meetingId"] != payload["meetingId"] {
				return nil, Fail("SCOPE_DENIED", "Transcription job can only commit its own meeting transcript")
			}
		default:
			return nil, Fail("UNSUPPORTED_SCHEMA", "No canonical result operation is registered for this job kind")
		}
	}
	if j.RebaseTranscriptDestination {
		if job.Kind != "TRANSCRIBE_AUDIO" {
			return nil, Fail("SCOPE_DENIED", "Only transcription can rebase an append destination")
		}
		payload, ok := job.Payload.Value.(contracts.JobsJobPayloadVariant5)
		if !ok {
			return nil, Fail("INVALID_REQUEST", "Expected transcription payload")
		}
		if err = k.rebaseTranscriptDestination(ctx, tx, e, j.JobID, payload, r); err != nil {
			return nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT entity_type,entity_id,revision FROM job_sources WHERE job_id=?", j.JobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []contracts.ServiceExpectedRevision{}
	for rows.Next() {
		var kind, id, revision string
		if err = rows.Scan(&kind, &id, &revision); err != nil {
			return nil, err
		}
		sources = append(sources, required(kind, id, revision))
	}
	rows.Close()
	for _, source := range sources {
		table := entityTables[source.Target.EntityType]
		var actual string
		if err = tx.QueryRowContext(ctx, "SELECT revision FROM "+table+" WHERE id=?", source.Target.EntityId).Scan(&actual); err != nil {
			return nil, err
		}
		if actual != source.Revision {
			return nil, &ServiceFailure{Code: "CONFLICT", Message: "Job input changed after enqueue", Current: []contracts.ServiceExpectedRevision{required(source.Target.EntityType, string(source.Target.EntityId), actual)}}
		}
		if err = k.authorizeEntity(ctx, tx, e, source.Target.EntityType, string(source.Target.EntityId)); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
func (k *Kernel) bindJobSources(ctx context.Context, tx *sql.Tx, id string, payload map[string]any) error {
	refs := map[string]string{}
	expected := map[string]string{}
	var walk func(map[string]any)
	walk = func(p map[string]any) {
		if page, revision := str(p["pageId"]), str(p["sourceRevision"]); page != "" && revision != "" {
			expected[guardKey("page", page)] = revision
		}
		for key, kind := range map[string]string{"pageId": "page", "fileId": "file", "meetingId": "meeting"} {
			if id := str(p[key]); id != "" {
				refs[guardKey(kind, id)] = kind
			}
		}
		for _, v := range p {
			switch x := v.(type) {
			case map[string]any:
				walk(x)
			case []any:
				for _, v := range x {
					if m, ok := v.(map[string]any); ok {
						walk(m)
					}
				}
			}
		}
	}
	walk(payload)
	for key, kind := range refs {
		entity := strings.TrimPrefix(key, kind+":")
		var rev string
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM "+entityTables[kind]+" WHERE id=?", entity).Scan(&rev); err != nil {
			return err
		}
		if want := expected[key]; want != "" && want != rev {
			return &ServiceFailure{Code: "CONFLICT", Message: "Enqueued source revision is stale", Current: []contracts.ServiceExpectedRevision{required(kind, entity, rev)}}
		}
		if kind == "page" {
			var deleted sql.NullString
			if err := tx.QueryRowContext(ctx, "SELECT deleted_at FROM pages WHERE id=?", entity).Scan(&deleted); err != nil {
				return err
			}
			if deleted.Valid {
				return Fail("NOT_FOUND", "Job source was deleted")
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO job_sources VALUES(?,?,?,?)", id, kind, entity, rev); err != nil {
			return err
		}
		if kind == "meeting" {
			var page, rev string
			if err := tx.QueryRowContext(ctx, "SELECT p.id,p.revision FROM pages p JOIN meetings m ON m.page_id=p.id WHERE m.id=?", entity).Scan(&page, &rev); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO job_sources VALUES(?,'page',?,?)", id, page, rev); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReserveJobIDs makes identities durable before native work so retries reuse them.
func (k *Kernel) ReserveJobIDs(ctx context.Context, id, lease, unit string, count int) ([]string, error) {
	if count < 1 || count > 512 || len(unit) > 128 {
		return nil, Fail("INVALID_REQUEST", "Invalid identity reservation")
	}
	var ids []string
	err := k.write(ctx, func(tx *sql.Tx) error {
		if _, err := k.checkLease(ctx, tx, id, lease, false); err != nil {
			return err
		}
		var raw string
		err := tx.QueryRowContext(ctx, "SELECT identities_json FROM job_units WHERE job_id=? AND unit_key=?", id, unit).Scan(&raw)
		if err == nil {
			if err = json.Unmarshal([]byte(raw), &ids); err != nil {
				return err
			}
			if len(ids) != count {
				return Fail("CONFLICT", "Reservation count changed")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for i := 0; i < count; i++ {
			ids = append(ids, NewID())
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO job_units VALUES(?,?,?)", id, unit, string(marshal(ids)))
		return err
	})
	return ids, err
}
