package workspace

// Imports retain source bytes before interpreting them. An import plan is a
// durable inventory, not a promise that a source export contains everything.
import (
	"archive/zip"
	"bytes"
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
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"trove.local/phase0/contracts"
	"unicode/utf8"
)

const importSchema = `
CREATE TABLE IF NOT EXISTS import_reviewed_identities_v1(source_system TEXT NOT NULL,source_namespace TEXT NOT NULL,identity_kind TEXT NOT NULL,identity_value TEXT NOT NULL,source_locator TEXT NOT NULL,binding_json TEXT NOT NULL CHECK(json_valid(binding_json)),PRIMARY KEY(source_system,source_namespace,identity_kind,identity_value,source_locator));
CREATE TABLE IF NOT EXISTS import_runs(id TEXT PRIMARY KEY,plan_hash TEXT NOT NULL,plan_json TEXT NOT NULL CHECK(json_valid(plan_json)),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS import_mapping_state(mapping_id TEXT PRIMARY KEY REFERENCES import_mappings(id),local_revision TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS import_baselines(mapping_id TEXT PRIMARY KEY REFERENCES import_mappings(id),normalizer_version INTEGER NOT NULL,snapshot_json TEXT NOT NULL CHECK(json_valid(snapshot_json)),source_revision TEXT NOT NULL,raw_file_id TEXT NOT NULL REFERENCES files(id),import_run_id TEXT NOT NULL REFERENCES import_runs(id));
CREATE TABLE IF NOT EXISTS import_entity_ids(source_system TEXT NOT NULL,source_namespace TEXT NOT NULL,entity_kind TEXT NOT NULL,source_identity TEXT NOT NULL,local_id TEXT NOT NULL,PRIMARY KEY(source_system,source_namespace,entity_kind,source_identity));
CREATE TABLE IF NOT EXISTS import_csv_bindings(source_system TEXT NOT NULL,source_namespace TEXT NOT NULL,source_identity TEXT NOT NULL,binding_json TEXT NOT NULL CHECK(json_valid(binding_json)),PRIMARY KEY(source_system,source_namespace,source_identity));
`
const maxImportEntries = 10000
const maxImportEntryBytes int64 = 64 * 1024 * 1024
const maxImportTotalBytes int64 = 1024 * 1024 * 1024

