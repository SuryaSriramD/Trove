package main

import (
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) ExportPortable() (string, error) {
	destination, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Export readable Markdown, CSV, and attachments", DefaultFilename: "Trove-portable.zip", Filters: []runtime.FileFilter{{DisplayName: "ZIP archive", Pattern: "*.zip"}}})
	if err != nil || destination == "" {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	result, err := a.kernel.ExportPortable(a.ctx, a.execution, destination)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(result)
	return string(raw), err
}

func (a *App) ExportHTML() (string, error) {
	destination, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Export offline HTML with canonical source", DefaultFilename: "Trove-HTML.zip", Filters: []runtime.FileFilter{{DisplayName: "ZIP archive", Pattern: "*.zip"}}})
	if err != nil || destination == "" {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	result, err := a.kernel.ExportHTML(a.ctx, a.execution, destination)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(result)
	return string(raw), err
}

func (a *App) ExportPDF() (string, error) {
	destination, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Export readable text PDF", DefaultFilename: "Trove-readable.pdf", Filters: []runtime.FileFilter{{DisplayName: "PDF document", Pattern: "*.pdf"}}})
	if err != nil || destination == "" {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	result, err := a.kernel.ExportPDF(a.ctx, a.execution, destination)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(result)
	return string(raw), err
}
