package native

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDocumentLinuxCapabilityRefusesBeforeLookupStagingOrIO(t *testing.T) {
	private := t.TempDir()
	t.Setenv("TMPDIR", private)
	t.Setenv("TROVE_DOCUMENT_WORKER", filepath.Join(private, "must-not-inspect"))
	t.Setenv("TROVE_NATIVE_BUNDLE", filepath.Join(private, "absent-bundle"))
	if h, err := findDocumentHelperForPlatform(context.Background(), "linux", "amd64"); h != nil || !errors.Is(err, ErrDocumentUnavailable) {
		t.Fatalf("Linux lookup crossed unavailable gate: %v %v", h, err)
	}
	// Nil objects and writer are intentional: unavailable Linux cannot dereference
	// a helper, stage a profile/resource map, read a source, or invoke output.
	var helper *DocumentHelper
	if job, err := helper.newJobForPlatform(context.Background(), "extract", "linux"); job != nil || !errors.Is(err, ErrDocumentUnavailable) {
		t.Fatalf("Linux staging crossed unavailable gate: %v %v", job, err)
	}
	var job *DocumentJob
	if err := job.runForPlatform(context.Background(), nil, "linux"); !errors.Is(err, ErrDocumentUnavailable) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(private)
	if err != nil || len(entries) != 0 {
		t.Fatal("unavailable capability touched private staging", entries, err)
	}
	for _, platform := range []string{"darwin", "windows"} {
		if err := documentPlatformCapability(platform); err != nil {
			t.Fatalf("existing %s route was disabled: %v", platform, err)
		}
	}
}

// A production Linux entry cannot bypass lookup or staging via a retained job.
func TestDocumentPlatformEntryBeforeProcessLaunch(t *testing.T) {
	if documentPlatformCapability(runtime.GOOS) == nil {
		return
	}
	private := t.TempDir()
	err := runDocumentProcess(context.Background(), filepath.Join(private, "never-execute"), "extract", private, private, io.Discard)
	if !errors.Is(err, ErrDocumentUnavailable) {
		t.Fatalf("process launch preceded availability check: %v", err)
	}
	entries, err := os.ReadDir(private)
	if err != nil || len(entries) != 0 {
		t.Fatal("unavailable process mutated staging", entries, err)
	}
}
