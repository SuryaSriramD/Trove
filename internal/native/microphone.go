package native

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const MicrophoneCommit = "miniaudio-0.11.25-trove-microphone-v1"

func validMicrophoneIdentity(h Helper, platform string) bool {
	return h.Name == "microphone-worker" && h.Runtime == "microphone" && ((platform == "darwin" && (h.RuntimeCommit == "trove-microphone-v1" || h.RuntimeCommit == MicrophoneCommit)) || ((platform == "windows" || platform == "linux") && h.RuntimeCommit == MicrophoneCommit))
}

type CaptureRequest struct {
	RequestID         string
	DurationMS        int64
	MaxBytes          int64
	RequestPermission bool
	Synthetic         bool
}
type CaptureResult struct {
	Bytes           int64  `json:"bytes"`
	SampleRate      int    `json:"sampleRate"`
	Channels        int    `json:"channels"`
	Format          string `json:"format"`
	Synthetic       bool   `json:"synthetic"`
	Backend         string `json:"backend,omitempty"`
	PermissionState string `json:"permissionState,omitempty"`
	DeviceState     string `json:"deviceState,omitempty"`
	SignalObserved  *bool  `json:"signalObserved,omitempty"`
	Cancelled       bool   `json:"cancelled"`
}

// Capture must only be invoked for an explicit recording gesture. Synthetic
// qualification uses a separate device-free portable fixture or the legacy Mac fixture.
// The caller owns the staging file and retains flushed audio after cancellation.
var errCaptureStartup = errors.New("AUDIO_STARTUP_TIMEOUT: microphone did not provide PCM within 30 seconds")

func (s *Supervisor) Capture(ctx context.Context, r CaptureRequest, destination *os.File, onEvent func(json.RawMessage)) (CaptureResult, error) {
	return s.captureWithStartupLimit(ctx, r, destination, onEvent, 30*time.Second)
}

