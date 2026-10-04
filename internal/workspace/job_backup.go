package workspace

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"
	"trove.local/phase0/contracts"
)

func init() { RegisterJobHandler("BACKUP_WORKSPACE", runBackupJob) }

// RegisterBackupDestination accepts only the trusted native save dialog result.
// The capability is bound to the authenticated owner and persisted for recovery.
func (k *Kernel) RegisterBackupDestination(ctx context.Context, e contracts.ServiceExecutionContext, path string) (string, error) {
	if err := k.authorize(e); err != nil {
		return "", err
	}
	if e.Actor.Kind != "user" || e.Origin != "user" || string(e.Actor.ActorId) != k.ownerID {
		return "", Fail("SCOPE_DENIED", "Backup destination requires the workspace owner")
	}
	if s, ok := e.Scope.Value.(contracts.DomainScopeVariant1); !ok || string(s.WorkspaceId) != k.id {
		return "", Fail("SCOPE_DENIED", "Backup requires workspace scope")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err = os.Lstat(absolute); !os.IsNotExist(err) {
		return "", Fail("INVALID_REQUEST", "Backup destination must not exist")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	absolute = filepath.Join(parent, filepath.Base(absolute))
	if absolute == k.path || len(absolute) > 4096 {
		return "", Fail("INVALID_REQUEST", "Invalid backup destination")
	}
	id := NewID()
	err = k.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO backup_destinations VALUES(?,?,?,?,?,NULL)", id, e.Actor.ActorId, e.ScopeGrantId, absolute, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339Nano))
		return err
	})
	return id, err
}
func runBackupJob(ctx context.Context, k *Kernel, j contracts.JobsJob) error {
	payload, ok := j.Payload.Value.(contracts.JobsJobPayloadVariant7)
	if !ok {
		return Fail("INVALID_REQUEST", "Expected backup payload")
	}
	var target, actor, grant, expiry string
	var existing sql.NullString
	err := k.db.QueryRowContext(ctx, "SELECT path,actor_id,grant_id,expires_at,job_id FROM backup_destinations WHERE id=?", payload.DestinationHandle).Scan(&target, &actor, &grant, &expiry, &existing)
	if err != nil {
		return err
	}
	if actor != k.ownerID {
		return Fail("SCOPE_DENIED", "Backup owner changed")
	}
	if existing.Valid && existing.String != string(j.JobId) {
		return Fail("SCOPE_DENIED", "Backup destination was consumed by another job")
	}
	var delegatedGrant string
	var approved int
	if err = k.db.QueryRowContext(ctx, "SELECT grant_id,state='approved' FROM job_authorizations WHERE job_id=?", j.JobId).Scan(&delegatedGrant, &approved); err != nil {
		return err
	}
	if grant != delegatedGrant || approved != 1 {
		return Fail("SCOPE_DENIED", "Backup destination grant is revoked or mismatched")
	}
	if existing.Valid {
		if manifest, verifyErr := VerifyBackup(ctx, target); verifyErr == nil {
			if manifest.WorkspaceID != k.id {
				return Fail("INTEGRITY_ERROR", "Recovered backup belongs to another workspace")
			}
			return nil
		}
	}
	expires, _ := time.Parse(time.RFC3339Nano, expiry)
	if time.Now().After(expires) {
		return Fail("SCOPE_DENIED", "Backup destination capability expired")
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		if _, err := k.checkLease(ctx, tx, string(j.JobId), string(*j.LeaseToken), false); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE backup_destinations SET job_id=? WHERE id=?", j.JobId, payload.DestinationHandle)
		return err
	})
	if err != nil {
		return err
	}
	scope := contracts.DomainScope{Value: contracts.DomainScopeVariant1{Kind: "workspace", WorkspaceId: contracts.DomainId(k.id)}}
	e := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(actor)}, "user", scope)
	defer k.RevokeGrant(string(e.ScopeGrantId))
	_, err = k.CreateBackup(ctx, e, target)
	return err
}
