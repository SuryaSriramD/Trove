package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"trove.local/phase0/contracts"
)

type Helper struct {
	Name          string `json:"name"`
	SHA256        string `json:"sha256"`
	Runtime       string `json:"runtime"`
	RuntimeCommit string `json:"runtimeCommit"`
	Architecture  string `json:"architecture"`
}
type BundleManifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	Helpers       []Helper `json:"helpers"`
}
type Supervisor struct {
	bundle    string
	manager   *Manager
	helpers   map[string]Helper
	Scheduler Scheduler
}
type Input struct {
	Handle contracts.JobsInputHandle
	File   *os.File
}
type Request struct {
	Wire     contracts.JobsWorkerRequest
	Inputs   []Input
	Priority Priority
}
type Event struct {
	Kind            string                    `json:"kind"`
	RequestID       string                    `json:"requestId"`
	LeaseToken      string                    `json:"leaseToken"`
	Sequence        int64                     `json:"sequence"`
	Text            string                    `json:"text"`
	Code            string                    `json:"code"`
	Message         string                    `json:"message"`
	ResultHandle    contracts.JobsInputHandle `json:"resultHandle"`
	ProtocolVersion int                       `json:"protocolVersion"`
	Runtime         string                    `json:"runtime"`
	RuntimeCommit   string                    `json:"runtimeCommit"`
	Architecture    string                    `json:"architecture"`
	Capabilities    []string                  `json:"capabilities"`
	Progress        json.RawMessage           `json:"progress"`
}

