package main

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

func (a *App) nativeQualification(ctx context.Context, report string) {
	result := map[string]any{"step": "native", "recordedAt": time.Now().UTC().Format(time.RFC3339Nano), "passed": false}
	if a.native == nil || a.native.supervisor == nil {
		result["error"] = "bundled native supervisor unavailable"
	} else {
		checks, err := a.native.supervisor.QualificationSmoke(ctx)
		result["checks"] = checks
		if err != nil {
			result["error"] = err.Error()
		} else {
			result["passed"] = true
		}
	}
	raw, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(report, raw, 0600)
}
