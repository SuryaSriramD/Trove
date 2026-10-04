package workspace

import (
	"context"
	"database/sql"
	"trove.local/phase0/contracts"
)

func init() { RegisterQuery("jobs.retry", retryJob) }

func retryJob(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := contracts.Validate("jobs", "CancelJobRequest", marshal(r)); err != nil {
		return nil, Fail("INVALID_REQUEST", "Retry requires a job identity and expected revision")
	}
	if e.Origin != "user" || e.Actor.Kind != "user" {
		return nil, Fail("APPROVAL_REQUIRED", "Only the user can resume a stopped job")
	}
	id := str(r["jobId"])
	if _, err := k.getAuthorizedJob(ctx, e, id); err != nil {
		return nil, err
	}
	err := k.transitionJob(ctx, id, func(tx *sql.Tx) error {
		if err := k.authorize(e); err != nil {
			return err
		}
		j, err := k.job(ctx, tx, id)
		if err != nil {
			return err
		}
		if string(j.Revision) != str(r["expectedRevision"]) {
			return Fail("CONFLICT", "Job changed; refresh before retrying")
		}
		if j.State != "failed" && j.State != "cancelled" && j.State != "interrupted" {
			return Fail("CONFLICT", "Only stopped jobs can be retried")
		}
		// Preserve immutable payload, source revision fences, checkpoints and unit
		// receipts. A retry can never approve changed content or duplicate a unit.
		if _, err = tx.ExecContext(ctx, "UPDATE job_authorizations SET state='approved',grant_id=?,policy_revision=? WHERE job_id=? AND actor_id=?", e.ScopeGrantId, e.PolicyRevision, id, e.Actor.ActorId); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE jobs SET state='queued',revision=revision+1,attempt=0,max_attempts=3,next_attempt_at=NULL,lease_token=NULL,lease_expires_at=NULL,error_json=NULL,updated_at=? WHERE job_id=?", now(), id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return k.job(ctx, k.db, id)
}
