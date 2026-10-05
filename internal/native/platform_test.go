package native

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func TestBundleTargetBinding(t *testing.T) {
	for _, c := range []struct {
		name, os, arch string
		manifest       BundleManifest
		valid          bool
	}{
		{"legacy-mac", "darwin", "arm64", BundleManifest{SchemaVersion: 1}, true},
		{"legacy-windows", "windows", "amd64", BundleManifest{SchemaVersion: 1}, false},
		{"legacy-linux", "linux", "amd64", BundleManifest{SchemaVersion: 1}, false},
		{"windows", "windows", "amd64", BundleManifest{SchemaVersion: 2, Platform: "windows", Architecture: "amd64"}, true},
		{"linux", "linux", "amd64", BundleManifest{SchemaVersion: 2, Platform: "linux", Architecture: "amd64"}, true},
		{"wrong-os", "linux", "amd64", BundleManifest{SchemaVersion: 2, Platform: "windows", Architecture: "amd64"}, false},
		{"wrong-isa", "windows", "amd64", BundleManifest{SchemaVersion: 2, Platform: "windows", Architecture: "arm64"}, false},
		{"unsupported", "linux", "arm64", BundleManifest{SchemaVersion: 2, Platform: "linux", Architecture: "arm64"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := validateBundleTarget(c.manifest, c.os, c.arch); (err == nil) != c.valid {
				t.Fatalf("validation: %v, expected valid %v", err, c.valid)
			}
		})
	}
}
func TestPortablePackAndHelloArchitecture(t *testing.T) {
	p := fixturePack([]byte("GGUFportable"))
	p.Architecture = "portable"
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"arm64", "amd64", "x64", "portable"} {
		hello := map[string]any{"kind": "hello", "protocolVersion": 1, "runtime": "llama.cpp", "runtimeCommit": LlamaCommit, "architecture": arch, "capabilities": []string{"generate"}}
		raw, _ := json.Marshal(hello)
		err := contracts.Validate("jobs", "WorkerOutput", raw)
		if (err == nil) != (arch == "arm64" || arch == "amd64") {
			t.Fatalf("hello architecture %q: %v", arch, err)
		}
	}
}

// Executed only as the explicitly selected, isolated subprocess below.
func TestNativeWorkerProcessChild(t *testing.T) {
	mode := os.Getenv("TROVE_NATIVE_TEST_CHILD")
	if mode == "" {
		return
	}
	// Mirror the hello/request barrier: do not consume inputs before assignment.
	var gate [1]byte
	if _, err := io.ReadFull(os.Stdin, gate[:]); err != nil {
		os.Exit(21)
	}
	if os.Getenv("TROVE_NATIVE_TEST_SECRET") != "" {
		os.Exit(22)
	}
	slot := func(n int) *os.File {
		fd := uintptr(n)
		if runtime.GOOS == "windows" {
			values := strings.Split(os.Getenv("TROVE_WORKER_HANDLES"), ",")
			value, err := strconv.ParseUint(values[n-3], 10, 64)
			if err != nil {
				os.Exit(23)
			}
			fd = uintptr(value)
		}
		return os.NewFile(fd, "approved")
	}
	data, err := io.ReadAll(slot(5))
	if err != nil {
		os.Exit(24)
	}
	if _, err = slot(3).Write(data); err != nil {
		os.Exit(25)
	}
	fmt.Fprintln(os.Stdout, "ready")
	if mode == "cooperative" {
		frame, err := ReadFrame(slot(4))
		if err != nil || !bytes.Contains(frame, []byte("\"cancel\"")) {
			os.Exit(26)
		}
		os.Exit(0)
	}
	for {
		time.Sleep(time.Hour)
	}
}
func TestNativeWorkerDescriptorTransportAndCancellation(t *testing.T) {
	t.Setenv("TROVE_NATIVE_TEST_SECRET", "must-not-be-inherited")
	for _, mode := range []string{"cooperative", "forced"} {
		t.Run(mode, func(t *testing.T) {
			dir, err := privateWorkerDirectory("trove-process-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			inputPath := filepath.Join(dir, "input")
			expected := []byte{0, 1, 10, 13, 26, 128, 255, 0}
			if err = os.WriteFile(inputPath, expected, 0600); err != nil {
				t.Fatal(err)
			}
			input, err := os.Open(inputPath)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			result, err := os.Create(filepath.Join(dir, "result"))
			if err != nil {
				t.Fatal(err)
			}
			defer result.Close()
			controlR, controlW, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer controlR.Close()
			defer controlW.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "-test.run=^TestNativeWorkerProcessChild$")
			cmd.Dir = dir
			cmd.Env = append(workerEnvironment(dir), "TROVE_NATIVE_TEST_CHILD="+mode)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			process, err := prepareWorker(cmd, []*os.File{result, controlR, input}, 512<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer process.close()
			if err = process.start(); err != nil {
				t.Fatal(err)
			}
			done := false
			defer func() {
				if !done {
					_ = process.stop(true)
					_ = cmd.Wait()
				}
			}()
			if _, err = stdin.Write([]byte{1}); err != nil {
				t.Fatal(err)
			}
			stdin.Close()
			ready := make(chan string, 1)
			go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
			select {
			case line := <-ready:
				if line != "ready\n" {
					t.Fatalf("child output: %q", line)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("child not ready")
			}
			actual := make([]byte, len(expected))
			if _, err = result.ReadAt(actual, 0); err != nil || !bytes.Equal(actual, expected) {
				t.Fatalf("binary descriptor bytes changed: %x %v", actual, err)
			}
			if rss, err := process.rss(); err != nil || rss <= 0 {
				t.Fatalf("worker RSS unavailable: %d %v", rss, err)
			}
			if mode == "cooperative" {
				if err = WriteFrame(controlW, map[string]string{"kind": "cancel"}); err != nil {
					t.Fatal(err)
				}
			} else if err = process.stop(true); err != nil {
				t.Fatal(err)
			}
			waited := make(chan error, 1)
			go func() { waited <- cmd.Wait() }()
			select {
			case err := <-waited:
				done = true
				if (err == nil) != (mode == "cooperative") {
					t.Fatalf("child result: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("worker not reaped")
			}
		})
	}
}
func TestNativeWorkerStartFailure(t *testing.T) {
	dir, err := privateWorkerDirectory("trove-start-failure-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command(filepath.Join(dir, helperFilename("does-not-exist")))
	cmd.Env = workerEnvironment(dir)
	f, err := os.Create(filepath.Join(dir, "input"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	process, err := prepareWorker(cmd, []*os.File{f}, 512<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer process.close()
	if err = process.start(); err == nil {
		t.Fatal("missing helper unexpectedly started")
	}
}
