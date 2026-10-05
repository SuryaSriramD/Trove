package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/internal/workspace"
)

type supportState struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	bundle *workspace.SupportBundle
	kernel *workspace.Kernel
	token  string
	timer  *time.Timer
}

var supportStates sync.Map

func (a *App) supportState() *supportState {
	v, _ := supportStates.LoadOrStore(a, &supportState{})
	return v.(*supportState)
}
func (s *supportState) discard() {
	s.bundle = nil
	s.kernel = nil
	s.token = ""
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}
func (a *App) beginSupport() (context.Context, func(), error) {
	s := a.supportState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil, nil, errors.New("A support operation is already running")
	}
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, 5*time.Minute)
	s.cancel = cancel
	return ctx, func() { cancel(); s.mu.Lock(); s.cancel = nil; s.mu.Unlock() }, nil
}

// All three bindings are guarded before token consumption, dialogs or I/O.
func (a *App) PreviewSupportBundle(raw string) (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}
	if len(raw) > 4096 {
		return "", errors.New("Support selection is too large")
	}
	var request struct {
		WorkspaceID string                     `json:"workspaceId"`
		Selection   workspace.SupportSelection `json:"selection"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return "", errors.New("Invalid support selection")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", errors.New("Invalid support selection")
	}
	ctx, finish, err := a.beginSupport()
	if err != nil {
		return "", err
	}
	defer finish()
	s := a.supportState()
	s.mu.Lock()
	s.discard()
	s.mu.Unlock()
	a.lifecycle.RLock()
	defer a.lifecycle.RUnlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil || a.kernel.ID() != request.WorkspaceID {
		return "", errors.New("Workspace changed; choose support content again")
	}
	env := workspace.SupportRuntime{}
	if info, ok := debug.ReadBuildInfo(); ok {
		env.AppVersion = info.Main.Version
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				env.SourceRevision = setting.Value
			case "vcs.modified":
				if setting.Value == "true" || setting.Value == "false" {
					value := setting.Value == "true"
					env.SourceModified = &value
				}
			}
		}
	}
	if a.native != nil {
		a.native.mu.Lock()
		env.NativeConfigured = a.native.supervisor != nil
		if a.native.supervisor != nil {
			env.VisionConfigured = a.native.supervisor.VisionAvailable()
		}
		manager := a.native.manager
		a.native.mu.Unlock()
		if manager != nil {
			if n, er := manager.SupportInstalledManifestCount(ctx); er == nil {
				env.InstalledManifestCount = &n
			}
		}
	}
	bundle, err := a.kernel.PrepareSupportBundle(ctx, a.execution, request.Selection, env)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	token := workspace.NewID()
	s.bundle = bundle
	s.kernel = a.kernel
	s.token = token
	s.timer = time.AfterFunc(5*time.Minute, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.token == token {
			s.discard()
		}
	})
	result := struct {
		PreviewID   string `json:"previewId"`
		WorkspaceID string `json:"workspaceId"`
		workspace.SupportPreview
	}{token, a.kernel.ID(), bundle.Preview()}
	b, err := json.Marshal(result)
	return string(b), err
}

var supportSaveDialog = func(ctx context.Context) (string, error) {
	return runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{Title: "Save reviewed local support bundle", DefaultFilename: "Trove-support.zip", Filters: []runtime.FileFilter{{DisplayName: "ZIP archive", Pattern: "*.zip"}}})
}

func (a *App) ExportSupportBundle(previewID string) (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}
	ctx, finish, err := a.beginSupport()
	if err != nil {
		return "", err
	}
	defer finish()
	s := a.supportState()
	s.mu.Lock()
	bundle, kernel := s.bundle, s.kernel
	valid := previewID != "" && s.token == previewID
	s.discard()
	s.mu.Unlock()
	if !valid || bundle == nil {
		return "", errors.New("Support preview expired or was discarded; review again")
	}
	a.lifecycle.RLock()
	defer a.lifecycle.RUnlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil || a.kernel != kernel {
		return "", errors.New("Workspace changed; review a new support bundle")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	destination, err := supportSaveDialog(a.ctx)
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if destination == "" {
		return "", nil
	}
	if err = a.kernel.ExportSupportBundle(ctx, a.execution, bundle, destination); err != nil {
		return "", err
	}
	result, err := json.Marshal(map[string]string{"message": "Support bundle saved locally. Nothing was uploaded.", "destination": destination})
	return string(result), err
}
func (a *App) CancelSupportBundle() error {
	if err := a.requireRollbackCommitted(); err != nil {
		return err
	}
	s := a.supportState()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discard()
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}
