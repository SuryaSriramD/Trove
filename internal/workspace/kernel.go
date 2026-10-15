// Package workspace is the single authority for canonical workspace mutations.
package workspace

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/diagnostics"
	"trove.local/phase0/internal/platformfs"
	"trove.local/phase0/internal/sqlitevec"
	"trove.local/phase0/schemas"
)

type Kernel struct {
	connectorContext      *ConnectorContext
	diagnostics           *diagnostics.Store
	db                    *sql.DB
	writeDB               *sql.DB
	recordQueryDB         *sql.DB
	path                  string
	id                    string
	ownerID               string
	lock                  *os.File
	writer                chan struct{}
	pending               chan struct{}
	mu                    sync.Mutex
	reviewMu              sync.Mutex
	semanticMaintenanceMu sync.Mutex
	closed                bool
	grants                map[string]contracts.ServiceExecutionContext
	cancellations         map[string]*cancellation
	listeners             map[uint64]chan json.RawMessage
	listenerSeq           uint64
	recordQueries         recordQueryCache
	compatiblePending     bool
}
type cancellation struct {
	cancel      context.CancelFunc
	transaction string
	cancelled   bool
}
type commitGrantKey struct{}
type approvalSourceGrantKey struct{}
type CommandHandler func(*Mutation, map[string]any) error
type QueryHandler func(context.Context, *Kernel, contracts.ServiceExecutionContext, map[string]any) (any, error)

var commandHandlers = map[string]CommandHandler{}
var queryHandlers = map[string]QueryHandler{}
var commitValidators []func(*Mutation) error
var kernelInitializers []func(context.Context, *Kernel) error
var commitProjections []func(*Mutation) error
var registeredJobs = map[string]func(context.Context, *Kernel, contracts.JobsJob) error{}

// RegisterInitializer installs additive package storage after the canonical and
// recovery schemas exist, before interrupted jobs can resume.
func RegisterInitializer(h func(context.Context, *Kernel) error) {
	kernelInitializers = append(kernelInitializers, h)
}

// RegisterCommitProjection updates derived state in the same transaction after
// canonical revision numbers are assigned. A failed projection rolls back the
// mutation rather than leaving search or evidence out of sync.
func RegisterCommitProjection(h func(*Mutation) error) {
	commitProjections = append(commitProjections, h)
}

func RegisterJobHandler(kind string, h func(context.Context, *Kernel, contracts.JobsJob) error) {
	registeredJobs[kind] = h
}
func (k *Kernel) RegisteredJobHandlers() map[string]func(context.Context, contracts.JobsJob) error {
	out := map[string]func(context.Context, contracts.JobsJob) error{}
	for kind, h := range registeredJobs {
		handler := h
		out[kind] = func(ctx context.Context, j contracts.JobsJob) error { return handler(ctx, k, j) }
	}
	return out
}

func RegisterCommitValidator(h func(*Mutation) error) { commitValidators = append(commitValidators, h) }

func RegisterCommand(kind string, h CommandHandler) {
	if _, ok := commandHandlers[kind]; ok {
		panic("duplicate handler " + kind)
	}
	commandHandlers[kind] = h
}
func RegisterQuery(method string, h QueryHandler) {
	if _, ok := queryHandlers[method]; ok {
		panic("duplicate query " + method)
	}
	queryHandlers[method] = h
}

var vecOnce sync.Once

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	ms := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = (b[6] & 15) | 0x70
	b[8] = (b[8] & 63) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func now() string                                    { return time.Now().UTC().Format(time.RFC3339Nano) }
func Create(path string) (*Kernel, error)            { return open(path, true) }
func Open(path string) (*Kernel, error)              { return open(path, false) }
func open(path string, create bool) (*Kernel, error) { return openMigrationObserved(path, create, nil) }

