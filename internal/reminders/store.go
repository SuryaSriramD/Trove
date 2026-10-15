package reminders

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"trove.local/phase0/internal/platformfs"
)

// Store is the desktop's sole derived-schedule publisher. The helper uses a
// different lock and writes only its state/receipt files in this directory.
type Store struct {
	root *os.Root
	lock *os.File
}

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid reminder state directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	dir, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	err = platformfs.AuditPrivateHandle(dir, true)
	dir.Close()
	if err != nil {
		root.Close()
		return nil, err
	}
	lock, err := platformfs.AcquireRootLock(root, "publisher.lock")
	if err != nil {
		root.Close()
		return nil, err
	}
	return &Store{root: root, lock: lock}, nil
}
func (s *Store) Close() error { return errors.Join(platformfs.ReleaseLock(s.lock), s.root.Close()) }
func (s *Store) Read(name string, value any) error {
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid reminder state name")
	}
	info, err := s.root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return fmt.Errorf("invalid reminder state file")
	}
	f, err := s.root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(info, current) {
		return fmt.Errorf("reminder state changed during read")
	}
	if err = platformfs.AuditPrivateHandle(f, false); err != nil {
		return err
	}
	dec := json.NewDecoder(io.LimitReader(f, 64<<20))
	if err = dec.Decode(value); err != nil {
		return err
	}
	var tail any
	if err = dec.Decode(&tail); err != io.EOF {
		return fmt.Errorf("trailing reminder state data")
	}
	return nil
}
func (s *Store) Write(name string, value any) error {
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid reminder state name")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > 64<<20 {
		return fmt.Errorf("reminder state exceeds 64 MiB")
	}
	tmp := ".publication-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer s.root.Remove(tmp)
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = s.root.Rename(tmp, name); err != nil {
		return err
	}
	d, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (s *Store) Publish(next Schedule) (Schedule, error) {
	var old Schedule
	err := s.Read("schedule.json", &old)
	if err != nil && !os.IsNotExist(err) {
		return next, err
	}
	if old.Generation < 0 || old.Generation >= 9007199254740991 {
		return next, fmt.Errorf("reminder publication counter exhausted")
	}
	next.Version = 1
	next.Generation = old.Generation + 1
	if next.Epoch == "" || next.WorkspaceID == "" {
		return next, fmt.Errorf("reminder publication lacks registration identity")
	}
	return next, s.Write("schedule.json", next)
}
func (s *Store) Receipt(next Schedule) (Receipt, error) {
	var out Receipt
	if filepath.Base(next.Epoch) != next.Epoch {
		return out, fmt.Errorf("invalid reminder epoch")
	}
	err := s.Read("receipt-"+next.Epoch+".json", &out)
	if err != nil {
		return out, err
	}
	if out.Version != 1 || out.Epoch != next.Epoch || out.Generation != next.Generation {
		return out, fmt.Errorf("reminder registration has not acknowledged this publication")
	}
	if _, err = time.Parse(time.RFC3339, out.AcknowledgedAt); err != nil {
		return out, fmt.Errorf("invalid reminder acknowledgement")
	}
	return out, nil
}
