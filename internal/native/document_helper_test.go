package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func documentDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func documentFixture(t *testing.T) *DocumentHelper {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "fixture")
	os.WriteFile(path, []byte("test executable bytes"), 0700)
	assets := map[string]DocumentResource{}
	for _, name := range []string{"print.html", "extract.html", "validate.html", "assets/script.js"} {
		raw := []byte("fixture " + name)
		dest := filepath.Join(root, "document-assets", name)
		os.MkdirAll(filepath.Dir(dest), 0700)
		os.WriteFile(dest, raw, 0600)
		assets[name] = DocumentResource{SHA256: documentDigest(raw), Size: int64(len(raw))}
	}
	return &DocumentHelper{path: path, bundle: root, manifest: documentManifest{SHA256: documentDigest([]byte("test executable bytes")), Assets: assets}}
}

// Fixture staging tests exercise the platform-independent inventory separately
// from production capability discovery. The explicit Darwin selector cannot
// enable Linux execution, whose entry point independently refuses it.
func TestDocumentPrivateStagingHashesModeAndSourceRetention(t *testing.T) {
	for _, mode := range []string{"extract", "render"} {
		t.Run(mode, func(t *testing.T) {
			helper := documentFixture(t)
			job, err := helper.newJobForPlatform(context.Background(), mode, "darwin")
			if err != nil {
				t.Fatal(err)
			}
			defer job.Close()
			original := []byte("%PDF-independent retained input")
			if err = job.AddFile(context.Background(), "input.pdf", bytes.NewReader(original), 128<<20, documentDigest(original)); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(filepath.Join(job.root, "input.pdf"))
			if !bytes.Equal(got, original) {
				t.Fatal("input altered")
			}
			absent := "extract.html"
			if mode == "extract" {
				absent = "print.html"
			}
			if _, err = os.Stat(filepath.Join(job.root, absent)); !os.IsNotExist(err) {
				t.Fatal("other mode leaked into map")
			}
			private := job.private
			if err = job.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(private); !os.IsNotExist(err) {
				t.Fatal("profile remained")
			}
		})
	}
}
func TestDocumentStagingRejectsChangedAssetsBoundsAndCancellation(t *testing.T) {
	helper := documentFixture(t)
	os.WriteFile(filepath.Join(helper.bundle, "document-assets", "print.html"), []byte("tampered"), 0600)
	if job, err := helper.newJobForPlatform(context.Background(), "render", "darwin"); err == nil {
		job.Close()
		t.Fatal("changed asset accepted")
	}
	helper = documentFixture(t)
	job, err := helper.newJobForPlatform(context.Background(), "extract", "darwin")
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	for _, name := range []string{"../outside", "/absolute", "assets/CON.js", "assets/a\\b", "assets/a//b"} {
		if err = job.AddFile(context.Background(), name, strings.NewReader("x"), 1, ""); err == nil {
			t.Fatal("unsafe path", name)
		}
	}
	if err = job.AddFile(context.Background(), "too-large.pdf", strings.NewReader("large"), 4, ""); err == nil {
		t.Fatal("size overflow accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = job.AddFile(ctx, "cancelled.pdf", strings.NewReader("private"), 10, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, ok := job.files["cancelled.pdf"]; ok {
		t.Fatal("cancelled content published")
	}
}
func TestDocumentManifestBindsTargetAndPolicy(t *testing.T) {
	helper := documentFixture(t)
	no := false
	good := documentManifest{SchemaVersion: 1, Name: "document-worker", OS: "linux", Architecture: "amd64", Runtime: documentRuntime("linux"), SHA256: strings.Repeat("a", 64), TestOnly: &no, WebExtensionSHA256: strings.Repeat("b", 64), Assets: helper.manifest.Assets}
	path := filepath.Join(t.TempDir(), "manifest.json")
	for _, kind := range []string{"valid", "test", "runtime", "extension", "os", "missing", "escape", "collision"} {
		t.Run(kind, func(t *testing.T) {
			raw, _ := json.Marshal(good)
			var m documentManifest
			json.Unmarshal(raw, &m)
			switch kind {
			case "test":
				yes := true
				m.TestOnly = &yes
			case "runtime":
				m.Runtime = "floating"
			case "extension":
				m.WebExtensionSHA256 = ""
			case "os":
				m.OS = "windows"
			case "missing":
				delete(m.Assets, "print.html")
			case "escape":
				m.Assets["../private"] = DocumentResource{SHA256: strings.Repeat("a", 64)}
			case "collision":
				m.Assets["assets/SCRIPT.js"] = m.Assets["assets/script.js"]
			}
			raw, _ = json.Marshal(m)
			os.WriteFile(path, raw, 0600)
			_, err := readDocumentManifest(context.Background(), path, "linux", "amd64")
			if (err == nil) != (kind == "valid") {
				t.Fatal(kind, err)
			}
		})
	}
}

// Test-only subprocess mode is entered before the testing flags are parsed. It
// uses the real process/job boundary and never enables a production bypass.
func init() {
	if len(os.Args) != 3 || os.Args[1] != "trove-document-test-child" {
		return
	}
	var gate [1]byte
	if _, err := io.ReadFull(os.Stdin, gate[:]); err != nil || gate[0] != 1 {
		os.Exit(9)
	}
	if err := os.WriteFile(filepath.Join(os.Args[2], "started"), []byte("started"), 0600); err != nil {
		os.Exit(10)
	}
	for {
		time.Sleep(time.Second)
	}
}
func TestDocumentProcessCancellationAndOutputBound(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux document execution intentionally unavailable; availability denial has a separate regression")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	private, err := newDocumentDirectory()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(private)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runDocumentProcess(ctx, executable, "trove-document-test-child", private, private, io.Discard)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err = os.Stat(filepath.Join(private, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("helper did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("cancelled process was not reaped")
	}
	var out bytes.Buffer
	bounded := documentOutput{w: &out, remaining: 4}
	if _, err = bounded.Write([]byte("large")); err == nil || out.Len() != 0 {
		t.Fatal("partial oversized output accepted")
	}
	// Restricted environment is separately inspected rather than relying on a model helper.
	t.Setenv("TROVE_SECRET", "do not pass")
	t.Setenv("HTTP_PROXY", "do not pass")
	command := exec.Command(executable)
	command.Env = documentEnvironment(private)
	for _, value := range command.Env {
		if strings.Contains(value, "do not pass") {
			t.Fatal("ambient secret inherited")
		}
	}
}