// observe is used by fault-injection tests at durable migration boundaries.
func openMigrationObserved(path string, create bool, observe func(string) error) (k *Kernel, err error) {
	return openReviewed(path, create, observe, nil)
}
func openReviewed(path string, create bool, observe func(string) error, reviewed *compatibleOpen) (k *Kernel, err error) {
	checkpoint := func(stage string) error {
		if observe != nil {
			return observe(stage)
		}
		return nil
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	incomplete, err := markerExists(path)
	if err != nil {
		return nil, err
	}
	journal, err := readMigrationJournal(path)
	if err != nil {
		return nil, err
	}
	if incomplete && journal == nil {
		return nil, Fail("INTEGRITY_ERROR", "Workspace restore, creation, or backup is incomplete")
	}
	if create {
		if err = platformfs.EnsurePrivateDirectory(path); err != nil {
			return nil, err
		}
	}
	if !create {
		if _, err = os.Stat(filepath.Join(path, "workspace.db")); err != nil {
			return nil, err
		}
	}
	existingDatabase := false
	if info, statErr := os.Lstat(filepath.Join(path, "workspace.db")); statErr == nil {
		existingDatabase = true
		if !info.Mode().IsRegular() {
			return nil, Fail("INTEGRITY_ERROR", "Workspace database must be a regular file")
		}
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	lock, err := platformfs.AcquireLock(filepath.Join(path, ".owner.lock"))
	if err != nil {
		if errors.Is(err, platformfs.ErrLocked) {
			return nil, Fail("WORKSPACE_LOCKED", "Workspace already has an active owner")
		}
		return nil, err
	}
	if _, e := os.Lstat(filepath.Join(path, "OWNER_TRANSFERRED")); e == nil || !os.IsNotExist(e) {
		platformfs.ReleaseLock(lock)
		return nil, Fail("WORKSPACE_LOCKED", "Workspace ownership was transferred; open the verified destination instead")
	}
	k = &Kernel{path: path, lock: lock, writer: make(chan struct{}, 1), pending: make(chan struct{}, 128), grants: map[string]contracts.ServiceExecutionContext{}, cancellations: map[string]*cancellation{}, listeners: map[uint64]chan json.RawMessage{}}
	allocated := k
	defer func() {
		if err != nil {
			allocated.Close()
		}
	}()
	if err = platformfs.AuditWorkspace(path); err != nil {
		return nil, err
	}
	// Re-read recovery state under the ownership lock. Another process may have
	// completed or failed a migration between the initial check and lock acquisition.
	incomplete, err = markerExists(path)
	if err != nil {
		return nil, err
	}
	journal, err = readMigrationJournal(path)
	if err != nil {
		return nil, err
	}
	if incomplete && journal == nil {
		return nil, Fail("INTEGRITY_ERROR", "Workspace restore, creation, or backup is incomplete")
	}
	// The database may also have been created while we waited for ownership.
	if info, e := os.Lstat(filepath.Join(path, "workspace.db")); e == nil {
		if !info.Mode().IsRegular() {
			return nil, Fail("INTEGRITY_ERROR", "Workspace database must be a regular file")
		}
		existingDatabase = true
	} else if !os.IsNotExist(e) {
		return nil, e
	} else {
		existingDatabase = false
	}
	recoveryOperation, recoveryStatus, err := inspectRecoveryPublication(path, existingDatabase, reviewed != nil)
	if err != nil {
		return nil, err
	}
	// Inspect existing storage read-only before configuring WAL or executing DDL.
	var before CompatibilityDescriptor
	if existingDatabase {
		if recoveryStatus.State == "complete" {
			before, err = readImmutableRecoveryCompatibility(context.Background(), path)
		} else {
			before, err = ReadWorkspaceCompatibility(context.Background(), path)
		}
		if err != nil {
			return nil, err
		}
	}
	if reviewed != nil {
		if err = reviewed.validate(path, existingDatabase, before, incomplete || journal != nil); err != nil {
			return nil, err
		}
	}
	if journal != nil && (journal.Target.UserVersion > CurrentUserVersion || journal.Target.StorageGeneration > CurrentStorageGeneration) {
		return nil, Fail("UNSUPPORTED_SCHEMA", "Workspace upgrade requires a newer application")
	}
	// Windows regular-file publication is supported independently. Creation,
	// migration and interrupted recovery still require a qualified directory/
	// marker protocol; reject before opening SQLite for mutation.
	if !existingDatabase || before != CurrentCompatibility() || journal != nil {
		if err = platformfs.RequireRecoveryDurability(); err != nil {
			return nil, err
		}
	}
	if recoveryStatus.State == "complete" {
		if err = platformfs.ActivateRecoveryAdmission(path, recoveryOperation, lock); err != nil {
			return nil, err
		}
	}
	vecOnce.Do(sqlitevec.Auto)
	dsn := (&url.URL{Scheme: "file", Path: filepath.Join(path, "workspace.db")}).String() + "?_foreign_keys=on&_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000"
	if reviewed != nil {
		dsn = (&url.URL{Scheme: "file", Path: filepath.Join(path, "workspace.db")}).String() + "?mode=ro&_query_only=true&_foreign_keys=on&_busy_timeout=5000"
	}
	k.db, err = sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	k.db.SetMaxOpenConns(8)
	k.db.SetMaxIdleConns(8)
	if err = k.db.Ping(); err != nil {
		return nil, err
	}
	if journal != nil {
		if err = k.resumeMigrationCompletion(context.Background(), journal, incomplete); err != nil {
			return nil, err
		}
		incomplete = false
	}
	version := before.UserVersion
	var migration *migrationJournal
	creatingMarker := []byte("Workspace creation is incomplete.\n")
	if existingDatabase && before != CurrentCompatibility() {
		if err = checkpoint("before_snapshot"); err != nil {
			return nil, err
		}
		migration, err = k.createMigrationSnapshot(context.Background(), before)
		if err != nil {
			return nil, err
		}
		if err = checkpoint("after_snapshot"); err != nil {
			return nil, err
		}
		if err = writeMigrationJSON(path, migrationJournalName, migration); err != nil {
			return nil, err
		}
		if err = checkpoint("after_journal"); err != nil {
			return nil, err
		}
		if err = createMigrationMarker(path, migrationMarker(migration.SnapshotID)); err != nil {
			return nil, err
		}
		if err = checkpoint("before_initializers"); err != nil {
			return nil, err
		}
	} else if !existingDatabase {
		if err = createMigrationMarker(path, creatingMarker); err != nil {
			return nil, err
		}
	}
	if version < 2 {
		if version == 0 && !create {
			return nil, Fail("INVALID_REQUEST", "Not a Trove workspace")
		}
		names := []string{"services.sql"}
		if version == 0 {
			names = []string{"workspace.sql", "services.sql"}
		}
		scripts := []string{}
		for _, name := range names {
			b, e := schemas.Files.ReadFile(name)
			if e != nil {
				return nil, e
			}
			scripts = append(scripts, string(b))
		}
		scripts = append(scripts, kernelMigration)
		if err = applySchemaScripts(k.db, scripts...); err != nil {
			return nil, err
		}

	}

	if err = k.db.QueryRow("SELECT id FROM workspaces LIMIT 1").Scan(&k.id); errors.Is(err, sql.ErrNoRows) {
		k.id = NewID()
		_, err = k.db.Exec("INSERT INTO workspaces(id,format_version) VALUES(?,1)", k.id)
	}
	if err != nil {
		return nil, err
	}
	if err = k.db.QueryRow("SELECT value FROM kernel_meta WHERE key='ownerId'").Scan(&k.ownerID); errors.Is(err, sql.ErrNoRows) && reviewed == nil {
		k.ownerID = NewID()
		_, err = k.db.Exec("INSERT INTO kernel_meta(key,value) VALUES('ownerId',?)", k.ownerID)
	}
	if err != nil {
		return nil, err
	}
	k.writeDB, err = sql.Open("sqlite3", dsn+"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	k.writeDB.SetMaxOpenConns(1)
	k.writeDB.SetMaxIdleConns(1)
	// Query-local typed functions are registered only on this pool, never on
	// the canonical writer, online-backup pool, or another workspace's handles.
	k.recordQueryDB, err = sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	k.recordQueryDB.SetMaxOpenConns(2)
	k.recordQueryDB.SetMaxIdleConns(2)
	if reviewed != nil {
		k.compatiblePending = true
		return k, nil
	}
	k.diagnostics = diagnostics.New(path)
	observedDiagnostics := k.diagnostics
	openedAt := time.Now()
	defer func() {
		outcome, code := diagnosticOutcome(err)
		observedDiagnostics.Record("application", "initialize", outcome, code, time.Since(openedAt), 0)
		if migration != nil {
			observedDiagnostics.Record("application", "post_migration_initialize", outcome, code, time.Since(openedAt), 0)
		}
		if err != nil {
			observedDiagnostics.Close()
		}
	}()
	if err = k.initServiceRuntime(context.Background()); err != nil {
		return nil, err
	}
	if err = k.initRecovery(context.Background()); err != nil {
		return nil, err
	}
	for _, initialize := range kernelInitializers {
		if err = initialize(context.Background(), k); err != nil {
			return nil, err
		}
	}
	if err = checkpoint("after_initializers"); err != nil {
		return nil, err
	}
	if migration != nil || !existingDatabase {
		id, digest := "", ""
		if migration != nil {
			id, digest = migration.SnapshotID, migration.ManifestSHA256
		}
		if err = k.stampCompatibility(context.Background(), id, digest); err != nil {
			return nil, err
		}
		if err = checkpoint("after_stamp"); err != nil {
			return nil, err
		}
		if migration != nil {
			if err = k.finishMigration(context.Background(), migration); err != nil {
				return nil, err
			}
			if err = checkpoint("after_completion"); err != nil {
				return nil, err
			}
			if err = removeMigrationMarker(path, migrationMarker(migration.SnapshotID)); err != nil {
				return nil, err
			}
		} else {
			if err = checkMigrationDatabase(context.Background(), path, CurrentCompatibility(), k.id); err != nil {
				return nil, err
			}
			if err = removeMigrationMarker(path, creatingMarker); err != nil {
				return nil, err
			}
		}
	}
	if err = k.recoverJobs(context.Background()); err != nil {
		return nil, err
	}
	return k, nil
}
func (k *Kernel) ID() string   { return k.id }
func (k *Kernel) Path() string { return k.path }
func (k *Kernel) Close() error {
	k.SetOCRProvider(nil)
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return nil
	}
	k.closed = true
	for _, c := range k.cancellations {
		if c.cancel != nil {
			c.cancel()
		}
	}
	for id, ch := range k.listeners {
		close(ch)
		delete(k.listeners, id)
	}
	k.mu.Unlock()
	k.SetEmbeddingProvider(nil)
	k.closeRecovery()
	k.recordQueries.close()
	var err error
	if k.writeDB != nil {
		k.writeDB.Close()
	}
	if k.recordQueryDB != nil {
		k.recordQueryDB.Close()
	}
	if k.db != nil {
		err = k.db.Close()
	}
	k.diagnostics.Record("application", "close", "completed", "none", 0, 0)
	k.diagnostics.Close()
	if k.lock != nil {
		platformfs.ReleaseLock(k.lock)
	}
	return err
}
func (k *Kernel) write(ctx context.Context, fn func(*sql.Tx) error) error {
	select {
	case k.pending <- struct{}{}:
	default:
		return Fail("RESOURCE_EXHAUSTED", "Workspace write queue is full")
	}
	defer func() { <-k.pending }()
	select {
	case k.writer <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-k.writer }()
	k.mu.Lock()
	closed := k.closed
	k.mu.Unlock()
	if closed {
		return Fail("WORKSPACE_LOCKED", "Workspace is closed")
	}
	tx, err := k.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Linearize revocation with the final commit, including revocation that
	// arrives while a handler is running. The permission mutex is held only for
	// this short commit fence, never while executing a command.
	if e, ok := ctx.Value(commitGrantKey{}).(contracts.ServiceExecutionContext); ok {
		k.mu.Lock()
		defer k.mu.Unlock()
		contexts := []contracts.ServiceExecutionContext{e}
		if parent, ok := ctx.Value(approvalSourceGrantKey{}).(contracts.ServiceExecutionContext); ok {
			contexts = append(contexts, parent)
		}
		for _, execution := range contexts {
			g, live := k.grants[string(execution.ScopeGrantId)]
			if !live || string(marshal(g)) != string(marshal(execution)) {
				return Fail("SCOPE_DENIED", "Connection or approving scope was revoked before commit")
			}
		}
	}
	return tx.Commit()
}
func (k *Kernel) OwnerContext() contracts.ServiceExecutionContext {
	return k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(k.ownerID)}, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant1{Kind: "workspace", WorkspaceId: contracts.DomainId(k.id)}})
}

