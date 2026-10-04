package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

var jobPayloadKinds = map[string]string{"IMPORT_NOTION": "import", "EXTRACT_DOCUMENT": "file", "CREATE_THUMBNAIL": "file", "INDEX_CONTENT": "index", "GENERATE_EMBEDDINGS": "embedding", "TRANSCRIBE_AUDIO": "transcribe", "SUMMARIZE_MEETING": "summarize", "BACKUP_WORKSPACE": "backup", "MODEL_PACK_ACQUIRE": "model", "INVALIDATE_MEMORY": "memory", "AUTOMATION_RUN": "automation"}

func (k *Kernel) enqueueJob(ctx context.Context, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("APPROVAL_REQUIRED", "Only an explicit authenticated user action can delegate a persistent job")
	}
	payload, _ := r["payload"].(map[string]any)
	kind := str(r["kind"])
	if expected := jobPayloadKinds[kind]; expected == "" || str(payload["kind"]) != expected {
		return nil, Fail("INVALID_REQUEST", "Job kind and payload mismatch")
	}
	if err := k.authorizeJobPayload(ctx, e, payload); err != nil {
		return nil, err
	}
	id := NewID()
	hash := sha256.Sum256(marshal(payload))
	digest := hex.EncodeToString(hash[:])
	err := k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorize(e); err != nil {
			return err
		}
		var old, actor string
		err := tx.QueryRowContext(ctx, "SELECT j.job_id,c.payload_hash,c.actor_id FROM jobs j JOIN job_context c USING(job_id) WHERE workspace_id=? AND kind=? AND idempotency_key=?", k.id, kind, r["idempotencyKey"]).Scan(&id, &old, &actor)
		if err == nil {
			if old != digest || actor != string(e.Actor.ActorId) {
				return Fail("IDEMPOTENCY_CONFLICT", "Job key already belongs to another request")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id = NewID()
		t := now()
		_, err = tx.ExecContext(ctx, "INSERT INTO jobs(job_id,workspace_id,kind,payload_version,payload_json,state,revision,idempotency_key,max_attempts,created_at,updated_at) VALUES(?,?,?,1,?,'queued',1,?,3,?,?)", id, k.id, kind, string(marshal(payload)), r["idempotencyKey"], t, t)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO job_context VALUES(?,?,?,?)", id, e.Actor.ActorId, string(marshal(e.Scope)), digest)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO job_authorizations VALUES(?,?,?,?,'approved')", id, e.Actor.ActorId, e.ScopeGrantId, e.PolicyRevision); err != nil {
			return err
		}
		return k.bindJobSources(ctx, tx, id, payload)
	})
	if err != nil {
		return nil, err
	}
	return k.job(ctx, k.db, id)
}
func (k *Kernel) authorizeJobPayload(ctx context.Context, e contracts.ServiceExecutionContext, p map[string]any) error {
	if err := k.authorize(e); err != nil {
		return err
	}
	// Administrative acquisitions and backups have workspace-wide effects.
	switch str(p["kind"]) {
	case "backup", "model", "import", "automation":
		if s, ok := e.Scope.Value.(contracts.DomainScopeVariant1); !ok || string(s.WorkspaceId) != k.id {
			return Fail("SCOPE_DENIED", "Job requires workspace scope")
		}
	}
	for key, kind := range map[string]string{"fileId": "file", "meetingId": "meeting"} {
		if id := str(p[key]); id != "" {
			if err := k.authorizeEntity(ctx, k.db, e, kind, id); err != nil {
				return err
			}
		}
	}
	if ids, ok := p["memoryIds"].([]any); ok {
		for _, id := range ids {
			if err := k.authorizeEntity(ctx, k.db, e, "memory", str(id)); err != nil {
				return err
			}
		}
	}
	if loc, ok := p["location"].(map[string]any); ok {
		if err := k.authorizeJobPayload(ctx, e, loc); err != nil {
			return err
		}
	}
	if locations, ok := p["locations"].([]any); ok {
		for _, value := range locations {
			if loc, ok := value.(map[string]any); ok {
				if err := k.authorizeJobPayload(ctx, e, loc); err != nil {
					return err
				}
			}
		}
	}
	for _, key := range []string{"pageId", "sourcePageId"} {
		if id := str(p[key]); id != "" {
			if err := k.authorizePage(ctx, k.db, e, id); err != nil {
				return err
			}
		}
	}
	if ids, ok := p["pageIds"].([]any); ok {
		for _, v := range ids {
			if err := k.authorizePage(ctx, k.db, e, str(v)); err != nil {
				return err
			}
		}
	}
	return nil
}
func (k *Kernel) job(ctx context.Context, q querier, id string) (contracts.JobsJob, error) {
	var j contracts.JobsJob
	var payload string
	var next, lease, expires, checkpoint, progress, er sql.NullString
	err := q.QueryRowContext(ctx, "SELECT job_id,workspace_id,kind,payload_version,payload_json,state,CAST(revision AS TEXT),idempotency_key,attempt,max_attempts,next_attempt_at,lease_token,lease_expires_at,checkpoint_ref,progress_json,created_at,updated_at,error_json FROM jobs WHERE job_id=?", id).Scan(&j.JobId, &j.WorkspaceId, &j.Kind, &j.PayloadVersion, &payload, &j.State, &j.Revision, &j.IdempotencyKey, &j.Attempt, &j.MaxAttempts, &next, &lease, &expires, &checkpoint, &progress, &j.CreatedAt, &j.UpdatedAt, &er)
	if err != nil {
		return j, err
	}
	if err = json.Unmarshal([]byte(payload), &j.Payload); err != nil {
		return j, err
	}
	if next.Valid {
		v := contracts.DomainTimestamp(next.String)
		j.NextAttemptAt = &v
	}
	if lease.Valid {
		v := contracts.DomainId(lease.String)
		j.LeaseToken = &v
	}
	if expires.Valid {
		v := contracts.DomainTimestamp(expires.String)
		j.LeaseExpiresAt = &v
	}
	if checkpoint.Valid {
		v := contracts.DomainId(checkpoint.String)
		j.CheckpointRef = &v
	}
	if progress.Valid {
		json.Unmarshal([]byte(progress.String), &j.Progress)
	}
	if er.Valid {
		json.Unmarshal([]byte(er.String), &j.Error)
	}
	return j, nil
}
func (k *Kernel) getAuthorizedJob(ctx context.Context, e contracts.ServiceExecutionContext, id string) (any, error) {
	var actor string
	if err := k.db.QueryRowContext(ctx, "SELECT actor_id FROM job_context WHERE job_id=?", id).Scan(&actor); err != nil {
		return nil, err
	}
	if actor != string(e.Actor.ActorId) {
		return nil, Fail("SCOPE_DENIED", "Job belongs to another principal")
	}
	j, err := k.job(ctx, k.db, id)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	json.Unmarshal(marshal(j.Payload), &payload)
	if err = k.authorizeJobPayload(ctx, e, payload); err != nil {
		return nil, err
	}
	return j, nil
}
func (k *Kernel) cancelJob(ctx context.Context, e contracts.ServiceExecutionContext, id, revision string) (any, error) {
	if _, err := k.getAuthorizedJob(ctx, e, id); err != nil {
		return nil, err
	}
	err := k.transitionJob(ctx, id, func(tx *sql.Tx) error {
		j, err := k.job(ctx, tx, id)
		if err != nil {
			return err
		}
		if string(j.Revision) != revision {
			return &ServiceFailure{Code: "CONFLICT", Message: "Job revision changed"}
		}
		state := string(j.State)
		switch state {
		case "running":
			state = "cancel_requested"
		case "queued", "retry_wait", "interrupted":
			state = "cancelled"
		default:
			return nil
		}
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET state=?,revision=revision+1,updated_at=? WHERE job_id=?", state, now(), id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return k.job(ctx, k.db, id)
}

// ClaimJob is only called by the trusted supervisor. A fresh lease fences old workers.
func (k *Kernel) ClaimJob(ctx context.Context, kinds []string) (contracts.JobsJob, error) {
	var result contracts.JobsJob
	err := k.write(ctx, func(tx *sql.Tx) error {
		query := "SELECT job_id,kind FROM jobs WHERE (state='queued' OR (state='retry_wait' AND next_attempt_at<=?)) AND attempt<max_attempts ORDER BY CASE kind WHEN 'GENERATE' THEN 0 WHEN 'TRANSCRIBE_AUDIO' THEN 1 ELSE 2 END,created_at"
		rows, err := tx.QueryContext(ctx, query, now())
		if err != nil {
			return err
		}
		id := ""
		for rows.Next() {
			var candidate, kind string
			rows.Scan(&candidate, &kind)
			allowed := len(kinds) == 0
			for _, k := range kinds {
				if kind == k {
					allowed = true
				}
			}
			if allowed {
				id = candidate
				break
			}
		}
		rows.Close()
		if id == "" {
			return sql.ErrNoRows
		}
		lease := NewID()
		expires := time.Now().UTC().Add(30 * time.Second).Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET state='running',attempt=attempt+1,revision=revision+1,lease_token=?,lease_expires_at=?,next_attempt_at=NULL,progress_json=NULL,updated_at=? WHERE job_id=?", lease, expires, now(), id)
		if err != nil {
			return err
		}
		result, err = k.job(ctx, tx, id)
		return err
	})
	return result, err
}
func (k *Kernel) HeartbeatJob(ctx context.Context, id, lease string) error {
	return k.write(ctx, func(tx *sql.Tx) error {
		if _, err := k.checkLease(ctx, tx, id, lease, false); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE jobs SET lease_expires_at=? WHERE job_id=?", time.Now().UTC().Add(30*time.Second).Format(time.RFC3339Nano), id)
		return err
	})
}
func (k *Kernel) checkLease(ctx context.Context, tx *sql.Tx, id, lease string, allowCancel bool) (contracts.JobsJob, error) {
	j, err := k.job(ctx, tx, id)
	if err != nil {
		return j, err
	}
	if j.LeaseToken == nil || string(*j.LeaseToken) != lease || j.LeaseExpiresAt == nil {
		return j, Fail("CONFLICT", "Worker lease is no longer active")
	}
	expiry, _ := time.Parse(time.RFC3339Nano, string(*j.LeaseExpiresAt))
	if !expiry.After(time.Now()) {
		return j, Fail("CONFLICT", "Worker lease expired")
	}
	if j.State != "running" && !(allowCancel && j.State == "cancel_requested") {
		return j, Fail("CANCELLED", "Job no longer accepts results")
	}
	return j, nil
}
func (k *Kernel) ProgressJob(ctx context.Context, id, lease string, p contracts.JobsProgress) error {
	if err := contracts.Validate("jobs", "Progress", marshal(p)); err != nil {
		return Fail("INVALID_REQUEST", err.Error())
	}
	var event json.RawMessage
	err := k.write(ctx, func(tx *sql.Tx) error {
		j, err := k.checkLease(ctx, tx, id, lease, false)
		if err != nil {
			return err
		}
		if j.Progress != nil && p.Sequence <= j.Progress.Sequence {
			return Fail("CONFLICT", "Progress sequence must increase")
		}
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET progress_json=?,revision=revision+1,updated_at=? WHERE job_id=?", string(marshal(p)), now(), id)
		if err != nil {
			return err
		}
		rev, _ := revNext(string(j.Revision))
		event = marshal(contracts.JobsJobProgressEvent{ContractVersion: 1, JobId: j.JobId, JobRevision: contracts.DomainRevision(rev), Attempt: j.Attempt, State: j.State, Progress: p})
		_, err = tx.ExecContext(ctx, "INSERT INTO job_events(job_id,payload_json) VALUES(?,?)", id, string(event))
		return err
	})
	if err == nil {
		k.publish(event)
	}
	return err
}
func (k *Kernel) FinishJob(ctx context.Context, id, lease string, workerErr error, retryable bool) error {
	return k.transitionJob(ctx, id, func(tx *sql.Tx) error {
		j, err := k.checkLease(ctx, tx, id, lease, true)
		if err != nil {
			return err
		}
		state := "succeeded"
		var next any
		var er any
		if j.State == "cancel_requested" {
			return Fail("CANCELLED", "Cancellation won; supervisor must confirm worker stopped")
		}
		if workerErr != nil {
			state = "failed"
			er = string(marshal(contracts.ServiceError{Code: "INTERNAL", Message: workerErr.Error(), Retryable: retryable, RequestId: contracts.DomainId(NewID())}))
			if retryable && j.Attempt < j.MaxAttempts {
				state = "retry_wait"
				delay := time.Second * time.Duration(1<<uint(j.Attempt-1))
				next = time.Now().UTC().Add(delay).Format(time.RFC3339Nano)
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET state=?,revision=revision+1,lease_token=NULL,lease_expires_at=NULL,next_attempt_at=?,error_json=?,updated_at=? WHERE job_id=?", state, next, er, now(), id)
		return err
	})
}
func (k *Kernel) ConfirmJobStopped(ctx context.Context, id, lease string) error {
	return k.transitionJob(ctx, id, func(tx *sql.Tx) error {
		j, err := k.checkLease(ctx, tx, id, lease, true)
		if err != nil {
			return err
		}
		if j.State != "cancel_requested" {
			return Fail("CONFLICT", "Job is not awaiting cancellation")
		}
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET state='cancelled',revision=revision+1,lease_token=NULL,lease_expires_at=NULL,updated_at=? WHERE job_id=?", now(), id)
		return err
	})
}
func (k *Kernel) recoverJobs(ctx context.Context) error {
	return k.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE jobs SET state='cancelled',lease_token=NULL,lease_expires_at=NULL,revision=revision+1,updated_at=? WHERE state='cancel_requested'", now())
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET state='interrupted',lease_token=NULL,lease_expires_at=NULL,revision=revision+1,updated_at=? WHERE state='running'", now())
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET state=CASE WHEN attempt<max_attempts THEN 'retry_wait' ELSE 'failed' END,next_attempt_at=?,revision=revision+1,updated_at=? WHERE state='interrupted'", now(), now())
		return err
	})
}
func (k *Kernel) ReadJob(ctx context.Context, id string) (contracts.JobsJob, error) {
	return k.job(ctx, k.db, id)
}

// RunJobs provides durable polling, lease heartbeats, and bounded one-worker admission.
// Handlers run outside the writer; all effects must call workspace services.
func (k *Kernel) RunJobs(ctx context.Context, handlers map[string]func(context.Context, contracts.JobsJob) error) error {
	kinds := []string{}
	for kind := range handlers {
		kinds = append(kinds, kind)
	}
	if len(kinds) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		j, err := k.ClaimJob(ctx, kinds)
		if errors.Is(err, sql.ErrNoRows) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
				continue
			}
		}
		if err != nil {
			return err
		}
		workCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
		finished := make(chan error, 1)
		go func() {
			defer func() {
				if recover() != nil {
					finished <- Fail("WORKER_FAILED", "Job handler panicked")
				}
			}()
			finished <- handlers[string(j.Kind)](workCtx, j)
		}()
		tick := time.NewTicker(250 * time.Millisecond)
		heartbeat := 0
		parentDone := ctx.Done()
		var stopTimer <-chan time.Time
		stopping := false
		for finished != nil {
			select {
			case runErr := <-finished:
				cancel()
				finishCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
				current, readErr := k.ReadJob(finishCtx, string(j.JobId))
				if readErr != nil {
					done()
					tick.Stop()
					return readErr
				}
				if current.State == "succeeded" || current.State == "failed" || current.State == "cancelled" {
					err = nil
				} else if stopping {
					err = k.interruptJob(finishCtx, string(j.JobId), string(*j.LeaseToken))
				} else if current.State == "cancel_requested" {
					err = k.ConfirmJobStopped(finishCtx, string(j.JobId), string(*j.LeaseToken))
				} else {
					err = k.FinishJob(finishCtx, string(j.JobId), string(*j.LeaseToken), runErr, transientJobError(runErr))
				}
				done()
				finished = nil
			case <-tick.C:
				heartbeat++
				current, e := k.ReadJob(context.Background(), string(j.JobId))
				if e != nil || current.State == "cancel_requested" {
					cancel()
				}
				if heartbeat%40 == 0 && current.State == "running" {
					if e = k.HeartbeatJob(context.Background(), string(j.JobId), string(*j.LeaseToken)); e != nil {
						cancel()
					}
				}
			case <-parentDone:
				cancel()
				stopping = true
				parentDone = nil
				stopTimer = time.After(6 * time.Second)
			case <-stopTimer:
				cancel()
				finishCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
				err = k.interruptJob(finishCtx, string(j.JobId), string(*j.LeaseToken))
				done()
				finished = nil
			}
		}
		tick.Stop()
		cancel()
		if err != nil {
			return fmt.Errorf("persist job %s: %w", j.JobId, err)
		}
		if stopping {
			return ctx.Err()
		}
	}
}

