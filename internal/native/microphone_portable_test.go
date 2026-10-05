package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func portableMicrophoneFixture(t *testing.T) *Supervisor {
	t.Helper()
	bundle := os.Getenv("TROVE_MICROPHONE_FIXTURE")
	if bundle == "" {
		t.Skip("requires separately compiled device-free microphone fixture")
	}
	raw, err := os.ReadFile(filepath.Join(bundle, "microphone-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Helper                Helper
		TestOnly              bool
		DeviceBackendCompiled bool
	}
	if json.Unmarshal(raw, &manifest) != nil || !manifest.TestOnly || manifest.DeviceBackendCompiled || manifest.Helper.RuntimeCommit != MicrophoneCommit+"-fixture" {
		t.Fatal("not a device-free fixture")
	}
	return &Supervisor{bundle: bundle, helpers: map[string]Helper{"microphone-worker": manifest.Helper}}
}

func TestMicrophoneProductionIdentityExcludesFixtures(t *testing.T) {
	for _, platform := range []string{"darwin", "windows", "linux"} {
		h := Helper{Name: "microphone-worker", Runtime: "microphone", RuntimeCommit: MicrophoneCommit + "-fixture"}
		if validMicrophoneIdentity(h, platform) {
			t.Fatal("synthetic worker accepted as production")
		}
		h.RuntimeCommit = MicrophoneCommit
		if !validMicrophoneIdentity(h, platform) {
			t.Fatal("portable production identity target mismatch")
		}
		h.RuntimeCommit = "trove-microphone-v1"
		if validMicrophoneIdentity(h, platform) != (platform == "darwin") {
			t.Fatal("legacy Mac identity target mismatch")
		}
	}
}

func TestMicrophoneProductionManifestRejectsFixture(t *testing.T) {
	bundle := t.TempDir()
	fixture := Helper{Name: "microphone-worker", Runtime: "microphone", RuntimeCommit: MicrophoneCommit + "-fixture", Architecture: runtime.GOARCH, SHA256: strings.Repeat("0", 64)}
	raw, err := json.Marshal(BundleManifest{SchemaVersion: 2, Platform: runtime.GOOS, Architecture: runtime.GOARCH, Helpers: []Helper{fixture}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bundle, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = NewSupervisor(bundle, nil); err == nil || !strings.Contains(err.Error(), "invalid production microphone identity") {
		t.Fatalf("production loader did not reject test-only runtime: %v", err)
	}
}

func TestPortableMicrophoneSyntheticBoundsAndRetainedCancellation(t *testing.T) {
	s := portableMicrophoneFixture(t)
	for _, cancelAfterProgress := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact_byte_limit", true: "cancel_retains_flushed_bytes"}[cancelAfterProgress], func(t *testing.T) {
			file, cleanup, err := NewPrivateStagingFile()
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			maximum := int64(64 * 100)
			if cancelAfterProgress {
				maximum = 64000 * 5
			}
			events := 0
			result, err := s.Capture(ctx, CaptureRequest{RequestID: NewID(), DurationMS: 5000, MaxBytes: maximum, Synthetic: true}, file, func(raw json.RawMessage) {
				var event struct {
					Kind  string
					Bytes int64
				}
				json.Unmarshal(raw, &event)
				if event.Kind == "progress" {
					stat, e := file.Stat()
					if e != nil || event.Bytes <= 0 || stat.Size() < event.Bytes {
						t.Fatal("reported bytes are not present")
					}
					events++
					if cancelAfterProgress {
						cancel()
					}
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if events == 0 || !result.Synthetic || result.Cancelled != cancelAfterProgress || result.Bytes <= 0 || result.Backend != "synthetic" || result.PermissionState != "not_requested" || result.DeviceState != "not_accessed" || result.SignalObserved == nil || !*result.SignalObserved {
				t.Fatalf("capture semantics: %+v", result)
			}
			if !cancelAfterProgress && result.Bytes != maximum {
				t.Fatalf("byte budget not exact: %d", result.Bytes)
			}
			if cancelAfterProgress && result.Bytes >= maximum {
				t.Fatal("cancelled capture exhausted full budget")
			}
			stat, _ := file.Stat()
			if stat.Size() != result.Bytes {
				t.Fatal("retained recording differs from result")
			}
		})
	}
}

func TestPortableMicrophoneRejectsUnsafeRequestsBeforeCapture(t *testing.T) {
	s := portableMicrophoneFixture(t)
	file, cleanup, err := NewPrivateStagingFile()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, request := range []CaptureRequest{
		{RequestID: NewID(), DurationMS: 100, MaxBytes: 6400, RequestPermission: true},
		{RequestID: NewID(), DurationMS: 100, MaxBytes: 3, Synthetic: true},
		{RequestID: NewID(), DurationMS: 7200001, MaxBytes: 6400, Synthetic: true},
	} {
		if _, err = s.Capture(context.Background(), request, file, nil); err == nil {
			t.Fatal("unsafe capture request accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Capture(ctx, CaptureRequest{RequestID: NewID(), DurationMS: 100, MaxBytes: 6400, Synthetic: true}, file, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	stat, _ := file.Stat()
	if stat.Size() != 0 {
		t.Fatal("invalid capture wrote audio")
	}
}

func TestPortableMicrophoneBufferSelfTest(t *testing.T) {
	s := portableMicrophoneFixture(t)
	// The self-test exists only in fixture builds, and waits for its contained
	// parent's gate before executing. Windows still receives explicit handles.
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	file, cleanup, err := NewPrivateStagingFile()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	dir, err := privateWorkerDirectory("trove-mic-buffer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command(filepath.Join(s.bundle, helperFilename("microphone-worker")), "--self-test")
	cmd.Dir = dir
	cmd.Env = workerEnvironment(dir)
	process, err := prepareWorker(cmd, []*os.File{input, file}, 512<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer process.close()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	var response bytes.Buffer
	cmd.Stdout = &response
	if err = process.start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.stop(true); _ = cmd.Wait() }()
	if _, err = stdin.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	data := response.Bytes()
	frame, err := ReadFrame(bytes.NewReader(data))
	if err != nil || !strings.Contains(string(frame), "PASS") || !strings.Contains(string(frame), "9") {
		t.Fatalf("buffer/permission classification self-test: %s %v", frame, err)
	}
}

func TestPortableMicrophoneRejectsUnknownWireOptions(t *testing.T) {
	s := portableMicrophoneFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	controlR, controlW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer controlR.Close()
	defer controlW.Close()
	file, cleanup, err := NewPrivateStagingFile()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	cmd := exec.CommandContext(ctx, filepath.Join(s.bundle, helperFilename("microphone-worker")))
	dir, err := privateWorkerDirectory("trove-mic-invalid-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	cmd.Dir = dir
	cmd.Env = workerEnvironment(dir)
	p, err := prepareWorker(cmd, []*os.File{controlR, file}, 512<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	cmd.Cancel = func() error { return p.stop(true) }
	if err = p.start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.stop(true); _ = cmd.Wait() }()
	if _, err = ReadFrame(out); err != nil {
		t.Fatal(err)
	}
	// The fixture cannot turn an arbitrary server URL or file path into an input.
	err = WriteFrame(in, map[string]any{"protocolVersion": 1, "requestId": NewID(), "kind": "record", "durationMs": 100, "maxBytes": 6400, "synthetic": true, "requestPermission": false, "server": "tcp:127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	in.Close()
	raw, err := ReadFrame(out)
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if json.Unmarshal(raw, &event) != nil || event.Kind != "error" || event.Code != "INVALID_INPUT" {
		t.Fatalf("unknown option accepted: %s", raw)
	}
	if err = cmd.Wait(); err == nil {
		t.Fatal("invalid worker request exited successfully")
	}
	stat, _ := file.Stat()
	if stat.Size() != 0 {
		t.Fatal("invalid wire request wrote audio")
	}
}

func TestLegacyMacMicrophoneSupervisor(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("legacy helper is Mac-only")
	}
	bundle := os.Getenv("TROVE_MICROPHONE_LEGACY_BUNDLE")
	if bundle == "" {
		t.Skip("requires existing Mac bundle")
	}
	raw, err := os.ReadFile(filepath.Join(bundle, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest BundleManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	var helper Helper
	for _, candidate := range manifest.Helpers {
		if candidate.Name == "microphone-worker" {
			helper = candidate
		}
	}
	s := &Supervisor{bundle: bundle, helpers: map[string]Helper{"microphone-worker": helper}}
	file, cleanup, err := NewPrivateStagingFile()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	result, err := s.Capture(context.Background(), CaptureRequest{RequestID: NewID(), DurationMS: 100, MaxBytes: 6400, Synthetic: true}, file, nil)
	if err != nil || result.Bytes != 6400 || !result.Synthetic || result.Cancelled {
		t.Fatalf("legacy Mac capture changed: %+v %v", result, err)
	}
}

func TestProductionMicrophoneSmokeCannotClaimSyntheticQualification(t *testing.T) {
	s := &Supervisor{helpers: map[string]Helper{"microphone-worker": {RuntimeCommit: MicrophoneCommit}}}
	value, err := s.qualificationMicrophone(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(map[string]any)
	if !ok || result["state"] != "not_run" || result["releaseQualified"] != false {
		t.Fatalf("production smoke claimed a fixture pass: %#v", value)
	}
}

func TestPortableMicrophoneStartupDeadlineAndCancellation(t *testing.T) {
	path := os.Getenv("TROVE_MICROPHONE_STARTUP_FIXTURE")
	if path == "" {
		t.Skip("requires compile-time device-free stalled-startup fixture")
	}
	data, err := os.ReadFile(filepath.Join(path, "microphone-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sidecar struct{ StartupStallFixture bool }
	if json.Unmarshal(data, &sidecar) != nil || !sidecar.StartupStallFixture {
		t.Fatal("not a stalled startup fixture")
	}
	t.Setenv("TROVE_MICROPHONE_FIXTURE", path)
	s := portableMicrophoneFixture(t)
	for _, explicitCancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "user_cancel"}[explicitCancel], func(t *testing.T) {
			file, cleanup, err := NewPrivateStagingFile()
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			startup := 50 * time.Millisecond
			if explicitCancel {
				startup = 30 * time.Second
				time.AfterFunc(50*time.Millisecond, stop)
			}
			began := time.Now()
			result, err := s.captureWithStartupLimit(ctx, CaptureRequest{RequestID: NewID(), DurationMS: 7200000, MaxBytes: 512 << 20, Synthetic: true}, file, nil, startup)
			if !explicitCancel && !errors.Is(err, errCaptureStartup) {
				t.Fatalf("two-hour request startup was not bounded: %+v %v", result, err)
			}
			if explicitCancel && err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("startup cancellation: %v", err)
			}
			if time.Since(began) > time.Second {
				t.Fatal("startup shutdown was not prompt")
			}
			stat, _ := file.Stat()
			if result.Bytes != 0 || stat.Size() != 0 {
				t.Fatal("stalled startup fabricated audio")
			}
		})
	}
}
