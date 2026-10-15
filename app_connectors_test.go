package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
	"trove.local/phase0/internal/connectors"
)

func TestConnectorSourceFlightCancellationAndAdmission(t *testing.T) {
	h := &connectorHost{flights: map[string]*connectorFlight{}}
	started := make(chan struct{})
	done := make(chan string, 1)
	go func() {
		done <- h.run(context.Background(), "preview", "account", func(ctx context.Context) string { close(started); <-ctx.Done(); return "cancelled" })
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("source flight did not start")
	}
	result := h.run(context.Background(), "preview", "account", func(context.Context) string { t.Fatal("duplicate admitted"); return "" })
	if result == "" {
		t.Fatal("duplicate did not return error")
	}
	h.mu.Lock()
	f := h.flights["preview"]
	f.cancel()
	h.mu.Unlock()
	select {
	case result = <-done:
		if result != "cancelled" {
			t.Fatal(result)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked")
	}
	select {
	case <-f.done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waiter not released")
	}
	h.mu.Lock()
	left := len(h.flights)
	h.mu.Unlock()
	if left != 0 {
		t.Fatal("flight leaked")
	}
	if got := h.run(context.Background(), "preview", "account", func(context.Context) string { return "completed" }); got != "completed" {
		t.Fatal(got)
	}
}

func TestConnectorAcquisitionRequiresAuthenticatedPreviewMembership(t *testing.T) {
	source := connectors.Snapshot{RemoteID: "thread", Content: json.RawMessage(`{"attachmentKeys":["gmail-attachment:chosen"]}`)}
	if !connectorAcquisitionAllowed(connectors.Gmail, source, "gmail-attachment:chosen") || connectorAcquisitionAllowed(connectors.Gmail, source, "gmail-attachment:other") || connectorAcquisitionAllowed(connectors.Gmail, source, "thread") {
		t.Fatal("Gmail acquisition not bound to reviewed thread attachments")
	}
	if !connectorAcquisitionAllowed(connectors.MCP, source, "thread") || connectorAcquisitionAllowed(connectors.GoogleDrive, source, "another-file") || connectorAcquisitionAllowed(connectors.MCP, source, "") {
		t.Fatal("resource acquisition not bound to preview")
	}
}
func TestConnectorSourceFlightSharesBoundWithBackgroundRefresh(t *testing.T) {
	h := &connectorHost{flights: map[string]*connectorFlight{}}
	var wg sync.WaitGroup
	ready := make(chan struct{}, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, id := range []string{"one", "two", "three", "four"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			h.run(ctx, id, "account", func(ctx context.Context) string { ready <- struct{}{}; <-ctx.Done(); return "" })
		}(id)
	}
	for i := 0; i < 4; i++ {
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatal("flight did not begin")
		}
	}
	if err := h.start(ctx, "refresh", "account", func(context.Context) { t.Error("fifth admitted") }); err == nil {
		t.Fatal("source jobs bypassed refresh bound")
	}
	cancel()
	wg.Wait()
}