type ImportColumn struct {
	SourceHeader *string  `json:"sourceHeader,omitempty"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	PropertyID   string   `json:"propertyId"`
	Samples      []string `json:"samples"`
	Warning      string   `json:"warning,omitempty"`
}
type ImportBlock struct {
	ID   string         `json:"id"`
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}
type ImportItem struct {
	IdentityReview          *ImportIdentityReview                   `json:"identityReview,omitempty"`
	IdentityAlias           string                                  `json:"identityAlias,omitempty"`
	SourceDeletionProof     *ImportSourceDeletionProof              `json:"sourceDeletionProof,omitempty"`
	CollectionSourceScope   string                                  `json:"collectionSourceScope,omitempty"`
	PortableIdentity        string                                  `json:"portableIdentity,omitempty"`
	PageSourceScope         string                                  `json:"pageSourceScope,omitempty"`
	AttachmentsComplete     bool                                    `json:"attachmentsComplete,omitempty"`
	AttachmentRelationships map[string]ImportAttachmentRelationship `json:"attachmentRelationships,omitempty"`
	CSVKeyColumn            string                                  `json:"csvKeyColumn,omitempty"`
	MergeGuards             map[string]string                       `json:"mergeGuards,omitempty"`
	ResolvedFields          map[string]json.RawMessage              `json:"resolvedFields,omitempty"`
	Merge                   *ImportMergeReview                      `json:"merge,omitempty"`
	Incoming                *ImportSnapshot                         `json:"incomingSnapshot,omitempty"`
	SourceSystem            string                                  `json:"sourceSystem,omitempty"`
	DisplayPreferences      *PageDisplayPreferences                 `json:"displayPreferences,omitempty"`
	Locator                 string                                  `json:"sourceLocator"`
	Kind                    string                                  `json:"kind"`
	Outcome                 string                                  `json:"outcome"`
	Reason                  string                                  `json:"reason"`
	RawFileID               string                                  `json:"retainedFileId,omitempty"`
	Hash                    string                                  `json:"hash,omitempty"`
	Size                    int64                                   `json:"sizeBytes"`
	PageID                  string                                  `json:"pageId,omitempty"`
	ParentID                string                                  `json:"parentPageId,omitempty"`
	Title                   string                                  `json:"title,omitempty"`
	ExternalID              string                                  `json:"externalId,omitempty"`
	IdentityKind            string                                  `json:"identityKind,omitempty"`
	MappingID               string                                  `json:"mappingId,omitempty"`
	ExpectedRevision        string                                  `json:"expectedRevision,omitempty"`
	Conflict                string                                  `json:"conflict,omitempty"`
	Choice                  string                                  `json:"choice,omitempty"`
	Committed               bool                                    `json:"committed"`
	Blocks                  []ImportBlock                           `json:"blocks,omitempty"`
	BlockParents            map[string]string                       `json:"blockParents,omitempty"`
	BlockSources            map[string]string                       `json:"blockSources,omitempty"`
	Columns                 []ImportColumn                          `json:"columns,omitempty"`
	Cells                   []string                                `json:"cells,omitempty"`
	CollectionID            string                                  `json:"collectionId,omitempty"`
	RecordID                string                                  `json:"recordId,omitempty"`
	CSVSource               string                                  `json:"csvSource,omitempty"`
	Provenance              any                                     `json:"provenance,omitempty"`
}
type ImportPlan struct {
	CollectionEnumerationScope string                  `json:"collectionEnumerationScope,omitempty"`
	PageDeletionOperationID    string                  `json:"pageDeletionOperationId,omitempty"`
	PageEnumerationScope       string                  `json:"pageEnumerationScope,omitempty"`
	CSVBindings                []ImportCSVBinding      `json:"csvBindings,omitempty"`
	EntityIdentities           []ImportEntityIdentity  `json:"entityIdentities,omitempty"`
	TypedDatabaseMerge         bool                    `json:"typedDatabaseMerge,omitempty"`
	NormalizerVersion          int                     `json:"normalizerVersion,omitempty"`
	NotionGraph                bool                    `json:"notionGraph,omitempty"`
	NotionKeepGraph            bool                    `json:"notionKeepGraph,omitempty"`
	NotionCopyGraph            bool                    `json:"notionCopyGraph,omitempty"`
	NotionContainers           []NotionContainerImport `json:"notionContainers,omitempty"`
	NotionIdentities           []NotionIdentityImport  `json:"notionIdentities,omitempty"`
	RootParentID               string                  `json:"rootParentPageId,omitempty"`
	ExpectedParentRevision     string                  `json:"expectedParentRevision,omitempty"`
	CaptureDigest              string                  `json:"captureDigest,omitempty"`
	CaptureExtension           string                  `json:"captureExtension,omitempty"`
	CaptureDestination         string                  `json:"captureDestination,omitempty"`
	CaptureDestinationTitle    string                  `json:"captureDestinationTitle,omitempty"`

	PortableCollections []PortableCollectionImport `json:"portableCollections,omitempty"`
	ID                  string                     `json:"planId"`
	Hash                string                     `json:"planHash"`
	Namespace           string                     `json:"sourceNamespace"`
	SourceSystem        string                     `json:"sourceSystem"`
	SourceFileID        string                     `json:"sourceFileId"`
	SourceName          string                     `json:"sourceName"`
	RootID              string                     `json:"rootPageId"`
	RootSkipped         bool                       `json:"rootSkipped,omitempty"`
	State               string                     `json:"state"`
	Cursor              int                        `json:"cursor"`
	Initialized         bool                       `json:"initialized"`
	Items               []ImportItem               `json:"items"`
	SourceCompleteness  string                     `json:"sourceCompleteness"`
	SourceOmissions     []string                   `json:"sourceOmissions"`
	UnknownCompleteness []string                   `json:"unknownCompleteness"`
	CommittedBatchIDs   []string                   `json:"committedBatchIds"`
	CreatedAt           string                     `json:"createdAt"`
	JobID               string                     `json:"jobId,omitempty"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error { return k.initImports(ctx) })
	RegisterQuery("imports.preview", previewPortableImport)
	RegisterQuery("previewImport", previewCanonicalImport)
	RegisterQuery("imports.plan", getPortableImport)
	RegisterQuery("imports.configure", configurePortableImport)
	RegisterQuery("imports.commit", commitPortableImport)
	RegisterQuery("imports.list", listPortableImports)
	RegisterQuery("imports.enqueue", enqueuePortableImport)
	RegisterQuery("getImportReport", getPortableImportReport)
	RegisterCommand("import.applyPlan", applyPortableImport)
	RegisterJobHandler("IMPORT_NOTION", runPortableImportJob)
}
func (k *Kernel) initImports(ctx context.Context) error {
	return k.write(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, importSchema); return err })
}
func importDigest(b []byte) string       { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func importPlanHash(p ImportPlan) string { p.Hash = ""; return importDigest(marshal(p)) }
func loadImportPlan(ctx context.Context, q querier, id string) (ImportPlan, error) {
	var p ImportPlan
	var raw string
	err := q.QueryRowContext(ctx, "SELECT plan_json FROM import_runs WHERE id=?", id).Scan(&raw)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}
func saveImportPlan(ctx context.Context, tx *sql.Tx, p ImportPlan) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO import_runs VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET plan_hash=excluded.plan_hash,plan_json=excluded.plan_json,updated_at=excluded.updated_at", p.ID, p.Hash, string(marshal(p)), p.CreatedAt, now())
	return err
}

