package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func sampleIdentity(build uint64) Identity {
	return Identity{BundleID: BundleID, TeamID: "ABCDEFGHIJ", Version: "1.0.0", Build: build, UserVersion: 3, StorageGeneration: 1, Architecture: "arm64", MinimumOS: "14.0"}
}
func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

// The test verifier reads identity from fixture bytes. It is deliberately not a
// platform signature substitute; macOS publisher authentication is tested separately.
type fixtureVerifier struct{}

func (fixtureVerifier) Inspect(ctx context.Context, path string) (Application, error) {
	var app Application
	raw, err := os.ReadFile(filepath.Join(path, "Contents", "identity.json"))
	if err != nil {
		return app, err
	}
	if err = json.Unmarshal(raw, &app.Identity); err != nil {
		return app, err
	}
	app.Inventory, err = InspectTree(ctx, path)
	return app, err
}
func fixtureApp(t *testing.T, parent, name string, build uint64) (string, Application) {
	t.Helper()
	path := filepath.Join(parent, name)
	raw, _ := json.Marshal(sampleIdentity(build))
	write(t, filepath.Join(path, "Contents", "identity.json"), raw)
	write(t, filepath.Join(path, "Contents", "MacOS", "Trove"), []byte(name))
	if err := os.Chmod(filepath.Join(path, "Contents", "MacOS", "Trove"), 0700); err != nil {
		t.Fatal(err)
	}
	app, err := (fixtureVerifier{}).Inspect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return path, app
}

var fixtureSnapshot = Snapshot{WorkspaceID: "workspace-independent", BackupID: "backup-independent", ManifestSHA256: digest([]byte("consistent snapshot")), UserVersion: 3, StorageGeneration: 1}

func fixtureBackup(ctx context.Context, path string) (Snapshot, error) {
	if err := os.Mkdir(path, 0700); err != nil {
		return Snapshot{}, err
	}
	err := os.WriteFile(filepath.Join(path, "snapshot"), []byte("consistent snapshot"), 0600)
	return fixtureSnapshot, err
}
func fixtureVerify(ctx context.Context, path string, expected Snapshot) error {
	if expected != fixtureSnapshot {
		return errors.New("snapshot descriptor changed")
	}
	raw, err := os.ReadFile(filepath.Join(path, "snapshot"))
	if err != nil {
		return err
	}
	if digest(raw) != expected.ManifestSHA256 {
		return errors.New("snapshot bytes changed")
	}
	return ctx.Err()
}

func TestUpdatePolicyRejectsUntrustedOrIncompatibleMetadata(t *testing.T) {
	current := Application{Identity: sampleIdentity(10), Inventory: Inventory{SHA256: digest([]byte("current"))}}
	next := Application{Identity: sampleIdentity(11), Inventory: Inventory{SHA256: digest([]byte("new"))}}
	if err := VerifyUpgrade(current, next); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Application){"different publisher": func(a *Application) { a.Identity.TeamID = "ZZZZZZZZZZ" }, "other app": func(a *Application) { a.Identity.BundleID = "other.app" }, "adhoc": func(a *Application) { a.Identity.TeamID = "" }, "replay": func(a *Application) { a.Identity.Build = 10 }, "older build": func(a *Application) { a.Identity.Build = 9 }, "older data": func(a *Application) { a.Identity.UserVersion = 2 }, "older generation": func(a *Application) { a.Identity.StorageGeneration = 0 }, "architecture": func(a *Application) { a.Identity.Architecture = "amd64" }, "missing minimum": func(a *Application) { a.Identity.MinimumOS = "" }, "bad digest": func(a *Application) { a.Inventory.SHA256 = "trusted" }, "same bytes": func(a *Application) { a.Inventory = current.Inventory }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := next
			mutate(&candidate)
			if VerifyUpgrade(current, candidate) == nil {
				t.Fatal("accepted invalid update")
			}
		})
	}
	for _, test := range []struct {
		host, minimum string
		want          bool
	}{{"14.1", "14.0", true}, {"14.0", "14.0.0", true}, {"14.0.1", "14.1", false}, {"13.9", "14.0", false}, {"15.0", "14.9.9", true}, {"unknown", "14.0", false}, {"14.0", "14", false}, {"14.0", "999999999999.0", false}} {
		if got := compatibleOS(test.host, test.minimum); got != test.want {
			t.Fatalf("OS %s/%s got %v", test.host, test.minimum, got)
		}
	}
	for _, value := range []string{"0", "-1", "01", "1.0", "18446744073709551616"} {
		if _, err := parseBuild(value); err == nil {
			t.Fatalf("accepted build %s", value)
		}
	}
}

