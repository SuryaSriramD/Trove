package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A new bound desktop method must be consciously classified. Reading a snapshot
// does not grant permission to open a native dialog, write a file, or start work.
func TestRollbackAuxiliaryBindingsHaveAnEntryGuard(t *testing.T) {
	readonly := map[string]bool{"ClipperStatus": true, "ClipperReviewPlans": true, "MeetingRecordings": true, "PendingDeepLink": true}
	delegated := map[string]bool{"PreviewApplicationUpdate": true, "PrepareApplicationUpdate": true, "PreviewApplicationRollback": true, "RestoreApplicationRollback": true, "RevealApplicationUpdate": true, "PreviewMigrationRecovery": true, "RestoreMigrationRecovery": true}
	paths, err := filepath.Glob("app_*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") || path == "app_qualification.go" || path == "app_update_launch.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Body == nil {
				continue
			}
			ptr, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			recv, ok := ptr.X.(*ast.Ident)
			if !ok || recv.Name != "App" {
				continue
			}
			if !fn.Name.IsExported() && fn.Name.Name != "beginUpdateOperation" {
				continue
			}
			name := fn.Name.Name
			seen[name] = true
			if readonly[name] {
				continue
			}
			if len(fn.Body.List) == 0 {
				t.Errorf("%s has no rollback gate", name)
				continue
			}
			if delegated[name] {
				assignment, ok := fn.Body.List[0].(*ast.AssignStmt)
				if !ok || len(assignment.Rhs) != 1 || !rollbackGuardCall(assignment.Rhs[0], "beginUpdateOperation") {
					t.Errorf("%s must enter guarded recovery operation before effects", name)
				}
				continue
			}
			guard, ok := fn.Body.List[0].(*ast.IfStmt)
			if !ok || guard.Init == nil {
				t.Errorf("%s must check rollback before any effect", name)
				continue
			}
			assignment, ok := guard.Init.(*ast.AssignStmt)
			if !ok || len(assignment.Rhs) != 1 || !rollbackGuardCall(assignment.Rhs[0], "requireRollbackCommitted") {
				t.Errorf("%s has no entry rollback guard", name)
				continue
			}
			condition, ok := guard.Cond.(*ast.BinaryExpr)
			if !ok || condition.Op != token.NEQ || len(guard.Body.List) != 1 {
				t.Errorf("%s must return immediately when pending", name)
				continue
			}
			if _, ok := guard.Body.List[0].(*ast.ReturnStmt); !ok {
				t.Errorf("%s guard does not return", name)
			}
		}
	}
	for name := range readonly {
		if !seen[name] {
			t.Errorf("stale read-only classification: %s", name)
		}
	}
	for name := range delegated {
		if !seen[name] {
			t.Errorf("stale recovery classification: %s", name)
		}
	}
	if !seen["beginUpdateOperation"] {
		t.Fatal("recovery gate was not checked")
	}
}
func rollbackGuardCall(expr ast.Expr, name string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	receiver, ok := selector.X.(*ast.Ident)
	return ok && receiver.Name == "a"
}

