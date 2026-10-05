package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func fixturePack(data []byte) Pack {
	h := sha256.Sum256(data)
	return Pack{SchemaVersion: 1, ID: "01990000-0000-7000-8000-000000000101", Name: "Fixture", Kind: "chat", Format: "gguf", SHA256: hex.EncodeToString(h[:]), SizeBytes: int64(len(data)), License: "MIT", LicenseURL: "https://example.test/license", SourceRevision: "fixture", RuntimeCommit: LlamaCommit, Architecture: "arm64", ContextTokens: 128, EstimatedMemoryBytes: 64 << 20}
}
func TestFramesRejectMalformed(t *testing.T) {
	for _, b := range [][]byte{{0, 0, 0, 0}, {0, 16, 0, 1}, {0, 0, 0, 3, '{', '}'}, {0, 0, 0, 3, '"', 255, '"'}, {0, 0, 0, 3, 'N', 'a', 'N'}} {
		if _, e := ReadFrame(bytes.NewReader(b)); e == nil {
			t.Fatalf("accepted malformed %v", b)
		}
	}
	var b bytes.Buffer
	if e := WriteFrame(&b, map[string]string{"text": "తెలుగు 中文 مرحبا"}); e != nil {
		t.Fatal(e)
	}
	raw, e := ReadFrame(&b)
	if e != nil || !json.Valid(raw) {
		t.Fatal(e)
	}
}
func TestModelImportVerifyLeaseRemove(t *testing.T) {
	data := append([]byte("GGUF"), bytes.Repeat([]byte("x"), 128)...)
	p := fixturePack(data)
	m, _ := NewManager(t.TempDir())
	if e := m.Import(context.Background(), p, bytes.NewReader(data), nil); e != nil {
		t.Fatal(e)
	}
	_, _, release, e := m.lease(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Remove(p.ID); e == nil {
		t.Fatal("removed leased model")
	}
	release()
	statuses, e := m.List()
	if e != nil || len(statuses) != 1 || statuses[0].State != "ready" {
		t.Fatal(statuses, e)
	}
	final, _, _ := m.paths(p)
	_ = os.WriteFile(final, []byte("GGUFtampered"), 0600)
	if _, _, _, e = m.lease(p.ID); e == nil {
		t.Fatal("accepted changed model")
	}
	if e = m.Remove(p.ID); e != nil {
		t.Fatal(e)
	}
	p.RuntimeCommit = "wrong"
	if e = m.Import(context.Background(), p, bytes.NewReader(data), nil); e == nil {
		t.Fatal("accepted incompatible runtime")
	}
}
func TestAcquisitionResumesAfterManagerRestart(t *testing.T) {
	data := append([]byte("GGUF"), bytes.Repeat([]byte("abcdefgh"), 65536)...)
	p := fixturePack(data)
	var ranged atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "\"fixture\"")
		start := 0
		if h := r.Header.Get("Range"); h != "" {
			if r.Header.Get("If-Range") != "\"fixture\"" {
				t.Error("missing If-Range")
			}
			fmt.Sscanf(h, "bytes=%d-", &start)
			ranged.Store(true)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.WriteHeader(206)
		}
		_, _ = w.Write(data[start:])
	}))
	defer server.Close()
	p.URL = server.URL
	root := t.TempDir()
	m, _ := NewManager(root)
	m.client = server.Client()
	ctx, cancel := context.WithCancel(context.Background())
	e := m.Acquire(ctx, p, func(n int64) {
		if n >= 65536 {
			cancel()
		}
	})
	if e == nil {
		t.Fatal("expected interrupted transfer")
	}
	m, _ = NewManager(root)
	m.client = server.Client()
	list, _ := m.List()
	if len(list) != 1 || list[0].State != "interrupted" {
		t.Fatalf("missing persisted interrupted state: %+v", list)
	}
	if e = m.Acquire(context.Background(), p, nil); e != nil {
		t.Fatal(e)
	}
	if !ranged.Load() {
		t.Fatal("did not resume transfer")
	}
	_, _, release, e := m.lease(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	release()
}
func TestSchedulerRecordingPreemptionAndEditing(t *testing.T) {
	var s Scheduler
	ctx, release, e := s.Enter(context.Background(), PriorityBackground)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() {
		_, r, e := s.Enter(context.Background(), PriorityRecording)
		if e != nil {
			t.Error(e)
		}
		r()
		close(done)
	}()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("recording did not preempt")
	}
	release()
	<-done
	s.SetEditing(true)
	wait, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, e = s.Enter(wait, PriorityBackground)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("background ran while editing")
	}
	s.SetEditing(false)
	_, r, e := s.Enter(context.Background(), PriorityChat)
	if e != nil {
		t.Fatal(e)
	}
	r()
}
func TestRealQualification(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("set TROVE_NATIVE_INTEGRATION=1 with verified packs and bundled helpers")
	}
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	bundle := os.Getenv("TROVE_NATIVE_BUNDLE")
	if bundle == "" {
		bundle = filepath.Join(root, "build/native")
	}
	modelRoot := os.Getenv("TROVE_MODEL_ROOT")
	if modelRoot == "" {
		modelRoot = "/private/tmp/trove-phase1-models"
	}
	m, e := NewManager(modelRoot)
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewSupervisor(bundle, m)
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.QualificationSmoke(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	speech, _ := json.Marshal(result["speech"])
	if !strings.Contains(strings.ToLower(string(speech)), "country") {
		t.Fatalf("unexpected transcription: %s", speech)
	}
	t.Logf("real models: %v", result)
	encoded, _ := json.MarshalIndent(map[string]any{"recordedAt": time.Now().UTC(), "host": "darwin/arm64", "status": "verified", "checks": result}, "", "  ")
	if e = os.WriteFile(filepath.Join(root, "evidence/phase1-native-smoke.json"), encoded, 0644); e != nil {
		t.Fatal(e)
	}
}