func TestApplicationCopyPreservesBytesAndExecutableMode(t *testing.T) {
	parent := t.TempDir()
	source, expected := fixtureApp(t, parent, "original.app", 1)
	target := filepath.Join(parent, "copy.app")
	if err := CopyTree(context.Background(), source, target, expected.Inventory); err != nil {
		t.Fatal(err)
	}
	actual, err := (fixtureVerifier{}).Inspect(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if !sameApplication(expected, actual) {
		t.Fatal("copy changed reviewed bytes or permissions")
	}
	if err := CopyTree(context.Background(), source, target, expected.Inventory); err == nil {
		t.Fatal("overwrote existing target")
	}
}

func TestApplicationInventoryRejectsLinksWritableAndOversizedFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "group writable", "oversize", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			path, _ := fixtureApp(t, parent, "test.app", 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			file := filepath.Join(path, "Contents", "bad")
			switch kind {
			case "symlink":
				if err := os.Symlink(filepath.Join(parent, "outside"), file); err != nil {
					t.Fatal(err)
				}
			case "group writable":
				if runtime.GOOS == "windows" {
					t.Skip("POSIX group mode is not a Windows ACL; native stream/read-only checks are separate")
				}
				write(t, file, []byte("bad"))
				if err := os.Chmod(file, 0660); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				f, err := os.Create(file)
				if err != nil {
					t.Fatal(err)
				}
				if err = f.Truncate(maxBundleBytes + 1); err != nil {
					t.Fatal(err)
				}
				f.Close()
			case "cancelled":
				cancel()
			}
			if _, err := InspectTree(ctx, path); err == nil {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
}

func TestCopyRejectsChangedSourceAndKeepsIncompleteFence(t *testing.T) {
	parent := t.TempDir()
	source, app := fixtureApp(t, parent, "source.app", 1)
	write(t, filepath.Join(source, "Contents", "MacOS", "Trove"), []byte("changed"))
	target := filepath.Join(parent, "partial.app")
	if err := CopyTree(context.Background(), source, target, app.Inventory); err == nil {
		t.Fatal("copied unreviewed source")
	}
	if _, err := os.Stat(filepath.Join(target, "INCOMPLETE")); err != nil {
		t.Fatal("missing incomplete fence", err)
	}
}

func TestRecoveryKitBindsApplicationsToExactSnapshot(t *testing.T) {
	parent := t.TempDir()
	oldPath, old := fixtureApp(t, parent, "old.app", 1)
	nextPath, next := fixtureApp(t, parent, "new.app", 2)
	path := filepath.Join(parent, "kit")
	kit, err := PrepareKit(context.Background(), fixtureVerifier{}, oldPath, nextPath, path, old, next, fixtureBackup, fixtureVerify)
	if err != nil {
		t.Fatal(err)
	}
	if kit.Snapshot != fixtureSnapshot {
		t.Fatal("snapshot identity changed")
	}
	verified, err := InspectKit(context.Background(), fixtureVerifier{}, path, fixtureVerify)
	if err != nil {
		t.Fatal(err)
	}
	if !sameApplication(verified.Previous, old) || !sameApplication(verified.Update, next) {
		t.Fatal("app pairing changed")
	}
	write(t, filepath.Join(path, "workspace", "snapshot"), []byte("newer workspace"))
	if _, err = InspectKit(context.Background(), fixtureVerifier{}, path, fixtureVerify); err == nil {
		t.Fatal("accepted replaced snapshot")
	}
}

func TestRecoveryKitRejectsPartialAndTamperedInputs(t *testing.T) {
	for _, kind := range []string{"backup failure", "backup wrong format", "application changed", "cancelled", "parent alias", "manifest replaced", "saved app changed"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			oldPath, old := fixtureApp(t, parent, "old.app", 1)
			nextPath, next := fixtureApp(t, parent, "new.app", 2)
			target := filepath.Join(parent, "kit")
			backup := Backup(fixtureBackup)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "backup failure":
				backup = func(context.Context, string) (Snapshot, error) { return Snapshot{}, errors.New("disk full") }
			case "backup wrong format":
				backup = func(ctx context.Context, p string) (Snapshot, error) {
					s, err := fixtureBackup(ctx, p)
					s.UserVersion++
					return s, err
				}
			case "application changed":
				write(t, filepath.Join(nextPath, "unreviewed"), []byte("new"))
			case "cancelled":
				cancel()
			case "parent alias":
				alias := filepath.Join(parent, "alias")
				if err := os.Symlink(oldPath, alias); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(alias, "kit")
			}
			_, err := PrepareKit(ctx, fixtureVerifier{}, oldPath, nextPath, target, old, next, backup, fixtureVerify)
			if kind == "manifest replaced" || kind == "saved app changed" {
				if err != nil {
					t.Fatal(err)
				}
				if kind == "manifest replaced" {
					raw, err := os.ReadFile(filepath.Join(target, "update-kit.json"))
					if err != nil {
						t.Fatal(err)
					}
					raw = []byte(strings.Replace(string(raw), `"storageGeneration": 1`, `"storageGeneration": 2`, 1))
					write(t, filepath.Join(target, "update-kit.json"), raw)
				} else {
					write(t, filepath.Join(target, "previous", "Trove.app", "Contents", "MacOS", "Trove"), []byte("tampered"))
				}
				_, err = InspectKit(ctx, fixtureVerifier{}, target, fixtureVerify)
				if err == nil {
					t.Fatal("accepted tampered kit")
				}
				return
			}
			if err == nil {
				t.Fatal("accepted failed preparation")
			}
			if _, err = InspectKit(context.Background(), fixtureVerifier{}, target, fixtureVerify); err == nil {
				t.Fatal("partial kit is usable")
			}
			if kind == "parent alias" {
				if _, err = os.Stat(filepath.Join(oldPath, "kit")); !os.IsNotExist(err) {
					t.Fatal("created inside app")
				}
			} else if _, err = os.Stat(filepath.Join(target, "INCOMPLETE")); err != nil {
				t.Fatal("failed kit not fenced", err)
			}
		})
	}
}
