package main

import (
	"context"
	"sync"
	"time"
	"trove.local/phase0/internal/workspace"
)

type providerPreparation struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// The owner key comes only from the authenticated native execution, never a
// renderer field. Early cancellation cannot target a later owner's request.
func providerPreparationKey(owner, id string) string { return owner + ":" + id }
func (h *providerHost) beginPreparation(ctx context.Context, owner, id string) (context.Context, *providerPreparation, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prunePreparationCancellations()
	key := providerPreparationKey(owner, id)
	if _, ok := h.preparationCancelled[key]; ok {
		delete(h.preparationCancelled, key)
		return nil, nil, context.Canceled
	}
	if len(h.preparations) >= 4 {
		return nil, nil, workspace.Fail("RESOURCE_EXHAUSTED", "Too many provider preparations; cancel one or wait")
	}
	if h.preparations[key] != nil {
		return nil, nil, workspace.Fail("CONFLICT", "This provider request is already preparing")
	}
	if h.preparations == nil {
		h.preparations = map[string]*providerPreparation{}
	}
	flow, cancel := context.WithCancel(ctx)
	f := &providerPreparation{cancel: cancel, done: make(chan struct{})}
	h.preparations[key] = f
	return flow, f, nil
}
func (h *providerHost) prunePreparationCancellations() {
	for key, expires := range h.preparationCancelled {
		if !time.Now().Before(expires) {
			delete(h.preparationCancelled, key)
		}
	}
}
func (h *providerHost) cancelPreparation(owner, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := providerPreparationKey(owner, id)
	if f := h.preparations[key]; f != nil {
		f.cancel()
		return nil
	}
	h.prunePreparationCancellations()
	if len(h.preparationCancelled) >= 128 {
		return workspace.Fail("RESOURCE_EXHAUSTED", "Too many pending provider cancellations; wait for requests to settle")
	}
	if h.preparationCancelled == nil {
		h.preparationCancelled = map[string]time.Time{}
	}
	h.preparationCancelled[key] = time.Now().Add(time.Minute)
	return nil
}
func (h *providerHost) finishPreparation(f *providerPreparation) {
	if f == nil {
		return
	}
	f.once.Do(func() {
		f.cancel()
		h.mu.Lock()
		for key, current := range h.preparations {
			if current == f {
				delete(h.preparations, key)
			}
		}
		h.mu.Unlock()
		close(f.done)
	})
}
func (h *providerHost) stopPreparations() {
	h.mu.Lock()
	h.preparationCancelled = nil
	waiting := []chan struct{}{}
	for _, f := range h.preparations {
		f.cancel()
		waiting = append(waiting, f.done)
	}
	h.mu.Unlock()
	for _, done := range waiting {
		<-done
	}
}
