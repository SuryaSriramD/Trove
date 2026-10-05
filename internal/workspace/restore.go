package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"trove.local/phase0/internal/platformfs"
)

func validDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func backupSize(s string) (int64, error) {
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || n < 0 || strconv.FormatInt(n, 10) != s {
		return 0, Fail("INVALID_REQUEST", "Invalid backup object size")
	}
	return n, nil
}
func backupRegular(root, relative string) (string, error) {
	st, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", Fail("INVALID_REQUEST", "Backup root must be a regular directory")
	}
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") || filepath.ToSlash(filepath.Clean(relative)) != relative || strings.HasPrefix(relative, "../") {
		return "", Fail("INVALID_REQUEST", "Unsafe backup path")
	}
	path := root
	parts := strings.Split(relative, "/")
	for i, part := range parts {
		path = filepath.Join(path, part)
		st, e := os.Lstat(path)
		if e != nil {
			return "", e
		}
		if st.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !st.IsDir()) || (i == len(parts)-1 && !st.Mode().IsRegular()) {
			return "", Fail("INVALID_REQUEST", "Backup object is not a regular file")
		}
	}
	return path, nil
}
func verifyBackupObject(ctx context.Context, root string, o BackupObject) error {
	if !validDigest(o.SHA256) {
		return Fail("INVALID_REQUEST", "Invalid backup checksum")
	}
	size, e := backupSize(o.SizeBytes)
	if e != nil {
		return e
	}
	p, e := backupRegular(root, o.Path)
	if e != nil {
		return e
	}
	return verifyAsset(p, o.SHA256, size)
}
func copyBackupObject(ctx context.Context, source, destination string, o BackupObject) error {
	src, e := backupRegular(source, o.Path)
	if e != nil {
		return e
	}
	f, e := openAsset(src)
	if e != nil {
		return e
	}
	defer f.Close()
	out := filepath.Join(destination, filepath.FromSlash(o.Path))
	if e = safeDirectory(filepath.Dir(out)); e != nil {
		return e
	}
	dst, e := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	size, e := backupSize(o.SizeBytes)
	if e == nil {
		_, e = copyBounded(ctx, dst, f, size)
	}
	if e == nil {
		e = dst.Sync()
	}
	ce := dst.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return verifyBackupObject(ctx, destination, o)
}

// Backup journals must describe recoverable snapshots. Active capture is copied
// as a backup-only interrupted checkpoint, never as a still-running recording.
func validateRecordingInventory(ctx context.Context, root string, objects []BackupObject) error {
	inventory := map[string]BackupObject{}
	for _, o := range objects {
		base := strings.TrimPrefix(o.Path, "recordings/")
		ext := filepath.Ext(base)
		if base == o.Path || (ext != ".json" && ext != ".f32le") || !recordingID.MatchString(strings.TrimSuffix(base, ext)) {
			return Fail("INVALID_REQUEST", "Unsafe recording backup path")
		}
		if _, ok := inventory[o.Path]; ok {
			return Fail("INVALID_REQUEST", "Duplicate recording backup path")
		}
		inventory[o.Path] = o
		if e := verifyBackupObject(ctx, root, o); e != nil {
			return e
		}
	}
	for path := range inventory {
		if strings.HasSuffix(path, ".f32le") {
			if _, ok := inventory[strings.TrimSuffix(path, ".f32le")+".json"]; !ok {
				return Fail("INVALID_REQUEST", "Recording is missing its journal")
			}
			continue
		}
		f, e := openAsset(filepath.Join(root, filepath.FromSlash(path)))
		if e != nil {
			return e
		}
		var d RecordingDraft
		e = json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&d)
		f.Close()
		if e != nil {
			return e
		}
		if path != "recordings/"+d.ID+".json" || d.State == "recording" || d.Bytes < 0 {
			return Fail("INVALID_REQUEST", "Invalid recording journal")
		}
		if d.State == "saved" {
			continue
		}
		if d.State != "captured" && d.State != "interrupted" {
			return Fail("INVALID_REQUEST", "Unknown recording state")
		}
		o, ok := inventory["recordings/"+d.ID+".f32le"]
		if !ok {
			return Fail("INVALID_REQUEST", "Recording source is absent")
		}
		size, _ := backupSize(o.SizeBytes)
		if size < d.Bytes || size > 16000*4*7200 {
			return Fail("INVALID_REQUEST", "Recording source length mismatch")
		}
		audio, e := openAsset(filepath.Join(root, filepath.FromSlash(o.Path)))
		if e != nil {
			return e
		}
		end := int64(0)
		for _, s := range d.Segments {
			if s.StartByte != end || s.EndByte <= s.StartByte || s.EndByte > d.Bytes || !validDigest(s.SHA256) {
				audio.Close()
				return Fail("INVALID_REQUEST", "Invalid recording segment journal")
			}
			h := sha256.New()
			_, e = io.Copy(h, io.NewSectionReader(audio, s.StartByte, s.EndByte-s.StartByte))
			if e != nil || hex.EncodeToString(h.Sum(nil)) != s.SHA256 {
				audio.Close()
				return Fail("INVALID_REQUEST", "Recording segment checksum mismatch")
			}
			end = s.EndByte
		}
		audio.Close()
		if end != d.Bytes {
			return Fail("INVALID_REQUEST", "Recording journal has unaccounted durable audio")
		}
	}
	return nil
}

