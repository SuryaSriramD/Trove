package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) PickImportFolder() (string, error) {
	selected, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Import a folder as canonical copies"})
	if err != nil || selected == "" {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	return a.kernel.RegisterImportFolder(a.ctx, a.execution, selected)
}
func (a *App) PickIndexRoot(repository bool) (string, error) {
	selected, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Approve a folder for reference-only indexing"})
	if err != nil || selected == "" {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	result, err := a.kernel.ApproveIndexRoot(a.ctx, a.execution, selected, repository)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(result)
	return string(raw), err
}

// The OS interactive picker is invoked only by this explicit desktop action.
// Screenshots become managed files only after the normal import review.
func (a *App) CaptureScreenshot() (string, error) {
	if goruntime.GOOS != "darwin" {
		return "", fmt.Errorf("Interactive screenshot capture is supported on macOS; import an OS screenshot on this platform")
	}
	f, err := os.CreateTemp("", "trove-screenshot-*.png")
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	defer os.Remove(name)
	cmd := exec.CommandContext(a.ctx, "/usr/sbin/screencapture", "-i", "-x", name)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin"}
	if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("Screenshot cancelled or denied by macOS: %w", err)
	}
	if _, err = os.Stat(name); os.IsNotExist(err) {
		return "", nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	return a.kernel.RegisterSelection(name)
}
