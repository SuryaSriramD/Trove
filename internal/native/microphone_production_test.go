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

// These requests stop before authorizationStatus/requestAccess/device setup.
// Real permission and capture are deliberately absent from this test.
func TestProductionMicrophoneRejectsBeforePermissionOrDevice(t *testing.T) {
	bundle := os.Getenv("TROVE_MICROPHONE_PRODUCTION")
	if bundle == "" {
		t.Skip("requires a production helper; no device use")
	}
	for _, synthetic := range []bool{false, true} {
		t.Run(map[bool]string{false: "unapproved", true: "synthetic_forbidden"}[synthetic], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
			dir, err := privateWorkerDirectory("trove-production-mic-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			cmd := exec.CommandContext(ctx, filepath.Join(bundle, helperFilename("microphone-worker")))
			cmd.Dir = dir
			cmd.Env = workerEnvironment(dir)
			process, err := prepareWorker(cmd, []*os.File{controlR, file}, 512<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer process.close()
			cmd.Cancel = func() error { return process.stop(true) }
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			output, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			if err = process.start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = process.stop(true); _ = cmd.Wait() }()
			raw, err := ReadFrame(output)
			if err != nil {
				t.Fatal(err)
			}
			var hello Event
			if json.Unmarshal(raw, &hello) != nil || hello.RuntimeCommit != MicrophoneCommit || hello.Kind != "hello" || len(hello.Capabilities) != 1 || hello.Capabilities[0] != "record" {
				t.Fatalf("production identity/capability: %s", raw)
			}
			err = WriteFrame(input, map[string]any{"protocolVersion": 1, "requestId": NewID(), "kind": "record", "durationMs": 100, "maxBytes": 6400, "synthetic": synthetic, "requestPermission": false})
			if err != nil {
				t.Fatal(err)
			}
			input.Close()
			raw, err = ReadFrame(output)
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Kind, Code, PermissionState, DeviceState string
				Bytes                                    int64
			}
			code := "APPROVAL_REQUIRED"
			if synthetic {
				code = "INVALID_INPUT"
			}
			if json.Unmarshal(raw, &event) != nil || event.Kind != "error" || event.Code != code || event.PermissionState != "not_requested" || event.DeviceState != "not_accessed" || event.Bytes != 0 {
				t.Fatalf("unexpected device/permission work: %s", raw)
			}
			if err = cmd.Wait(); err == nil {
				t.Fatal("invalid request succeeded")
			}
			stat, _ := file.Stat()
			if stat.Size() != 0 {
				t.Fatal("invalid request wrote audio")
			}
		})
	}
}