// storeImportBytes never constructs a pathname from a ZIP member. Publication
// uses the content-addressed asset store and checks hash and size on reuse.
func (k *Kernel) storeImportBytes(ctx context.Context, name string, b []byte) (string, error) {
	digest := importDigest(b)
	f, err := os.CreateTemp(filepath.Join(k.path, ".staging"), "import-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return "", err
	}
	if err = k.publishAsset(f.Name(), digest, int64(len(b))); err != nil {
		return "", err
	}
	media := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if media == "" {
		media = http.DetectContentType(b)
	}
	id := NewID()
	err = k.write(ctx, func(tx *sql.Tx) error {
		if policy, ok := ctx.Value(importExternalPolicyKey{}).(*externalContextPolicy); ok && policy != nil {
			if err := retainFileExternalContextPolicy(ctx, tx, digest, *policy); err != nil {
				return err
			}
		}
		var existing string
		e := tx.QueryRowContext(ctx, "SELECT id FROM files WHERE content_hash=?", digest).Scan(&existing)
		if e == nil {
			id = existing
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO files VALUES(?,?,?,?,?,?,?,1)", id, k.id, digest, path.Base(name), media, len(b), "files/"+digest[:2]+"/"+digest)
		return e
	})
	return id, err
}
func (k *Kernel) readImportBytes(ctx context.Context, id string) ([]byte, error) {
	var digest string
	var size int64
	if err := k.db.QueryRowContext(ctx, "SELECT content_hash,size_bytes FROM files WHERE id=?", id).Scan(&digest, &size); err != nil {
		return nil, err
	}
	if size > maxImportTotalBytes {
		return nil, Fail("RESOURCE_EXHAUSTED", "Import exceeds 1 GiB")
	}
	name := filepath.Join(k.path, "files", digest[:2], digest)
	if err := verifyAsset(name, digest, size); err != nil {
		return nil, err
	}
	f, err := openAsset(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxImportTotalBytes+1))
}
func safeImportName(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return false
		}
	}
	return path.Clean(name) != "."
}

