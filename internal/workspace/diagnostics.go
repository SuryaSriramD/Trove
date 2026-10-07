package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"trove.local/phase0/internal/diagnostics"
)

func diagnosticBatchResponse(raw json.RawMessage) (string, string) {
	if !bytes.HasPrefix(raw, []byte(`{"error":`)) {
		return "completed", "none"
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if len(raw) <= 16384 && json.Unmarshal(raw, &response) == nil {
		return "failed", response.Error.Code
	}
	return "failed", "INTERNAL"
}

// Called while the native lifecycle excludes renderer queries, after the
// authenticated rollback commit. Pending children create no diagnostic files.
func (k *Kernel) startCompatibleDiagnostics() {
	k.diagnostics = diagnostics.New(k.path)
	k.diagnostics.Record("application", "open", "passed", "none", 0, 0)
}

func diagnosticOutcome(err error) (string, string) {
	if err == nil {
		return "passed", "none"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled", "CANCELLED"
	}
	var failure *ServiceFailure
	if errors.As(err, &failure) {
		return "failed", failure.Code
	}
	return "failed", "INTERNAL"
}

// Prefixes only select fixed labels. No request method or payload is retained.
func diagnosticComponent(method string) string {
	switch {
	case strings.HasPrefix(method, "imports."), method == "previewImport", method == "getImportReport":
		return "import"
	case strings.HasPrefix(method, "search"), method == "resolveCitation":
		return "search"
	case strings.HasPrefix(method, "ai."), strings.HasPrefix(method, "image."), strings.HasPrefix(method, "review."):
		return "model"
	case strings.HasPrefix(method, "jobs."), method == "enqueueJob", method == "getJob", method == "cancelJob":
		return "job"
	default:
		return "application"
	}
}