func TestDocumentUnverifiedProcessExitRetainsProfile(t *testing.T) {
	helper := documentFixture(t)
	job, err := helper.newJobForPlatform(context.Background(), "extract", "darwin")
	if err != nil {
		t.Fatal(err)
	}
	private := job.private
	defer os.RemoveAll(private)
	marker := filepath.Join(private, "webview-profile", "locked-private-content")
	os.WriteFile(marker, []byte("still in use"), 0600)
	original := errors.Join(context.Canceled, errDocumentCleanupUnverified)
	if !errors.Is(job.recordProcessResult(original), context.Canceled) {
		t.Fatal("cancellation lost")
	}
	if err = job.Close(); err == nil {
		t.Fatal("unverified cleanup claimed success")
	}
	if bytes, err := os.ReadFile(marker); err != nil || string(bytes) != "still in use" {
		t.Fatal("unverified live profile was removed", err)
	}
}

func TestDocumentFailedMemoryMonitorStopsActiveProcess(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux document execution intentionally unavailable; availability denial has a separate regression")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	private, err := newDocumentDirectory()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(private)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	measured := false
	err = runDocumentProcessWithMonitor(ctx, executable, "trove-document-test-child", private, private, io.Discard, func(*workerProcess) (int64, error) {
		if _, er := os.Stat(filepath.Join(private, "started")); er != nil {
			t.Error("measurement occurred before test process started", er)
		}
		measured = true
		return 0, errors.New("OS memory measurement unavailable")
	})
	if !measured || err == nil || !strings.Contains(err.Error(), "usage could not be verified") || ctx.Err() != nil {
		t.Fatal("failed memory bound did not stop active process", measured, err, ctx.Err())
	}
}