// Grant is called only by a trusted Go adapter after authenticating the principal.
func (k *Kernel) Grant(actor contracts.ServiceActor, origin string, scope contracts.DomainScope) contracts.ServiceExecutionContext {
	e := contracts.ServiceExecutionContext{Actor: actor, Origin: origin, Scope: scope, ScopeGrantId: contracts.DomainId(NewID()), PolicyRevision: "1"}
	k.mu.Lock()
	k.grants[string(e.ScopeGrantId)] = e
	k.mu.Unlock()
	return e
}
func (k *Kernel) RevokeGrant(id string) {
	k.mu.Lock()
	delete(k.grants, id)
	k.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = k.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE job_authorizations SET state='revoked' WHERE grant_id=?", id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE jobs SET state=CASE WHEN state='running' THEN 'cancel_requested' ELSE 'cancelled' END,revision=revision+1,updated_at=? WHERE job_id IN(SELECT job_id FROM job_authorizations WHERE grant_id=?) AND state IN('queued','retry_wait','running','interrupted')", now(), id)
		return err
	})
}

func marshal(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func str(v any) string              { s, _ := v.(string); return s }
func number(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case int64:
		return x
	}
	return 0
}
func nullable(v any) any {
	if v == nil || str(v) == "" {
		return nil
	}
	return v
}
func revNext(v string) (string, error) {
	n, e := strconv.ParseInt(v, 10, 64)
	if e != nil || n == int64(^uint64(0)>>1) {
		return "", Fail("RESOURCE_EXHAUSTED", "Revision overflow")
	}
	return strconv.FormatInt(n+1, 10), nil
}