func TestRealWorkerCancellationCrashAndMetal(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("real native pack qualification is opt-in")
	}
	root, _ := filepath.Abs("../..")
	bundle := os.Getenv("TROVE_NATIVE_BUNDLE")
	if bundle == "" {
		bundle = filepath.Join(root, "build/native")
	}
	modelRoot := os.Getenv("TROVE_MODEL_ROOT")
	if modelRoot == "" {
		modelRoot = "/private/tmp/trove-phase1-models"
	}
	m, e := NewManager(modelRoot)
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewSupervisor(bundle, m)
	if e != nil {
		t.Fatal(e)
	}
	crashMode := false
	run := func(ctx context.Context, gpu int, callback func(Event)) (json.RawMessage, error) {
		f, e := os.CreateTemp(t.TempDir(), "input-")
		if e != nil {
			return nil, e
		}
		defer f.Close()
		metadata := map[string]any{"schemaVersion": 1, "text": "Once upon a time", "contextTokens": 128, "gpuLayers": gpu}
		if crashMode {
			texts := []string{}
			locations := []map[string]string{}
			for i := 0; i < 16; i++ {
				texts = append(texts, "A note safely kept in the local workspace.")
				locations = append(locations, map[string]string{"pageId": NewID(), "sourceRevision": "1"})
			}
			metadata = map[string]any{"schemaVersion": 1, "texts": texts, "locations": locations, "contextTokens": 128, "gpuLayers": 0}
		}
		_ = json.NewEncoder(f).Encode(metadata)
		in, e := OpenInput(f, "text", NewID())
		if e != nil {
			return nil, e
		}
		wire := contracts.JobsWorkerRequest{ProtocolVersion: 1, RequestId: contracts.DomainId(NewID()), JobId: contracts.DomainId(NewID()), LeaseToken: contracts.DomainId(NewID()), CancellationId: contracts.DomainId(NewID()), Operation: "generate", ModelPackId: "01990000-0000-7000-8000-000000000101", Limits: contracts.JobsResourceLimits{Threads: 2, MemoryBytes: 1 << 30, TimeoutMs: 120000, MaxOutputBytes: 1 << 20, MaxTokens: 16}}
		if crashMode {
			wire.Operation = "embed"
			wire.ModelPackId = "01990000-0000-7000-8000-000000000102"
		}
		return s.Run(ctx, Request{Wire: wire, Inputs: []Input{in}, Priority: PriorityChat}, callback)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, e = run(ctx, 0, func(event Event) {
		if event.Kind == "progress" {
			cancel()
		}
	})
	cancel()
	if e == nil {
		t.Fatal("cancelled native request succeeded")
	}
	killed := false
	crashMode = true
	_, e = run(context.Background(), 0, func(event Event) {
		if event.Kind == "progress" && !killed {
			raw, err := exec.Command("/usr/bin/pgrep", "-P", strconv.Itoa(os.Getpid()), "-f", "llama-worker").Output()
			if err != nil {
				t.Error(err)
				return
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Error(err)
				return
			}
			killed = true
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	if e == nil || !killed {
		t.Fatalf("worker crash not observed: %v %v", killed, e)
	}
	crashMode = false
	result, e := run(context.Background(), -1, nil)
	if e != nil {
		t.Fatal("Metal worker did not recover after previous crash:", e)
	}
	var body struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(result, &body)
	if body.Text == "" {
		t.Fatal("empty Metal inference")
	}
	evidence := map[string]any{"recordedAt": time.Now().UTC(), "cancelledWorkerStopped": true, "crashedWorkerReaped": true, "subsequentMetalInference": body.Text}
	raw, _ := json.MarshalIndent(evidence, "", "  ")
	_ = os.WriteFile(filepath.Join(root, "evidence/phase1-native-failure.json"), raw, 0644)
}
