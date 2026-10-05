package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type CaptureRequest struct {
	RequestID         string
	DurationMS        int64
	MaxBytes          int64
	RequestPermission bool
	Synthetic         bool
}
type CaptureResult struct {
	Bytes      int64  `json:"bytes"`
	SampleRate int    `json:"sampleRate"`
	Channels   int    `json:"channels"`
	Format     string `json:"format"`
	Synthetic  bool   `json:"synthetic"`
	Cancelled  bool   `json:"cancelled"`
}

// Capture must only be invoked for an explicit recording gesture. Synthetic
// qualification bypasses AVFoundation entirely and never prompts for microphone access.
// The caller owns the staging file and retains flushed audio after cancellation.
func (s *Supervisor) Capture(ctx context.Context, r CaptureRequest, destination *os.File, onEvent func(json.RawMessage)) (CaptureResult, error) {
	var result CaptureResult
	if s == nil || runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return result, errors.New("UNSUPPORTED_PLATFORM: microphone capture requires macOS arm64")
	}
	if !uuidPattern.MatchString(r.RequestID) || r.DurationMS < 1 || r.DurationMS > 7200000 || r.MaxBytes < 1 || r.MaxBytes > 512<<20 || (!r.Synthetic && !r.RequestPermission) {
		return result, errors.New("APPROVAL_REQUIRED: explicit microphone action required")
	}
	st, e := destination.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return result, errors.New("invalid capture destination")
	}
	if st.Size() != 0 {
		return result, errors.New("capture destination must be empty staging file")
	}
	run, release, e := s.Scheduler.Enter(ctx, PriorityRecording)
	if e != nil {
		return result, e
	}
	defer release()
	run, cancel := context.WithTimeout(run, time.Duration(r.DurationMS+30000)*time.Millisecond)
	defer cancel()
	h := s.helpers["microphone-worker"]
	if e = s.verifyHelper(h); e != nil {
		return result, e
	}
	controlR, controlW, e := os.Pipe()
	if e != nil {
		return result, e
	}
	defer controlR.Close()
	defer controlW.Close()
	dir, e := os.MkdirTemp("", "trove-recording-")
	if e != nil {
		return result, e
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command(filepath.Join(s.bundle, h.Name))
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir}
	process, e := prepareWorker(cmd, []*os.File{controlR, destination}, 512<<20)
	if e != nil {
		return result, e
	}
	defer process.close()
	cmd.Stderr = &ringLog{}
	in, e := cmd.StdinPipe()
	if e != nil {
		return result, e
	}
	defer in.Close()
	out, e := cmd.StdoutPipe()
	if e != nil {
		return result, e
	}
	defer out.Close()
	if e = process.start(); e != nil {
		return result, e
	}
	controlR.Close()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-run.Done():
			_ = WriteFrame(controlW, map[string]any{"kind": "cancel", "requestId": r.RequestID})
			select {
			case <-stopped:
				return
			case <-time.After(2 * time.Second):
				_ = process.stop(false)
			}
			select {
			case <-stopped:
				return
			case <-time.After(3 * time.Second):
				_ = process.stop(true)
			}
		case <-stopped:
		}
	}()
	fail := func(err error) (CaptureResult, error) {
		cancel()
		_ = process.stop(true)
		_ = cmd.Wait()
		_ = destination.Sync()
		return result, err
	}
	raw, e := ReadFrame(out)
	if e != nil {
		return fail(e)
	}
	var hello Event
	if e = json.Unmarshal(raw, &hello); e != nil || hello.Kind != "hello" || hello.Runtime != "microphone" || hello.RuntimeCommit != h.RuntimeCommit || hello.ProtocolVersion != 1 {
		return fail(errors.New("microphone hello mismatch"))
	}
	if e = WriteFrame(in, map[string]any{"protocolVersion": 1, "requestId": r.RequestID, "kind": "record", "durationMs": r.DurationMS, "maxBytes": r.MaxBytes, "synthetic": r.Synthetic, "requestPermission": r.RequestPermission}); e != nil {
		return fail(e)
	}
	in.Close()
	seq := int64(0)
	terminal := false
	for {
		raw, e = ReadFrame(out)
		if e == io.EOF {
			break
		}
		if e != nil {
			return fail(e)
		}
		var ev struct {
			Kind      string `json:"kind"`
			RequestID string `json:"requestId"`
			Sequence  int64  `json:"sequence"`
			Bytes     int64  `json:"bytes"`
			Code      string `json:"code"`
		}
		if e = json.Unmarshal(raw, &ev); e != nil || ev.RequestID != r.RequestID || ev.Sequence != seq || terminal || ev.Bytes > r.MaxBytes {
			return fail(errors.New("invalid microphone event"))
		}
		seq++
		switch ev.Kind {
		case "progress":
		case "result":
			terminal = true
			_ = json.Unmarshal(raw, &result)
		case "error":
			return fail(fmt.Errorf("microphone: %s", ev.Code))
		default:
			return fail(errors.New("unknown microphone event"))
		}
		if onEvent != nil {
			onEvent(raw)
		}
	}
	if e = cmd.Wait(); e != nil {
		return result, e
	}
	if !terminal {
		return result, errors.New("microphone stopped without result; staged audio retained")
	}
	st, e = destination.Stat()
	if e != nil || st.Size() != result.Bytes || result.SampleRate != 16000 || result.Channels != 1 || result.Format != "f32le" {
		return result, errors.New("capture output mismatch")
	}
	return result, nil
}