const kernelMigration = `BEGIN IMMEDIATE;
CREATE TABLE IF NOT EXISTS kernel_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS page_metadata(page_id TEXT PRIMARY KEY REFERENCES pages(id),archived INTEGER NOT NULL DEFAULT 0 CHECK(archived IN(0,1)),icon TEXT,cover_file_id TEXT REFERENCES files(id),favorite INTEGER NOT NULL DEFAULT 0,last_opened_at TEXT);
CREATE TABLE IF NOT EXISTS page_links(source_page_id TEXT NOT NULL REFERENCES pages(id),source_block_id TEXT NOT NULL REFERENCES blocks(id),target_page_id TEXT NOT NULL REFERENCES pages(id),PRIMARY KEY(source_block_id,target_page_id));
CREATE TABLE IF NOT EXISTS entity_tombstones(entity_type TEXT NOT NULL,entity_id TEXT NOT NULL,revision TEXT NOT NULL,deleted_at TEXT NOT NULL,PRIMARY KEY(entity_type,entity_id));
CREATE TABLE IF NOT EXISTS page_snapshots(snapshot_id TEXT PRIMARY KEY,page_id TEXT NOT NULL,revision TEXT NOT NULL,snapshot_json TEXT NOT NULL CHECK(json_valid(snapshot_json)),created_at TEXT NOT NULL,UNIQUE(page_id,revision));
CREATE TABLE IF NOT EXISTS job_context(job_id TEXT PRIMARY KEY REFERENCES jobs(job_id),actor_id TEXT NOT NULL,scope_json TEXT NOT NULL CHECK(json_valid(scope_json)),payload_hash TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS job_events(cursor INTEGER PRIMARY KEY AUTOINCREMENT,job_id TEXT NOT NULL REFERENCES jobs(job_id),payload_json TEXT NOT NULL CHECK(json_valid(payload_json)));
PRAGMA user_version=2;
COMMIT;`

func applySchemaScripts(db *sql.DB, scripts ...string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, script := range scripts {
		script = strings.ReplaceAll(script, "BEGIN IMMEDIATE;", "")
		script = strings.ReplaceAll(script, "COMMIT;", "")
		if _, err = tx.Exec(script); err != nil {
			return err
		}
	}
	return tx.Commit()
}
