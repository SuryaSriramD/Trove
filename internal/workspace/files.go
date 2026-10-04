package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"trove.local/phase0/contracts"
)

const maxAssetBytes int64 = 1 << 30

type selectedFile struct {
	file    *os.File
	name    string
	expires time.Time
}
type recoveryState struct {
	mu         sync.Mutex
	selections map[string]selectedFile
}

var recoveryStates sync.Map

func (k *Kernel) recoveryState() *recoveryState {
	v, _ := recoveryStates.LoadOrStore(k, &recoveryState{selections: map[string]selectedFile{}})
	return v.(*recoveryState)
}
func (k *Kernel) closeRecovery() {
	if v, ok := recoveryStates.LoadAndDelete(k); ok {
		s := v.(*recoveryState)
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, f := range s.selections {
			f.file.Close()
		}
	}
}

func (k *Kernel) initRecovery(ctx context.Context) error {
	for _, name := range []string{"files", ".staging"} {
		if err := safeDirectory(filepath.Join(k.path, name)); err != nil {
			return err
		}
	}
	return k.write(ctx, func(tx *sql.Tx) error {
		var version string
		if err := tx.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key='recoverySchemaVersion'").Scan(&version); err == nil && version != "1" {
			return Fail("UNSUPPORTED_SCHEMA", "Recovery metadata requires a newer application")
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err := tx.ExecContext(ctx, recoveryMigration)
		if err != nil {
			return err
		}
		// An abandoned backup never authorizes deleting canonical assets. Partial
		// exports remain plainly incomplete and can be discarded by their owner.
		_, err = tx.ExecContext(ctx, "UPDATE backups SET state='interrupted',updated_at=? WHERE state='active'", now())
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM backup_pins WHERE backup_id IN(SELECT id FROM backups WHERE state='interrupted')")
		return err
	})
}

const recoveryMigration = `
CREATE TABLE IF NOT EXISTS file_ingests(handle TEXT PRIMARY KEY,file_id TEXT NOT NULL,content_hash TEXT NOT NULL,original_name TEXT NOT NULL,media_type TEXT NOT NULL,size_bytes INTEGER NOT NULL,actor_id TEXT NOT NULL,scope_grant_id TEXT NOT NULL,expires_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS file_ingest_hash ON file_ingests(content_hash);
CREATE TABLE IF NOT EXISTS file_references(page_id TEXT NOT NULL REFERENCES pages(id),source_kind TEXT NOT NULL,source_id TEXT NOT NULL,file_id TEXT NOT NULL REFERENCES files(id),PRIMARY KEY(page_id,source_kind,source_id,file_id));
CREATE TABLE IF NOT EXISTS restore_previews(id TEXT PRIMARY KEY,page_id TEXT NOT NULL,restore_revision TEXT NOT NULL,actor_id TEXT NOT NULL,grant_id TEXT NOT NULL,guards_json TEXT NOT NULL,snapshot_hash TEXT NOT NULL,expires_at TEXT NOT NULL,consumed INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS recovery_points(id TEXT PRIMARY KEY,label TEXT NOT NULL,created_at TEXT NOT NULL,event_cursor TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS recovery_point_pages(point_id TEXT NOT NULL REFERENCES recovery_points(id),page_id TEXT NOT NULL,revision TEXT NOT NULL,PRIMARY KEY(point_id,page_id));
CREATE TABLE IF NOT EXISTS backups(id TEXT PRIMARY KEY,state TEXT NOT NULL CHECK(state IN('active','complete','failed','interrupted')),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS backup_pins(backup_id TEXT NOT NULL REFERENCES backups(id),content_hash TEXT NOT NULL,PRIMARY KEY(backup_id,content_hash));
INSERT INTO kernel_meta(key,value) VALUES('recoverySchemaVersion','1') ON CONFLICT(key) DO NOTHING;
`

func init() {
	RegisterQuery("prepareFile", prepareFile)
	RegisterCommand("file.attach", attachFile)
	RegisterCommitValidator(syncFileReferences)
	RegisterSnapshotReader("files", fileSnapshot)
}

