package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

// App is the trusted desktop adapter. It never accepts execution actors or raw
// workspace paths from a renderer command. Native dialogs mint input handles.
type App struct {
	mu              sync.RWMutex
	lifecycle       sync.RWMutex
	ctx             context.Context
	kernel          *workspace.Kernel
	execution       contracts.ServiceExecutionContext
	startupError    string
	pendingDeepLink string
	stopEvents      func()
	native          *nativeState
	jobCancel       context.CancelFunc
	jobDone         <-chan struct{}
	uploadGate      chan struct{}
	startupDone     chan struct{}
	startupOnce     sync.Once
	shutdownDone    chan struct{}
	shutdownOnce    sync.Once
	rollback        *rollbackChildState
	performance     *qualificationPerformance
}

func NewApp() *App {
	return &App{uploadGate: make(chan struct{}, 2), startupDone: make(chan struct{}), shutdownDone: make(chan struct{}), performance: newQualificationPerformance(os.Getenv("TROVE_PERFORMANCE_REPORT"))}
}
func defaultWorkspacePath() (string, error) {
	if p := os.Getenv("TROVE_WORKSPACE"); p != "" {
		return filepath.Abs(p)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if path, err := selectedWorkspace(filepath.Join(base, "Trove", "workspace-selection.json")); path != "" || err != nil {
		return path, err
	}
	return filepath.Join(base, "Trove", "workspaces", "default"), nil
}
func (a *App) startup(ctx context.Context) {
	markStartupStage("native_startup_enter")
	a.initializeStartup(ctx, func() error {
		a.initNative()
		markStartupStage("native_runtime_ready")
		runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
			for _, path := range paths {
				handle, err := a.selectFile(path)
				if err != nil {
					runtime.EventsEmit(ctx, "native:fileError", err.Error())
					continue
				}
				runtime.EventsEmit(ctx, "native:fileSelected", map[string]any{"selectionHandle": handle, "x": x, "y": y})
			}
		})
		path, err := defaultWorkspacePath()
		if err != nil {
			return err
		}
		select {
		case <-a.shutdownDone:
			return fmt.Errorf("application shut down during workspace startup")
		default:
		}
		markStartupStage("workspace_open_start")
		if a.rollback != nil {
			target := a.rollback.child.Target()
			if path != target.WorkspacePath {
				return fmt.Errorf("rollback startup preference differs from reviewed destination")
			}
			k, err := workspace.OpenCompatibleReviewed(ctx, target.WorkspacePath, target.WorkspaceID, workspace.CompatibilityDescriptor{UserVersion: target.UserVersion, StorageGeneration: target.StorageGeneration})
			if err != nil {
				return err
			}
			return a.installKernelLocked(k)
		}
		return a.openLocked(path, true)
	})
}

// Wails starts OnStartup asynchronously with the webview. Both bound renderer
// calls and OnDomReady can arrive before it has opened the workspace.
func (a *App) initializeStartup(ctx context.Context, initialize func() error) {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	select {
	case <-a.shutdownDone:
		a.completeStartup(fmt.Errorf("application shut down before workspace startup"))
		return
	default:
	}
	a.completeStartup(initialize())
}

func (a *App) completeStartup(err error) {
	if err == nil {
		markStartupStage("startup_complete")
	}
	a.mu.Lock()
	if err != nil {
		a.startupError = err.Error()
	}
	a.mu.Unlock()
	a.startupOnce.Do(func() { close(a.startupDone) })
}

