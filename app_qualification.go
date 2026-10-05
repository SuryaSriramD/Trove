package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Measured from process initialization until the renderer has loaded canonical
// workspace data and yielded two animation frames. Enabled only by a test path.
var processStarted = time.Now()
var shellReported sync.Once

func (a *App) ShellReady() {
	a.confirmRollbackShellReady()
	path := os.Getenv("TROVE_SHELL_REPORT")
	if path == "" {
		return
	}
	shellReported.Do(func() {
		raw, _ := json.MarshalIndent(map[string]any{
			"usableShellMs": float64(time.Since(processStarted).Microseconds()) / 1000,
			"definition":    "Go process initialization to canonical snapshot rendered plus two animation frames",
			"recordedAt":    time.Now().UTC().Format(time.RFC3339Nano),
		}, "", "  ")
		_ = os.WriteFile(path, raw, 0600)
		if os.Getenv("TROVE_SHELL_QUIT") == "1" && a.ctx != nil {
			runtime.Quit(a.ctx)
		}
	})
}
