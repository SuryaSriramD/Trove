package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// Snapshot records the exact pre-update workspace, including its format. The
// trusted workspace adapter supplies/verifies these fields; a model never does.
type Snapshot struct {
	WorkspaceID       string `json:"workspaceId"`
	BackupID          string `json:"backupId"`
	ManifestSHA256    string `json:"manifestSha256"`
	UserVersion       int    `json:"userVersion"`
	StorageGeneration int    `json:"storageGeneration"`
}
type Kit struct {
	FormatVersion int         `json:"formatVersion"`
	CreatedAt     string      `json:"createdAt"`
	Previous      Application `json:"previous"`
	Update        Application `json:"update"`
	Snapshot      Snapshot    `json:"snapshot"`
}
type Backup func(context.Context, string) (Snapshot, error)
type VerifySnapshot func(context.Context, string, Snapshot) error

// PrepareKit requires a native-selected, unused destination. INCOMPLETE fences
// interrupted copies. Every app is reauthenticated after copying; the snapshot
// is verified and bound to the old app's format before a complete kit is exposed.
func PrepareKit(ctx context.Context, v Verifier, currentPath, updatePath, destination string, current, next Application, backup Backup, verify VerifySnapshot) (kit Kit, err error) {
	if err = VerifyUpgrade(current, next); err != nil {
		return kit, err
	}
	if backup == nil || verify == nil {
		return kit, errors.New("workspace backup adapter is unavailable")
	}
	for _, source := range []string{currentPath, updatePath} {
		if pathsOverlap(source, destination) {
			return kit, errors.New("recovery kit must be outside both applications")
		}
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return kit, err
	}
	marker := filepath.Join(destination, "INCOMPLETE")
	if err = writeExclusive(marker, []byte("Update preparation is incomplete. Do not install or restore from this folder.\n")); err != nil {
		return kit, err
	}
	if err = syncDirectory(destination); err != nil {
		return kit, err
	}
	for _, item := range []struct {
		source, name string
		expected     Application
	}{{currentPath, "previous", current}, {updatePath, "update", next}} {
		observed, inspectErr := v.Inspect(ctx, item.source)
		if inspectErr != nil {
			return kit, inspectErr
		}
		if !sameApplication(observed, item.expected) {
			return kit, errors.New("reviewed application changed; choose it again")
		}
		parent := filepath.Join(destination, item.name)
		if err = os.Mkdir(parent, 0700); err != nil {
			return kit, err
		}
		copyPath := filepath.Join(parent, applicationDirectory(item.expected.Identity))
		if err = CopyTree(ctx, item.source, copyPath, item.expected.Inventory); err != nil {
			return kit, err
		}
		copied, inspectErr := v.Inspect(ctx, copyPath)
		if inspectErr != nil {
			return kit, inspectErr
		}
		if !sameApplication(copied, item.expected) {
			return kit, errors.New("copied application differs from reviewed identity")
		}
		if err = syncDirectory(parent); err != nil {
			return kit, err
		}
	}
	snapshot, err := backup(ctx, filepath.Join(destination, "workspace"))
	if err != nil {
		return kit, err
	}
	if snapshot.WorkspaceID == "" || snapshot.BackupID == "" || !validDigest(snapshot.ManifestSHA256) || snapshot.UserVersion != current.Identity.UserVersion || snapshot.StorageGeneration != current.Identity.StorageGeneration {
		return kit, errors.New("workspace backup is not compatible with the saved application")
	}
	if err = verify(ctx, filepath.Join(destination, "workspace"), snapshot); err != nil {
		return kit, err
	}
	formatVersion := 1
	if current.Identity.Platform == "linux" || current.Identity.Platform == "windows" {
		formatVersion = 2
	}
	kit = Kit{FormatVersion: formatVersion, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Previous: current, Update: next, Snapshot: snapshot}
	raw, err := json.MarshalIndent(kit, "", "  ")
	if err != nil {
		return kit, err
	}
	if len(raw) > 16<<20 {
		return kit, errors.New("recovery kit inventory exceeds 16 MiB")
	}
	if err = ctx.Err(); err != nil {
		return kit, err
	}
	if err = writeExclusive(filepath.Join(destination, "update-kit.json"), append(raw, '\n')); err != nil {
		return kit, err
	}
	if err = syncDirectory(destination); err != nil {
		return kit, err
	}
	if err = ctx.Err(); err != nil {
		return kit, err
	}
	if err = os.Remove(marker); err != nil {
		return kit, err
	}
	if err = syncDirectory(destination); err != nil {
		_ = writeExclusive(marker, []byte("Recovery kit publication was not durable.\n"))
		return kit, err
	}
	return kit, nil
}