func previewPortableImport(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	namespace := strings.TrimSpace(str(r["sourceNamespace"]))
	if namespace == "" || len(namespace) > 512 {
		return nil, Fail("INVALID_REQUEST", "Choose a stable source namespace (up to 512 characters) for re-import matching")
	}
	handle := str(r["sourceHandle"])
	if handle == "" {
		handle = str(r["selectionHandle"])
	}
	state := k.recoveryState()
	state.mu.Lock()
	selection, ok := state.selections[handle]
	if ok {
		delete(state.selections, handle)
	}
	state.mu.Unlock()
	if !ok {
		return nil, Fail("NOT_FOUND", "Select an import file again")
	}
	defer selection.file.Close()
	if time.Now().After(selection.expires) {
		return nil, Fail("NOT_FOUND", "Selection expired")
	}
	emitProgress := func(stage string, completed, total int64) {
		k.publish(marshal(map[string]any{"eventType": "imports.previewProgress", "workspaceId": k.ID(), "requestId": str(r["requestId"]), "stage": stage, "completed": completed, "total": total}))
	}
	var sourceBytes int64
	if info, err := selection.file.Stat(); err == nil {
		sourceBytes = info.Size()
	}
	emitProgress("reading", 0, sourceBytes)
	reader := &importPreviewReader{ctx: ctx, reader: io.LimitReader(selection.file, maxImportTotalBytes+1), progress: func(n int64) { emitProgress("reading", n, sourceBytes) }}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxImportTotalBytes {
		return nil, Fail("RESOURCE_EXHAUSTED", "Import exceeds 1 GiB")
	}
	sourcePolicy, err := fileExternalContextPolicy(ctx, k.db, importDigest(data))
	if err != nil {
		return nil, err
	}
	if sourcePolicy != nil {
		ctx = context.WithValue(ctx, importExternalPolicyKey{}, sourcePolicy)
	}
	p := ImportPlan{ID: NewID(), Namespace: namespace, SourceSystem: "notion", SourceName: selection.name, RootID: NewID(), State: "preview", SourceCompleteness: "unknown", SourceOmissions: []string{}, UnknownCompleteness: []string{"The export alone cannot establish whether all original pages, comments, permissions, history, formulas, relations, or views were supplied."}, Items: []ImportItem{}, CommittedBatchIDs: []string{}, CreatedAt: now()}
	if str(r["sourceSystem"]) != "" {
		p.SourceSystem = str(r["sourceSystem"])
	}
	p.SourceFileID, err = k.storeImportBytes(ctx, selection.name, data)
	if err != nil {
		return nil, err
	}
	var total int64
	add := func(name string, b []byte, directory bool, reason string) error {
		item := ImportItem{Locator: name, Kind: "unsupported", Outcome: "unsupported_but_retained", Reason: "Original bytes retained with a readable fallback", Title: sourceTitle(name), Size: int64(len(b)), PageID: NewID(), Blocks: []ImportBlock{}}
		if reason != "" {
			item.Kind = "rejected"
			item.Outcome = "failed"
			item.Reason = reason + "; original container retained"
			item.RawFileID = p.SourceFileID
			item.PageID = ""
			p.Items = append(p.Items, item)
			return nil
		}
		if directory {
			item.Kind = "directory"
			item.Outcome = "preserved"
			item.Reason = "Directory hierarchy retained"
			item.PageID = ""
			p.Items = append(p.Items, item)
			return nil
		}
		var er error
		item.RawFileID, er = k.storeImportBytes(ctx, name, b)
		if er != nil {
			return er
		}
		item.Hash = importDigest(b)
		switch strings.ToLower(path.Ext(name)) {
		case ".html", ".htm":
			item.Kind = "html"
			item.Outcome = "adapted"
			item.Reason = "Inert HTML converted to canonical blocks; exact source retained"
		case ".txt", ".docx":
			item.Kind = "document"
			item.Outcome = "adapted"
			item.Reason = "Readable document content extracted; exact source retained"
		case ".md", ".markdown":
			item.Kind = "markdown"
			item.Outcome = "adapted"
			item.Reason = "Readable Markdown converted; exact original retained"
		case ".csv":
			item.Kind = "csv"
			item.Outcome = "adapted"
			item.Reason = "CSV display values imported with reviewed types; exact source retained"
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".pdf", ".wav", ".mp3", ".m4a", ".ogg", ".mp4", ".mov":
			item.Kind = "asset"
			item.Outcome = "preserved"
			item.Reason = "Managed attachment bytes preserved"
			item.PageID = ""
		}
		if (item.Kind == "markdown" || item.Kind == "html" || item.Kind == "csv") && !utf8.Valid(b) {
			item.Kind = "unsupported"
			item.Outcome = "unsupported_but_retained"
			item.Reason = "Non-UTF-8 text retained as original bytes; convert encoding before re-import"
		}
		p.Items = append(p.Items, item)
		return nil
	}
	if strings.EqualFold(path.Ext(selection.name), ".zip") {
		zr, er := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if er != nil {
			return nil, Fail("INVALID_REQUEST", "Cannot read ZIP: "+er.Error())
		}
		if len(zr.File) > maxImportEntries {
			return nil, Fail("RESOURCE_EXHAUSTED", "ZIP exceeds 10,000 entries; split the export")
		}
		seen := map[string]bool{}
		lastProgress := time.Time{}
		for index, entry := range zr.File {
			if time.Since(lastProgress) >= 100*time.Millisecond {
				emitProgress("inventory", int64(index), int64(len(zr.File)))
				lastProgress = time.Now()
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			name := entry.Name
			reason := ""
			clean := path.Clean(name)
			fold := strings.ToLower(clean)
			if !safeImportName(name) {
				reason = "Unsafe archive member path"
			} else if entry.Mode()&os.ModeSymlink != 0 || (!entry.Mode().IsRegular() && !entry.FileInfo().IsDir()) {
				reason = "Links and special archive members are not extracted"
			} else if seen[fold] {
				reason = "Duplicate or case-colliding archive member"
			} else if entry.UncompressedSize64 > uint64(maxImportEntryBytes) {
				reason = "Member exceeds 64 MiB"
			} else if entry.CompressedSize64 > 0 && entry.UncompressedSize64/entry.CompressedSize64 > 1000 {
				reason = "Suspicious compression ratio"
			}
			seen[fold] = true
			if reason != "" || entry.FileInfo().IsDir() {
				if err = add(name, nil, entry.FileInfo().IsDir(), reason); err != nil {
					return nil, err
				}
				continue
			}
			if total+int64(entry.UncompressedSize64) > maxImportTotalBytes {
				if err = add(name, nil, false, "Expanded archive exceeds 1 GiB"); err != nil {
					return nil, err
				}
				continue
			}
			reader, er := entry.Open()
			if er != nil {
				add(name, nil, false, "Cannot decode member: "+er.Error())
				continue
			}
			b, er := io.ReadAll(io.LimitReader(reader, maxImportEntryBytes+1))
			reader.Close()
			if er != nil || int64(len(b)) > maxImportEntryBytes {
				add(name, nil, false, "Member failed CRC/size/decode validation")
				continue
			}
			total += int64(len(b))
			if err = add(clean, b, false, ""); err != nil {
				return nil, err
			}
		}
	} else {
		if int64(len(data)) > maxImportEntryBytes {
			return nil, Fail("RESOURCE_EXHAUSTED", "Text import exceeds 64 MiB")
		}
		if err = add(selection.name, data, false, ""); err != nil {
			return nil, err
		}
	}
	emitProgress("parsing", int64(len(p.Items)), int64(len(p.Items)))
	if err = k.parseImportPlan(ctx, &p); err != nil {
		return nil, err
	}
	if len(p.Items) > maxImportEntries {
		return nil, Fail("RESOURCE_EXHAUSTED", "Expanded import exceeds 10,000 files and rows; split the export")
	}
	p.Hash = importPlanHash(p)
	err = k.write(ctx, func(tx *sql.Tx) error { return saveImportPlan(ctx, tx, p) })
	if err != nil {
		return nil, err
	}
	emitProgress("ready", int64(len(p.Items)), int64(len(p.Items)))
	return p, nil
}
func getPortableImport(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	return loadImportPlan(ctx, k.db, str(r["planId"]))
}
func listPortableImports(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	rows, err := k.db.QueryContext(ctx, "SELECT plan_json FROM import_runs ORDER BY created_at DESC LIMIT 50")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p ImportPlan
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"planId": p.ID, "sourceName": p.SourceName, "state": p.State, "cursor": p.Cursor, "total": len(p.Items), "createdAt": p.CreatedAt})
	}
	return out, rows.Err()
}
func getPortableImportReport(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	p, err := loadImportPlan(ctx, k.db, str(r["runId"]))
	if err != nil {
		return nil, err
	}
	return portableImportReport(p), nil
}
func portableImportReport(p ImportPlan) map[string]any {
	items := []any{}
	unresolved := []string{}
	for _, i := range p.Items {
		item := map[string]any{"sourceLocator": i.Locator, "outcome": i.Outcome, "reason": i.Reason}
		if i.RawFileID != "" {
			item["retainedFileId"] = i.RawFileID
		}
		items = append(items, item)
		if i.Outcome == "failed" {
			unresolved = append(unresolved, i.Locator)
		}
	}
	state := p.State
	if state == "complete" {
		state = "completed"
		for _, i := range p.Items {
			if i.Outcome == "failed" || i.Outcome == "partial" {
				state = "partial"
			}
		}
	}

	return map[string]any{"runId": p.ID, "jobId": importReportJobID(p), "state": state, "sourceCompleteness": p.SourceCompleteness, "discoveredCount": len(p.Items), "accountedCount": len(p.Items), "items": items, "committedBatchIds": p.CommittedBatchIDs, "unresolvedSourceIds": unresolved, "sourceOmissions": p.SourceOmissions}
}

