package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/platformfs"
)

const supportContentLimit = 256 << 10
const supportBundleLimit = 2 << 20

// SupportSelection is consent to these exact categories on at most five pages.
// It is a native-service DTO, not an authorization or destination supplied by AI.
type SupportSelection struct {
	Diagnostics bool     `json:"diagnostics,omitempty"`
	PageIDs     []string `json:"pageIds"`
	Titles      bool     `json:"titles"`
	Text        bool     `json:"text"`
}
type SupportRuntime struct {
	AppVersion             string
	SourceRevision         string
	SourceModified         *bool
	NativeConfigured       bool `json:"nativeConfigured"`
	VisionConfigured       bool `json:"visionConfigured"`
	InstalledManifestCount *int `json:"installedManifestCount"`
}
type SupportFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}
type SupportPreview struct {
	Files     []SupportFile `json:"files"`
	ExpiresAt string        `json:"expiresAt"`
}
type SupportBundle struct {
	preview   SupportPreview
	kernel    *Kernel
	execution contracts.ServiceExecutionContext
	revisions map[string]string
	expires   time.Time
}

func (b *SupportBundle) Preview() SupportPreview {
	p := b.preview
	p.Files = append([]SupportFile(nil), p.Files...)
	return p
}

var supportExclusions = []string{"Workspace/account/model names and identifiers, file paths, credentials, job payloads and raw errors/logs are not collected.", "Attachments, database properties, linked/reusable content, OCR, transcript-linked blocks, history, archived/trash pages and raw block JSON are not collected.", "Optional selected page titles/text may contain secrets. No automatic secret redaction is promised for content you include.", "Metadata is a point-in-time snapshot; configured helpers do not prove device, permission, model quality or platform release qualification.", "Only explicitly selected structured operational diagnostics can be included; raw logs are never archived. Nothing is uploaded. This is not a full workspace backup."}

// PrepareSupportBundle uses the same authenticated owner-scope boundary as native
// export. No arbitrary diagnostic strings can enter the default allowlist.
func (k *Kernel) PrepareSupportBundle(ctx context.Context, e contracts.ServiceExecutionContext, selection SupportSelection, env SupportRuntime) (*SupportBundle, error) {
	return k.prepareSupportBundle(ctx, e, selection, env, nil)
}