// InspectKit authenticates the saved apps and checks the pre-upgrade snapshot.
// The old application is paired only with this snapshot, never with newer data.
func InspectKit(ctx context.Context, v Verifier, path string, verify VerifySnapshot) (Kit, error) {
	var kit Kit
	if _, err := os.Lstat(filepath.Join(path, "INCOMPLETE")); !os.IsNotExist(err) {
		return kit, errors.New("recovery kit is incomplete")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return kit, err
	}
	defer root.Close()
	st, err := root.Lstat("update-kit.json")
	if err != nil || !st.Mode().IsRegular() || st.Size() > 16<<20 {
		return kit, errors.New("invalid recovery kit manifest")
	}
	file, err := root.Open("update-kit.json")
	if err != nil {
		return kit, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil || len(raw) > 16<<20 {
		return kit, errors.New("recovery kit manifest exceeds its bound")
	}
	if json.Unmarshal(raw, &kit) != nil || (kit.FormatVersion != 1 && kit.FormatVersion != 2) {
		return kit, errors.New("unsupported recovery kit manifest")
	}
	if kit.FormatVersion == 1 {
		platform, _, e := canonicalPublisher(kit.Previous.Identity)
		if e != nil || platform != "darwin" {
			return kit, errors.New("legacy recovery kits require macOS identity")
		}
	}
	if err = VerifyUpgrade(kit.Previous, kit.Update); err != nil {
		return kit, err
	}
	if kit.Snapshot.UserVersion != kit.Previous.Identity.UserVersion || kit.Snapshot.StorageGeneration != kit.Previous.Identity.StorageGeneration || !validDigest(kit.Snapshot.ManifestSHA256) {
		return kit, errors.New("saved application and workspace formats do not match")
	}
	for _, item := range []struct {
		name     string
		expected Application
	}{{"previous", kit.Previous}, {"update", kit.Update}} {
		app, err := v.Inspect(ctx, filepath.Join(path, item.name, applicationDirectory(item.expected.Identity)))
		if err != nil {
			return kit, err
		}
		if !sameApplication(app, item.expected) {
			return kit, errors.New("recovery kit application changed")
		}
	}
	if verify == nil {
		return kit, errors.New("workspace verification is unavailable")
	}
	if err = verify(ctx, filepath.Join(path, "workspace"), kit.Snapshot); err != nil {
		return kit, err
	}
	return kit, nil
}

func sameApplication(a, b Application) bool {
	return EqualIdentity(a.Identity, b.Identity) && reflect.DeepEqual(a.Inventory, b.Inventory)
}
func pathsOverlap(a, b string) bool {
	a, ea := resolvedPath(a)
	b, eb := resolvedPath(b)
	if ea != nil || eb != nil {
		return true
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		r, err := filepath.Rel(pair[0], pair[1])
		if err != nil {
			return true
		}
		if r == "." || r != ".." && !filepath.IsAbs(r) && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// PathsOverlap fails closed if either path cannot be resolved safely.
func PathsOverlap(a, b string) bool { return pathsOverlap(a, b) }
func resolvedPath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err = os.Lstat(path); err == nil {
		return filepath.EvalSymlinks(path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}
func validDigest(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == sha256.Size && hex.EncodeToString(raw) == s
}
func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func applicationDirectory(id Identity) string {
	platform, _, _ := canonicalPublisher(id)
	if platform == "darwin" {
		return "Trove.app"
	}
	return "Trove"
}
