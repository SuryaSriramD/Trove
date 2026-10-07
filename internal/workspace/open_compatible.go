package workspace

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
)

type compatibleOpen struct {
	ctx    context.Context
	id     string
	format CompatibilityDescriptor
}

// OpenCompatibleReviewed opens only the exact already-current workspace identity
// reviewed for a native rollback. Checks run under the workspace owner lock and
// before writable SQLite configuration, migration or package initialization.
func OpenCompatibleReviewed(ctx context.Context, path, expectedID string, expected CompatibilityDescriptor) (*Kernel, error) {
	if ctx == nil || expectedID == "" || expected != CurrentCompatibility() {
		return nil, errors.New("reviewed rollback workspace requires this exact storage format")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return openReviewed(path, false, nil, &compatibleOpen{ctx, expectedID, expected})
}
func (r *compatibleOpen) validate(path string, exists bool, format CompatibilityDescriptor, recoveryPending bool) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if !exists || recoveryPending || format != r.format {
		return errors.New("reviewed rollback workspace is missing, changed or requires migration/recovery")
	}
	db, err := openMigrationDatabase(path)
	if err != nil {
		return err
	}
	defer db.Close()
	var count int
	var id string
	if err = db.QueryRowContext(r.ctx, "SELECT COUNT(*), COALESCE(MIN(id),'') FROM workspaces").Scan(&count, &id); err != nil {
		return err
	}
	if count != 1 || id != r.id {
		return errors.New("reviewed rollback workspace identity changed")
	}
	return r.ctx.Err()
}

// CompleteCompatibleStartup initializes the already-reviewed current format only
// after native rollback commit. No background worker is started by this method.
func (k *Kernel) CompleteCompatibleStartup(ctx context.Context) error {
	if !k.compatiblePending {
		return errors.New("compatible startup is not pending")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	format, err := k.DescribeCompatibility(ctx)
	if err != nil {
		return err
	}
	if format != CurrentCompatibility() {
		return errors.New("compatible workspace format changed")
	}
	if err = k.activateCompatiblePools(ctx); err != nil {
		return err
	}
	if err = k.initServiceRuntime(ctx); err != nil {
		return err
	}
	if err = k.initRecovery(ctx); err != nil {
		return err
	}
	for _, initialize := range kernelInitializers {
		if err = initialize(ctx, k); err != nil {
			return err
		}
	}
	if err = k.recoverJobs(ctx); err != nil {
		return err
	}
	k.compatiblePending = false
	k.startCompatibleDiagnostics()
	return nil
}

// The caller excludes renderer queries across this transition and keeps the
// original owner lock. No writable SQLite connection exists before commit.
func (k *Kernel) activateCompatiblePools(ctx context.Context) error {
	dsn := (&url.URL{Scheme: "file", Path: filepath.Join(k.path, "workspace.db")}).String() + "?_foreign_keys=on&_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000"
	opened := []*sql.DB{}
	success := false
	defer func() {
		if !success {
			for _, db := range opened {
				db.Close()
			}
		}
	}()
	for _, suffix := range []string{"", "&_txlock=immediate", ""} {
		db, err := sql.Open("sqlite3", dsn+suffix)
		if err != nil {
			return err
		}
		opened = append(opened, db)
		if err = db.PingContext(ctx); err != nil {
			return err
		}
	}
	opened[0].SetMaxOpenConns(8)
	opened[0].SetMaxIdleConns(8)
	opened[1].SetMaxOpenConns(1)
	opened[1].SetMaxIdleConns(1)
	opened[2].SetMaxOpenConns(2)
	opened[2].SetMaxIdleConns(2)
	old := []*sql.DB{k.db, k.writeDB, k.recordQueryDB}
	k.db, k.writeDB, k.recordQueryDB = opened[0], opened[1], opened[2]
	success = true
	for _, db := range old {
		if err := db.Close(); err != nil {
			return err
		}
	}
	return nil
}
