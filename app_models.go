package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/internal/native"
)

type nativeState struct {
	mu                sync.Mutex
	manager           *native.Manager
	supervisor        *native.Supervisor
	catalog           []native.Pack
	cancellations     map[string]context.CancelFunc
	operations        map[string]modelOperation
	operationRevision uint64
	err               string
}

func nativeBundlePath() string {
	if p := os.Getenv("TROVE_NATIVE_BUNDLE"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err == nil {
		bundle := filepath.Join(filepath.Dir(exe), "..", "Resources", "native")
		if _, e := os.Stat(filepath.Join(bundle, "manifest.json")); e == nil {
			return bundle
		}
	}
	return filepath.Join("build", "native")
}
func (a *App) initNative() {
	a.native = &nativeState{cancellations: map[string]context.CancelFunc{}, catalog: []native.Pack{}}
	root := os.Getenv("TROVE_MODEL_ROOT")
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			a.native.err = err.Error()
			return
		}
		root = filepath.Join(base, "Trove", "models")
	}
	manager, err := native.NewManager(root)
	if err != nil {
		a.native.err = err.Error()
		return
	}
	a.native.manager = manager
	bundle := nativeBundlePath()
	raw, err := os.ReadFile(filepath.Join(bundle, "models", "catalog.json"))
	if err == nil {
		var list []native.Pack
		if json.Unmarshal(raw, &list) != nil {
			var wrapped struct {
				Packs []native.Pack `json:"packs"`
			}
			if json.Unmarshal(raw, &wrapped) == nil {
				list = wrapped.Packs
			}
		}
		for _, pack := range list {
			if pack.Validate() == nil {
				a.native.catalog = append(a.native.catalog, pack)
			}
		}
	}
	a.native.supervisor, err = native.NewSupervisor(bundle, manager)
	if err != nil {
		a.native.err = err.Error()
	}
}
func (a *App) nativeDispatch(method string, raw json.RawMessage) string {
	if a.native == nil || a.native.manager == nil {
		return errorEnvelope("WORKER_FAILED", "Model manager is unavailable")
	}
	n := a.native
	var input struct {
		PackID string `json:"packId"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return errorEnvelope("INVALID_REQUEST", "Invalid model request")
		}
	}
	success := func(v any) string { b, _ := json.Marshal(map[string]any{"ok": true, "result": v}); return string(b) }
	switch method {
	case "models.list", "models.status":
		n.mu.Lock()
		observedRevision := n.operationRevision
		n.mu.Unlock()
		packs, err := n.manager.List()
		if err != nil {
			return errorEnvelope("INTERNAL", err.Error())
		}
		return success(map[string]any{"packs": packs, "operations": n.modelOperations(packs, observedRevision), "catalog": n.catalog, "runtime": map[string]any{"available": n.supervisor != nil, "visionAvailable": n.supervisor.VisionAvailable(), "error": n.err}})
	case "models.cancel":
		result, ok := n.cancelModelOperation(input.PackID)
		if !ok {
			return errorEnvelope("NOT_FOUND", "No model operation is active")
		}
		return success(result)
	case "models.remove":
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.cancellations[input.PackID] != nil {
			return errorEnvelope("RESOURCE_EXHAUSTED", "Model acquisition is still running")
		}
		if err := n.manager.Remove(input.PackID); err != nil {
			return errorEnvelope("RESOURCE_EXHAUSTED", err.Error())
		}
		return success(n.updateModelOperationLocked(input.PackID, "removed", 0, ""))
	case "models.acquire":
		var pack *native.Pack
		for _, p := range n.catalog {
			if p.ID == input.PackID {
				copy := p
				pack = &copy
				break
			}
		}
		if pack == nil {
			return errorEnvelope("NOT_FOUND", "Model pack is not in the approved catalogue")
		}
		n.mu.Lock()
		if _, busy := n.cancellations[pack.ID]; busy {
			n.mu.Unlock()
			return errorEnvelope("RESOURCE_EXHAUSTED", "Model acquisition is already running")
		}
		ctx, cancel := context.WithCancel(a.ctx)
		n.cancellations[pack.ID] = cancel
		started := n.startModelOperationLocked(pack.ID, "acquiring")
		n.mu.Unlock()
		go func() {
			defer cancel()
			err := n.manager.Acquire(ctx, *pack, a.modelProgress(pack.ID, "acquiring"))
			state, failure := "ready", ""
			if err != nil {
				state, failure = "interrupted", err.Error()
			}
			runtime.EventsEmit(a.ctx, "models:progress", n.updateModelOperation(pack.ID, state, -1, failure))
		}()
		return success(started)
	default:
		return errorEnvelope("INVALID_REQUEST", "Unknown model operation")
	}
}
func (a *App) PickModelPack(packID string) error {
	if err := a.requireRollbackCommitted(); err != nil {
		return err
	}

	if a.native == nil || a.native.manager == nil {
		return fmt.Errorf("model manager unavailable")
	}
	var pack *native.Pack
	for _, p := range a.native.catalog {
		if p.ID == packID {
			copy := p
			pack = &copy
			break
		}
	}
	if pack == nil {
		return fmt.Errorf("unknown pack")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Import verified model pack"})
	if err != nil || path == "" {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	n := a.native
	n.mu.Lock()
	if _, busy := n.cancellations[packID]; busy {
		n.mu.Unlock()
		return fmt.Errorf("model acquisition is already running")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	n.cancellations[packID] = cancel
	started := n.startModelOperationLocked(packID, "importing")
	n.mu.Unlock()
	defer cancel()
	runtime.EventsEmit(a.ctx, "models:progress", started)
	if pack.Projector != nil {
		path, pickErr := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Import matching verified vision projector"})
		if pickErr != nil {
			err = pickErr
		} else if path == "" {
			err = context.Canceled
		} else {
			companion, openErr := os.Open(path)
			if openErr != nil {
				err = openErr
			} else {
				err = n.manager.ImportVision(ctx, *pack, f, companion, a.modelProgress(packID, "importing"))
				companion.Close()
			}
		}
	} else {
		err = n.manager.Import(ctx, *pack, f, a.modelProgress(packID, "importing"))
	}
	state := "ready"
	if err != nil {
		state = "failed"
	}
	failure := ""
	if err != nil {
		failure = err.Error()
	}
	if errors.Is(err, context.Canceled) {
		state = "interrupted"
	}
	runtime.EventsEmit(a.ctx, "models:progress", n.updateModelOperation(packID, state, -1, failure))
	return err
}

func (a *App) modelProgress(packID, state string) func(int64) {
	var last time.Time
	return func(bytes int64) {
		if time.Since(last) < 100*time.Millisecond {
			return
		}
		last = time.Now()
		runtime.EventsEmit(a.ctx, "models:progress", a.native.updateModelOperation(packID, state, bytes, ""))
	}
}

// SetEditing is a scheduling hint, not an authorization or mutation capability.
func (a *App) SetEditing(active bool) {
	if err := a.requireRollbackCommitted(); err != nil {
		return
	}

	if a.native != nil && a.native.supervisor != nil {
		a.native.supervisor.Scheduler.SetEditing(active)
	}
}