func (k *Kernel) listJobs(ctx context.Context, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	query := "SELECT j.job_id FROM jobs j JOIN job_context c USING(job_id) WHERE c.actor_id=?"
	args := []any{e.Actor.ActorId}
	if state := str(r["state"]); state != "" {
		query += " AND j.state=?"
		args = append(args, state)
	}
	query += " ORDER BY j.created_at DESC,j.job_id LIMIT ?"
	args = append(args, number(r["limit"]))
	rows, err := k.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	jobs := []contracts.JobsJob{}
	for _, id := range ids {
		j, err := k.getAuthorizedJob(ctx, e, id)
		if err != nil {
			continue
		}
		jobs = append(jobs, j.(contracts.JobsJob))
	}
	return map[string]any{"jobs": jobs}, nil
}
func (k *Kernel) interruptJob(ctx context.Context, id, lease string) error {
	return k.transitionJob(ctx, id, func(tx *sql.Tx) error {
		j, err := k.job(ctx, tx, id)
		if err != nil {
			return err
		}
		if j.LeaseToken == nil || string(*j.LeaseToken) != lease {
			return nil
		}
		state := "retry_wait"
		if j.State == "cancel_requested" {
			state = "cancelled"
		} else if j.Attempt >= j.MaxAttempts {
			state = "failed"
		}
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET state=?,revision=revision+1,lease_token=NULL,lease_expires_at=NULL,next_attempt_at=?,updated_at=? WHERE job_id=?", state, now(), now(), id)
		return err
	})
}
func (k *Kernel) transitionJob(ctx context.Context, id string, fn func(*sql.Tx) error) error {
	var event json.RawMessage
	err := k.write(ctx, func(tx *sql.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		j, err := k.job(ctx, tx, id)
		if err != nil {
			return err
		}
		progress := contracts.JobsProgress{Sequence: 0, Completed: 0, Unit: "steps"}
		if j.Progress != nil {
			progress = *j.Progress
		}
		event = marshal(contracts.JobsJobProgressEvent{ContractVersion: 1, JobId: j.JobId, JobRevision: j.Revision, Attempt: j.Attempt, State: j.State, Progress: progress})
		_, err = tx.ExecContext(ctx, "INSERT INTO job_events(job_id,payload_json) VALUES(?,?)", id, string(event))
		return err
	})
	if err == nil {
		k.publish(event)
	}
	return err
}
func transientJobError(err error) bool {
	if err == nil {
		return false
	}
	var failure *ServiceFailure
	if errors.As(err, &failure) {
		return failure.Code == "WORKER_FAILED"
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "RUNTIME_FAILURE") || strings.Contains(s, "native process failed") || strings.Contains(s, "unexpected EOF") || strings.Contains(s, "temporarily unavailable")
}