// RegisterSelection accepts only a path chosen by a trusted native adapter. Its
// open descriptor fixes identity before the renderer receives an opaque handle.
func (k *Kernel) RegisterSelection(path string) (string, error) {
	k.mu.Lock()
	closed := k.closed
	k.mu.Unlock()
	if closed {
		return "", Fail("WORKSPACE_LOCKED", "Workspace is closed")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", Fail("INVALID_REQUEST", "Select a regular file, not a symlink or device")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(fd), filepath.Base(path))
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(st, actual) {
		f.Close()
		return "", Fail("INVALID_REQUEST", "Selected file changed while opening")
	}
	if actual.Size() > maxAssetBytes {
		f.Close()
		return "", Fail("RESOURCE_EXHAUSTED", "Attachment exceeds 1 GiB")
	}
	s := k.recoveryState()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.selections {
		if time.Now().After(old.expires) {
			old.file.Close()
			delete(s.selections, id)
		}
	}
	if len(s.selections) >= 64 {
		f.Close()
		return "", Fail("RESOURCE_EXHAUSTED", "Too many pending file selections")
	}
	handle := NewID()
	s.selections[handle] = selectedFile{f, filepath.Base(path), time.Now().Add(30 * time.Minute)}
	return handle, nil
}
func prepareFile(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("SCOPE_DENIED", "Native file ingestion requires a direct user selection")
	}
	s := k.recoveryState()
	s.mu.Lock()
	selection, ok := s.selections[str(r["selectionHandle"])]
	if ok {
		delete(s.selections, str(r["selectionHandle"]))
	}
	s.mu.Unlock()
	if !ok {
		return nil, Fail("NOT_FOUND", "Selection handle is unknown or already consumed")
	}
	defer selection.file.Close()
	if time.Now().After(selection.expires) {
		return nil, Fail("NOT_FOUND", "Selection handle expired")
	}
	dst, err := os.CreateTemp(filepath.Join(k.path, ".staging"), "ingest-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(dst.Name())
	hash := sha256.New()
	head := make([]byte, 512)
	n, err := selection.file.Read(head)
	if err != nil && err != io.EOF {
		dst.Close()
		return nil, err
	}
	head = head[:n]
	reader := io.MultiReader(strings.NewReader(string(head)), selection.file)
	size, err := copyBounded(ctx, io.MultiWriter(dst, hash), reader, maxAssetBytes)
	if err == nil {
		err = dst.Sync()
	}
	closeErr := dst.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	media := http.DetectContentType(head)
	ext := strings.ToLower(filepath.Ext(selection.name))
	if candidate := mime.TypeByExtension(ext); candidate != "" && (strings.HasPrefix(media, "text/") || media == "application/octet-stream") {
		media = candidate
	}
	result := contracts.ServicePrepareFileResult{IngestHandle: contracts.DomainId(NewID()), ContentHash: contracts.DomainHash(digest), SizeBytes: strconv.FormatInt(size, 10), MediaType: media, ExpiresAt: contracts.DomainTimestamp(timeAfterMinutes(30))}
	err = k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorize(e); err != nil {
			return err
		}
		var id string
		err := tx.QueryRowContext(ctx, "SELECT id FROM files WHERE content_hash=? UNION ALL SELECT file_id FROM file_ingests WHERE content_hash=? LIMIT 1", digest, digest).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			id = NewID()
		} else if err != nil {
			return err
		}
		result.FileId = contracts.DomainId(id)
		_, err = tx.ExecContext(ctx, "INSERT INTO file_ingests VALUES(?,?,?,?,?,?,?,?,?)", result.IngestHandle, id, digest, selection.name, media, size, e.Actor.ActorId, e.ScopeGrantId, result.ExpiresAt)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err = k.publishAsset(dst.Name(), digest, size); err != nil {
		return nil, err
	}
	return result, nil
}

func (k *Kernel) publishAsset(staged, digest string, size int64) error {
	state := k.recoveryState()
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, directory := range []string{filepath.Join(k.path, "files"), filepath.Join(k.path, "files", digest[:2])} {
		if err := safeDirectory(directory); err != nil {
			return err
		}
	}
	target := filepath.Join(k.path, "files", digest[:2], digest)
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		// Rename is atomic on the workspace volume, including volumes without
		// hard links. The process lock and publication mutex prevent replacement.
		if err = os.Rename(staged, target); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := verifyAsset(target, digest, size); err != nil {
		return err
	}
	if err := os.Chmod(target, 0444); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(target)); err == nil {
		err = dir.Sync()
		dir.Close()
		return err
	} else {
		return err
	}
}

