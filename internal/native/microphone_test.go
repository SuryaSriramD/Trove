package native

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These tests launch the actual Swift executable. They use only synthetic PCM,
// never request microphone permission, and exercise the real control descriptor.
func TestRealMicrophoneControlCancellationAndParentExit(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires bundled native microphone helper")
	}
	bundle := os.Getenv("TROVE_NATIVE_BUNDLE")
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
			cmd := exec.CommandContext(ctx, filepath.Join(bundle, "microphone-worker"))
			cmd.ExtraFiles = []*os.File{controlR, destination}
			cmd.Env = []string{"PATH=/usr/bin:/bin"}
			in, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
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
