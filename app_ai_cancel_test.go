package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"trove.local/phase0/internal/workspace"
)

func TestAIPreparationCancelRemainsReachableThroughDispatch(t *testing.T) {
	k, err := workspace.Create(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.kernel = k
	a.execution = k.OwnerContext()
	a.completeStartup(nil)
	s := a.aiState()
	defer aiAdapters.Delete(a)
	flow, f, err := s.begin(context.Background(), "preparing-request")
	if err != nil {
		t.Fatal(err)
	}
	defer s.finish(f)
	if _, _, err := s.begin(context.Background(), "second"); err == nil {
		t.Fatal("concurrent answer admitted")
	}
	// ai.start holds the shared lifecycle guard while it prepares context. Cancel
	// must remain reachable without waiting for that slow preparation to finish.
	a.lifecycle.RLock()
	defer a.lifecycle.RUnlock()
	raw, _ := json.Marshal(map[string]any{"contractVersion": 1, "workspaceId": k.ID(), "runId": "preparing-request"})
	response := make(chan string, 1)
	go func() { response <- a.Dispatch("ai.cancel", string(raw)) }()
	select {
	case result := <-response:
		var wire struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal([]byte(result), &wire) != nil || !wire.OK {
			t.Fatal(result)
		}
	case <-time.After(time.Second):
		t.Fatal("preparation blocked cancellation")
	}
	select {
	case <-flow.Done():
	case <-time.After(time.Second):
		t.Fatal("preparation context not cancelled")
	}
}

func TestAIFlightCancellationAliasAndShutdownCleanup(t *testing.T) {
	a := NewApp()
	s := a.aiState()
	defer aiAdapters.Delete(a)
	flow, f, err := s.begin(context.Background(), "request")
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.flights["run"] = f
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { a.stopAI(); close(done) }()
	select {
	case <-flow.Done():
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel preparation/generation")
	}
	select {
	case <-done:
		t.Fatal("stop returned before flight finished")
	default:
	}
	s.finish(f)
	s.finish(f)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("alias duplicated or blocked shutdown")
	}
	s.mu.Lock()
	left := len(s.flights)
	s.mu.Unlock()
	if left != 0 {
		t.Fatal("flight aliases retained", left)
	}
	_, next, err := s.begin(context.Background(), "next")
	if err != nil {
		t.Fatal(err)
	}
	s.finish(next)
}

func TestAIEarlyCancellationIsBoundedAndSurvivesAdmissionRace(t *testing.T) {
	s := &aiAdapter{flights: map[string]*aiFlight{}}
	if err := s.cancelRequest("pending"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.begin(context.Background(), "pending"); !errors.Is(err, context.Canceled) {
		t.Fatal("early cancellation was lost", err)
	}
	s.cancelled["expired"] = time.Now().Add(-time.Second)
	_, flight, err := s.begin(context.Background(), "expired")
	if err != nil {
		t.Fatal(err)
	}
	s.finish(flight)
	for n := 0; n < 128; n++ {
		if err = s.cancelRequest(fmt.Sprint(n)); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.cancelRequest("overflow"); err == nil {
		t.Fatal("cancellation intents were unbounded")
	}
}