// VerifyBackup verifies exactly the declared immutable inventory and the complete
// SQLite foreign-key graph before any workspace is opened or migrated.
func VerifyBackup(ctx context.Context, root string) (BackupManifest, error) {
	var m BackupManifest
	if _, e := os.Lstat(filepath.Join(root, "INCOMPLETE")); e == nil {
		return m, Fail("INVALID_REQUEST", "Backup did not complete")
	} else if !errors.Is(e, os.ErrNotExist) {
		return m, e
	}
	path, e := backupRegular(root, "manifest.json")
	if e != nil {
		return m, e
	}
	f, e := openAsset(path)
	if e != nil {
		return m, e
	}
	raw, e := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	f.Close()
	if e != nil {
		return m, e
	}
	if len(raw) > 4<<20 {
		return m, Fail("RESOURCE_EXHAUSTED", "Backup manifest exceeds 4 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&m); e != nil {
		return m, e
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return m, Fail("INVALID_REQUEST", "Trailing backup manifest data")
	}
	if (m.FormatVersion != 1 && m.FormatVersion != 2) || m.Database.Path != "workspace.db" || m.WorkspaceID == "" || m.BackupID == "" {
		return m, Fail("UNSUPPORTED_SCHEMA", "Unsupported backup format")
	}
	if m.FormatVersion == 1 && len(m.Recordings) > 0 {
		return m, Fail("INVALID_REQUEST", "Version 1 backup cannot declare recordings")
	}
	if e = verifyBackupObject(ctx, root, m.Database); e != nil {
		return m, e
	}
	seen := map[string]bool{}
	for _, a := range m.Assets {
		if !validDigest(a.ContentHash) || a.FileID == "" || seen[a.FileID] || a.Path != "files/"+a.ContentHash[:2]+"/"+a.ContentHash {
			return m, Fail("INVALID_REQUEST", "Unsafe or duplicate backup asset")
		}
		seen[a.FileID] = true
		if e = verifyBackupObject(ctx, root, BackupObject{Path: a.Path, SHA256: a.ContentHash, SizeBytes: a.SizeBytes}); e != nil {
			return m, e
		}
	}
	if e = validateRecordingInventory(ctx, root, m.Recordings); e != nil {
		return m, e
	}
	db, e := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: filepath.Join(root, "workspace.db")}).String()+"?mode=ro&immutable=1")
	if e != nil {
		return m, e
	}
	defer db.Close()
	if e = verifyBackupDatabase(ctx, db, m); e != nil {
		return m, e
	}
	return m, nil
}
func verifyBackupDatabase(ctx context.Context, db *sql.DB, m BackupManifest) error {
	var integrity, wid, cursor string
	var count int
	if e := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); e != nil {
		return e
	}
	if integrity != "ok" {
		return Fail("INTEGRITY_ERROR", "Backup database failed integrity verification")
	}
	rows, e := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	invalid := rows.Next()
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if invalid {
		return Fail("INTEGRITY_ERROR", "Backup database has broken references")
	}
	if e = db.QueryRowContext(ctx, "SELECT id FROM workspaces").Scan(&wid); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, "SELECT count(*) FROM files").Scan(&count); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, "SELECT CAST(COALESCE(MAX(cursor),0) AS TEXT) FROM committed_events").Scan(&cursor); e != nil {
		return e
	}
	if wid != m.WorkspaceID || count != len(m.Assets) || cursor != m.EventCursor {
		return Fail("INVALID_REQUEST", "Backup manifest does not match its database")
	}
	for _, a := range m.Assets {
		if e = db.QueryRowContext(ctx, "SELECT count(*) FROM files WHERE id=? AND content_hash=? AND size_bytes=? AND storage_key=?", a.FileID, a.ContentHash, parseSize(a.SizeBytes), a.Path).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return Fail("INVALID_REQUEST", "Backup asset metadata differs from its database")
		}
	}
	return nil
}

