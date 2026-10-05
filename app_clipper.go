package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"trove.local/phase0/internal/clipper"
)

type appClipper struct {
	bridge    *clipper.Bridge
	extension string
	allowed   []string
}

var clippers sync.Map

// Bound native UI method. Starting an HTTP listener always requires this
// explicit owner action; no listener is created at app startup or by a web page.
func (a *App) BeginClipperPairing(extensionID string, destinationIDs []string) (string, error) {
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()
	a.stopClipper()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	allowed := append([]string(nil), destinationIDs...)
	if _, err := a.kernel.ClipperDestinations(a.ctx, a.execution, allowed); err != nil {
		return "", err
	}
	workspaceID := a.kernel.ID()
	current := func() error {
		if a.kernel == nil || a.kernel.ID() != workspaceID {
			return fmt.Errorf("paired workspace is no longer open")
		}
		return nil
	}
	bridge, pairing, err := clipper.Start(a.ctx, clipper.Config{ExtensionID: extensionID, Destinations: func(ctx context.Context) ([]clipper.Destination, error) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		if err := current(); err != nil {
			return nil, err
		}
		return a.kernel.ClipperDestinations(ctx, a.execution, allowed)
	}, Preview: func(ctx context.Context, capture clipper.Capture) (clipper.Receipt, error) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		if err := current(); err != nil {
			return clipper.Receipt{}, err
		}
		plan, err := a.kernel.PreviewClipperCapture(ctx, a.execution, extensionID, capture)
		return clipper.Receipt{PlanID: plan.ID, State: "awaiting_owner_review", Title: plan.SourceName}, err
	}})
	if err != nil {
		return "", err
	}
	clippers.Store(a, &appClipper{bridge: bridge, extension: extensionID, allowed: allowed})
	raw, err := json.Marshal(pairing)
	return string(raw), err
}
func (a *App) stopClipper() {
	if state, ok := clippers.LoadAndDelete(a); ok {
		state.(*appClipper).bridge.Close()
	}
}
func (a *App) RevokeClipperPairing() { a.lifecycle.Lock(); defer a.lifecycle.Unlock(); a.stopClipper() }
func (a *App) ClipperStatus() (string, error) {
	state := map[string]any{"listening": false, "paired": false, "sessionOnly": true}
	if value, ok := clippers.Load(a); ok {
		session := value.(*appClipper)
		state["listening"] = true
		state["paired"] = session.bridge.Paired()
		state["extensionId"] = session.extension
	}
	raw, err := json.Marshal(state)
	return string(raw), err
}
func (a *App) ClipperReviewPlans() (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	plans, err := a.kernel.ClipperReviewPlans(a.ctx, a.execution)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(plans)
	return string(raw), err
}

func (a *App) RefreshClipperDestination(planID, planHash string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	plan, err := a.kernel.RefreshClipperDestination(a.ctx, a.execution, planID, planHash)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(plan)
	return string(raw), err
}
func (a *App) ShowClipperExtension() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	folder := filepath.Join(filepath.Dir(executable), "../Resources/browser-clipper")
	if _, err = os.Stat(filepath.Join(folder, "manifest.json")); err != nil {
		if strings.Contains(executable, ".app/Contents/MacOS/") {
			return fmt.Errorf("bundled browser clipper is missing")
		}
		cwd, er := os.Getwd()
		if er != nil {
			return er
		}
		folder = filepath.Join(cwd, "browser-clipper")
		if _, er = os.Stat(filepath.Join(folder, "manifest.json")); er != nil {
			return fmt.Errorf("bundled browser clipper is missing")
		}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	command, args, err := desktopFolderCommand(runtime.GOOS, folder)
	if err != nil {
		return err
	}
	return exec.CommandContext(ctx, command, args...).Run()
}