func copyBounded(ctx context.Context, dst io.Writer, src io.Reader, limit int64) (int64, error) {
	buf := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := src.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return total, Fail("RESOURCE_EXHAUSTED", "File exceeds the permitted size")
			}
			w, we := dst.Write(buf[:n])
			if we != nil {
				return total, we
			}
			if w != n {
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
func safeDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return Fail("INVALID_REQUEST", "Managed asset directory is not a regular directory")
	}
	return nil
}
func openAsset(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, Fail("INVALID_REQUEST", "Managed asset is not a regular file")
	}
	return f, nil
}
func verifyAsset(path, digest string, size int64) error {
	f, err := openAsset(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != digest {
		return Fail("INVALID_REQUEST", "Managed asset hash or length mismatch")
	}
	return nil
}
func attachFile(m *Mutation, c map[string]any) error {
	page, id, handle := str(c["pageId"]), str(c["fileId"]), str(c["ingestHandle"])
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if err := m.Require("file", id); err != nil {
		return err
	}
	var actual, digest, name, media, actor, grant, expires string
	var size int64
	err := m.Tx.QueryRowContext(m.Context, "SELECT file_id,content_hash,original_name,media_type,size_bytes,actor_id,scope_grant_id,expires_at FROM file_ingests WHERE handle=?", handle).Scan(&actual, &digest, &name, &media, &size, &actor, &grant, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Fail("NOT_FOUND", "Ingest handle is absent")
	}
	if err != nil {
		return err
	}
	expiry, _ := time.Parse(time.RFC3339Nano, expires)
	if actual != id || actor != string(m.Execution.Actor.ActorId) || grant != string(m.Execution.ScopeGrantId) || !expiry.After(time.Now()) {
		return Fail("SCOPE_DENIED", "Ingest handle is expired or belongs to another selection scope")
	}
	key := "files/" + digest[:2] + "/" + digest
	if m.GuardRevision("file", id) == "0" {
		if err := m.Touch("file", id); err != nil {
			return err
		}
		if _, err = m.Exec("INSERT INTO files VALUES(?,?,?,?,?,?,?,1)", id, m.Kernel.id, digest, name, media, size, key); err != nil {
			return err
		}
	}
	sourceKind, sourceID := "explicit", id
	if block := str(c["blockId"]); block != "" {
		sourceKind, sourceID = "explicit_block", block
	}
	if _, err = m.Exec("INSERT OR IGNORE INTO file_references VALUES(?,?,?,?)", page, sourceKind, sourceID, id); err != nil {
		return err
	}
	// The final validator resolves future block IDs after every batch command.
	_, err = m.Exec("DELETE FROM file_ingests WHERE handle=?", handle)
	return err
}
func readFileMetadata(ctx context.Context, q querier, id string) (contracts.DomainFile, error) {
	var f contracts.DomainFile
	var size int64
	err := q.QueryRowContext(ctx, "SELECT id,workspace_id,content_hash,original_name,media_type,size_bytes,storage_key,revision FROM files WHERE id=?", id).Scan(&f.Id, &f.WorkspaceId, &f.ContentHash, &f.OriginalName, &f.MediaType, &size, &f.StorageKey, &f.Revision)
	f.SizeBytes = strconv.FormatInt(size, 10)
	return f, err
}
func (k *Kernel) ReadFile(ctx context.Context, e contracts.ServiceExecutionContext, id string) (*os.File, contracts.DomainFile, error) {
	var meta contracts.DomainFile
	if err := k.authorizeEntity(ctx, k.db, e, "file", id); err != nil {
		return nil, meta, err
	}
	meta, err := readFileMetadata(ctx, k.db, id)
	if err != nil {
		return nil, meta, err
	}
	expected := "files/" + string(meta.ContentHash)[:2] + "/" + string(meta.ContentHash)
	if meta.StorageKey != expected {
		return nil, meta, Fail("INVALID_REQUEST", "Invalid asset location")
	}
	if err = safeDirectory(filepath.Join(k.path, "files")); err != nil {
		return nil, meta, err
	}
	if err = safeDirectory(filepath.Join(k.path, "files", string(meta.ContentHash)[:2])); err != nil {
		return nil, meta, err
	}
	f, err := openAsset(filepath.Join(k.path, filepath.FromSlash(expected)))
	if err != nil {
		return nil, meta, err
	}
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil || strconv.FormatInt(size, 10) != meta.SizeBytes || hex.EncodeToString(h.Sum(nil)) != string(meta.ContentHash) {
		f.Close()
		return nil, meta, Fail("INVALID_REQUEST", "Attachment failed integrity verification")
	}
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		f.Close()
		return nil, meta, err
	}
	return f, meta, nil
}
func fileSnapshot(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext) (any, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM files ORDER BY id")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []contracts.DomainFile{}
	for _, id := range ids {
		if k.authorizeEntity(ctx, tx, e, "file", id) != nil {
			continue
		}
		f, err := readFileMetadata(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}
func collectFileIDs(value any, ids map[string]bool) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "fileId" || key == "rawFileId" || key == "coverFileId" || key == "recordingFileId" {
				if id := str(child); id != "" {
					ids[id] = true
				}
			} else {
				collectFileIDs(child, ids)
			}
		}
	case []any:
		for _, child := range v {
			collectFileIDs(child, ids)
		}
	}
}
func syncFileReferences(m *Mutation) error {
	for _, command := range m.Request.Commands {
		var c map[string]any
		json.Unmarshal(marshal(command), &c)
		if str(c["kind"]) == "file.attach" && str(c["blockId"]) != "" {
			var page string
			if err := m.Tx.QueryRowContext(m.Context, "SELECT page_id FROM blocks WHERE id=?", c["blockId"]).Scan(&page); err != nil {
				return Fail("INVALID_REQUEST", "Attachment block must exist at commit")
			}
			if page != str(c["pageId"]) {
				return Fail("INVALID_REQUEST", "Attachment block belongs to another page")
			}
		}
	}
	pages := []string{}
	for key, c := range m.touched {
		_ = key
		if c.Target.EntityType == "page" {
			pages = append(pages, string(c.Target.EntityId))
		}
	}
	for _, page := range pages {
		if _, err := m.Exec("DELETE FROM file_references WHERE page_id=? AND source_kind IN('content','cover','properties','meeting')", page); err != nil {
			return err
		}
		// A removed block removes its explicit relationship; history still retains it.
		if _, err := m.Exec("DELETE FROM file_references WHERE page_id=? AND source_kind='explicit_block' AND source_id NOT IN(SELECT id FROM blocks WHERE page_id=?)", page, page); err != nil {
			return err
		}
		ids := map[string]bool{}
		rows, err := m.Tx.QueryContext(m.Context, "SELECT data_json FROM blocks WHERE page_id=? UNION ALL SELECT value_json FROM property_values WHERE record_id IN(SELECT id FROM records WHERE page_id=?)", page, page)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			var v any
			json.Unmarshal([]byte(raw), &v)
			collectFileIDs(v, ids)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		var cover sql.NullString
		if err = m.Tx.QueryRowContext(m.Context, "SELECT cover_file_id FROM page_metadata WHERE page_id=?", page).Scan(&cover); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if cover.Valid {
			ids[cover.String] = true
		}
		rows, err = m.Tx.QueryContext(m.Context, "SELECT recording_file_id FROM meetings WHERE page_id=? AND recording_file_id IS NOT NULL", page)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids[id] = true
		}
		rows.Close()
		for id := range ids {
			var n int
			if err = m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM files WHERE id=?", id).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return Fail("NOT_FOUND", "Content references an unknown attachment")
			}
			if err = m.Kernel.authorizeEntity(m.Context, m.Tx, m.Execution, "file", id); err != nil {
				var own int
				m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM file_references WHERE page_id=? AND file_id=?", page, id).Scan(&own)
				if own == 0 {
					return err
				}
			}
			if _, err = m.Exec("INSERT OR IGNORE INTO file_references VALUES(?,'content',?,?)", page, id, id); err != nil {
				return err
			}
		}
		if _, err = m.Exec("DELETE FROM attachments WHERE page_id=?", page); err != nil {
			return err
		}
		if _, err = m.Exec("INSERT INTO attachments(page_id,file_id) SELECT DISTINCT page_id,file_id FROM file_references WHERE page_id=?", page); err != nil {
			return err
		}
	}
	return nil
}

