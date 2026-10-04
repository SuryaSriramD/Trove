package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"trove.local/phase0/contracts"
)

type BackupObject struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes string `json:"sizeBytes"`
}
type BackupAsset struct {
	FileID      string `json:"fileId"`
	ContentHash string `json:"contentHash"`
	SizeBytes   string `json:"sizeBytes"`
	Path        string `json:"path"`
}
type BackupManifest struct {
	FormatVersion int            `json:"formatVersion"`
	BackupID      string         `json:"backupId"`
	WorkspaceID   string         `json:"workspaceId"`
	CreatedAt     string         `json:"createdAt"`
	EventCursor   string         `json:"eventCursor"`
	Database      BackupObject   `json:"database"`
	Assets        []BackupAsset  `json:"assets"`
	Recordings    []BackupObject `json:"recordings,omitempty"`
}

func parseSize(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

// CreateBackup requires a trusted native destination. It never accepts renderer
// paths and never overwrites an existing directory. SQLite VACUUM INTO uses the
// engine's consistent snapshot facility, including committed WAL transactions.
func (k *Kernel) CreateBackup(ctx context.Context, e contracts.ServiceExecutionContext, destination string) (manifest BackupManifest, err error) {
	if err = requireOwnerScope(k, e); err != nil {
		return manifest, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return manifest, err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return manifest, err
	}
	incomplete := filepath.Join(destination, "INCOMPLETE")
	if err = os.WriteFile(incomplete, []byte("Backup has not completed. Do not restore this directory.\n"), 0600); err != nil {
		return manifest, err
	}
	manifest = BackupManifest{FormatVersion: 2, BackupID: NewID(), WorkspaceID: k.id, CreatedAt: now(), Assets: []BackupAsset{}}
	dbPath := filepath.Join(destination, "workspace.db")
	defer func() {
		state := "complete"
		if err != nil {
			state = "failed"
		}
		clean := context.WithoutCancel(ctx)
		_ = k.write(clean, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(clean, "UPDATE backups SET state=?,updated_at=? WHERE id=?", state, now(), manifest.BackupID); e != nil {
				return e
			}
			_, e := tx.ExecContext(clean, "DELETE FROM backup_pins WHERE backup_id=?", manifest.BackupID)
			return e
		})
	}()
	captureLock := recordingLock(k)
	captureLock.Lock()
	recordingErr := k.backupRecordingFiles(ctx, destination, &manifest)
	if recordingErr == nil {
		recordingErr = k.backupSnapshot(ctx, e, manifest.BackupID, manifest.CreatedAt, dbPath)
	}
	captureLock.Unlock()
	if err = recordingErr; err != nil {
		return manifest, err
	}
	snap, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: dbPath}).String()+"?mode=ro&_foreign_keys=on")
	if err != nil {
		return manifest, err
	}
	defer snap.Close()
	var integrity string
	if err = snap.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return manifest, err
	}
	if integrity != "ok" {
		return manifest, Fail("INVALID_REQUEST", "Backup database failed integrity verification")
	}
	if err = snap.QueryRowContext(ctx, "SELECT CAST(COALESCE(MAX(cursor),0) AS TEXT) FROM committed_events").Scan(&manifest.EventCursor); err != nil {
		return manifest, err
	}
	rows, err := snap.QueryContext(ctx, "SELECT id,content_hash,CAST(size_bytes AS TEXT),storage_key FROM files ORDER BY id")
	if err != nil {
		return manifest, err
	}
	for rows.Next() {
		var a BackupAsset
		if err = rows.Scan(&a.FileID, &a.ContentHash, &a.SizeBytes, &a.Path); err != nil {
			rows.Close()
			return manifest, err
		}
		manifest.Assets = append(manifest.Assets, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return manifest, err
	}
	for _, asset := range manifest.Assets {
		if err = ctx.Err(); err != nil {
			return manifest, err
		}
		if !validDigest(asset.ContentHash) || asset.Path != "files/"+asset.ContentHash[:2]+"/"+asset.ContentHash {
			return manifest, Fail("INVALID_REQUEST", "Unsafe backup asset path")
		}
		src, err := openAsset(filepath.Join(k.path, filepath.FromSlash(asset.Path)))
		if err != nil {
			return manifest, err
		}
		out := filepath.Join(destination, filepath.FromSlash(asset.Path))
		if err = safeDirectory(filepath.Dir(out)); err != nil {
			src.Close()
			return manifest, err
		}
		dst, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			src.Close()
			return manifest, err
		}
		hash := sha256.New()
		n, err := copyBounded(ctx, io.MultiWriter(dst, hash), src, maxAssetBytes)
		src.Close()
		if err == nil {
			err = dst.Sync()
		}
		closeErr := dst.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return manifest, err
		}
		if strconv.FormatInt(n, 10) != asset.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != asset.ContentHash {
			return manifest, Fail("INVALID_REQUEST", "Backup asset failed hash verification")
		}
		if err = os.Chmod(out, 0444); err != nil {
			return manifest, err
		}
	}
	hash, size, err := hashFile(ctx, dbPath)
	if err != nil {
		return manifest, err
	}
	manifest.Database = BackupObject{Path: "workspace.db", SHA256: hash, SizeBytes: strconv.FormatInt(size, 10)}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return manifest, err
	}
	if len(raw) > 4<<20 {
		return manifest, Fail("RESOURCE_EXHAUSTED", "Backup manifest exceeds 4 MiB")
	}
	f, err := os.OpenFile(filepath.Join(destination, "manifest.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return manifest, err
	}
	_, err = f.Write(append(raw, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return manifest, err
	}
	if err = os.Remove(incomplete); err != nil {
		return manifest, err
	}
	if dir, e := os.Open(destination); e == nil {
		err = dir.Sync()
		dir.Close()
	}
	return manifest, err
}
func (k *Kernel) backupSnapshot(ctx context.Context, e contracts.ServiceExecutionContext, id, created, path string) error {
	select {
	case k.pending <- struct{}{}:
	default:
		return Fail("RESOURCE_EXHAUSTED", "Workspace write queue is full")
	}
	defer func() { <-k.pending }()
	select {
	case k.writer <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-k.writer }()
	if err := requireOwnerScope(k, e); err != nil {
		return err
	}
	tx, err := k.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO backups VALUES(?,'active',?,?)", id, created, created); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO backup_pins SELECT ?,content_hash FROM files", id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Content writers remain serialized through the snapshot boundary. Asset copy
	// runs after releasing this lock; persistent pins protect the captured set.
	_, err = k.db.ExecContext(ctx, "VACUUM INTO ?", path)
	return err
}
func hashFile(ctx context.Context, path string) (string, int64, error) {
	f, err := openAsset(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := copyBounded(ctx, h, f, 1<<50)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