// The injectable duration is internal test plumbing, never a user/provider input.
func (s *Supervisor) captureWithStartupLimit(ctx context.Context, r CaptureRequest, destination *os.File, onEvent func(json.RawMessage), startupLimit time.Duration) (CaptureResult, error) {
	var result CaptureResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s == nil || !supportedNativeTarget(runtime.GOOS, runtime.GOARCH) {
		return result, errors.New("UNSUPPORTED_PLATFORM: microphone capture unavailable")
	}
	h, available := s.helpers["microphone-worker"]
	fixture := available && h.RuntimeCommit == MicrophoneCommit+"-fixture" && r.Synthetic && h.Runtime == "microphone" && h.Name == "microphone-worker"
	if !available || h.Architecture != runtime.GOARCH || (!fixture && !validMicrophoneIdentity(h, runtime.GOOS)) {
		return result, errors.New("UNSUPPORTED_PLATFORM: verified microphone helper is unavailable")
	}
	if r.Synthetic && h.RuntimeCommit == MicrophoneCommit {
		return result, errors.New("INVALID_INPUT: production microphone cannot synthesize recordings")
	}
	if !uuidPattern.MatchString(r.RequestID) || r.DurationMS < 1 || r.DurationMS > 7200000 || r.MaxBytes < 4 || r.MaxBytes > 512<<20 || r.MaxBytes%4 != 0 || (!r.Synthetic && !r.RequestPermission) {
		return result, errors.New("APPROVAL_REQUIRED: explicit microphone action required")
	}
	if destination == nil {
		return result, errors.New("missing capture destination")
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
	run, timeout := context.WithTimeout(run, time.Duration(r.DurationMS+30000)*time.Millisecond)
	defer timeout()
	run, cancel := context.WithCancelCause(run)
	defer cancel(context.Canceled)
	if e = s.verifyHelper(h); e != nil {
		return result, e
	}
	controlR, controlW, e := os.Pipe()
	if e != nil {
		return result, e
	}
	defer controlR.Close()
	defer controlW.Close()
	dir, e := privateWorkerDirectory("trove-recording-")
	if e != nil {
		return result, e
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command(filepath.Join(s.bundle, helperFilename(h.Name)))
	cmd.Dir = dir
	cmd.Env = workerEnvironment(dir)
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
	firstPCM := make(chan struct{})
	var firstPCMOnce sync.Once
	var watchers sync.WaitGroup
	watchers.Add(1)
	defer func() { close(stopped); watchers.Wait() }()
	go func() {
		defer watchers.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		startup := time.NewTimer(startupLimit)
		defer startup.Stop()
		deadline := startup.C
		var ready <-chan struct{} = firstPCM
		for {
			select {
			case <-stopped:
				return
			case <-ready:
				ready = nil
				deadline = nil
				startup.Stop()
			case <-deadline:
				deadline = nil
				cancel(errCaptureStartup)
			case <-ticker.C:
				if stat, err := destination.Stat(); err == nil && stat.Size() > r.MaxBytes {
					cancel(errors.New("AUDIO_LIMIT: capture output exceeds request"))
				}
				if rss, err := process.rss(); err == nil && rss > 512<<20 {
					cancel(errors.New("AUDIO_LIMIT: capture memory budget"))
				}
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
				return
			}
		}
	}()

	fail := func(err error) (CaptureResult, error) {
		if run.Err() != nil {
			err = context.Cause(run)
		}
		cancel(err)
		_ = process.stop(true)
		_ = cmd.Wait()
		_ = destination.Sync()
		if stat, e := destination.Stat(); e == nil && stat.Size() >= 0 && stat.Size() <= r.MaxBytes {
			result.Bytes = stat.Size()
		}
		return result, err
	}
	raw, e := ReadFrame(out)
	if e != nil {
		return fail(e)
	}
	var hello Event
	if e = json.Unmarshal(raw, &hello); e != nil || hello.Kind != "hello" || hello.Runtime != "microphone" || hello.RuntimeCommit != h.RuntimeCommit || hello.ProtocolVersion != 1 || hello.Architecture != runtime.GOARCH {
		return fail(errors.New("microphone hello mismatch"))
	}
	canRecord := false
	for _, capability := range hello.Capabilities {
		if capability == "record" || r.Synthetic && capability == "record_synthetic" {
			canRecord = true
		}
	}
	if !canRecord {
		return fail(errors.New("microphone capability mismatch"))
	}
	if e = WriteFrame(in, map[string]any{"protocolVersion": 1, "requestId": r.RequestID, "kind": "record", "durationMs": r.DurationMS, "maxBytes": r.MaxBytes, "synthetic": r.Synthetic, "requestPermission": r.RequestPermission}); e != nil {
		return fail(e)
	}
	in.Close()
	seq := int64(0)
	previousBytes := int64(0)
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
		e = json.Unmarshal(raw, &ev)
		// The legacy Mac helper omits byte counts from terminal errors.
		if e == nil && ev.Kind == "error" && h.RuntimeCommit == "trove-microphone-v1" && ev.Bytes == 0 {
			ev.Bytes = previousBytes
		}
		if e != nil || ev.RequestID != r.RequestID || ev.Sequence != seq || terminal || ev.Bytes < previousBytes || ev.Bytes > r.MaxBytes || ev.Bytes%4 != 0 {
			return fail(errors.New("invalid microphone event"))
		}
		seq++
		previousBytes = ev.Bytes
		switch ev.Kind {
		case "progress":
			if ev.Bytes > 0 {
				firstPCMOnce.Do(func() { close(firstPCM) })
			}
		case "result":
			terminal = true
			_ = json.Unmarshal(raw, &result)
		case "error":
			_ = json.Unmarshal(raw, &result)
			if onEvent != nil {
				onEvent(raw)
			}
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
	if e != nil || st.Size() != result.Bytes || result.Bytes > r.DurationMS*64 || result.Synthetic != r.Synthetic ||
		result.SampleRate != 16000 || result.Channels != 1 || result.Format != "f32le" {
		return result, errors.New("capture output mismatch")
	}
	samples := make([]byte, 64<<10)
	for offset := int64(0); offset < result.Bytes; {
		count := min(int64(len(samples)), result.Bytes-offset)
		n, err := destination.ReadAt(samples[:count], offset)
		if err != nil || int64(n) != count {
			return result, errors.New("capture bytes unreadable")
		}
		for i := 0; i < n; i += 4 {
			value := float64(math.Float32frombits(binary.LittleEndian.Uint32(samples[i:])))
			if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1.01 {
				return result, errors.New("capture contains invalid samples")
			}
		}
		offset += count
	}
	if errors.Is(context.Cause(run), errCaptureStartup) {
		return result, errCaptureStartup
	}
	return result, nil
}