func TestRollbackPendingActionsDoNotConsumeReviewsCancelOrBeginIO(t *testing.T) {
	a := NewApp()
	a.rollback = &rollbackChildState{}
	a.rollback.pending.Store(true)
	// No Wails context, kernel or model manager is installed. The guard must reject
	// before even consulting those dependencies or opening a picker/listener.
	expected := a.requireRollbackCommitted()
	if expected == nil {
		t.Fatal("fixture is not pending")
	}
	check := func(t *testing.T, err error) {
		t.Helper()
		if err == nil || err.Error() != expected.Error() {
			t.Fatalf("action crossed the pending gate: %v", err)
		}
	}
	review := a.saveRestorePreview(nativeRestorePreview{source: "must-not-read", expires: time.Now().Add(time.Minute)})
	update := a.saveUpdateReview(updateReview{kind: "update", source: "must-not-read"})
	defer nativeRestoreStates.Delete(a)
	defer updateStates.Delete(a)
	defer appRecordings.Delete(a)
	actions := map[string]func() error{
		"backup":            func() error { _, e := a.CreateBackup(); return e },
		"encrypted backup":  func() error { _, e := a.CreateEncryptedBackup("unused"); return e },
		"encrypted preview": func() error { _, e := a.PreviewEncryptedBackup("unused"); return e },
		"backup progress":   func() error { _, e := a.NativeBackupProgress(); return e },
		"import picker":     func() error { _, e := a.PickImportFolder(); return e },
		"index picker":      func() error { _, e := a.PickIndexRoot(true); return e },
		"screenshot":        func() error { _, e := a.CaptureScreenshot(); return e },
		"portable export":   func() error { _, e := a.ExportPortable(); return e },
		"HTML export":       func() error { _, e := a.ExportHTML(); return e },
		"PDF export":        func() error { _, e := a.ExportPDF(); return e },
		"restore preview":   func() error { _, e := a.PreviewNativeRestore(); return e },
		"restore":           func() error { _, e := a.RestoreNativeBackup(review); return e },
		"cleanup preview":   func() error { _, e := a.PreviewBackupCleanup(); return e },
		"cleanup":           func() error { _, e := a.ApplyBackupCleanup(review); return e },
		"clipper listener":  func() error { _, e := a.BeginClipperPairing("extension", nil); return e },
		"clipper refresh":   func() error { _, e := a.RefreshClipperDestination("plan", "hash"); return e },
		"reveal clipper":    a.ShowClipperExtension,
		"model picker":      func() error { return a.PickModelPack("pack") },
		"start capture":     func() error { _, e := a.StartMeetingRecording("title"); return e },
		"stop capture":      func() error { _, e := a.StopMeetingRecording("recording"); return e },
		"save capture":      func() error { _, e := a.SaveMeetingRecording("recording"); return e },
		"audio picker":      func() error { _, e := a.PickMeetingAudio("title"); return e },
		"update preview":    func() error { _, e := a.PreviewApplicationUpdate(); return e },
		"prepare update":    func() error { _, e := a.PrepareApplicationUpdate(update); return e },
		"rollback preview":  func() error { _, e := a.PreviewApplicationRollback(); return e },
		"restore rollback":  func() error { _, e := a.RestoreApplicationRollback(update); return e },
		"reveal update":     func() error { return a.RevealApplicationUpdate(update) },
		"migration preview": func() error { _, e := a.PreviewMigrationRecovery(); return e },
		"migration restore": func() error { _, e := a.RestoreMigrationRecovery(update); return e },
	}
	cancelled := false
	state := a.recordingState()
	state.active["recording"] = &activeRecording{cancel: func() { cancelled = true }, done: make(chan struct{})}
	a.updatesState().cancel = func() { cancelled = true }
	for name, action := range actions {
		t.Run(name, func(t *testing.T) { check(t, action()) })
	}
	a.CancelUpdateOperation()
	a.CancelBackupOperation()
	a.CloseNativeRestorePreview(review)
	if cancelled {
		t.Fatal("pending operation cancelled an existing native operation")
	}
	if _, ok := a.restoreState().previews[review]; !ok {
		t.Fatal("pending restore consumed a reviewed token")
	}
	if _, ok := a.updatesState().reviews[update]; !ok {
		t.Fatal("pending update consumed a reviewed token")
	}
	// A nil bridge would panic if the void method crossed its guard.
	clipper := &appClipper{}
	clippers.Store(a, clipper)
	defer clippers.Delete(a)
	a.RevokeClipperPairing()
	if got, ok := clippers.Load(a); !ok || got != clipper {
		t.Fatal("pending revoke changed listener ownership")
	}
	a.SetEditing(true)
	a.ctx = context.Background()
	a.rollback.pending.Store(false)
	if err := a.requireRollbackCommitted(); err != nil {
		t.Fatal("committed child remained blocked", err)
	}
}
