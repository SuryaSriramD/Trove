package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"trove.local/phase0/contracts"
)

const externalIndexSchema = `
CREATE TABLE IF NOT EXISTS external_roots(id TEXT PRIMARY KEY,path TEXT NOT NULL UNIQUE,identity TEXT NOT NULL,repository INTEGER NOT NULL,approved INTEGER NOT NULL DEFAULT 1,last_scan TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS external_sources(id TEXT PRIMARY KEY,root_id TEXT NOT NULL REFERENCES external_roots(id),path TEXT NOT NULL,identity TEXT NOT NULL,content_hash TEXT NOT NULL,state TEXT NOT NULL,language TEXT NOT NULL,indexed_at TEXT NOT NULL,extractor_version TEXT NOT NULL,UNIQUE(root_id,path));
CREATE TABLE IF NOT EXISTS external_chunks(source_id TEXT NOT NULL REFERENCES external_sources(id),ordinal INTEGER NOT NULL,start_line INTEGER NOT NULL,end_line INTEGER NOT NULL,symbol TEXT NOT NULL,text TEXT NOT NULL,PRIMARY KEY(source_id,ordinal));
CREATE TABLE IF NOT EXISTS external_page_references(source_id TEXT NOT NULL REFERENCES external_sources(id),page_id TEXT NOT NULL REFERENCES pages(id),PRIMARY KEY(source_id,page_id));
`

