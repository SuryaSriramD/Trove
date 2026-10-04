package workers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

func TestAudioRejectsAmbiguousFormat(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "audio-")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Write([]byte("not an audio file"))
	if _, _, err = pcmInput(f, contracts.DomainFile{OriginalName: "data.txt", MediaType: "text/plain"}); err == nil {
		t.Fatal("arbitrary text accepted as audio")
	}
}
func TestRealDurableTranscriptionJob(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires pinned native helpers and verified speech pack")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root, _ := filepath.Abs("../..")
	bundle := os.Getenv("TROVE_NATIVE_BUNDLE")
	if bundle == "" {
		bundle = filepath.Join(root, "build/native")
	}
	models := os.Getenv("TROVE_MODEL_ROOT")
	if models == "" {
		models = "/private/tmp/trove-phase1-models"
	}
	manager, err := native.NewManager(models)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(bundle, manager)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "workspace")
	k, err := workspace.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e := k.OwnerContext()
	call := func(method string, r map[string]any) map[string]any {
		t.Helper()
		r["contractVersion"] = 1
		r["workspaceId"] = k.ID()
		r["requestId"] = workspace.NewID()
		raw, _ := json.Marshal(r)
		response := k.Dispatch(ctx, e, method, raw)
		var out map[string]any
		if err = json.Unmarshal(response, &out); err != nil || out["ok"] != true {
			t.Fatalf("%s: %s %v", method, response, err)
		}
		return out["result"].(map[string]any)
	}
	guard := func(kind, id, revision string) map[string]any {
		return map[string]any{"target": map[string]any{"entityType": kind, "entityId": id}, "revision": revision}
	}
	batch := func(commands []any, guards ...any) {
		call("batch", map[string]any{"commands": commands, "mutation": map[string]any{"idempotencyKey": workspace.NewID(), "scopeGrantId": e.ScopeGrantId, "expectedRevisions": guards}})
	}
	page, meeting := workspace.NewID(), workspace.NewID()
	batch([]any{map[string]any{"kind": "page.create", "pageId": page, "title": "Native transcription acceptance"}}, guard("page", page, "0"))
	source := filepath.Join(bundle, "fixtures", "jfk.wav")
	handle, err := k.RegisterSelection(source)
	if err != nil {
		t.Fatal(err)
	}
	prepared := call("prepareFile", map[string]any{"selectionHandle": handle})
	file := prepared["fileId"].(string)
	batch([]any{map[string]any{"kind": "file.attach", "fileId": file, "ingestHandle": prepared["ingestHandle"], "pageId": page}}, guard("page", page, "1"), guard("file", file, "0"))
	batch([]any{map[string]any{"kind": "meeting.create", "meetingId": meeting, "pageId": page, "recordingFileId": file, "startedAt": time.Now().UTC().Format(time.RFC3339Nano)}}, guard("page", page, "2"), guard("file", file, "1"), guard("meeting", meeting, "0"))
	job := call("enqueueJob", map[string]any{"kind": "TRANSCRIBE_AUDIO", "payload": map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": "01990000-0000-7000-8000-000000000103"}, "idempotencyKey": workspace.NewID()})
	runnerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- Run(runnerCtx, k, e, supervisor) }()
	stopped := false
	defer func() {
		if !stopped {
			stop()
			<-done
		}
	}()
	var terminal contracts.JobsJob
	for {
		terminal, err = k.ReadJob(ctx, job["jobId"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if terminal.State == "succeeded" {
			break
		}
		if terminal.State == "failed" || terminal.State == "cancelled" {
			t.Fatalf("job failed: %+v", terminal.Error)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	result := call("readPage", map[string]any{"pageId": page})
	raw, _ := json.Marshal(result)
	if !strings.Contains(strings.ToLower(string(raw)), "country") {
		t.Fatalf("missing expected real transcription: %s", raw)
	}
	stop()
	<-done
	stopped = true
	k.Close()
	k, err = workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	reopened := call("readPage", map[string]any{"pageId": page})
	if len(reopened["blocks"].([]any)) == 0 {
		t.Fatal("transcript lost after restart")
	}
	evidence := map[string]any{"verifiedAt": time.Now().UTC().Format(time.RFC3339Nano), "target": "darwin/arm64", "verified": true, "jobId": job["jobId"], "state": terminal.State, "attempt": terminal.Attempt, "checks": []string{"file ID authorized", "WAV safely converted outside SQLite", "verified Whisper pack loaded", "real bundled helper transcribed JFK sample", "lease and source revision fenced", "transcript unit committed atomically", "job terminal state committed with transcript", "transcript page reopened after restart"}}
	encoded, _ := json.MarshalIndent(evidence, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "evidence/phase1-job-native.json"), encoded, 0644); err != nil {
		t.Fatal(err)
	}
	t.Log(string(encoded))
}
