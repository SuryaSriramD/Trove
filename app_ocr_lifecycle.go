package main

import (
	"context"
	"sync"
)

type nativeOCRRead struct{ cancel context.CancelFunc }

// The app owns admission, so overlapping workspace transitions also suspend
// OCR on replacement kernels and helpers installed before the last transition
// finishes. No lifecycle lock is held while cancelling existing reads.
func (a *App) cancelOCRForLifecycle() func() {
	a.ocrAdmissionMu.Lock()
	a.ocrSuspensions++
	for flight := range a.ocrReads {
		flight.cancel()
	}
	a.ocrAdmissionMu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { a.ocrAdmissionMu.Lock(); a.ocrSuspensions--; a.ocrAdmissionMu.Unlock() }) }
}
func (a *App) beginOCRRead(ctx context.Context) (context.Context, func(), error) {
	a.ocrAdmissionMu.Lock()
	defer a.ocrAdmissionMu.Unlock()
	if a.ocrSuspensions > 0 {
		return nil, nil, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	bounded, cancel := context.WithCancel(ctx)
	flight := &nativeOCRRead{cancel: cancel}
	if a.ocrReads == nil {
		a.ocrReads = map[*nativeOCRRead]struct{}{}
	}
	a.ocrReads[flight] = struct{}{}
	var once sync.Once
	return bounded, func() {
		once.Do(func() { cancel(); a.ocrAdmissionMu.Lock(); delete(a.ocrReads, flight); a.ocrAdmissionMu.Unlock() })
	}, nil
}
