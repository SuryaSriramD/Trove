package workspace

import (
	"context"
	"sync"
	"time"

	"trove.local/phase0/contracts"
)

type ocrFlight struct {
	key    string
	cancel context.CancelFunc
}

func ocrRequestKey(e contracts.ServiceExecutionContext, id string) string {
	return string(e.Actor.ActorId) + ":" + string(e.ScopeGrantId) + ":" + id
}
func (r *ocrRuntime) prune() {
	for key, expiry := range r.cancelled {
		if !time.Now().Before(expiry) {
			delete(r.cancelled, key)
		}
	}
}
func (r *ocrRuntime) begin(ctx context.Context, e contracts.ServiceExecutionContext, id string) (context.Context, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.suspended > 0 {
		return nil, nil, Fail("CONFLICT", "OCR engine changed; retry with the current helper")
	}
	r.prune()
	key := ocrRequestKey(e, id)
	if _, ok := r.cancelled[key]; ok {
		return nil, nil, context.Canceled
	}
	if r.flight != nil {
		return nil, nil, Fail("RESOURCE_EXHAUSTED", "One local OCR operation is already running")
	}
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	flight := &ocrFlight{key: key, cancel: cancel}
	r.flight = flight
	var once sync.Once
	return bounded, func() {
		once.Do(func() {
			cancel()
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.flight == flight {
				r.flight = nil
			}
		})
	}, nil
}
func (r *ocrRuntime) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.flight != nil {
		r.flight.cancel()
	}
	r.cancelled = nil
}

// SuspendOCR is a native lifecycle operation, not a remote capability. It
// blocks new admission and interrupts the active read before the adapter waits
// for its exclusive lifecycle lock. Failed workspace selection can resume it.
func (k *Kernel) SuspendOCR() func() {
	value, ok := ocrProviders.Load(k)
	if !ok {
		return func() {}
	}
	r := value.(*ocrRuntime)
	r.mu.Lock()
	r.suspended++
	if r.flight != nil {
		r.flight.cancel()
	}
	r.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); defer r.mu.Unlock(); r.suspended-- }) }
}
func cancelOCR(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, req map[string]any) (any, error) {
	if e.Origin != "user" || e.Actor.Kind != "user" {
		return nil, Fail("SCOPE_DENIED", "OCR cancellation requires a direct user request")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, ok := ocrProviders.Load(k)
	if !ok {
		return nil, Fail("WORKER_FAILED", "Local OCR helper is unavailable")
	}
	r := value.(*ocrRuntime)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, Fail("CONFLICT", "OCR engine changed")
	}
	key := ocrRequestKey(e, str(req["runRequestId"]))
	if r.flight != nil && r.flight.key == key {
		r.flight.cancel()
		return map[string]any{"runRequestId": req["runRequestId"], "state": "cancellation_requested"}, nil
	}
	r.prune()
	if _, exists := r.cancelled[key]; !exists && len(r.cancelled) >= 128 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Too many pending OCR cancellations; retry after earlier requests settle")
	}
	r.cancelled[key] = time.Now().Add(time.Minute)
	return map[string]any{"runRequestId": req["runRequestId"], "state": "cancellation_requested"}, nil
}
