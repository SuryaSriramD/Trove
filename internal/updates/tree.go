// Package updates owns immutable, verified application copies. It never accepts
// renderer paths and never treats an archive checksum as publisher authentication.
package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const maxBundleBytes int64 = 4 << 30
const maxBundleFiles = 10000

type Object struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256,omitempty"`
	Size      int64  `json:"size"`
	Mode      uint32 `json:"mode"`
	Directory bool   `json:"directory,omitempty"`
}
type Inventory struct {
	SHA256  string   `json:"sha256"`
	Bytes   int64    `json:"bytes"`
	Objects []Object `json:"objects"`
}

// InspectTree bounds allocation and bytes and rejects links and special files.
// Root handles keep every open inside the selected bundle even if a path races.
func InspectTree(ctx context.Context, path string) (Inventory, error) {
	var out Inventory
	root, err := os.OpenRoot(path)
	if err != nil {
		return out, err
	}
	defer root.Close()
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			st, err := root.Lstat(name)
			if err != nil {
				return err
			}
			return inventoryDirectoryPolicy(root, name, st)
		}
		if len(name) > 4096 || strings.ContainsAny(name, "\\\x00\r\n") || len(out.Objects) >= maxBundleFiles {
			return errors.New("application inventory exceeds supported bounds")
		}
		st, err := root.Lstat(name)
		if err != nil {
			return err
		}
		mode, err := inventoryMode(st)
		if err != nil {
			return err
		}
		object := Object{Path: name, Mode: mode, Directory: st.IsDir()}
		if st.IsDir() {
			if err := inventoryDirectoryPolicy(root, name, st); err != nil {
				return err
			}
			out.Objects = append(out.Objects, object)
			return nil
		}
		if !st.Mode().IsRegular() {
			return errors.New("application must contain regular files and directories only")
		}
		if st.Size() < 0 || st.Size() > maxBundleBytes-out.Bytes {
			return errors.New("application exceeds 4 GiB")
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		opened, err := f.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(st, opened) || !opened.Mode().IsRegular() {
			return errors.New("application changed while inspecting")
		}
		if err = inventoryFilePolicy(f); err != nil {
			return err
		}
		h := sha256.New()
		n, err := copyContext(ctx, h, f, st.Size())
		if err != nil {
			return err
		}
		if n != st.Size() {
			return errors.New("application object size changed")
		}
		object.Size = n
		object.SHA256 = hex.EncodeToString(h.Sum(nil))
		out.Bytes += n
		out.Objects = append(out.Objects, object)
		return nil
	})
	if err != nil {
		return Inventory{}, err
	}
	if len(out.Objects) == 0 {
		return Inventory{}, errors.New("application bundle is empty")
	}
	raw, _ := json.Marshal(out.Objects)
	digest := sha256.Sum256(raw)
	out.SHA256 = hex.EncodeToString(digest[:])
	return out, nil
}

func copyContext(ctx context.Context, w io.Writer, r io.Reader, limit int64) (int64, error) {
	buf := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := r.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return total, errors.New("application object exceeded declared size")
			}
			written, we := w.Write(buf[:n])
			if we != nil {
				return total, we
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

// CopyTree publishes only to a new directory and keeps a failed partial copy
// visibly incomplete. Caller verifies the copied publisher signature again.
func CopyTree(ctx context.Context, source, destination string, expected Inventory) error {
	if len(expected.Objects) == 0 || expected.SHA256 == "" {
		return errors.New("missing reviewed application inventory")
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	out, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer out.Close()
	if err = out.WriteFile("INCOMPLETE", []byte("Unverified application copy; do not run.\n"), 0600); err != nil {
		return err
	}
	for _, object := range expected.Objects {
		if err = ctx.Err(); err != nil {
			return err
		}
		if object.Path == "" || object.Path == "." || !fs.ValidPath(object.Path) || strings.Contains(object.Path, "\\") || object.Path == "INCOMPLETE" {
			return errors.New("invalid reviewed application path")
		}
		if object.Directory {
			if err = out.Mkdir(object.Path, 0700); err != nil {
				return err
			}
			continue
		}
		if err = copyObject(ctx, root, out, object); err != nil {
			return err
		}
	}
	// Restore directory permissions only after all children are written.
	for i := len(expected.Objects) - 1; i >= 0; i-- {
		o := expected.Objects[i]
		if o.Directory {
			if err = out.Chmod(o.Path, fs.FileMode(o.Mode)); err != nil {
				return err
			}
			if err = syncDirectory(filepath.Join(destination, filepath.FromSlash(o.Path))); err != nil {
				return err
			}
		}
	}
	if err = out.Remove("INCOMPLETE"); err != nil {
		return err
	}
	actual, err := InspectTree(ctx, destination)
	if err != nil || actual.SHA256 != expected.SHA256 {
		_ = out.WriteFile("INCOMPLETE", []byte("Application inventory did not verify.\n"), 0600)
		if err != nil {
			return fmt.Errorf("copied application failed integrity verification: %w", err)
		}
		return errors.New("copied application differs from reviewed inventory")
	}
	return syncDirectory(destination)
}

func copyObject(ctx context.Context, source, destination *os.Root, o Object) error {
	if o.Size < 0 || o.Size > maxBundleBytes || o.Mode&^0777 != 0 || o.Mode&0022 != 0 {
		return errors.New("invalid reviewed application metadata")
	}
	st, err := source.Lstat(o.Path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("application source object is not a regular file")
	}
	in, err := source.Open(o.Path)
	if err != nil {
		return err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(st, opened) {
		return errors.New("application source changed")
	}
	out, err := destination.OpenFile(o.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	h := sha256.New()
	n, err := copyContext(ctx, io.MultiWriter(out, h), in, o.Size)
	if err != nil {
		return err
	}
	if n != o.Size || hex.EncodeToString(h.Sum(nil)) != o.SHA256 {
		return errors.New("application source differs from reviewed bytes")
	}
	if err = out.Chmod(fs.FileMode(o.Mode)); err != nil {
		return err
	}
	return out.Sync()
}

func syncDirectory(path string) error {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
