package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"trove.local/phase0/internal/platformfs"
)

// Test tooling only. A closed synthetic benchmark workspace is copied after
// every timed sample, never while SQLite/diagnostic writers still own it.
// Preserve failures and refuse existing destinations rather than replacing data.
func retainDatabaseBenchmarkFixture(k *Kernel, destination string) (string, error) {
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return "", fmt.Errorf("retained fixture requires an absolute clean path")
	}
	if relative, err := filepath.Rel(k.Path(), destination); err != nil || relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("retained fixture must be outside the source workspace")
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return "", err
	}
	root, err := os.Open(destination)
	if err != nil {
		return "", err
	}
	err = platformfs.AuditPrivateHandle(root, true)
	root.Close()
	if err != nil {
		return "", err
	}
	if err = k.Close(); err != nil {
		return "", err
	}
	before, err := databaseBenchmarkFixtureDigest(k.Path())
	if err != nil {
		return "", err
	}
	if err = os.CopyFS(destination, os.DirFS(k.Path())); err != nil {
		return "", err
	}
	if err = filepath.WalkDir(destination, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		mode := fs.FileMode(0600)
		if entry.IsDir() {
			mode = 0700
		}
		return os.Chmod(path, mode)
	}); err != nil {
		return "", err
	}
	after, err := databaseBenchmarkFixtureDigest(k.Path())
	if err != nil || before != after {
		return "", fmt.Errorf("closed fixture changed during retention")
	}
	copied, err := databaseBenchmarkFixtureDigest(destination)
	if err != nil || copied != before {
		return "", fmt.Errorf("retained fixture bytes differ")
	}
	return copied, nil
}

func databaseBenchmarkFixtureDigest(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("retained fixture contains a link or special object")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%d:%s:%t:", len(relative), relative, info.IsDir())
		if info.IsDir() {
			return nil
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		content := sha256.New()
		_, err = io.Copy(content, source)
		source.Close()
		if err != nil {
			return err
		}
		h.Write(content.Sum(nil))
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

func TestDatabaseBenchmarkRetainedFixtureReopensWithoutChangingValues(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Retained café తెలుగు 🦉")
	testOK(t, setValue(t, k, e, f, record, page, f.status, "status", f.done))
	before := testOK(t, queryDB(t, k, e, f.collection, "", 200, ""))
	destination := filepath.Join(t.TempDir(), "retained")
	if _, err := retainDatabaseBenchmarkFixture(k, destination); err != nil {
		t.Fatal(err)
	}
	sourceDigest, err := databaseBenchmarkFixtureDigest(k.Path())
	if err != nil {
		t.Fatal(err)
	}
	copyDigest, err := databaseBenchmarkFixtureDigest(destination)
	if err != nil || sourceDigest != copyDigest {
		t.Fatal("closed bytes differ", err)
	}
	reopened, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after := testOK(t, queryDB(t, reopened, reopened.OwnerContext(), f.collection, "", 200, ""))
	if string(marshal(before["records"])) != string(marshal(after["records"])) || string(marshal(before["recordIds"])) != string(marshal(after["recordIds"])) {
		t.Fatal("retained canonical identities/typed values differ")
	}
	if _, err := retainDatabaseBenchmarkFixture(reopened, destination); err == nil {
		t.Fatal("existing fixture overwritten")
	}
	testOK(t, queryDB(t, reopened, reopened.OwnerContext(), f.collection, "", 200, ""))
	if digest, err := databaseBenchmarkFixtureDigest(k.Path()); err != nil || digest != sourceDigest {
		t.Fatal("reopening copy changed source", err)
	}
}

func TestDatabaseBenchmarkRetentionRefusesLinkedContent(t *testing.T) {
	k, _ := testKernel(t)
	if err := os.Symlink("workspace.db", filepath.Join(k.Path(), "unsafe-link")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "retained")
	if _, err := retainDatabaseBenchmarkFixture(k, destination); err == nil {
		t.Fatal("linked source retained")
	}
	if _, err := os.Lstat(filepath.Join(destination, "unsafe-link")); !os.IsNotExist(err) {
		t.Fatal("linked source copied", err)
	}
}