type ExternalRoot struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Identity   string `json:"-"`
	Repository bool   `json:"repository"`
	Approved   bool   `json:"approved"`
	LastScan   string `json:"lastScan"`
}
type ExternalSource struct {
	ID        string `json:"id"`
	RootID    string `json:"rootId"`
	Path      string `json:"path"`
	Identity  string `json:"-"`
	Hash      string `json:"hash"`
	State     string `json:"state"`
	Language  string `json:"language"`
	IndexedAt string `json:"indexedAt"`
}
type ExternalChunk struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Symbol    string `json:"symbol"`
	Text      string `json:"text"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, externalIndexSchema); return err })
	})
	RegisterQuery("index.roots", listExternalRoots)
	RegisterQuery("index.entries", listExternalSources)
	RegisterQuery("index.scan", scanExternalRoot)
	RegisterQuery("index.search", searchExternalSources)
	RegisterQuery("index.revoke", revokeExternalRoot)
	RegisterQuery("index.reference", referenceExternalSource)
}
func externalIdentity(info os.FileInfo) string {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
	}
	return ""
}

// This method is called only by a trusted native folder picker, never by a
// renderer-supplied path. The persisted inode prevents retargeting after approval.
func (k *Kernel) ApproveIndexRoot(ctx context.Context, e contracts.ServiceExecutionContext, selected string, repository bool) (ExternalRoot, error) {
	out := ExternalRoot{}
	if err := requireOwnerScope(k, e); err != nil {
		return out, err
	}
	absolute, err := filepath.Abs(selected)
	if err != nil {
		return out, err
	}
	st, err := os.Lstat(absolute)
	if err != nil {
		return out, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return out, Fail("INVALID_REQUEST", "Choose a real directory")
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return out, err
	}
	workspacePath, err := filepath.EvalSymlinks(k.path)
	if err != nil {
		return out, err
	}
	if canonical == workspacePath || strings.HasPrefix(canonical, workspacePath+string(os.PathSeparator)) {
		return out, Fail("SCOPE_DENIED", "Canonical workspace storage cannot be externally indexed")
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return out, err
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(st, actual) {
		return out, Fail("CONFLICT", "Folder changed during approval")
	}
	out = ExternalRoot{ID: NewID(), Path: canonical, Identity: externalIdentity(st), Repository: repository, Approved: true}
	if out.Identity == "" {
		return out, Fail("UNSUPPORTED_SCHEMA", "Stable filesystem identities are unavailable on this platform")
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		if err := requireOwnerScope(k, e); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO external_roots(id,path,identity,repository) VALUES(?,?,?,?) ON CONFLICT(path) DO UPDATE SET identity=excluded.identity,repository=excluded.repository,approved=1", out.ID, out.Path, out.Identity, out.Repository)
		if err == nil {
			err = tx.QueryRowContext(ctx, "SELECT id FROM external_roots WHERE path=?", out.Path).Scan(&out.ID)
		}
		return err
	})
	return out, err
}
func listExternalRoots(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	rows, err := k.db.QueryContext(ctx, "SELECT id,path,repository,approved,last_scan FROM external_roots ORDER BY path")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExternalRoot{}
	for rows.Next() {
		var x ExternalRoot
		if err = rows.Scan(&x.ID, &x.Path, &x.Repository, &x.Approved, &x.LastScan); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func listExternalSources(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	rows, err := k.db.QueryContext(ctx, "SELECT id,root_id,path,content_hash,state,language,indexed_at FROM external_sources WHERE root_id=? ORDER BY path LIMIT 10000", str(r["rootId"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExternalSource{}
	for rows.Next() {
		var x ExternalSource
		if err = rows.Scan(&x.ID, &x.RootID, &x.Path, &x.Hash, &x.State, &x.Language, &x.IndexedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func defaultExternalExclude(name string) bool {
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		switch part {
		case ".git", ".svn", ".hg", "node_modules", "vendor", ".ssh", ".aws", ".gnupg", ".azure", ".kube", ".npm", ".npmrc", ".netrc", ".pypirc", ".git-credentials", ".venv", "venv", "__pycache__", ".next", "dist", "build", "target", "coverage", ".ds_store", "workspace.db", "credentials", "credentials.json", "secrets.json", "id_rsa", "id_ed25519", "id_ecdsa":
			return true
		}
		if strings.HasPrefix(part, "._") || part == ".env" || strings.HasPrefix(part, ".env.") {
			return true
		}
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".pem", ".key", ".p12", ".pfx", ".keystore", ".sqlite", ".db", ".zip", ".gz", ".tar", ".exe", ".dll", ".so", ".dylib", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".mp4", ".mp3", ".pdf", ".docx", ".woff", ".woff2":
		return true
	}
	return false
}

type externalIgnore struct {
	base, pattern string
	negative      bool
}

func loadExternalIgnore(root *os.Root, dir string) []externalIgnore {
	out := []externalIgnore{}
	for _, filename := range []string{".gitignore", ".troveignore"} {
		name := path.Join(dir, filename)
		st, err := root.Lstat(name)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 65536 {
			continue
		}
		f, err := root.Open(name)
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(f, 65537))
		f.Close()
		if err != nil || len(b) > 65536 {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			neg := strings.HasPrefix(line, "!")
			line = strings.TrimPrefix(line, "!")
			out = append(out, externalIgnore{dir, line, neg})
		}
	}
	return out
}
func externalIgnored(name string, dir bool, rules []externalIgnore) bool {
	ignored := false
	for _, rule := range rules {
		rel := strings.TrimPrefix(name, strings.TrimSuffix(rule.base, "/")+"/")
		if rule.base == "." {
			rel = name
		} else if rel == name {
			continue
		}
		pattern := rule.pattern
		directory := strings.HasSuffix(pattern, "/")
		pattern = strings.Trim(pattern, "/")
		if pattern == "" {
			continue
		}
		matched := false
		parts := strings.Split(rel, "/")
		if !strings.Contains(pattern, "/") {
			for i, part := range parts {
				ok, _ := path.Match(pattern, part)
				if ok && (!directory || i < len(parts)-1 || dir) {
					matched = true
				}
			}
		} else {
			rx := regexp.QuoteMeta(pattern)
			rx = strings.ReplaceAll(rx, `\*\*/`, `(?:.*/)?`)
			rx = strings.ReplaceAll(rx, `\*\*`, `.*`)
			rx = strings.ReplaceAll(rx, `\*`, `[^/]*`)
			rx = strings.ReplaceAll(rx, `\?`, `[^/]`)
			matched, _ = regexp.MatchString("^"+rx+"(?:/.*)?$", rel)
		}
		if matched {
			ignored = !rule.negative
		}
	}
	return ignored
}
func walkApprovedRoot(ctx context.Context, root *os.Root, fn func(string, *os.File, os.FileInfo) error) error {
	count := 0
	var total int64
	var walk func(string, []externalIgnore) error
	walk = func(dir string, rules []externalIgnore) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		rules = append(append([]externalIgnore{}, rules...), loadExternalIgnore(root, dir)...)
		folder, err := root.Open(dir)
		if err != nil {
			return err
		}
		entries, err := folder.ReadDir(-1)
		folder.Close()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := path.Join(dir, entry.Name())
			if defaultExternalExclude(name) || externalIgnored(name, entry.IsDir(), rules) {
				continue
			}
			st, err := root.Lstat(name)
			if err != nil {
				return err
			}
			if st.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if st.IsDir() {
				if strings.Count(name, "/") >= 64 {
					return Fail("RESOURCE_EXHAUSTED", "Folder nesting exceeds 64 levels")
				}
				if err = walk(name, rules); err != nil {
					return err
				}
				continue
			}
			if !st.Mode().IsRegular() || st.Size() > 4<<20 {
				continue
			}
			count++
			total += st.Size()
			if count > 10000 || total > 128<<20 {
				return Fail("RESOURCE_EXHAUSTED", "Index scan exceeds 10,000 files or 128 MiB")
			}
			f, err := root.Open(name)
			if err != nil {
				return err
			}
			actual, err := f.Stat()
			if err != nil || !os.SameFile(st, actual) || !actual.Mode().IsRegular() {
				f.Close()
				return Fail("CONFLICT", "Source changed while opening")
			}
			err = fn(name, f, actual)
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	return walk(".", nil)
}

var codeSymbol = regexp.MustCompile(`^\s*(?:(?:export|public|private|protected|static|async|pub)\s+)*(?:func|function|def|class|interface|type|struct|enum|fn)\s+([^\s({:]+)`)

func languageChunks(name, text string) (string, []ExternalChunk) {
	languages := map[string]string{".go": "go", ".py": "python", ".js": "javascript", ".jsx": "javascript", ".ts": "typescript", ".tsx": "typescript", ".rs": "rust", ".java": "java", ".c": "c", ".h": "c", ".cpp": "cpp", ".rb": "ruby", ".swift": "swift", ".md": "markdown"}
	lang := languages[strings.ToLower(path.Ext(name))]
	if lang == "" {
		lang = "text"
	}
	lines := strings.Split(text, "\n")
	chunks := []ExternalChunk{}
	start := 0
	symbol := ""
	size := 0
	flush := func(end int) {
		if end > start {
			chunks = append(chunks, ExternalChunk{start + 1, end, symbol, strings.Join(lines[start:end], "\n")})
		}
		start = end
		size = 0
	}
	for i, line := range lines {
		m := codeSymbol.FindStringSubmatch(line)
		heading := lang == "markdown" && strings.HasPrefix(line, "#")
		if i > start && (i-start >= 80 || size+len(line) > 8192 || len(m) > 0 || heading) {
			flush(i)
		}
		if len(m) > 0 {
			symbol = m[1]
		} else if heading {
			symbol = strings.TrimLeft(line, "# ")
		}
		size += len(line) + 1
	}
	flush(len(lines))
	return lang, chunks
}
func scanExternalRoot(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	var selected ExternalRoot
	err := k.db.QueryRowContext(ctx, "SELECT id,path,identity,repository,approved FROM external_roots WHERE id=?", str(r["rootId"])).Scan(&selected.ID, &selected.Path, &selected.Identity, &selected.Repository, &selected.Approved)
	if err != nil {
		return nil, err
	}
	if !selected.Approved {
		return nil, Fail("SCOPE_DENIED", "Folder approval was revoked")
	}
	root, err := os.OpenRoot(selected.Path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = k.write(ctx, func(tx *sql.Tx) error {
				if _, er := tx.ExecContext(ctx, "UPDATE external_sources SET state='missing' WHERE root_id=?", selected.ID); er != nil {
					return er
				}
				_, er := tx.ExecContext(ctx, "DELETE FROM external_chunks WHERE source_id IN(SELECT id FROM external_sources WHERE root_id=?)", selected.ID)
				return er
			})
		}
		return nil, err
	}
	defer root.Close()
	st, err := root.Stat(".")
	if err != nil || externalIdentity(st) != selected.Identity {
		_ = k.write(ctx, func(tx *sql.Tx) error {
			if err := requireOwnerScope(k, e); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE external_sources SET state='missing' WHERE root_id=?", selected.ID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "DELETE FROM external_chunks WHERE source_id IN(SELECT id FROM external_sources WHERE root_id=?)", selected.ID)
			return err
		})
		return nil, Fail("SCOPE_DENIED", "Approved folder identity changed; select it again")
	}
	type source struct {
		entry  ExternalSource
		chunks []ExternalChunk
	}
	sources := []source{}
	stamp := now()
	err = walkApprovedRoot(ctx, root, func(name string, f *os.File, info fs.FileInfo) error {
		b, er := io.ReadAll(io.LimitReader(f, (4<<20)+1))
		if er != nil {
			return er
		}
		if len(b) > 4<<20 || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
			return nil
		}
		if strings.Contains(string(b), "PRIVATE KEY-----") {
			return nil
		}
		lang, chunks := languageChunks(name, string(b))
		sources = append(sources, source{ExternalSource{NewID(), selected.ID, name, externalIdentity(info), importDigest(b), "present", lang, stamp}, chunks})
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	moved := 0
	err = k.write(ctx, func(tx *sql.Tx) error {
		if err := requireOwnerScope(k, e); err != nil {
			return err
		}
		var approved bool
		if err := tx.QueryRowContext(ctx, "SELECT approved FROM external_roots WHERE id=?", selected.ID).Scan(&approved); err != nil {
			return err
		}
		if !approved {
			return Fail("SCOPE_DENIED", "Folder approval was revoked during scan")
		}
		// Keep absent identities and page reference rows. Only their derived chunks disappear.
		if _, err := tx.ExecContext(ctx, "UPDATE external_sources SET state='missing' WHERE root_id=?", selected.ID); err != nil {
			return err
		}
		for _, s := range sources {
			x := s.entry
			var old, oldPath string
			er := tx.QueryRowContext(ctx, "SELECT id,path FROM external_sources WHERE root_id=? AND identity=? ORDER BY indexed_at DESC LIMIT 1", selected.ID, x.Identity).Scan(&old, &oldPath)
			if er == nil {
				x.ID = old
				if oldPath != x.Path {
					var conflicting string
					ce := tx.QueryRowContext(ctx, "SELECT id FROM external_sources WHERE root_id=? AND path=?", selected.ID, x.Path).Scan(&conflicting)
					if ce == nil && conflicting != old {
						if _, er = tx.ExecContext(ctx, "UPDATE external_sources SET path=path||' [replaced '||id||']' WHERE id=?", conflicting); er != nil {
							return er
						}
					}
					moved++
				}
			} else if er != sql.ErrNoRows {
				return er
			} else {
				er = tx.QueryRowContext(ctx, "SELECT id FROM external_sources WHERE root_id=? AND path=?", selected.ID, x.Path).Scan(&old)
				if er == nil {
					x.ID = old
				} else if er != sql.ErrNoRows {
					return er
				}
			}
			_, er = tx.ExecContext(ctx, "INSERT INTO external_sources VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET path=excluded.path,identity=excluded.identity,content_hash=excluded.content_hash,state='present',language=excluded.language,indexed_at=excluded.indexed_at,extractor_version=excluded.extractor_version", x.ID, x.RootID, x.Path, x.Identity, x.Hash, x.State, x.Language, x.IndexedAt, "trove-external-v1")
			if er != nil {
				return er
			}
			if _, er = tx.ExecContext(ctx, "DELETE FROM external_chunks WHERE source_id=?", x.ID); er != nil {
				return er
			}
			for i, c := range s.chunks {
				if _, er = tx.ExecContext(ctx, "INSERT INTO external_chunks VALUES(?,?,?,?,?,?)", x.ID, i, c.StartLine, c.EndLine, c.Symbol, c.Text); er != nil {
					return er
				}
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM external_chunks WHERE source_id IN(SELECT id FROM external_sources WHERE root_id=? AND state='missing')", selected.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE external_roots SET last_scan=? WHERE id=?", stamp, selected.ID)
		return err
	})
	return map[string]any{"indexed": len(sources), "moved": moved, "scannedAt": stamp}, err
}
func revokeExternalRoot(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	err := k.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE external_roots SET approved=0 WHERE id=?", str(r["rootId"])); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM external_chunks WHERE source_id IN(SELECT id FROM external_sources WHERE root_id=?)", str(r["rootId"]))
		return err
	})
	return map[string]any{"revoked": err == nil}, err
}
func searchExternalSources(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	query := str(r["query"])
	if len(query) < 2 || len(query) > 256 {
		return nil, Fail("INVALID_REQUEST", "Search must contain 2–256 characters")
	}
	rows, err := k.db.QueryContext(ctx, "SELECT s.id,s.path,s.language,c.start_line,c.end_line,c.symbol,c.text FROM external_chunks c JOIN external_sources s ON s.id=c.source_id JOIN external_roots r ON r.id=s.root_id WHERE r.approved=1 AND s.state='present' AND instr(lower(c.text),lower(?))>0 LIMIT 50", query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var id, name, lang string
		var c ExternalChunk
		if err = rows.Scan(&id, &name, &lang, &c.StartLine, &c.EndLine, &c.Symbol, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"sourceId": id, "path": name, "language": lang, "chunk": c})
	}
	return out, rows.Err()
}
func referenceExternalSource(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	pageID := str(r["pageId"])
	if err := k.authorizePage(ctx, k.db, e, pageID); err != nil {
		return nil, err
	}
	err := k.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO external_page_references(source_id,page_id) VALUES(?,?)", str(r["sourceId"]), pageID)
		return err
	})
	return map[string]any{"referenced": err == nil}, err
}

// WatchExternalRoots polls approved roots only while the workspace is open.
// Cancellation is tied to the app job lifecycle; it does not schedule OS work.
// Errors are emitted only when the per-root diagnostic changes.
func (k *Kernel) WatchExternalRoots(ctx context.Context) {
	e := k.OwnerContext()
	defer k.RevokeGrant(string(e.ScopeGrantId))
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	diagnostics := map[string]string{}
	scan := func() {
		out, err := listExternalRoots(ctx, k, e, nil)
		if err != nil {
			return
		}
		for _, root := range out.([]ExternalRoot) {
			if !root.Approved {
				continue
			}
			bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
			result, err := scanExternalRoot(bounded, k, e, map[string]any{"rootId": root.ID})
			cancel()
			if ctx.Err() != nil {
				return
			}
			message := ""
			if err != nil {
				message = err.Error()
			}
			if message != diagnostics[root.ID] {
				k.publish(marshal(map[string]any{"eventType": "index.diagnostic", "workspaceId": k.id, "rootId": root.ID, "message": message, "result": result}))
				diagnostics[root.ID] = message
			}
		}
	}
	scan()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scan()
		}
	}
}
