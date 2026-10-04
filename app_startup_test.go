package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/internal/integration"
	"trove.local/phase0/internal/workspace"
)

func TestDesktopStartupWaitsForWorkspaceBeforeRendererAndIntegration(t *testing.T) {
	a := NewApp()
	root := t.TempDir()
	entered, release, initialized := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(initialized)
		a.initializeStartup(context.Background(), func() error {
			close(entered)
			<-release
			k, err := workspace.Create(filepath.Join(root, "workspace"))
			if err != nil {
				return err
			}
			a.mu.Lock()
			a.kernel, a.execution = k, k.OwnerContext()
			a.mu.Unlock()
			return nil
		})
	}()
	<-entered
	renderer := make(chan string, 1)
	fixture := make(chan integration.Result, 1)
	go func() { renderer <- a.Dispatch("workspace.snapshot", "{}") }()
	go func() {
		fixture <- a.runIntegration(context.Background(), "p0-create", filepath.Join(root, "state.json"))
	}()
	select {
	case response := <-renderer:
		close(release)
		t.Fatalf("renderer returned before workspace readiness: %s", response)
	case response := <-fixture:
		close(release)
		t.Fatalf("fixture returned before workspace readiness: %+v", response)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-initialized
	a.mu.RLock()
	k := a.kernel
	a.mu.RUnlock()
	if k != nil {
		defer k.Close()
	}
	select {
	case raw := <-renderer:
		var response struct {
			OK     bool `json:"ok"`
			Result struct {
				WorkspaceID string `json:"workspaceId"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(raw), &response); err != nil || !response.OK || k == nil || response.Result.WorkspaceID != k.ID() {
			t.Fatalf("renderer bootstrap did not resume with the trusted workspace: %s %v", raw, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("renderer hung after startup finished")
	}
	select {
	case result := <-fixture:
		if !result.Passed || result.Step != "p0-create" || result.RecordedAt == "" || result.State.WorkspaceID != k.ID() {
			t.Fatalf("P0 fixture did not execute after delayed startup: %+v", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("integration hung after startup finished")
	}
}

func TestDesktopStartupFailureReachesRendererAndIntegration(t *testing.T) {
	a := NewApp()
	entered, release := make(chan struct{}), make(chan struct{})
	go a.initializeStartup(context.Background(), func() error {
		close(entered)
		<-release
		return errors.New("fixture workspace volume is unavailable")
	})
	<-entered
	renderer := make(chan string, 1)
	fixture := make(chan integration.Result, 1)
	go func() { renderer <- a.Dispatch("workspace.snapshot", "{}") }()
	go func() {
		fixture <- a.runIntegration(context.Background(), "p0-create", filepath.Join(t.TempDir(), "state.json"))
	}()
	close(release)
	select {
	case raw := <-renderer:
		var response struct {
			OK    bool                           `json:"ok"`
			Error struct{ Code, Message string } `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &response); err != nil || response.OK || response.Error.Code != "WORKSPACE_LOCKED" || !strings.Contains(response.Error.Message, "volume is unavailable") {
			t.Fatalf("startup failure lost its diagnostic: %s %v", raw, err)
		}
	case <-time.After(time.Second):
		t.Fatal("renderer hung after startup failure")
	}
	select {
	case result := <-fixture:
		if result.Passed || result.Step != "p0-create" || result.RecordedAt == "" || !strings.Contains(result.Error, "volume is unavailable") {
			t.Fatalf("startup failure produced an empty or successful integration result: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("integration hung after startup failure")
	}
	// The terminal startup barrier records completion, not permanent failure:
	// selecting a valid workspace must recover the ordinary renderer bootstrap.
	if err := a.open(filepath.Join(t.TempDir(), "recovered-workspace"), true); err != nil {
		t.Fatal(err)
	}
	defer a.shutdown(context.Background())
	var recovered struct {
		OK     bool `json:"ok"`
		Result struct {
			WorkspaceID string `json:"workspaceId"`
		} `json:"result"`
	}
	raw := a.Dispatch("workspace.snapshot", "{}")
	if err := json.Unmarshal([]byte(raw), &recovered); err != nil || !recovered.OK || recovered.Result.WorkspaceID == "" {
		t.Fatalf("opening another workspace did not recover bootstrap: %s %v", raw, err)
	}
}

func TestDesktopStartupWaitIsCancellableAndShutdownUnblocksRenderer(t *testing.T) {
	a := NewApp()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := a.runIntegration(ctx, "p0-create", filepath.Join(t.TempDir(), "unused.json"))
	if result.Passed || result.RecordedAt == "" || !strings.Contains(result.Error, "context deadline exceeded") {
		t.Fatalf("startup cancellation was not recorded: %+v", result)
	}
	renderer := make(chan string, 1)
	go func() { renderer <- a.Dispatch("workspace.snapshot", "{}") }()
	a.shutdown(context.Background())
	select {
	case raw := <-renderer:
		if !strings.Contains(raw, "shutting down") {
			t.Fatalf("shutdown did not cancel bootstrap: %s", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("renderer hung when shutdown preceded startup")
	}
	called := false
	a.initializeStartup(context.Background(), func() error { called = true; return nil })
	if called {
		t.Fatal("startup initialized resources after shutdown")
	}
}

func TestDesktopStartupCannotReportReadyWithoutAWorkspace(t *testing.T) {
	a := NewApp()
	a.initializeStartup(context.Background(), func() error { return nil })
	result := a.runIntegration(context.Background(), "p0-create", filepath.Join(t.TempDir(), "unused.json"))
	if result.Passed || result.RecordedAt == "" || !strings.Contains(result.Error, "without an open workspace") {
		t.Fatalf("empty initialization produced an empty or successful report: %+v", result)
	}
}