// beforeFence provides deterministic race coverage without a public bypass.
func (k *Kernel) prepareSupportBundle(ctx context.Context, e contracts.ServiceExecutionContext, selection SupportSelection, env SupportRuntime, beforeFence func()) (*SupportBundle, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	if len(selection.PageIDs) > 5 || ((selection.Titles || selection.Text) != (len(selection.PageIDs) > 0)) {
		return nil, Fail("INVALID_REQUEST", "Choose one to five pages for optional content; metadata alone needs no pages")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	counts := map[string]int64{}
	for _, item := range []struct{ name, query string }{
		{"livePages", "SELECT count(*) FROM pages WHERE deleted_at IS NULL"},
		{"blocks", "SELECT count(*) FROM blocks"},
		{"jobs", "SELECT count(*) FROM jobs"},
		{"searchChunks", "SELECT count(*) FROM search_chunks"},
		{"pendingSearchPages", "SELECT count(*) FROM search_dirty"},
		{"embeddingRows", "SELECT count(*) FROM search_embeddings"},
		{"vectorGenerations", "SELECT count(*) FROM search_vector_models"},
	} {
		var n int64
		if err = tx.QueryRowContext(ctx, item.query).Scan(&n); err != nil {
			return nil, err
		}
		counts[item.name] = n
	}
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return nil, err
	}
	// SQL maps untrusted labels to constants before returning them. Aggregation
	// output is finite even if the workspace has many jobs or hostile labels.
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN kind IN ('IMPORT_NOTION','EXTRACT_DOCUMENT','CREATE_THUMBNAIL','INDEX_CONTENT','GENERATE_EMBEDDINGS','TRANSCRIBE_AUDIO','SUMMARIZE_MEETING','BACKUP_WORKSPACE','MODEL_PACK_ACQUIRE','INVALIDATE_MEMORY','AUTOMATION_RUN') THEN kind ELSE 'OTHER' END,
 CASE WHEN state IN ('queued','running','retry_wait','cancel_requested','cancelled','succeeded','failed','interrupted') THEN state ELSE 'OTHER' END, count(*) FROM jobs GROUP BY 1,2 ORDER BY 1,2`)
	if err != nil {
		return nil, err
	}
	jobs := []map[string]any{}
	for rows.Next() {
		var kind, state string
		var n int64
		if err = rows.Scan(&kind, &state, &n); err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, map[string]any{"kind": kind, "state": state, "count": n})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT CASE WHEN length(CAST(error_json AS BLOB))<=16384 AND json_valid(error_json) THEN CASE WHEN json_extract(error_json,'$.code') IN ('CANCELLED','INVALID_REQUEST','NOT_FOUND','SCOPE_DENIED','CONFLICT','RESOURCE_EXHAUSTED','INTERNAL','MODEL_INCOMPATIBLE','WORKSPACE_LOCKED','UNAVAILABLE') THEN json_extract(error_json,'$.code') ELSE 'OTHER' END ELSE 'OTHER' END,count(*) FROM jobs WHERE error_json IS NOT NULL GROUP BY 1 ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	codes := map[string]int64{}
	for rows.Next() {
		var code string
		var n int64
		if err = rows.Scan(&code, &n); err != nil {
			rows.Close()
			return nil, err
		}
		codes[code] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	capabilities := map[string]any{"nativeConfigured": env.NativeConfigured, "visionConfigured": env.VisionConfigured, "microphonePermission": "unknown_not_probed", "microphoneDevice": "unknown_not_probed", "pdf": "unknown_not_probed", "installedManifestCount": env.InstalledManifestCount, "manifestCountIntegrity": "not_verified"}
	if runtime.GOOS == "linux" {
		capabilities["pdf"] = "unavailable_containment"
	}
	if env.InstalledManifestCount != nil && (*env.InstalledManifestCount < 0 || *env.InstalledManifestCount > 4096) {
		return nil, Fail("INVALID_REQUEST", "Invalid bounded runtime inventory")
	}
	appVersion, sourceRevision := "unknown", "unknown"
	var sourceModified *bool
	if len(env.AppVersion) <= 64 && regexp.MustCompile(`^v?[0-9]{1,9}\.[0-9]{1,9}\.[0-9]{1,9}(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`).MatchString(env.AppVersion) {
		appVersion = env.AppVersion
	}
	if regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`).MatchString(env.SourceRevision) {
		sourceRevision = env.SourceRevision
		sourceModified = env.SourceModified
	}
	metadata := map[string]any{"formatVersion": 1, "application": "Trove", "appVersion": appVersion, "sourceRevision": sourceRevision, "sourceModified": sourceModified, "os": runtime.GOOS, "architecture": runtime.GOARCH, "capturedAt": time.Now().UTC().Format(time.RFC3339), "schemaUserVersion": version, "counts": counts, "jobs": jobs, "errorCodes": codes, "capabilities": capabilities}
	files := []SupportFile{}
	add := func(name string, v any) error {
		raw, er := json.MarshalIndent(v, "", "  ")
		if er == nil {
			files = append(files, SupportFile{name, string(raw) + "\n"})
		}
		return er
	}
	if err = add("metadata.json", metadata); err != nil {
		return nil, err
	}
	categories := []string{"metadata"}
	if selection.Diagnostics {
		if err = add("diagnostics.json", k.diagnostics.Snapshot()); err != nil {
			return nil, err
		}
		categories = append(categories, "diagnostics")
	}
	revisions := map[string]string{}
	content := []map[string]any{}
	total := 0
	for i, id := range selection.PageIDs {
		if _, exists := revisions[id]; exists {
			return nil, Fail("INVALID_REQUEST", "Duplicate page selection")
		}
		if err = contracts.Validate("domain", "Id", marshal(id)); err != nil {
			return nil, Fail("INVALID_REQUEST", "Invalid selected page")
		}
		var rev string
		var titleBytes, sourceBytes, blockCount int
		err = tx.QueryRowContext(ctx, `SELECT p.revision,length(CAST(p.title AS BLOB)),(SELECT coalesce(sum(length(CAST(data_json AS BLOB))),0) FROM blocks WHERE page_id=p.id),(SELECT count(*) FROM blocks WHERE page_id=p.id) FROM pages p WHERE p.id=? AND p.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM page_metadata m WHERE m.page_id=p.id AND m.archived=1)`, id).Scan(&rev, &titleBytes, &sourceBytes, &blockCount)
		if err != nil {
			return nil, Fail("CONFLICT", "A selected page is unavailable; choose pages again")
		}
		if titleBytes > supportContentLimit || (selection.Text && (sourceBytes > supportContentLimit || blockCount > 4096)) {
			return nil, Fail("RESOURCE_EXHAUSTED", "Selected content exceeds the bounded support preview")
		}
		revisions[id] = rev
		entry := map[string]any{"selection": i + 1}
		if selection.Titles {
			var title string
			if err = tx.QueryRowContext(ctx, "SELECT title FROM pages WHERE id=?", id).Scan(&title); err != nil {
				return nil, err
			}
			entry["title"] = title
			total += len(title)
		}
		if selection.Text {
			blocks, er := tx.QueryContext(ctx, "SELECT type,CASE WHEN EXISTS(SELECT 1 FROM transcript_segments s WHERE s.block_id=blocks.id) THEN NULL ELSE data_json END FROM blocks WHERE page_id=? ORDER BY sort_key,id LIMIT 4097", id)
			if er != nil {
				return nil, er
			}
			texts := []string{}
			omitted := 0
			for blocks.Next() {
				var kind string
				var raw sql.NullString
				if err = blocks.Scan(&kind, &raw); err != nil {
					blocks.Close()
					return nil, err
				}
				if !raw.Valid {
					omitted++
					continue
				}
				var data map[string]any
				if err = json.Unmarshal([]byte(raw.String), &data); err != nil {
					blocks.Close()
					return nil, err
				}
				s, ok := supportBlockText(kind, data)
				if !ok {
					omitted++
					continue
				}
				total += len(s)
				if total > supportContentLimit {
					blocks.Close()
					return nil, Fail("RESOURCE_EXHAUSTED", "Selected text exceeds 256 KiB")
				}
				texts = append(texts, s)
			}
			err = blocks.Err()
			blocks.Close()
			if err != nil {
				return nil, err
			}
			entry["blockText"] = texts
			entry["omittedBlocks"] = omitted
		}
		if total > supportContentLimit {
			return nil, Fail("RESOURCE_EXHAUSTED", "Selected content exceeds 256 KiB")
		}
		content = append(content, entry)
	}
	if selection.Titles {
		categories = append(categories, "selected_page_titles")
	}
	if selection.Text {
		categories = append(categories, "selected_page_text")
	}
	if len(content) > 0 {
		if err = add("selected-content.json", content); err != nil {
			return nil, err
		}
	}
	inventory := []map[string]any{}
	for _, f := range files {
		h := sha256.Sum256([]byte(f.Content))
		inventory = append(inventory, map[string]any{"name": f.Name, "bytes": len(f.Content), "sha256": hex.EncodeToString(h[:])})
	}
	if err = add("manifest.json", map[string]any{"formatVersion": 1, "categories": categories, "files": inventory, "exclusionsAndLimitations": supportExclusions}); err != nil {
		return nil, err
	}
	size := 0
	for _, f := range files {
		size += len(f.Content)
	}
	if size > supportBundleLimit {
		return nil, Fail("RESOURCE_EXHAUSTED", "Support preview exceeds 2 MiB")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// Close the historical snapshot before checking live revisions. The final
	// publication barrier serializes canonical mutation, close and revocation.
	if err = tx.Rollback(); err != nil {
		return nil, err
	}
	if beforeFence != nil {
		beforeFence()
	}
	if err = k.supportPublicationFence(ctx, e, revisions, func() error { return nil }); err != nil {
		return nil, err
	}
	expires := time.Now().Add(5 * time.Minute)
	return &SupportBundle{preview: SupportPreview{files, expires.UTC().Format(time.RFC3339)}, kernel: k, execution: e, revisions: revisions, expires: expires}, nil
}

func supportBlockText(kind string, data map[string]any) (string, bool) {
	// Deliberately exclude raw JSON, URLs, file names, references and embedded
	// databases. Unsupported blocks are counted, never recursively expanded.
	switch kind {
	case "paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list_item", "numbered_list_item", "quote", "callout", "toggle", "toggle_heading":
		return searchRichText(data["richText"])
	case "code":
		s, ok := data["source"].(string)
		return s, ok
	default:
		return "", false
	}
}

type supportWriter struct {
	ctx context.Context
	w   io.Writer
	n   int
}

func (w *supportWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if w.n+len(p) > supportBundleLimit {
		return 0, Fail("RESOURCE_EXHAUSTED", "Support archive exceeds 2 MiB")
	}
	n, err := w.w.Write(p)
	w.n += n
	return n, err
}

// ExportSupportBundle receives only a trusted native destination. The reviewed
// bytes are immutable; publication is no-replace and permission/revision fenced.
func (k *Kernel) ExportSupportBundle(ctx context.Context, e contracts.ServiceExecutionContext, b *SupportBundle, destination string) (err error) {
	if err = requireOwnerScope(k, e); err != nil {
		return err
	}
	if b == nil || b.kernel != k || string(marshal(b.execution)) != string(marshal(e)) || !time.Now().Before(b.expires) {
		return Fail("CONFLICT", "Support preview expired or belongs to another workspace; review again")
	}
	if !filepath.IsAbs(destination) {
		return Fail("INVALID_REQUEST", "Native destination must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return err
	}
	parent := filepath.Dir(destination)
	st, er := os.Lstat(parent)
	if er != nil {
		return er
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return Fail("INVALID_REQUEST", "Choose a regular destination folder")
	}
	staging := filepath.Join(parent, ".trove-support-"+NewID())
	if err = platformfs.EnsurePrivateDirectory(staging); err != nil {
		return err
	}
	defer func() {
		if cleanup := os.RemoveAll(staging); cleanup != nil {
			err = errors.Join(err, Fail("CLEANUP_REQUIRED", "Private support staging could not be removed from the chosen destination folder"))
		}
	}()
	staged := filepath.Join(staging, "bundle.zip")
	f, err := os.OpenFile(staged, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	writer := &supportWriter{ctx: ctx, w: f}
	archive := zip.NewWriter(writer)
	for _, item := range b.preview.Files {
		var out io.Writer
		out, err = archive.CreateHeader(&zip.FileHeader{Name: item.Name, Method: zip.Store})
		if err == nil {
			_, err = io.Copy(out, strings.NewReader(item.Content))
		}
		if err != nil {
			break
		}
	}
	err = errors.Join(err, archive.Close(), f.Close())
	if err != nil {
		return err
	}
	return k.supportPublicationFence(ctx, e, b.revisions, func() error {
		if !time.Now().Before(b.expires) {
			return Fail("CONFLICT", "Support preview expired; review again")
		}
		if err := platformfs.PublishFile(staged, destination, false); err != nil {
			return fmt.Errorf("Support archive was not confirmed saved: %w", err)
		}
		return nil
	})
}

func (k *Kernel) supportPublicationFence(ctx context.Context, e contracts.ServiceExecutionContext, revisions map[string]string, publish func() error) error {
	select {
	case k.writer <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-k.writer }()
	for id, revision := range revisions {
		var current string
		var active int
		err := k.db.QueryRowContext(ctx, "SELECT revision,deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM page_metadata WHERE page_id=pages.id AND archived=1) FROM pages WHERE id=?", id).Scan(&current, &active)
		if err != nil || current != revision || active != 1 {
			return Fail("CONFLICT", "Selected content changed; review a new support bundle")
		}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	grant, ok := k.grants[string(e.ScopeGrantId)]
	if k.closed || !ok || !bytes.Equal(marshal(grant), marshal(e)) {
		return Fail("SCOPE_DENIED", "Support permission is no longer valid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return publish()
}