func NewSupervisor(bundle string, manager *Manager) (*Supervisor, error) {
	raw, e := os.ReadFile(filepath.Join(bundle, "manifest.json"))
	if e != nil {
		return nil, e
	}
	var m BundleManifest
	if e = json.Unmarshal(raw, &m); e != nil {
		return nil, e
	}
	if m.SchemaVersion != 1 {
		return nil, errors.New("unsupported native bundle")
	}
	s := &Supervisor{bundle: bundle, manager: manager, helpers: map[string]Helper{}}
	for _, h := range m.Helpers {
		if h.Name != "llama-worker" && h.Name != "whisper-worker" && h.Name != "microphone-worker" {
			return nil, errors.New("unknown helper")
		}
		if h.Architecture != "arm64" || !digestPattern.MatchString(h.SHA256) {
			return nil, errors.New("invalid helper manifest")
		}
		if e = s.verifyHelper(h); e != nil {
			return nil, e
		}
		s.helpers[h.Name] = h
	}
	for _, name := range []string{"llama-worker", "whisper-worker", "microphone-worker"} {
		if _, ok := s.helpers[name]; !ok {
			return nil, errors.New("missing bundled helper")
		}
	}
	return s, nil
}
func (s *Supervisor) verifyHelper(h Helper) error {
	path := filepath.Join(s.bundle, h.Name)
	if e := regular(path); e != nil {
		return e
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	digest := sha256.New()
	if _, e = io.Copy(digest, f); e != nil {
		return e
	}
	if hex.EncodeToString(digest.Sum(nil)) != h.SHA256 {
		return errors.New("native executable hash mismatch")
	}
	return nil
}
func fileHandle(f *os.File, kind, id string) (contracts.JobsInputHandle, error) {
	var h contracts.JobsInputHandle
	info, e := f.Stat()
	if e != nil {
		return h, e
	}
	if !info.Mode().IsRegular() || info.Size() > 512<<20 {
		return h, errors.New("invalid native input")
	}
	sum := sha256.New()
	if _, e = io.Copy(sum, io.NewSectionReader(f, 0, info.Size())); e != nil {
		return h, e
	}
	return contracts.JobsInputHandle{HandleId: contracts.DomainId(id), Kind: kind, Sha256: hex.EncodeToString(sum.Sum(nil)), SizeBytes: info.Size()}, nil
}

// OpenInput hashes an already authorized descriptor; Run makes a verified private snapshot.
// Workers receive descriptors only; no workspace paths or database handles cross IPC.
func OpenInput(f *os.File, kind, id string) (Input, error) {
	h, e := fileHandle(f, kind, id)
	return Input{h, f}, e
}
func memoryLimit() int64 {
	out, e := exec.Command("/usr/sbin/sysctl", "-n", "hw.memsize").Output()
	if e != nil {
		return 512 << 20
	}
	n, e := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if e != nil {
		return 512 << 20
	}
	n = n/2 - (2 << 30)
	if n < 64<<20 {
		n = 64 << 20
	}
	if n > 16<<30 {
		n = 16 << 30
	}
	return n
}

type ringLog struct {
	mu    sync.Mutex
	bytes []byte
	total int64
}

func (r *ringLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total += int64(len(p))
	r.bytes = append(r.bytes, p...)
	if len(r.bytes) > 256<<10 {
		r.bytes = r.bytes[len(r.bytes)-(256<<10):]
	}
	return len(p), nil
}
func (s *Supervisor) Run(ctx context.Context, r Request, onEvent func(Event)) (json.RawMessage, error) {
	p, model, release, e := s.manager.lease(string(r.Wire.ModelPackId))
	if e != nil {
		return nil, e
	}
	defer release()
	operation := r.Wire.Operation
	if ((operation == "generate" || operation == "tokenize") && p.Kind != "chat") || (operation == "embed" && p.Kind != "embedding") || (operation == "transcribe" && p.Kind != "speech") {
		return nil, errors.New("MODEL_INCOMPATIBLE: operation")
	}
	if r.Wire.Limits.MemoryBytes > memoryLimit() || p.EstimatedMemoryBytes > r.Wire.Limits.MemoryBytes {
		return nil, errors.New("RESOURCE_EXHAUSTED: model memory budget")
	}
	if len(r.Inputs) < 1 || len(r.Inputs) > 30 {
		return nil, errors.New("INVALID_INPUT: inputs")
	}
	if r.Wire.Limits.MaxTokens > int64(p.ContextTokens) && operation == "generate" {
		return nil, errors.New("MODEL_INCOMPATIBLE: token budget")
	}
	r.Wire.Inputs = []contracts.JobsInputHandle{{HandleId: r.Wire.ModelPackId, Kind: "model", Sha256: p.SHA256, SizeBytes: p.SizeBytes}}
	for _, input := range r.Inputs {
		h, e := fileHandle(input.File, input.Handle.Kind, string(input.Handle.HandleId))
		if e != nil {
			return nil, e
		}
		if h != input.Handle {
			return nil, errors.New("INVALID_INPUT: changed input descriptor")
		}
		r.Wire.Inputs = append(r.Wire.Inputs, h)
	}
	metadata, e := validateInputMetadata(p, operation, r.Inputs)
	if e != nil {
		return nil, e
	}
	wire, e := json.Marshal(r.Wire)
	if e != nil {
		return nil, e
	}
	if e = contracts.Validate("jobs", "WorkerRequest", wire); e != nil {
		return nil, e
	}
	run, done, e := s.Scheduler.Enter(ctx, r.Priority)
	if e != nil {
		return nil, e
	}
	defer done()
	run, timeoutCancel := context.WithTimeout(run, time.Duration(r.Wire.Limits.TimeoutMs)*time.Millisecond)
	defer timeoutCancel()
	run, cancelCause := context.WithCancelCause(run)
	cancel := func() { cancelCause(context.Canceled) }
	defer cancel()
	name := "llama-worker"
	if operation == "transcribe" {
		name = "whisper-worker"
	}
	helper := s.helpers[name]
	if e = s.verifyHelper(helper); e != nil {
		return nil, e
	}
	dir, e := os.MkdirTemp("", "trove-worker-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	result, e := os.OpenFile(filepath.Join(dir, "result"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	defer result.Close()
	controlR, controlW, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	defer controlR.Close()
	defer controlW.Close()
	cmd := exec.Command(filepath.Join(s.bundle, name))
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir, "LANG=en_US.UTF-8", "TROVE_GPU_LAYERS=0"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.ExtraFiles = []*os.File{controlR, result, model}
	for _, in := range r.Inputs {
		// Snapshot every approved input into private, read-only descriptors. A
		// concurrent editor/import cannot change bytes underneath a native run.
		snapshotPath := filepath.Join(dir, fmt.Sprintf("input-%d", len(cmd.ExtraFiles)))
		destination, err := os.OpenFile(snapshotPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(destination, io.NewSectionReader(in.File, 0, in.Handle.SizeBytes))
		destination.Close()
		if err != nil {
			return nil, err
		}
		snapshot, err := os.Open(snapshotPath)
		if err != nil {
			return nil, err
		}
		defer snapshot.Close()
		actual, err := fileHandle(snapshot, in.Handle.Kind, string(in.Handle.HandleId))
		if err != nil || actual != in.Handle {
			return nil, errors.New("INVALID_INPUT: input changed while staging")
		}
		cmd.ExtraFiles = append(cmd.ExtraFiles, snapshot)
	}
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	logs := &ringLog{}
	cmd.Stderr = logs
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	controlR.Close()
	stopped := make(chan struct{})
	defer close(stopped)
	// A separate control pipe and process-group escalation stay responsive even when stdout is backpressured.
	go func() {
		select {
		case <-run.Done():
			_ = WriteFrame(controlW, map[string]any{"protocolVersion": 1, "kind": "cancel", "requestId": r.Wire.RequestId, "cancellationId": r.Wire.CancellationId})
			select {
			case <-stopped:
				return
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			}
			select {
			case <-stopped:
				return
			case <-time.After(3 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-stopped:
		}
	}()
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				raw, e := exec.Command("/bin/ps", "-o", "rss=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
				if e == nil {
					rss, _ := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
					if rss*1024 > r.Wire.Limits.MemoryBytes {
						cancelCause(errors.New("RESOURCE_EXHAUSTED: worker memory limit"))
					}
				}
				if st, e := result.Stat(); e == nil && st.Size() > r.Wire.Limits.MaxOutputBytes {
					cancelCause(errors.New("RESOURCE_EXHAUSTED: worker output limit"))
				}
			case <-stopped:
				return
			}
		}
	}()
	fail := func(e error) (json.RawMessage, error) {
		cause := context.Cause(run)
		cancel()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if cause != nil {
			return nil, cause
		}
		return nil, e
	}
	frame, e := ReadFrame(out)
	if e != nil {
		return fail(e)
	}
	if e = contracts.Validate("jobs", "WorkerOutput", frame); e != nil {
		return fail(e)
	}
	var event Event
	_ = json.Unmarshal(frame, &event)
	if event.Kind != "hello" || event.Runtime != helper.Runtime || event.RuntimeCommit != helper.RuntimeCommit || event.Architecture != "arm64" || event.ProtocolVersion != 1 {
		return fail(errors.New("native hello mismatch"))
	}
	capable := false
	for _, v := range event.Capabilities {
		if v == operation {
			capable = true
		}
	}
	if !capable {
		return fail(errors.New("worker capability mismatch"))
	}
	if e = WriteFrame(in, r.Wire); e != nil {
		return fail(e)
	}
	in.Close()
	seq := int64(0)
	total := int64(0)
	terminal := false
	var resultHandle contracts.JobsInputHandle
	for {
		frame, e = ReadFrame(out)
		if e == io.EOF {
			break
		}
		if e != nil {
			return fail(e)
		}
		total += int64(len(frame))
		if total > r.Wire.Limits.MaxOutputBytes {
			return fail(errors.New("RESOURCE_EXHAUSTED: output budget"))
		}
		if e = contracts.Validate("jobs", "WorkerOutput", frame); e != nil {
			return fail(e)
		}
		event = Event{}
		_ = json.Unmarshal(frame, &event)
		if terminal || event.Kind == "hello" || event.RequestID != string(r.Wire.RequestId) || event.Sequence != seq {
			return fail(errors.New("native output sequence violation"))
		}
		seq++
		switch event.Kind {
		case "result":
			if event.LeaseToken != string(r.Wire.LeaseToken) {
				return fail(errors.New("stale worker lease"))
			}
			terminal = true
			resultHandle = event.ResultHandle
		case "error":
			return fail(fmt.Errorf("%s: %s", event.Code, event.Message))
		case "cancelled":
			return fail(context.Canceled)
		}
		if onEvent != nil {
			onEvent(event)
		}
	}
	if e = cmd.Wait(); e != nil {
		return nil, fmt.Errorf("native process failed: %w", e)
	}
	if run.Err() != nil {
		return nil, context.Cause(run)
	}
	if !terminal {
		return nil, errors.New("native process ended without result")
	}
	if resultHandle.Kind != "result" || resultHandle.SizeBytes > r.Wire.Limits.MaxOutputBytes {
		return nil, errors.New("invalid result handle")
	}
	h, e := fileHandle(result, "result", string(resultHandle.HandleId))
	if e != nil || h != resultHandle {
		return nil, errors.New("native result digest mismatch")
	}
	raw := make([]byte, resultHandle.SizeBytes)
	if _, e = result.ReadAt(raw, 0); e != nil {
		return nil, e
	}
	if e = contracts.Validate("jobs", "WorkerResult", raw); e != nil {
		return nil, e
	}
	var body struct {
		Kind        string            `json:"kind"`
		Counts      []int             `json:"counts"`
		Dimension   int               `json:"dimension"`
		Vectors     [][]float64       `json:"vectors"`
		Locations   []json.RawMessage `json:"locations"`
		ModelPackID string            `json:"modelPackId"`
		Segments    []struct {
			Start int64 `json:"startMs"`
			End   int64 `json:"endMs"`
		} `json:"segments"`
	}
	_ = json.Unmarshal(raw, &body)
	if operation == "tokenize" && (body.Kind != "token_counts" || len(body.Counts) != len(metadata.Texts)) {
		return nil, errors.New("tokenizer result mismatch")
	}
	if operation == "embed" {
		if body.Kind != "embedding" || body.ModelPackID != p.ID || body.Dimension != p.Dimension || len(body.Vectors) != len(body.Locations) {
			return nil, errors.New("embedding shape mismatch")
		}
		for _, v := range body.Vectors {
			if len(v) != p.Dimension {
				return nil, errors.New("embedding dimension mismatch")
			}
		}
	}
	if operation == "generate" && body.Kind != "generation" {
		return nil, errors.New("generation result mismatch")
	}
	if operation == "transcribe" {
		if body.Kind != "transcript" {
			return nil, errors.New("transcript result mismatch")
		}
		for _, seg := range body.Segments {
			if seg.End < seg.Start {
				return nil, errors.New("transcript offsets invalid")
			}
		}
	}
	if operation == "embed" {
		var actual struct {
			Locations []contracts.DomainLocation `json:"locations"`
		}
		_ = json.Unmarshal(raw, &actual)
		a, _ := json.Marshal(actual.Locations)
		b, _ := json.Marshal(metadata.Locations)
		if string(a) != string(b) {
			return nil, errors.New("embedding source identity mismatch")
		}
	}
	if operation == "transcribe" {
		var actual struct {
			MeetingID string `json:"meetingId"`
			Segments  []struct {
				SegmentID string `json:"segmentId"`
				BlockID   string `json:"blockId"`
				StartMS   int64  `json:"startMs"`
				EndMS     int64  `json:"endMs"`
			} `json:"segments"`
		}
		_ = json.Unmarshal(raw, &actual)
		if actual.MeetingID != metadata.MeetingID || len(actual.Segments) > len(metadata.SegmentIDs) {
			return nil, errors.New("transcript identity mismatch")
		}
		duration := r.Inputs[1].Handle.SizeBytes / 4 * 1000 / 16000
		for i, segment := range actual.Segments {
			reserved := metadata.SegmentIDs[i]
			if segment.SegmentID != reserved.SegmentID || segment.BlockID != reserved.BlockID || segment.EndMS > duration {
				return nil, errors.New("transcript source mismatch")
			}
		}
	}
	return raw, nil
}

// MemoryBudget reports the supervisor's current platform admission cap so callers
// cannot accidentally request more than the same supervisor will admit.
func (s *Supervisor) MemoryBudget() int64 { return memoryLimit() }
