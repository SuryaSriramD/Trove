package main

import (
	"context"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"path/filepath"
	"strings"
	"time"
	"trove.local/phase0/internal/integration"
)

func (a *App) domReady(ctx context.Context) {
	markStartupStage("dom_ready")
	step := os.Getenv("TROVE_INTEGRATION_STEP")
	if step == "" {
		return
	}
	report := os.Getenv("TROVE_INTEGRATION_REPORT")
	if report == "" {
		return
	}
	go func() {
		if os.Getenv("TROVE_INTEGRATION_QUIT") == "1" {
			defer runtime.Quit(ctx)
		}
		if err := a.waitForStartup(ctx); err != nil {
			_ = integration.WriteResult(report, integrationStartupFailure(step, err))
			return
		}
		if step == "native" {
			a.nativeQualification(ctx, report)
			return
		}
		state := filepath.Join(filepath.Dir(report), "integration-state.json")
		result := a.runIntegration(ctx, step, state)
		_ = integration.WriteResult(report, result)
		runtime.EventsEmit(ctx, "workspace:changed", map[string]any{"integration": result.Passed})
	}()
}

func integrationStartupFailure(step string, err error) integration.Result {
	return integration.Result{Step: step, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Error: err.Error()}
}

func (a *App) runIntegration(ctx context.Context, step, state string) integration.Result {
	if err := a.waitForStartup(ctx); err != nil {
		return integrationStartupFailure(step, err)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return integrationStartupFailure(step, fmt.Errorf("workspace closed before integration started"))
	}
	if strings.HasPrefix(step, "p1-") {
		return integration.RunP1(ctx, a.kernel, a.execution, step, state+".p1")
	}
	if strings.HasPrefix(step, "p2-") {
		return integration.RunP2(ctx, a.kernel, a.execution, step, state+".p2")
	}
	if strings.HasPrefix(step, "p0-") {
		return integration.RunP0(ctx, a.kernel, a.execution, step, state)
	}
	return integration.Run(ctx, a.kernel, a.execution, step, state)
}
