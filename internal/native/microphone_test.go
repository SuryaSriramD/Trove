package native

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// These tests launch the legacy Mac worker or separate portable fixture. They use only synthetic PCM,
// never request microphone permission, and exercise the real control descriptor.
func TestRealMicrophoneControlCancellationAndParentExit(t *testing.T) {
	if !supportedNativeTarget(runtime.GOOS, runtime.GOARCH) {
		t.Skip("unsupported microphone helper on this target")
	}
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires bundled native microphone helper")
	}
	bundle := os.Getenv("TROVE_MICROPHONE_FIXTURE")
	if bundle == "" {
		bundle = os.Getenv("TROVE_NATIVE_BUNDLE")
	}
	if bundle == "" {
		bundle, _ = filepath.Abs("../../build/native")
	}
	for _, action := range []string{"cancel", "parent_exit"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			controlR, controlW, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer controlR.Close()
			defer controlW.Close()
			destination, err := os.CreateTemp(t.TempDir(), "staged-audio-")
			if err != nil {
				t.Fatal(err)
			}
			defer destination.Close()
			cmd := exec.CommandContext(ctx, filepath.Join(bundle, helperFilename("microphone-worker")))
			directory, err := privateWorkerDirectory("trove-mic-control-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(directory)
			cmd.Dir = directory
			cmd.Env = workerEnvironment(directory)
			process, err := prepareWorker(cmd, []*os.File{controlR, destination}, 512<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer process.close()
			cmd.Cancel = func() error { return process.stop(true) }
			in, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = process.start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = process.stop(true); _ = cmd.Wait() }()
			controlR.Close()
			hello, err := ReadFrame(out)
			if err != nil {
				t.Fatal(err)
			}
			var event Event
			if err = json.Unmarshal(hello, &event); err != nil || event.Kind != "hello" || event.Runtime != "microphone" {
				t.Fatalf("invalid hello: %s %v", hello, err)
			}
			requestID := NewID()
			const maximum = int64(16000 * 4 * 5)
			if err = WriteFrame(in, map[string]any{"protocolVersion": 1, "requestId": requestID, "kind": "record", "durationMs": 5000, "maxBytes": maximum, "synthetic": true, "requestPermission": false}); err != nil {
				t.Fatal(err)
			}
			in.Close()
			progress, err := ReadFrame(out)
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(progress, &event); err != nil || event.Kind != "progress" {
				t.Fatalf("missing durable audio progress: %s %v", progress, err)
			}
			interrupted := time.Now()
			if action == "parent_exit" {
				// Closing the parent's final writer is the exact EOF a crashed
				// desktop process leaves on the worker's inherited descriptor.
				if err = controlW.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err = WriteFrame(controlW, map[string]any{"kind": "cancel", "requestId": requestID}); err != nil {
				t.Fatal(err)
			}
			var result CaptureResult
			for {
				raw, err := ReadFrame(out)
				if err != nil {
					t.Fatalf("capture ended before retaining a result: %v", err)
				}
				if err = json.Unmarshal(raw, &event); err != nil {
					t.Fatal(err)
				}
				if event.Kind == "result" {
					if err = json.Unmarshal(raw, &result); err != nil {
						t.Fatal(err)
					}
					break
				}
				if event.Kind != "progress" {
					t.Fatalf("unexpected event: %s", raw)
				}
			}
			if err = cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			if elapsed := time.Since(interrupted); elapsed > 2*time.Second {
				t.Fatalf("helper did not stop promptly: %s", elapsed)
			}
			stat, err := destination.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if !result.Cancelled || !result.Synthetic || result.Bytes == 0 || result.Bytes >= maximum || result.Bytes != stat.Size() || result.Format != "f32le" || result.SampleRate != 16000 || result.Channels != 1 {
				t.Fatalf("interrupted audio was not retained: %+v size=%d", result, stat.Size())
			}
		})
	}
}
