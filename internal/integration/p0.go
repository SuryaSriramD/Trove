package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

// P0State holds only fixture identities. It is kept outside the workspace and
// reused by a separately launched packaged app to prove persisted behavior.
type P0State struct {
	WorkspaceID  string `json:"workspaceId"`
	ImportID     string `json:"importId"`
	PageID       string `json:"pageId"`
	BlockID      string `json:"blockId"`
	RecordID     string `json:"recordId"`
	RecordPageID string `json:"recordPageId"`
	ForgottenID  string `json:"forgottenId"`
	StaleID      string `json:"staleId"`
	RecordingID  string `json:"recordingId"`
}

// RunP0 drives public workspace services from the packaged desktop process.
// Native inference, microphone devices and OS permissions are qualified by
// separate app fixtures; this scenario never fabricates inference results.
func RunP0(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, step, statePath string) Result {
	result := Result{Step: step, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Checks: []string{}}
	var state P0State
	var err error
	switch step {
	case "p0-create":
		if _, err = os.Stat(statePath); !os.IsNotExist(err) {
			result.Error = "refusing to overwrite existing P0 integration fixture"
			return result
		}
		state, err = createP0(ctx, k, e)
		if err == nil {
			var raw []byte
			raw, err = json.MarshalIndent(state, "", "  ")
			if err == nil {
				err = os.MkdirAll(filepath.Dir(statePath), 0700)
			}
			if err == nil {
				err = os.WriteFile(statePath, raw, 0600)
			}
		}
	case "p0-verify":
		var raw []byte
		raw, err = os.ReadFile(statePath)
		if err == nil {
			err = json.Unmarshal(raw, &state)
		}
	default:
		err = fmt.Errorf("unknown P0 integration step %q", step)
	}
	result.State = State{WorkspaceID: state.WorkspaceID, PageID: state.PageID, BlockID: state.BlockID, RecordID: state.RecordID, RecordPageID: state.RecordPageID}
	if err == nil {
		err = verifyP0(ctx, k, e, state)
	}
	if err == nil {
		result.Checks = append(result.Checks, "Notion ZIP inventory and five-outcome reconciliation retained", "imported page body and database record edits persisted", "keyword search works without inference", "forgotten and stale memory excluded from retrieval", "durable interrupted recording remains recoverable")
	}
	if err == nil && step == "p0-verify" {
		err = backupRestoreP0(ctx, k, e, state, filepath.Dir(statePath))
		if err == nil {
			result.Checks = append(result.Checks, "separate desktop restart preserved the complete data fixture", "verified native backup restored to an unused workspace", "restored canonical snapshot equals the source", "restored interrupted recording finalized with its complete audio tail")
		}
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Passed = true
	return result
}

func createP0(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext) (state P0State, err error) {
	state.WorkspaceID = k.ID()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range [][2]string{{"P0 Notebook 0123456789abcdef0123456789abcdef.md", "# P0ImportedEvidence\nEditable source body.\n[child](P0%20Notebook%200123456789abcdef0123456789abcdef/Child.md)"}, {"P0 Notebook 0123456789abcdef0123456789abcdef/Child.md", "# Child\nUnicode source: తెలుగు 中文 العربية 🌱"}, {"Tasks.csv", "Name,Count,Done\nImported task,42,false\n"}, {"Unsupported.html", "<p>Retained source evidence</p>"}, {"../outside.md", "must remain rejected inside original source container"}} {
		var w interface{ Write([]byte) (int, error) }
		w, err = zw.Create(file[0])
		if err == nil {
			_, err = w.Write([]byte(file[1]))
		}
		if err != nil {
			return
		}
	}
	if err = zw.Close(); err != nil {
		return
	}
	var f *os.File
	f, err = os.CreateTemp("", "trove-p0-notion-*.zip")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	_, err = f.Write(buf.Bytes())
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return
	}
	var handle string
	handle, err = k.RegisterSelection(f.Name())
	if err != nil {
		return
	}
	var plan map[string]any
	plan, err = dispatch(ctx, k, e, "imports.preview", map[string]any{"sourceHandle": handle, "sourceNamespace": "p0-packaged-qualification"})
	if err != nil {
		return
	}
	state.ImportID, _ = plan["planId"].(string)
	for attempts := 0; plan["state"] != "complete" && attempts < 30; attempts++ {
		plan, err = dispatch(ctx, k, e, "imports.commit", map[string]any{"planId": state.ImportID, "planHash": plan["planHash"]})
		if err != nil {
			return
		}
	}
	if plan["state"] != "complete" {
		return state, fmt.Errorf("P0 import did not finish")
	}
	for _, item := range plan["items"].([]any) {
		i := item.(map[string]any)
		if i["title"] == "P0 Notebook" {
			state.PageID, _ = i["pageId"].(string)
		}
		if i["kind"] == "row" {
			state.RecordID, _ = i["recordId"].(string)
			state.RecordPageID, _ = i["pageId"].(string)
		}
	}
	if state.PageID == "" || state.RecordID == "" {
		return state, fmt.Errorf("import did not expose page and database identities")
	}
	state.BlockID = workspace.NewID()
	err = batch(ctx, k, e, []map[string]any{{"kind": "page.update", "pageId": state.PageID, "title": "P0 edited imported page"}, {"kind": "block.insert", "block": map[string]any{"blockId": state.BlockID, "pageId": state.PageID, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "P0PersistedJourney editable content"}}}}}, {"kind": "page.update", "pageId": state.RecordPageID, "title": "P0 edited database record"}}, map[string]string{state.BlockID: "block"})
	if err != nil {
		return
	}
	source := workspace.NewID()
	forgottenPage, stalePage := workspace.NewID(), workspace.NewID()
	state.ForgottenID, state.StaleID = workspace.NewID(), workspace.NewID()
	err = batch(ctx, k, e, []map[string]any{{"kind": "page.create", "pageId": source, "title": "P0 memory source"}, {"kind": "page.create", "pageId": forgottenPage, "title": "P0 forgotten memory"}, {"kind": "page.create", "pageId": stalePage, "title": "P0 stale memory"}}, map[string]string{source: "page", forgottenPage: "page", stalePage: "page"})
	if err != nil {
		return
	}
	for _, item := range [][3]string{{state.ForgottenID, forgottenPage, "P0ForgottenSecret"}, {state.StaleID, stalePage, "P0InvalidatedSecret"}} {
		err = batch(ctx, k, e, []map[string]any{{"kind": "memory.propose", "memoryId": item[0], "pageId": item[1], "scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}, "memoryKind": "decision", "content": item[2], "sources": []any{map[string]any{"pageId": source, "sourceRevision": "1"}}}}, map[string]string{item[0]: "memory"})
		if err == nil {
			err = batch(ctx, k, e, []map[string]any{{"kind": "memory.approve", "memoryId": item[0]}}, nil)
		}
		if err != nil {
			return
		}
	}
	err = batch(ctx, k, e, []map[string]any{{"kind": "memory.forget", "memoryId": state.ForgottenID}, {"kind": "page.update", "pageId": source, "title": "P0 source invalidated"}}, nil)
	if err != nil {
		return
	}
	var draft workspace.RecordingDraft
	var audio *os.File
	draft, audio, err = k.BeginRecording(ctx, e, "P0 durable recording fixture")
	if err != nil {
		return
	}
	defer audio.Close()
	state.RecordingID = draft.ID
	if _, err = audio.Write(make([]byte, 64000)); err == nil {
		err = audio.Sync()
	}
	if err == nil {
		_, err = k.CheckpointRecording(ctx, e, draft.ID, true, "Deliberate qualification interruption")
	}
	// Persist a tail beyond the last journal checkpoint, as a capture process can
	// do just before dying. It must survive restart, backup and restoration.
	if err == nil {
		_, err = audio.Write(make([]byte, 64000))
	}
	if err == nil {
		err = audio.Sync()
	}
	return
}

func verifyP0(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, state P0State) error {
	if state.WorkspaceID != k.ID() {
		return fmt.Errorf("P0 workspace identity changed")
	}
	report, err := dispatch(ctx, k, e, "getImportReport", map[string]any{"runId": state.ImportID})
	if err != nil {
		return err
	}
	if report["state"] != "partial" || report["sourceCompleteness"] != "unknown" || report["accountedCount"] != report["discoveredCount"] {
		return fmt.Errorf("P0 import reconciliation changed: %v", report)
	}
	for _, expected := range [][2]string{{state.PageID, "P0 edited imported page"}, {state.RecordPageID, "P0 edited database record"}} {
		p, err := dispatch(ctx, k, e, "readPage", map[string]any{"pageId": expected[0]})
		if err != nil {
			return err
		}
		if p["page"].(map[string]any)["title"] != expected[1] {
			return fmt.Errorf("P0 edited page title did not persist")
		}
	}
	search, err := dispatch(ctx, k, e, "search.query", map[string]any{"query": "P0PersistedJourney", "mode": "keyword", "limit": 10})
	if err != nil {
		return err
	}
	hits, _ := search["hits"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["location"].(map[string]any)["blockId"] != state.BlockID {
		return fmt.Errorf("P0 exact keyword result missing")
	}
	snap, err := snapshot(ctx, k, e)
	if err != nil {
		return err
	}
	memories, _ := snap["memories"].([]any)
	for _, expected := range [][2]string{{state.ForgottenID, "forgotten"}, {state.StaleID, "stale"}} {
		found := false
		for _, item := range memories {
			m := item.(map[string]any)
			if m["id"] == expected[0] && m["status"] == expected[1] {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("P0 %s memory state missing", expected[1])
		}
	}
	// Preparation exercises actual retrieval without pretending to run a model.
	prepared, err := k.PrepareAI(ctx, e, workspace.AIRequest{Mode: "answer", Prompt: "P0PersistedJourney", UseMemory: true})
	if err != nil {
		return err
	}
	if strings.Contains(prepared.Prompt, "P0ForgottenSecret") || strings.Contains(prepared.Prompt, "P0InvalidatedSecret") {
		return fmt.Errorf("forgotten or stale memory entered AI retrieval")
	}
	if _, err = k.FinishAI(ctx, e, prepared.Run.ID, "", "cancelled", "Retrieval-only qualification; no inference requested"); err != nil {
		return err
	}
	drafts, err := k.ListRecordings(ctx, e)
	if err != nil {
		return err
	}
	for _, draft := range drafts {
		if draft.ID == state.RecordingID && draft.State == "interrupted" {
			return nil
		}
	}
	return fmt.Errorf("P0 interrupted recording missing")
}

func backupRestoreP0(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, state P0State, root string) error {
	backup := filepath.Join(root, "p0-backup-"+workspace.NewID())
	manifest, err := k.CreateBackup(ctx, e, backup)
	if err != nil {
		return err
	}
	restoredPath := filepath.Join(root, "p0-restored-"+workspace.NewID())
	if _, err = workspace.RestoreBackupReviewed(ctx, backup, restoredPath, manifest); err != nil {
		return err
	}
	restored, err := workspace.Open(restoredPath)
	if err != nil {
		return err
	}
	defer restored.Close()
	before, err := snapshot(ctx, k, e)
	if err != nil {
		return err
	}
	after, err := snapshot(ctx, restored, restored.OwnerContext())
	if err != nil {
		return err
	}
	for _, field := range []string{"pages", "blocks", "collections", "properties", "records", "views", "files", "memories", "meetings", "transcriptSegments", "importMappings"} {
		a, _ := json.Marshal(before[field])
		b, _ := json.Marshal(after[field])
		if !bytes.Equal(a, b) {
			return fmt.Errorf("native restore changed %s", field)
		}
	}
	if err = verifyP0(ctx, restored, restored.OwnerContext(), state); err != nil {
		return err
	}
	draft, err := restored.FinalizeRecording(ctx, restored.OwnerContext(), state.RecordingID)
	if err != nil {
		return err
	}
	if draft.DurationMS != 2000 || len(draft.Segments) != 2 {
		return fmt.Errorf("restored audio tail lost: %+v", draft)
	}
	_, err = workspace.VerifyBackup(ctx, backup)
	return err
}
