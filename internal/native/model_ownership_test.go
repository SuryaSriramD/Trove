package native

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type heldModelReader struct {
	data    *bytes.Reader
	ready   chan struct{}
	proceed chan struct{}
	held    bool
}

func (r *heldModelReader) Read(p []byte) (int, error) {
	if !r.held {
		r.held = true
		close(r.ready)
		<-r.proceed
	}
	return r.data.Read(p)
}
func TestModelAcquisitionOwnershipSurvivesAnotherManager(t *testing.T) {
	data := []byte("GGUFownership fixture")
	p := fixturePack(data)
	root := t.TempDir()
	first, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	input := &heldModelReader{data: bytes.NewReader(data), ready: make(chan struct{}), proceed: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- first.Import(context.Background(), p, input, nil) }()
	defer func() {
		close(input.proceed)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("acquisition did not release")
		}
	}()
	select {
	case <-input.ready:
	case <-time.After(time.Second):
		t.Fatal("acquisition did not begin")
	}
	if err = second.Remove(p.ID); err == nil {
		t.Fatal("another manager removed an active acquisition")
	}
	if err = second.Import(context.Background(), p, bytes.NewReader(data), nil); err == nil {
		t.Fatal("another manager overwrote an active acquisition")
	}
}
func TestModelLeaseOwnershipSurvivesAnotherManager(t *testing.T) {
	data := []byte("GGUFlease ownership")
	p := fixturePack(data)
	root := t.TempDir()
	first, _ := NewManager(root)
	second, _ := NewManager(root)
	if err := first.Import(context.Background(), p, bytes.NewReader(data), nil); err != nil {
		t.Fatal(err)
	}
	_, f, release, err := first.lease(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = second.Remove(p.ID); err == nil {
		release()
		t.Fatal("another manager removed a leased model")
	}
	if _, err = f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("leased bytes changed", err)
	}
	release()
	if err = second.Remove(p.ID); err != nil {
		t.Fatal("released model remained unavailable", err)
	}
}

// Ownership inodes intentionally survive release and removal: unlinking a lock
// allows a waiter on the old inode and a new opener to own the same pack at once.
func assertNoModelPayload(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".owner.lock")
		info, err := os.Lstat(filepath.Join(root, entry.Name()))
		if !ok || !uuidPattern.MatchString(id) || err != nil || !info.Mode().IsRegular() || info.Size() != 0 || info.Mode().Perm() != 0600 {
			t.Fatalf("unexpected retained model payload/control file %s: %v", entry.Name(), err)
		}
	}
}
func TestModelOwnershipPersistsUntilLastLocalLease(t *testing.T) {
	data := []byte("GGUFmultiple leases")
	p := fixturePack(data)
	root := t.TempDir()
	first, _ := NewManager(root)
	second, _ := NewManager(root)
	if err := first.Import(context.Background(), p, bytes.NewReader(data), nil); err != nil {
		t.Fatal(err)
	}
	_, _, releaseFirst, err := first.lease(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFirst()
	_, _, releaseLast, err := first.lease(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLast()
	releaseFirst()
	if err := second.Remove(p.ID); err == nil {
		t.Fatal("released ownership before final local lease")
	}
	releaseLast()
	releaseLast()
	if err := second.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	assertNoModelPayload(t, root)
}
func TestVisionCompanionOwnershipSurvivesAnotherManager(t *testing.T) {
	model, projector := []byte("GGUFmodel"), []byte("GGUFprojector")
	p := visionFixture(model, projector)
	root := t.TempDir()
	first, _ := NewManager(root)
	second, _ := NewManager(root)
	if err := first.ImportVision(context.Background(), p, bytes.NewReader(model), bytes.NewReader(projector), nil); err != nil {
		t.Fatal(err)
	}
	_, release, err := first.leaseProjector(p)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := second.Remove(p.ID); err == nil {
		t.Fatal("removed externally leased companion")
	}
	final, _, _ := first.paths(p)
	if err := verifyFile(final, p); err != nil {
		t.Fatal("failed companion removal changed parent", err)
	}
	release()
	if err := second.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	assertNoModelPayload(t, root)
}