type RestoreResult struct {
	WorkspaceID      string `json:"workspaceId"`
	Destination      string `json:"destination"`
	BackupID         string `json:"backupId"`
	Assets           int    `json:"assets"`
	RecordingObjects int    `json:"recordingObjects"`
	IndexRebuilt     bool   `json:"indexRebuilt"`
}

// RestoreBackup copies a verified backup into a sibling staging directory,
// migrates/rebuilds there and publishes only into a new destination. No active
// workspace or source backup is modified and no inference jobs run in staging.
func RestoreBackup(ctx context.Context, source, destination string) (result RestoreResult, err error) {
	m, err := VerifyBackup(ctx, source)
	if err != nil {
		return result, err
	}
	return restoreBackupVerified(ctx, source, destination, m)
}

func RestoreBackupReviewed(ctx context.Context, source, destination string, expected BackupManifest) (RestoreResult, error) {
	current, err := VerifyBackup(ctx, source)
	if err != nil {
		return RestoreResult{}, err
	}
	if string(marshal(current)) != string(marshal(expected)) {
		return RestoreResult{}, Fail("CONFLICT", "Backup changed since its review")
	}
	return restoreBackupVerified(ctx, source, destination, current)
}
func restoreBackupVerified(ctx context.Context, source, destination string, m BackupManifest) (result RestoreResult, err error) {
	if err = platformfs.RequireRecoveryDurability(); err != nil {
		return result, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	if _, err = os.Lstat(destination); err == nil {
		return result, os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".trove-restore-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	objects := []BackupObject{m.Database}
	for _, a := range m.Assets {
		objects = append(objects, BackupObject{Path: a.Path, SHA256: a.ContentHash, SizeBytes: a.SizeBytes})
	}
	objects = append(objects, m.Recordings...)
	for _, object := range objects {
		if err = copyBackupObject(ctx, source, stage, object); err != nil {
			return result, err
		}
	}
	// Search is derived. The backed-up canonical rows, history and evidence remain.
	k, err := Open(stage)
	if err != nil {
		return result, err
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		// A native backup preserves external references, not permission to read
		// this installation's filesystem. The user must select each root again;
		// retained source identities remain available for that reconciliation.
		_, err := tx.ExecContext(ctx, "UPDATE approvals SET state='revoked' WHERE state IN('pending','approved'); UPDATE restore_previews SET consumed=1; DELETE FROM file_ingests; UPDATE external_roots SET approved=0; DELETE FROM external_chunks;")
		return err
	})
	if err == nil {
		err = k.RebuildSearch(ctx)
	}
	if err == nil {
		_, err = k.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	}
	closeErr := k.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	// Reserve the final name without replacing any existing destination. All
	// partial output keeps INCOMPLETE until publication and directory fsync.
	if err = os.Mkdir(destination, 0700); err != nil {
		return result, err
	}
	if err = durableRecoveryFile(filepath.Join(destination, "INCOMPLETE"), []byte("Restore publication in progress\n")); err != nil {
		return result, err
	}
	if err = syncRecoveryDirectory(destination); err != nil {
		return result, err
	}
	if err = syncRecoveryDirectory(filepath.Dir(destination)); err != nil {
		return result, err
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if err = os.Rename(filepath.Join(stage, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
			return result, err
		}
	}
	receipt := map[string]any{"backupId": m.BackupID, "workspaceId": m.WorkspaceID, "restoredAt": now(), "sourceManifest": m}
	if err = durableRecoveryFile(filepath.Join(destination, "restore-receipt.json"), marshal(receipt)); err != nil {
		return result, err
	}
	if err = syncRecoveryTree(destination); err != nil {
		return result, err
	}
	if err = os.Remove(filepath.Join(destination, "INCOMPLETE")); err != nil {
		return result, err
	}
	d, err := os.Open(destination)
	if err != nil {
		return result, err
	}
	err = d.Sync()
	d.Close()
	if err != nil {
		return result, err
	}
	result = RestoreResult{WorkspaceID: m.WorkspaceID, Destination: destination, BackupID: m.BackupID, Assets: len(m.Assets), RecordingObjects: len(m.Recordings), IndexRebuilt: true}
	return result, nil
}

func durableRecoveryFile(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}
func syncRecoveryDirectory(path string) error {
	return platformfs.SyncDirectory(path)
}
func syncRecoveryTree(root string) error {
	dirs := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return Fail("INTEGRITY_ERROR", "Unexpected symlink in staged restore")
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err = syncRecoveryDirectory(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}
