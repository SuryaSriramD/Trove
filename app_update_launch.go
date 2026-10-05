package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/internal/updates"
	"trove.local/phase0/internal/workspace"
)

type rollbackLaunchReview struct {
	source, destination string
	application         updates.Application
	workspace           rollbackWorkspace
	expires             time.Time
}
type rollbackWorkspace struct {
	ID        string
	Format    workspace.CompatibilityDescriptor
	Inventory updates.Inventory
}
type rollbackLaunchState struct {
	mu      sync.Mutex
	reviews map[string]rollbackLaunchReview
}

var rollbackLaunchStates sync.Map

func (a *App) rollbackLaunchState() *rollbackLaunchState {
	value, _ := rollbackLaunchStates.LoadOrStore(a, &rollbackLaunchState{reviews: map[string]rollbackLaunchReview{}})
	return value.(*rollbackLaunchState)
}

// Only the native restore path can mint these handles. Capture includes the
// restored database after permission cleanup, every asset, recording and receipt.
func (a *App) saveRollbackLaunchReview(sourceApp string, app updates.Application, destination string) string {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	id, _ := a.saveRollbackLaunchReviewContext(ctx, sourceApp, app, destination)
	return id
}
func (a *App) saveRollbackLaunchReviewContext(ctx context.Context, sourceApp string, app updates.Application, destination string) (string, error) {
	source, err := canonicalRollbackPath(sourceApp)
	if err != nil {
		return "", err
	}
	destination, err = canonicalRollbackPath(destination)
	if err != nil {
		return "", err
	}
	if updates.PathsOverlap(source, destination) {
		return "", errors.New("rollback application and workspace must be separate")
	}
	snapshot, err := inspectRollbackWorkspace(ctx, destination)
	if err != nil {
		return "", err
	}
	if snapshot.Format.UserVersion != app.Identity.UserVersion || snapshot.Format.StorageGeneration != app.Identity.StorageGeneration {
		return "", errors.New("restored workspace format does not match the previous application")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	// Copy the slice so a later caller mutation cannot rewrite an approved inventory.
	app.Inventory.Objects = append([]updates.Object(nil), app.Inventory.Objects...)
	review := rollbackLaunchReview{source, destination, app, snapshot, time.Now().Add(10 * time.Minute)}
	s := a.rollbackLaunchState()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.reviews {
		if !old.expires.After(time.Now()) {
			delete(s.reviews, id)
		}
	}
	if len(s.reviews) >= 16 {
		clear(s.reviews)
	}
	id := workspace.NewID()
	s.reviews[id] = review
	return id, nil
}
func (a *App) takeRollbackLaunchReview(id string) (rollbackLaunchReview, error) {
	s := a.rollbackLaunchState()
	s.mu.Lock()
	defer s.mu.Unlock()
	review, ok := s.reviews[id]
	delete(s.reviews, id)
	if !ok || !review.expires.After(time.Now()) {
		return rollbackLaunchReview{}, errors.New("rollback launch review expired; review and restore the backup again")
	}
	return review, nil
}

type rollbackLaunchDependencies struct {
	verifier    updates.Verifier
	preference  string
	environment []string
	start       func(context.Context, string, []string) error
	quit        func()
}

// LaunchApplicationRollback is a separate explicit approval from restoring. No
// renderer-controlled path, executable argument, environment or verifier enters it.
func (a *App) LaunchApplicationRollback(id string) error {
	ctx, done, err := a.beginUpdateOperation()
	if err != nil {
		return err
	}
	defer done()
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	return a.launchApplicationRollback(ctx, id, rollbackLaunchDependencies{
		verifier: updates.MacVerifier{}, preference: filepath.Join(config, "Trove", "workspace-selection.json"),
		environment: os.Environ(), start: updates.LaunchRollback,
		quit: func() { runtime.Quit(a.ctx) },
	})
}
func (a *App) launchApplicationRollback(ctx context.Context, id string, deps rollbackLaunchDependencies) error {
	// Exclude native workspace switches until the new selection and child process
	// agree. Quit outside this lock because shutdown needs it to flush and close.
	a.lifecycle.Lock()
	err := a.launchApplicationRollbackLocked(ctx, id, deps)
	a.lifecycle.Unlock()
	if err == nil {
		deps.quit()
	}
	return err
}
func (a *App) launchApplicationRollbackLocked(ctx context.Context, id string, deps rollbackLaunchDependencies) error {
	review, err := a.takeRollbackLaunchReview(id)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	select {
	case <-a.shutdownDone:
		return errors.New("application is shutting down")
	default:
	}
	a.mu.RLock()
	current := a.kernel
	if current != nil && updates.PathsOverlap(current.Path(), review.destination) {
		a.mu.RUnlock()
		return errors.New("restored workspace is already open; restore and review a fresh copy")
	}
	a.mu.RUnlock()
	for _, path := range []string{review.source, review.destination} {
		actual, e := canonicalRollbackPath(path)
		if e != nil || actual != path {
			return errors.New("reviewed rollback location changed")
		}
	}
	application, err := deps.verifier.Inspect(ctx, review.source)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(application, review.application) {
		return errors.New("saved previous application changed after review")
	}
	snapshot, err := inspectRollbackWorkspace(ctx, review.destination)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(snapshot, review.workspace) {
		return errors.New("restored workspace changed after review; restore a fresh copy before rollback")
	}
	if snapshot.Format.UserVersion != application.Identity.UserVersion || snapshot.Format.StorageGeneration != application.Identity.StorageGeneration {
		return errors.New("restored workspace is incompatible with the saved application")
	}
	// Workspace hashing can be long. Recheck exact signed application bytes after
	// it, immediately before changing the startup preference and spawning.
	inventory, err := updates.InspectTree(ctx, review.source)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(inventory, review.application.Inventory) {
		return errors.New("saved previous application changed during workspace verification")
	}
	previous, existed, err := readRollbackPreference(deps.preference)
	if err != nil {
		return err
	}
	selected, _ := json.Marshal(workspaceSelection{Path: review.destination})
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = saveWorkspaceSelection(deps.preference, review.destination); err != nil {
		return errors.Join(err, restoreRollbackPreference(deps.preference, selected, previous, existed))
	}
	// Cancellation before process creation is reversible. Once Start succeeds the
	// handoff has committed; cancellation must not kill the newly selected app.
	if err = ctx.Err(); err == nil {
		err = deps.start(ctx, review.source, rollbackEnvironment(deps.environment, review.destination))
	}
	if err != nil {
		return errors.Join(err, restoreRollbackPreference(deps.preference, selected, previous, existed))
	}
	return nil
}

func rollbackEnvironment(inherited []string, destination string) []string {
	allowed := map[string]bool{"HOME": true, "USER": true, "LOGNAME": true, "TMPDIR": true, "LANG": true, "LC_ALL": true}
	values := map[string]string{}
	for _, pair := range inherited {
		key, value, ok := strings.Cut(pair, "=")
		if ok && allowed[key] && !strings.ContainsRune(value, '\x00') {
			values[key] = value
		}
	}
	out := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	for _, key := range []string{"HOME", "USER", "LOGNAME", "TMPDIR", "LANG", "LC_ALL"} {
		if value, ok := values[key]; ok {
			out = append(out, key+"="+value)
		}
	}
	return append(out, "TROVE_WORKSPACE="+destination)
}
func canonicalRollbackPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("rollback path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

// Restored workspaces are closed copies. A complete bounded inventory rejects
// links, partial restores and uncheckpointed SQLite writes rather than guessing
// which new files might be harmless. Unlike application inventories, this admits
// up to 100,000 objects / 1 TiB; cancellation is checked on every 128 KiB read.
func inspectRollbackWorkspace(ctx context.Context, path string) (rollbackWorkspace, error) {
	var out rollbackWorkspace
	root, err := os.OpenRoot(path)
	if err != nil {
		return out, err
	}
	defer root.Close()
	for _, name := range []string{"INCOMPLETE", "workspace.db-wal", "workspace.db-journal"} {
		if st, e := root.Lstat(name); e == nil {
			if name == "INCOMPLETE" || !st.Mode().IsRegular() || st.Size() != 0 {
				return out, errors.New("rollback workspace is incomplete or has active database writes")
			}
		} else if !os.IsNotExist(e) {
			return out, e
		}
	}
	if st, e := root.Lstat("workspace.db"); e != nil || !st.Mode().IsRegular() {
		return out, errors.New("restored database is unavailable")
	}
	out.Format, err = workspace.ReadWorkspaceCompatibility(ctx, path)
	if err != nil {
		return out, err
	}
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: filepath.Join(path, "workspace.db")}).String()+"?mode=ro&immutable=1")
	if err != nil {
		return out, err
	}
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(MIN(id),'') FROM workspaces").Scan(&count, &out.ID)
	closeErr := db.Close()
	if err != nil {
		return out, err
	}
	if closeErr != nil {
		return out, closeErr
	}
	if count != 1 || out.ID == "" {
		return out, errors.New("restored workspace identity is invalid")
	}
	buffer := make([]byte, 128<<10)
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(name) > 4096 || len(out.Inventory.Objects) >= 100000 {
			return errors.New("rollback workspace inventory exceeds supported bounds")
		}
		st, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if st.Mode().Perm()&0022 != 0 || st.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
			return errors.New("rollback workspace contains writable or unsafe objects")
		}
		object := updates.Object{Path: name, Mode: uint32(st.Mode().Perm()), Directory: st.IsDir()}
		if st.IsDir() {
			out.Inventory.Objects = append(out.Inventory.Objects, object)
			return nil
		}
		if !st.Mode().IsRegular() {
			return errors.New("rollback workspace must contain regular files and directories only")
		}
		if st.Size() < 0 || st.Size() > (1<<40)-out.Inventory.Bytes {
			return errors.New("rollback workspace exceeds 1 TiB")
		}
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !os.SameFile(st, opened) {
			return errors.New("restored object changed while opening")
		}
		hash := sha256.New()
		var copied int64
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, readErr := file.Read(buffer)
			copied += int64(n)
			if copied > st.Size() {
				return errors.New("restored object grew during verification")
			}
			if n > 0 {
				_, _ = hash.Write(buffer[:n])
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
		after, err := file.Stat()
		if err != nil || copied != st.Size() || after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
			return errors.New("restored object changed during verification")
		}
		object.Size, object.SHA256 = copied, hex.EncodeToString(hash.Sum(nil))
		out.Inventory.Bytes += copied
		out.Inventory.Objects = append(out.Inventory.Objects, object)
		return nil
	})
	if err != nil {
		return rollbackWorkspace{}, err
	}
	if err = ctx.Err(); err != nil {
		return rollbackWorkspace{}, err
	}
	raw, _ := json.Marshal(out.Inventory.Objects)
	digest := sha256.Sum256(raw)
	out.Inventory.SHA256 = hex.EncodeToString(digest[:])
	return out, nil
}

func readRollbackPreference(path string) ([]byte, bool, error) {
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !st.Mode().IsRegular() || st.Size() > 16384 {
		return nil, false, errors.New("workspace selection preference is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(st, opened) {
		return nil, false, errors.New("workspace selection preference changed")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil {
		return nil, false, err
	}
	if len(raw) > 16384 {
		return nil, false, errors.New("workspace selection preference exceeds bounds")
	}
	return raw, true, nil
}
func restoreRollbackPreference(path string, selected, previous []byte, existed bool) error {
	current, present, err := readRollbackPreference(path)
	if err != nil {
		return fmt.Errorf("could not restore previous workspace selection: %w", err)
	}
	if present == existed && bytes.Equal(current, previous) {
		return nil
	}
	if !present || !bytes.Equal(current, selected) {
		return errors.New("workspace selection changed during failed launch; its newer selection was preserved")
	}
	if existed {
		file, err := os.CreateTemp(filepath.Dir(path), ".selection-rollback-")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if _, err = file.Write(previous); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err = os.Rename(file.Name(), path); err != nil {
			return err
		}
	} else if err = os.Remove(path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
