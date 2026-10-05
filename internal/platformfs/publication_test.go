package platformfs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func publicationDirectory(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "managed")
	if err := EnsurePrivateDirectory(p); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestPublicationExactBytesAndReplacement(t *testing.T) {
	dir := publicationDirectory(t)
	from, to := filepath.Join(dir, "staged"), filepath.Join(dir, "published")
	payload := []byte("preserve CRLF\r\n and résumé 🦉 " + string([]byte{0, 1, 255}))
	if e := os.WriteFile(from, payload, 0600); e != nil {
		t.Fatal(e)
	}
	if e := PublishFile(from, to, false); e != nil {
		t.Fatal(e)
	}
	if got, e := os.ReadFile(to); e != nil || !bytes.Equal(got, payload) {
		t.Fatalf("published bytes: %q %v", got, e)
	}
	if _, e := os.Stat(from); !os.IsNotExist(e) {
		t.Fatal("source name retained", e)
	}
	if e := os.WriteFile(from, []byte("reviewed replacement"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := PublishFile(from, to, true); e != nil {
		t.Fatal(e)
	}
	if got, e := os.ReadFile(to); e != nil || string(got) != "reviewed replacement" {
		t.Fatal(string(got), e)
	}
}
func TestPublicationNoReplaceRaceRetainsLoserAndExistingBytes(t *testing.T) {
	dir := publicationDirectory(t)
	target := filepath.Join(dir, "winner")
	sources := []string{filepath.Join(dir, "first"), filepath.Join(dir, "second")}
	for i, p := range sources {
		if e := os.WriteFile(p, []byte{byte('A' + i)}, 0600); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	gate := make(chan struct{})
	results := make([]error, 2)
	for i := range sources {
		wg.Add(1)
		go func(i int) { defer wg.Done(); <-gate; results[i] = PublishFile(sources[i], target, false) }(i)
	}
	close(gate)
	wg.Wait()
	winner := -1
	for i, e := range results {
		if e == nil {
			if winner >= 0 {
				t.Fatal("both no-replace publishers succeeded")
			}
			winner = i
		} else if !os.IsExist(e) {
			t.Fatal(e)
		}
	}
	if winner < 0 {
		t.Fatal("no winner")
	}
	loser := 1 - winner
	for path, expected := range map[string]byte{target: byte('A' + winner), sources[loser]: byte('A' + loser)} {
		b, e := os.ReadFile(path)
		if e != nil || len(b) != 1 || b[0] != expected {
			t.Fatalf("data replaced/lost %s: %q %v", path, b, e)
		}
	}
}
func TestPublicationRejectsUnsafeObjectsWithoutChangingTarget(t *testing.T) {
	dir := publicationDirectory(t)
	source, target := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	if e := os.WriteFile(source, []byte("new"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(target, 0700); e != nil {
		t.Fatal(e)
	}
	if e := PublishFile(source, target, true); e == nil {
		t.Fatal("directory target replaced")
	}
	if e := PublishFile(source, source, true); e == nil {
		t.Fatal("self publication accepted")
	}
	if e := PublishFile(dir, filepath.Join(dir, "child"), false); e == nil {
		t.Fatal("directory source accepted")
	}
	if b, e := os.ReadFile(source); e != nil || string(b) != "new" {
		t.Fatal("source lost", e)
	}
}
func TestPublicationSymlinkRefusal(t *testing.T) {
	dir := publicationDirectory(t)
	victim := filepath.Join(dir, "victim")
	source := filepath.Join(dir, "source")
	link := filepath.Join(dir, "link")
	os.WriteFile(victim, []byte("retained"), 0600)
	os.WriteFile(source, []byte("new"), 0600)
	if e := os.Symlink(victim, link); e != nil {
		t.Skipf("test symlink unavailable: %v", e)
	}
	if e := PublishFile(link, filepath.Join(dir, "destination"), false); e == nil {
		t.Fatal("source symlink followed")
	}
	if e := PublishFile(source, link, true); e == nil {
		t.Fatal("target symlink replaced/followed")
	}
	if b, e := os.ReadFile(victim); e != nil || string(b) != "retained" {
		t.Fatal("victim changed", e)
	}
}
func TestRecoveryDirectoryCapabilityIsExplicit(t *testing.T) {
	dir := publicationDirectory(t)
	if runtime.GOOS == "windows" {
		if !errors.Is(SyncDirectory(dir), ErrDirectoryDurability) || !errors.Is(RequireRecoveryDurability(), ErrDirectoryDurability) {
			t.Fatal("unsupported directory barrier became success")
		}
	} else if e := SyncDirectory(dir); e != nil {
		t.Fatal(e)
	}
}
