package main

import (
	"strings"
	"testing"
)

func TestRollbackPendingCoreCannotDispatchOrOpenNativeInputs(t *testing.T) {
	a := NewApp()
	a.rollback = &rollbackChildState{}
	a.rollback.pending.Store(true)
	for _, method := range []string{"applyBatch", "search.rebuild", "models.download", "providers.remote.start", "ai.start", "enqueueJob"} {
		if result := a.Dispatch(method, `{}`); !strings.Contains(result, "WORKSPACE_LOCKED") || !strings.Contains(result, "waiting for verified rollback") {
			t.Fatal("pending dispatch escaped gate", method, result)
		}
	}
	for name, action := range map[string]func() error{
		"open": a.OpenWorkspace, "create": a.CreateWorkspace,
		"direct open":      func() error { return a.open("must-not-open", true) },
		"picker":           func() error { _, e := a.PickFile(); return e },
		"upload":           func() error { _, e := a.StageUpload("file", "text/plain", "Ym9keQ=="); return e },
		"native selection": func() error { _, e := a.selectFile("must-not-read"); return e },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); err == nil || !strings.Contains(err.Error(), "waiting for verified rollback") {
				t.Fatal("native operation escaped pending gate", err)
			}
		})
	}
	if len(a.uploadGate) != 0 || a.kernel != nil || a.jobCancel != nil {
		t.Fatal("pending action changed native state")
	}
}