func configurePortableImport(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	var p ImportPlan
	err := k.write(ctx, func(tx *sql.Tx) error {
		var err error
		p, err = loadImportPlan(ctx, tx, str(r["planId"]))
		if err != nil {
			return err
		}
		if p.Initialized {
			return Fail("CONFLICT", "A started import is immutable; resume it or create another preview")
		}
		if str(r["planHash"]) != p.Hash {
			return Fail("CONFLICT", "Preview changed; reload it")
		}
		if err := configureImportIdentities(ctx, k, tx, e, &p, asList(r["identityResolutions"])); err != nil {
			return err
		}
		changes, _ := r["columns"].([]any)
		for _, raw := range changes {
			c, ok := raw.(map[string]any)
			if !ok {
				return Fail("INVALID_REQUEST", "Invalid column override")
			}
			found := false
			for i := range p.Items {
				item := &p.Items[i]
				if item.Locator != str(c["sourceLocator"]) || item.Kind != "csv" {
					continue
				}
				index := int(number(c["index"]))
				if index < 1 || index >= len(item.Columns) {
					return Fail("INVALID_REQUEST", "Only non-title CSV columns may be changed")
				}
				kind := str(c["type"])
				if !validImportType(kind) {
					return Fail("INVALID_REQUEST", "Unsupported import property type")
				}
				item.Columns[index].Type = kind
				found = true
			}
			if !found {
				return Fail("NOT_FOUND", "CSV source not found")
			}
		}
		for _, raw := range asList(r["csvKeys"]) {
			binding := asObject(raw)
			found := false
			for i := range p.Items {
				if p.Items[i].Locator == str(binding["sourceLocator"]) && p.Items[i].Kind == "csv" {
					if err := configureCSVKey(ctx, tx, &p, i, int(number(binding["index"])), true); err != nil {
						return err
					}
					found = true
					break
				}
			}
			if !found {
				return Fail("INVALID_REQUEST", "Select a key before this CSV is converted into a typed import")
			}
		}
		resolutions, _ := r["resolutions"].([]any)
		for _, raw := range resolutions {
			resolution, _ := raw.(map[string]any)
			found := false
			for i := range p.Items {
				item := &p.Items[i]
				if item.Locator != str(resolution["sourceLocator"]) {
					continue
				}
				fields, _ := resolution["fields"].([]any)
				if err := configureImportMerge(item, fields); err != nil {
					return err
				}
				found = true
			}
			if !found {
				return Fail("NOT_FOUND", "Merge source not found")
			}
		}
		choices, _ := r["choices"].([]any)
		for _, raw := range choices {
			c, ok := raw.(map[string]any)
			if !ok {
				return Fail("INVALID_REQUEST", "Invalid conflict choice")
			}
			found := false
			for i := range p.Items {
				item := &p.Items[i]
				if item.Locator != str(c["sourceLocator"]) {
					continue
				}
				choice := str(c["choice"])
				if item.IdentityReview != nil && choice == "duplicate" {
					return Fail("CONFLICT", "A reviewed identity cannot be changed by a content choice; create a fresh identity preview")
				}
				if isSourceDeletion(item.Kind) && choice != "keep_local" && choice != "merge" {
					return Fail("INVALID_REQUEST", "A missing source page can only be kept or explicitly reviewed for deletion")
				}
				if choice != "keep_local" && choice != "replace" && choice != "duplicate" && choice != "merge" {
					return Fail("INVALID_REQUEST", "Choose merge, keep_local, replace, or duplicate")
				}
				if (item.Kind == "csv" || item.Kind == "portable_collection" || p.NotionGraph && notionGraphPage(*item)) && choice == "replace" {
					return Fail("INVALID_REQUEST", "Database schema replacement requires a separate reviewed schema migration; keep local or import a copy")
				}
				if choice == "merge" {
					if err := configureImportMerge(item, nil); err != nil {
						return err
					}
					if item.Choice != "merge" {
						return Fail("CONFLICT", "Resolve every conflicting field first")
					}
				} else {
					item.Choice = choice
				}
				found = true
			}
			if !found {
				return Fail("NOT_FOUND", "Conflict source not found")
			}
		}
		p.Hash = importPlanHash(p)
		return saveImportPlan(ctx, tx, p)
	})
	return p, err
}

// Files use deterministic ordering so parent records and databases commit before
// their descendants. Discovery order is otherwise not semantically significant.
func sortImportItems(items []ImportItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Kind == "row" && b.Kind != "row" {
			return false
		}
		if a.Kind != "row" && b.Kind == "row" {
			return true
		}
		di, dj := strings.Count(a.Locator, "/"), strings.Count(b.Locator, "/")
		if di != dj {
			return di < dj
		}
		return a.Locator < b.Locator
	})
}

func importReportJobID(p ImportPlan) string {
	if p.JobID != "" {
		return p.JobID
	}
	return p.ID
}
