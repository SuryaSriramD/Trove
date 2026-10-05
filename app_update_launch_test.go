package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/internal/updates"
)

type rollbackVerifierFunc func(context.Context, string) (updates.Application, error)

func (f rollbackVerifierFunc) Inspect(ctx context.Context, path string) (updates.Application, error) {
	return f(ctx, path)
}

type rollbackFixture struct {
	a                               *App
	source, destination, preference string
	application                     updates.Application
	id                              string
}

func newRollbackFixture(t *testing.T) rollbackFixture {
	t.Helper()
	base, resolveErr := filepath.EvalSymlinks(t.TempDir())
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	f := rollbackFixture{a: NewApp(), source: filepath.Join(base, "Previous.app"), destination: filepath.Join(base, "restored"), preference: filepath.Join(base, "config", "workspace-selection.json")}
	f.a.ctx = context.Background()
	t.Cleanup(func() { rollbackLaunchStates.Delete(f.a); updateStates.Delete(f.a) })
	for _, dir := range []string{filepath.Join(f.source, "Contents", "MacOS"), filepath.Join(f.destination, "files"), filepath.Join(f.destination, "recordings")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		filepath.Join(f.source, "Contents", "MacOS", "Trove"): "test fixture; never execute",
		filepath.Join(f.destination, "files", "asset"):        "original image bytes",
		filepath.Join(f.destination, "recordings", "audio"):   "original recording bytes",
		filepath.Join(f.destination, "restore-receipt.json"):  `{"workspaceId":"restored-identity","backupId":"reviewed-backup"}`,
	} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Independent minimal database describes the supported on-disk contract. No
	// workspace.Open or migration initializer runs while preparing a launch review.
	db, err := sql.Open("sqlite3", filepath.Join(f.destination, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`PRAGMA user_version=3; CREATE TABLE kernel_meta(key TEXT PRIMARY KEY, value TEXT); INSERT INTO kernel_meta VALUES('storageGeneration','1'); CREATE TABLE workspaces(id TEXT PRIMARY KEY); INSERT INTO workspaces VALUES('restored-identity');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(f.destination, "workspace.db"), 0600); err != nil {
		t.Fatal(err)
	}
	inventory, err := updates.InspectTree(context.Background(), f.source)
	if err != nil {
		t.Fatal(err)
	}
	f.application = updates.Application{Identity: updates.Identity{BundleID: updates.BundleID, TeamID: "ABCDEFGHIJ", Version: "1.0.0", Build: 1, UserVersion: 3, StorageGeneration: 1, RollbackHandshake: 1, Architecture: "arm64", MinimumOS: "13.0"}, Inventory: inventory}
	f.id, err = f.a.saveRollbackLaunchReviewContext(context.Background(), f.source, f.application, f.destination)
	if err != nil || f.id == "" {
		t.Fatalf("save launch review: %v", err)
	}
	return f
}
func (f rollbackFixture) dependencies(t *testing.T) rollbackLaunchDependencies {
	t.Helper()
	return rollbackLaunchDependencies{
		verifier: rollbackVerifierFunc(func(_ context.Context, path string) (updates.Application, error) {
			if path != f.source {
				t.Fatal("renderer changed executable source")
			}
			return f.application, nil
		}),
		preference: f.preference, environment: []string{"HOME=/native/home", "TROVE_WORKSPACE=/newer/workspace", "DYLD_INSERT_LIBRARIES=/injected", "OPENAI_API_KEY=never-forward"},
		start: func(context.Context, string, []string, updates.RollbackTarget) error {
			t.Fatal("unexpected launch")
			return nil
		},
		quit: func() { t.Fatal("unexpected quit") },
	}
}
func TestRollbackLaunchSelectsRestoredBeforeFirstOpenAndQuit(t *testing.T) {
	f := newRollbackFixture(t)
	if err := saveWorkspaceSelection(f.preference, "/newer/workspace"); err != nil {
		t.Fatal(err)
	}
	deps := f.dependencies(t)
	var events []string
	deps.start = func(ctx context.Context, path string, env []string, target updates.RollbackTarget) error {
		if ctx.Err() != nil || path != f.source {
			t.Fatal("incorrect executable")
		}
		raw, err := os.ReadFile(f.preference)
		if err != nil {
			t.Fatal(err)
		}
		var selected workspaceSelection
		if json.Unmarshal(raw, &selected) != nil || selected.Path != f.destination {
			t.Fatal("spawn preceded compatible workspace selection")
		}
		if !reflect.DeepEqual(env, []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=/native/home", "TROVE_WORKSPACE=" + f.destination}) {
			t.Fatalf("unsafe launch environment: %q", env)
		}
		events = append(events, "spawn")
		return nil
	}
	deps.quit = func() {
		if !f.a.lifecycle.TryLock() {
			t.Fatal("quit while holding shutdown lifecycle lock")
		}
		f.a.lifecycle.Unlock()
		events = append(events, "quit")
	}
	if err := f.a.launchApplicationRollback(context.Background(), f.id, deps); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"spawn", "quit"}) {
		t.Fatal(events)
	}
	selected, err := selectedWorkspace(f.preference)
	if err != nil || selected != f.destination {
		t.Fatal("future default does not select restored copy", err)
	}
	if err = f.a.launchApplicationRollback(context.Background(), f.id, deps); err == nil {
		t.Fatal("reused launch review")
	}
}
func TestRollbackLaunchRejectsChangedRestoredContent(t *testing.T) {
	for _, kind := range []string{"database", "workspace-identity", "asset", "recording", "receipt", "new-file", "missing-file", "symlink", "active-wal", "incomplete", "mode", "format"} {
		t.Run(kind, func(t *testing.T) {
			f := newRollbackFixture(t)
			var err error
			switch kind {
			case "database", "workspace-identity", "format":
				var db *sql.DB
				db, err = sql.Open("sqlite3", filepath.Join(f.destination, "workspace.db"))
				if err != nil {
					t.Fatal(err)
				}
				statement := `CREATE TABLE unrelated_data(value TEXT); INSERT INTO unrelated_data VALUES('changed after review')`
				if kind == "workspace-identity" {
					statement = `UPDATE workspaces SET id='different'`
				}
				if kind == "format" {
					statement = `PRAGMA user_version=2`
				}
				_, err = db.Exec(statement)
				closeErr := db.Close()
				if err == nil {
					err = closeErr
				}
			case "asset":
				err = os.WriteFile(filepath.Join(f.destination, "files", "asset"), []byte("edited image bytes"), 0600)
			case "recording":
				err = os.WriteFile(filepath.Join(f.destination, "recordings", "audio"), []byte("edited recording bytes"), 0600)
			case "receipt":
				err = os.WriteFile(filepath.Join(f.destination, "restore-receipt.json"), []byte(`{"backupId":"another"}`), 0600)
			case "new-file":
				err = os.WriteFile(filepath.Join(f.destination, "added"), []byte("new"), 0600)
			case "missing-file":
				err = os.Remove(filepath.Join(f.destination, "files", "asset"))
			case "symlink":
				err = os.Symlink(filepath.Join(f.destination, "workspace.db"), filepath.Join(f.destination, "unexpected-link"))
			case "active-wal":
				err = os.WriteFile(filepath.Join(f.destination, "workspace.db-wal"), []byte("pending sqlite writes"), 0600)
			case "incomplete":
				err = os.WriteFile(filepath.Join(f.destination, "INCOMPLETE"), nil, 0600)
			case "mode":
				err = os.Chmod(filepath.Join(f.destination, "files", "asset"), 0666)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = f.a.launchApplicationRollback(context.Background(), f.id, f.dependencies(t)); err == nil {
				t.Fatal("accepted modified restore")
			}
			if _, err = os.Stat(f.preference); !os.IsNotExist(err) {
				t.Fatal("changed preference before rejecting content")
			}
		})
	}
}
func TestRollbackLaunchRechecksAfterVerifierAndRejectsUntrustedApp(t *testing.T) {
	for _, kind := range []string{"publisher", "inventory", "signature", "changed-during-verification", "cancel-during-verification", "app-bytes-after-verification"} {
		t.Run(kind, func(t *testing.T) {
			f := newRollbackFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deps := f.dependencies(t)
			deps.verifier = rollbackVerifierFunc(func(context.Context, string) (updates.Application, error) {
				app := f.application
				switch kind {
				case "publisher":
					app.Identity.TeamID = "ZZZZZZZZZZ"
				case "inventory":
					app.Inventory.SHA256 = strings.Repeat("0", 64)
				case "signature":
					return app, errors.New("signature refused")
				case "changed-during-verification":
					if err := os.WriteFile(filepath.Join(f.destination, "files", "asset"), []byte("changed"), 0600); err != nil {
						t.Fatal(err)
					}
				case "cancel-during-verification":
					cancel()
				case "app-bytes-after-verification":
					if err := os.WriteFile(filepath.Join(f.source, "Contents", "MacOS", "Trove"), []byte("replacement executable"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return app, nil
			})
			if err := f.a.launchApplicationRollback(ctx, f.id, deps); err == nil {
				t.Fatal("unsafe launch accepted")
			}
			if _, err := os.Stat(f.preference); !os.IsNotExist(err) {
				t.Fatal("preference changed after rejected verification")
			}
		})
	}
}
func TestRollbackLaunchFailureRestoresPreferenceAndKeepsCurrentApp(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "concurrent-change", "cancel-before-spawn"} {
		t.Run(kind, func(t *testing.T) {
			f := newRollbackFixture(t)
			previous := []byte("{\n  \"path\": \"/newer/workspace\"\n}\n")
			if kind != "absent" {
				if err := os.MkdirAll(filepath.Dir(f.preference), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.preference, previous, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deps := f.dependencies(t)
			called := false
			deps.start = func(ctx context.Context, _ string, _ []string, _ updates.RollbackTarget) error {
				called = true
				if kind == "concurrent-change" {
					if err := saveWorkspaceSelection(f.preference, "/another/selection"); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "cancel-before-spawn" {
					cancel()
					return ctx.Err()
				}
				return errors.New("injected spawn failure")
			}
			err := f.a.launchApplicationRollback(ctx, f.id, deps)
			if err == nil || !called {
				t.Fatal("expected refused spawn")
			}
			raw, err := os.ReadFile(f.preference)
			switch kind {
			case "absent":
				if !os.IsNotExist(err) {
					t.Fatal("failed launch left a new default")
				}
			case "concurrent-change":
				if !strings.Contains(string(raw), "/another/selection") {
					t.Fatal("overwrote concurrent preference")
				}
			default:
				if err != nil || string(raw) != string(previous) {
					t.Fatalf("previous preference not restored exactly: %s %v", raw, err)
				}
			}
		})
	}
}
func TestRollbackLaunchExpiryUnknownAndBoundedNativeReviews(t *testing.T) {
	f := newRollbackFixture(t)
	state := f.a.rollbackLaunchState()
	state.mu.Lock()
	old := state.reviews[f.id]
	old.expires = time.Now().Add(-time.Second)
	state.reviews[f.id] = old
	state.mu.Unlock()
	for _, id := range []string{f.id, f.destination, "unknown"} {
		if err := f.a.launchApplicationRollback(context.Background(), id, f.dependencies(t)); err == nil {
			t.Fatal("invalid approval accepted")
		}
	}
	for i := 0; i < 18; i++ {
		if _, err := f.a.saveRollbackLaunchReviewContext(context.Background(), f.source, f.application, f.destination); err != nil {
			t.Fatal(err)
		}
	}
	state.mu.Lock()
	count := len(state.reviews)
	state.mu.Unlock()
	if count > 16 {
		t.Fatal("unbounded launch reviews")
	}
	// A caller's slice mutation must not change what was approved.
	id, err := f.a.saveRollbackLaunchReviewContext(context.Background(), f.source, f.application, f.destination)
	if err != nil {
		t.Fatal(err)
	}
	f.application.Inventory.Objects[0].Path = "rewritten"
	review, err := f.a.takeRollbackLaunchReview(id)
	if err != nil || review.application.Inventory.Objects[0].Path == "rewritten" {
		t.Fatal("mutable approved app inventory")
	}
}
func TestRollbackLaunchEnvironmentAllowlist(t *testing.T) {
	env := rollbackEnvironment([]string{"HOME=/safe", "HOME=/actual", "USER=user", "LOGNAME=user", "TMPDIR=/private/tmp/", "LANG=en_US.UTF-8", "LC_ALL=C", "PATH=/evil", "TROVE_WORKSPACE=/newer", "DYLD_LIBRARY_PATH=/evil", "DYLD_INSERT_LIBRARIES=/evil", "LD_PRELOAD=/evil", "SSH_AUTH_SOCK=/secret", "ANTHROPIC_API_KEY=secret", "OPENAI_API_KEY=secret", "TROVE_PROVIDER_TOKEN=secret", "BASH_ENV=/evil"}, "/native/restored")
	expected := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=/actual", "USER=user", "LOGNAME=user", "TMPDIR=/private/tmp/", "LANG=en_US.UTF-8", "LC_ALL=C", "TROVE_WORKSPACE=/native/restored"}
	if !reflect.DeepEqual(env, expected) {
		t.Fatalf("environment differs: %q", env)
	}
}
func TestRollbackLaunchCaptureCancellationAndCompatibility(t *testing.T) {
	f := newRollbackFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if id, err := f.a.saveRollbackLaunchReviewContext(ctx, f.source, f.application, f.destination); err == nil || id != "" {
		t.Fatal("canceled capture minted approval")
	}
	app := f.application
	app.Identity.StorageGeneration = 2
	if id, err := f.a.saveRollbackLaunchReviewContext(context.Background(), f.source, app, f.destination); err == nil || id != "" {
		t.Fatal("mismatched format minted approval")
	}
	if id := f.a.saveRollbackLaunchReview("renderer-relative", f.application, f.destination); id != "" {
		t.Fatal("relative executable accepted")
	}
}

func TestRollbackLaunchRejectsMissingSignedHandshakeBeforeSelection(t *testing.T) {
	f := newRollbackFixture(t)
	f.application.Identity.RollbackHandshake = 0
	f.id = f.a.saveRollbackLaunchReview(f.source, f.application, f.destination)
	if err := f.a.launchApplicationRollback(context.Background(), f.id, f.dependencies(t)); err == nil {
		t.Fatal("launched protocol-less old application")
	}
	if _, err := os.Stat(f.preference); !os.IsNotExist(err) {
		t.Fatal("changed preference before capability refusal")
	}
}
func TestRollbackLaunchDoesNotQuitUntilReadinessAndInitializationAcknowledged(t *testing.T) {
	f := newRollbackFixture(t)
	deps := f.dependencies(t)
	entered, ack, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	quit := make(chan struct{}, 1)
	deps.start = func(ctx context.Context, path string, env []string, target updates.RollbackTarget) error {
		if target.WorkspacePath != f.destination || target.WorkspaceID != "restored-identity" || target.UserVersion != 3 || target.StorageGeneration != 1 {
			return errors.New("incorrect reviewed target")
		}
		close(entered)
		<-ack
		return nil
	}
	deps.quit = func() { quit <- struct{}{} }
	go func() { done <- f.a.launchApplicationRollback(context.Background(), f.id, deps) }()
	<-entered
	select {
	case <-quit:
		t.Fatal("quit before verified startup")
	default:
	}
	close(ack)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-quit:
	default:
		t.Fatal("did not finish acknowledged handoff")
	}
}
func TestRollbackUnverifiedChildCleanupRetainsSelectionAndCurrentApp(t *testing.T) {
	f := newRollbackFixture(t)
	deps := f.dependencies(t)
	deps.start = func(context.Context, string, []string, updates.RollbackTarget) error {
		return updates.ErrRollbackCleanupUnverified
	}
	if err := f.a.launchApplicationRollback(context.Background(), f.id, deps); !errors.Is(err, updates.ErrRollbackCleanupUnverified) {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(f.preference)
	if err != nil || !strings.Contains(string(raw), f.destination) {
		t.Fatal("silently redirected a potentially live rollback child", err)
	}
}