// CollectAssets removes only abandoned, uncommitted bytes. Canonical files are
// deliberately retained for the entire P0 history lifetime; no hidden retention
// policy can invalidate a historical page or recovery point.
func (k *Kernel) CollectAssets(ctx context.Context, e contracts.ServiceExecutionContext) ([]string, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	removed := []string{}
	err := k.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM file_ingests WHERE expires_at<?", now()); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT content_hash FROM files UNION SELECT content_hash FROM file_ingests UNION SELECT content_hash FROM backup_pins")
		if err != nil {
			return err
		}
		keep := map[string]bool{}
		for rows.Next() {
			var hash string
			if err = rows.Scan(&hash); err != nil {
				rows.Close()
				return err
			}
			keep[hash] = true
		}
		rows.Close()
		staged, err := os.ReadDir(filepath.Join(k.path, ".staging"))
		if err != nil {
			return err
		}
		for _, entry := range staged {
			if !strings.HasPrefix(entry.Name(), "ingest-") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() && time.Since(info.ModTime()) > 24*time.Hour {
				if err = os.Remove(filepath.Join(k.path, ".staging", entry.Name())); err != nil {
					return err
				}
			}
		}
		return filepath.WalkDir(filepath.Join(k.path, "files"), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				return Fail("INVALID_REQUEST", "Unexpected symlink in asset store")
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if len(name) != 64 {
				return nil
			}
			if keep[name] {
				return nil
			}
			st, err := d.Info()
			if err != nil {
				return err
			}
			if time.Since(st.ModTime()) < 24*time.Hour {
				return nil
			}
			if err = os.Remove(path); err != nil {
				return err
			}
			removed = append(removed, name)
			return nil
		})
	})
	return removed, err
}
func requireOwnerScope(k *Kernel, e contracts.ServiceExecutionContext) error {
	if err := k.authorize(e); err != nil {
		return err
	}
	if _, ok := e.Scope.Value.(contracts.DomainScopeVariant1); !ok || e.Actor.Kind != "user" || e.Origin != "user" {
		return Fail("SCOPE_DENIED", "Workspace owner scope is required")
	}
	return nil
}
