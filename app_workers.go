package main

import (
	"context"
	"errors"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"time"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workers"
	"trove.local/phase0/internal/workspace"
)

func (a *App) startJobs(k *workspace.Kernel) {
	var supervisor *native.Supervisor
	if a.native != nil {
		supervisor = a.native.supervisor
	}
	workers.ConfigureSearch(k, supervisor)
	bundle := nativeBundlePath()
	if native.OCRAvailable(bundle) {
		k.SetOCRProvider(func(ctx context.Context, f *os.File) (workspace.OCRResult, error) {
			result, err := native.RunLocalOCR(ctx, f, bundle)
			out := workspace.OCRResult{Warning: result.Warning}
			for _, line := range result.Lines {
				out.Lines = append(out.Lines, workspace.OCRLine{Text: line.Text, Confidence: line.Confidence})
			}
			return out, err
		})
	}
	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	a.mu.Lock()
	a.jobCancel = cancel
	a.jobDone = done
	a.mu.Unlock()
	go func() {
		defer close(done)
		indexDone := make(chan struct{})
		go func() { defer close(indexDone); k.WatchExternalRoots(ctx) }()
		reviewDone := make(chan struct{})
		go func() { defer close(reviewDone); a.runPersonalReviewGenerations(ctx, k) }()
		maintenanceDone := make(chan struct{})
		go func() {
			defer close(maintenanceDone)
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			automationTicker := time.NewTicker(30 * time.Second)
			defer automationTicker.Stop()
			e := k.OwnerContext()
			defer k.RevokeGrant(string(e.ScopeGrantId))
			if err := k.RunPersonalReviewTick(ctx, e, time.Now()); err != nil && ctx.Err() == nil {
				runtime.EventsEmit(a.ctx, "jobs:error", map[string]any{"message": err.Error()})
			}
			if err := k.RunAutomationTick(ctx, e, time.Now()); err != nil && ctx.Err() == nil {
				runtime.EventsEmit(a.ctx, "jobs:error", map[string]any{"message": err.Error()})
			}
			for {
				select {
				case <-ctx.Done():
					return
				case at := <-automationTicker.C:
					if err := k.RunPersonalReviewTick(ctx, e, at); err != nil && ctx.Err() == nil {
						runtime.EventsEmit(a.ctx, "jobs:error", map[string]any{"message": err.Error()})
					}
					if err := k.RunAutomationTick(ctx, e, at); err != nil && ctx.Err() == nil {
						runtime.EventsEmit(a.ctx, "jobs:error", map[string]any{"message": err.Error()})
					}
				case <-ticker.C:
					if err := k.QueueSemanticUpdates(ctx, e); err != nil && ctx.Err() == nil {
						runtime.EventsEmit(a.ctx, "jobs:error", map[string]any{"message": err.Error()})
					}
				}
			}
		}()
		defer func() { cancel(); <-maintenanceDone; <-reviewDone; <-indexDone }()
		err := workers.Run(ctx, k, k.OwnerContext(), supervisor)
		if err != nil && !errors.Is(err, context.Canceled) {
			runtime.EventsEmit(a.ctx, "jobs:error", map[string]any{"message": err.Error()})
		}
	}()
}