func (a *App) waitForStartup(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	wait, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	select {
	case <-a.shutdownDone:
		return fmt.Errorf("application is shutting down")
	case <-wait.Done():
		return fmt.Errorf("workspace startup did not complete: %w", wait.Err())
	case <-a.startupDone:
	}
	if err := wait.Err(); err != nil {
		return fmt.Errorf("workspace startup did not complete: %w", err)
	}
	select {
	case <-a.shutdownDone:
		return fmt.Errorf("application is shutting down")
	default:
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.startupError != "" {
		return fmt.Errorf("workspace startup failed: %s", a.startupError)
	}
	if a.kernel == nil {
		return fmt.Errorf("workspace startup completed without an open workspace")
	}
	return nil
}

func (a *App) shutdown(context.Context) {
	defer a.performance.close()
	markStartupStage("shutdown_begin")
	defer markStartupStage("shutdown_complete")
	a.shutdownOnce.Do(func() { close(a.shutdownDone) })
	a.CancelUpdateOperation()
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	a.stopClipper()
	a.stopRecordings()
	a.stopAI()
	a.stopProviderRemote()
	a.stopProviders()
	a.stopProviderClaude()
	a.stopProviderAuth()
	if a.native != nil {
		a.native.mu.Lock()
		for _, cancel := range a.native.cancellations {
			cancel()
		}
		a.native.mu.Unlock()
	}
	a.mu.Lock()
	k, cancel, done := a.kernel, a.jobCancel, a.jobDone
	a.kernel, a.jobCancel, a.jobDone = nil, nil, nil
	if a.stopEvents != nil {
		a.stopEvents()
		a.stopEvents = nil
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	if k != nil {
		k.Close()
	}
}
func (a *App) open(path string, create bool) error {
	if err := a.requireRollbackCommitted(); err != nil {
		return err
	}
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	return a.openLocked(path, create)
}
func (a *App) openLocked(path string, create bool) error {
	a.stopClipper()
	a.stopRecordings()
	a.stopAI()
	a.stopProviderRemote()
	a.stopProviders()
	a.stopProviderClaude()
	a.stopProviderAuth()
	var k *workspace.Kernel
	var err error
	if _, statErr := os.Stat(filepath.Join(path, "workspace.db")); statErr == nil {
		k, err = workspace.Open(path)
	} else if create {
		k, err = workspace.Create(path)
	} else {
		return fmt.Errorf("selected folder is not a Trove workspace")
	}
	if err != nil {
		return err
	}
	return a.installKernelLocked(k)
}
func (a *App) installKernelLocked(k *workspace.Kernel) error {
	markStartupStage("workspace_open_complete")
	a.mu.Lock()
	old := a.kernel
	cancel, done := a.jobCancel, a.jobDone
	a.jobCancel, a.jobDone = nil, nil
	if a.stopEvents != nil {
		a.stopEvents()
	}
	a.kernel = k
	a.execution = k.OwnerContext()
	a.startupError = ""
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	if old != nil {
		old.Close()
	}
	a.subscribe(k)
	if a.requireRollbackCommitted() == nil {
		a.startJobs(k)
	}
	return nil
}
func errorEnvelope(code, message string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error": map[string]any{"code": code, "message": message, "retryable": false, "requestId": workspace.NewID()}})
	return string(b)
}
func (a *App) Dispatch(method, raw string) string {
	if finish := a.performance.dispatch(method); finish != nil {
		defer finish()
	}
	if err := a.requireRollbackCommitted(); err != nil && method != "workspace.snapshot" {
		return errorEnvelope("WORKSPACE_LOCKED", err.Error())
	}
	if len(raw) > 4*1024*1024 {
		return errorEnvelope("RESOURCE_EXHAUSTED", "Request exceeds 4 MiB")
	}
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if err := a.waitForStartup(ctx); err != nil {
		return errorEnvelope("WORKSPACE_LOCKED", err.Error())
	}
	if strings.HasPrefix(method, "models.") {
		return a.nativeDispatch(method, json.RawMessage(raw))
	}
	if strings.HasPrefix(method, "providers.") {
		if method == "providers.auth.signOut" || method == "providers.claude.login" || method == "providers.claude.logout" || strings.HasPrefix(method, "providers.remote.") && method != "providers.remote.status" {
			a.lifecycle.Lock()
			defer a.lifecycle.Unlock()
			return a.providerDispatch(method, json.RawMessage(raw))
		}
		a.lifecycle.RLock()
		defer a.lifecycle.RUnlock()
		return a.providerDispatch(method, json.RawMessage(raw))
	}
	if method == "image.start" || method == "image.cancel" {
		a.lifecycle.RLock()
		defer a.lifecycle.RUnlock()
		return a.imageDispatch(method, json.RawMessage(raw))
	}
	if method == "review.cancel" {
		a.lifecycle.RLock()
		defer a.lifecycle.RUnlock()
		return a.reviewCancel(json.RawMessage(raw))
	}
	if method == "ai.start" || method == "ai.cancel" {
		a.lifecycle.Lock()
		defer a.lifecycle.Unlock()
		return a.aiDispatch(method, json.RawMessage(raw))
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return errorEnvelope("WORKSPACE_LOCKED", "Workspace is not open")
	}
	// Bootstrap is a trusted adapter operation: the renderer does not know the
	// workspace ID until the first snapshot. All later requests carry headers.
	if method == "workspace.snapshot" {
		var bootstrap map[string]any
		if strings.TrimSpace(raw) == "" {
			raw = "{}"
		}
		if json.Unmarshal([]byte(raw), &bootstrap) == nil && bootstrap != nil && bootstrap["workspaceId"] == nil {
			// Preserve caller options and unknown fields for contract validation.
			bootstrap["contractVersion"], bootstrap["requestId"], bootstrap["workspaceId"] = 1, workspace.NewID(), a.kernel.ID()
			payload, _ := json.Marshal(bootstrap)
			raw = string(payload)
		}
	}
	result := a.kernel.Dispatch(a.ctx, a.execution, method, json.RawMessage(raw))
	if method == "workspace.snapshot" && startupTracePending("snapshot_ready") {
		var envelope struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal(result, &envelope) == nil && envelope.OK {
			markStartupStage("snapshot_ready")
		}
	}
	return string(result)
}
func (a *App) WorkspaceInfo() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := map[string]any{"contractVersion": "1.3.0", "startupError": a.startupError}
	if a.kernel != nil {
		out["workspaceId"] = a.kernel.ID()
		out["path"] = a.kernel.Path()
	}
	return out
}
func (a *App) ChooseWorkspace(create bool) error {
	if err := a.requireRollbackCommitted(); err != nil {
		return err
	}
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose a Trove workspace", CanCreateDirectories: create, ResolvesAliases: true, TreatPackagesAsDirectories: true})
	if err != nil || path == "" {
		return err
	}
	if err = a.open(path, create); err != nil {
		return err
	}
	if os.Getenv("TROVE_WORKSPACE") == "" {
		base, configErr := os.UserConfigDir()
		if configErr == nil {
			configErr = saveWorkspaceSelection(filepath.Join(base, "Trove", "workspace-selection.json"), path)
		}
		if configErr != nil {
			return fmt.Errorf("workspace opened, but its restart preference could not be saved: %w", configErr)
		}
	}
	runtime.EventsEmit(a.ctx, "workspace:opened", a.WorkspaceInfo())
	return nil
}
func (a *App) OpenWorkspace() error   { return a.ChooseWorkspace(false) }
func (a *App) CreateWorkspace() error { return a.ChooseWorkspace(true) }
func (a *App) selectFile(path string) (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	return a.kernel.RegisterSelection(path)
}
func (a *App) PickFile() (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Attach a file"})
	if err != nil || path == "" {
		return "", err
	}
	return a.selectFile(path)
}
func (a *App) StageUpload(name, mediaType, encoded string) (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}
	// This is a byte upload capability, never a renderer-supplied filesystem path.
	select {
	case a.uploadGate <- struct{}{}:
		defer func() { <-a.uploadGate }()
	default:
		return "", fmt.Errorf("another file upload is in progress; retry after it finishes")
	}
	if len(encoded) > 36*1024*1024 {
		return "", fmt.Errorf("upload exceeds 25 MiB")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("invalid upload encoding")
	}
	if len(decoded) > 25*1024*1024 {
		return "", fmt.Errorf("upload exceeds 25 MiB")
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		name = "attachment"
	}
	directory, err := os.MkdirTemp("", "trove-upload-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, name)
	if err = os.WriteFile(path, decoded, 0600); err != nil {
		os.RemoveAll(directory)
		return "", err
	}
	// RegisterSelection owns an open descriptor. On macOS it remains readable
	// after unlinking this temporary path; close/expiry releases the staged bytes.
	handle, err := a.selectFile(path)
	if err != nil {
		os.RemoveAll(directory)
		return "", err
	}
	_ = mediaType // MIME is detected/verified from bytes by the file service.
	return handle, nil
}
func (a *App) serveFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/files/")
	if id == r.URL.Path || strings.ContainsAny(id, "/\\") || len(id) != 36 {
		http.NotFound(w, r)
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		http.Error(w, "workspace unavailable", http.StatusServiceUnavailable)
		return
	}
	file, meta, err := a.kernel.ReadFile(r.Context(), a.execution, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; media-src 'self'; img-src 'self' data:; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "private, no-store")
	mediaType := meta.MediaType
	if mediaType == "audio/wave" {
		mediaType = "audio/wav"
	}
	w.Header().Set("Content-Type", mediaType)
	inline := strings.HasPrefix(meta.MediaType, "image/") || strings.HasPrefix(meta.MediaType, "audio/") || strings.HasPrefix(meta.MediaType, "video/") || meta.MediaType == "application/pdf"
	if !inline {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": meta.OriginalName}))
	}
	http.ServeContent(w, r, meta.OriginalName, time.Time{}, file)
}

// Ensure the range-serving path remains a seekable stream, not a full RAM copy.
var _ io.ReadSeeker = (*os.File)(nil)
