package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"trove.local/phase0/internal/workspace"
)

func TestProviderPreparationLifecycleCancelsBeforeExclusiveLock(t *testing.T) {
	for _, operation := range []string{"open", "shutdown"} {
		t.Run(operation, func(t *testing.T) {
			k, err := workspace.Create(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer k.Close()
			a := NewApp()
			a.ctx, a.kernel, a.execution = context.Background(), k, k.OwnerContext()
			a.completeStartup(nil)
			h := &providerHost{bridge: workspace.NewProviderBridge(k, a.execution)}
			providerHosts.Store(a, h)
			defer a.stopProviders()
			flow, flight, err := h.beginPreparation(a.ctx, string(a.execution.ScopeGrantId), workspace.NewID())
			if err != nil {
				t.Fatal(err)
			}
			// A source read admitted by Dispatch retains the lifecycle read lock until
			// its cancellable native preparation returns.
			a.lifecycle.RLock()
			readerDone := make(chan struct{})
			go func() {
				<-flow.Done()
				h.finishPreparation(flight)
				a.lifecycle.RUnlock()
				close(readerDone)
			}()
			changed := make(chan error, 1)
			go func() {
				if operation == "open" {
					changed <- a.open(filepath.Join(t.TempDir(), "missing"), false)
				} else {
					a.shutdown(context.Background())
					changed <- nil
				}
			}()
			select {
			case <-readerDone:
			case <-time.After(time.Second):
				flight.cancel()
				<-readerDone
				<-changed
				t.Fatal("lifecycle waits for preparation instead of cancelling before the exclusive lock")
			}
			select {
			case err := <-changed:
				if operation == "open" && err == nil {
					t.Fatal("missing workspace unexpectedly opened")
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled preparation did not release lifecycle transition")
			}
		})
	}
}

func TestProviderPreparationSuspensionCoversNewHostsAndOverlappingTransitions(t *testing.T) {
	a := NewApp()
	first := a.suspendProviderPreparations()
	second := a.suspendProviderPreparations()
	h := &providerHost{}
	providerHosts.Store(a, h)
	defer providerHosts.Delete(a)
	first()
	first() // Each transition's cleanup is idempotent, not a global reopen.
	if _, _, err := a.beginProviderPreparation(h, context.Background(), "owner", "during-change"); !errors.Is(err, context.Canceled) {
		t.Fatal("new host admitted preparation while another transition is pending", err)
	}
	second()
	flow, flight, err := a.beginProviderPreparation(h, context.Background(), "owner", "after-change")
	if err != nil || flow.Err() != nil {
		t.Fatal("preparations did not resume", err)
	}
	h.finishPreparation(flight)
}
