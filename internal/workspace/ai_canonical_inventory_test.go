package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// This test-only audit is deliberately independent of mutation handlers,
// RegisterSnapshotReader, backup projections and mutation-history counts.
// All persisted tables are protected unless named below. New tables therefore
// become protected automatically, including schema, every column and every row.
// In particular memory_entries/memory_sources, history, revisions, tombstones,
// files/references, approvals, and all page/database/meeting content are included.
// Exclusions permit generated drafts and rebuildable retrieval bookkeeping;
// none authorizes changing approved memories or canonical source content.
var aiCanonicalExcludedTables = map[string]string{
	"ai_runs":                "Generated run/draft output and citations; intentionally written by PrepareAI/FinishAI, not applied page content.",
	"ai_proposals":           "Unapplied review proposals; their canonical effects are protected in destination tables.",
	"search_chunks":          "Rebuildable text projection of protected canonical sources.",
	"search_dirty":           "Pending refresh bookkeeping for the derived text index.",
	"search_meta":            "Derived search-index generation/version metadata.",
	"search_fts":             "Derived full-text virtual index over search_chunks.",
	"search_fts_data":        "SQLite FTS5 shadow storage for the derived full-text index.",
	"search_fts_idx":         "SQLite FTS5 shadow storage for the derived full-text index.",
	"search_fts_docsize":     "SQLite FTS5 shadow storage for the derived full-text index.",
	"search_fts_config":      "SQLite FTS5 shadow storage for the derived full-text index.",
	"search_embeddings":      "Rebuildable embedding projection; actual model inputs are bound separately.",
	"search_vector_models":   "Derived vector-index identity registry; new vector tables remain protected by default.",
	"semantic_auto_attempts": "Derived semantic-index scheduling bookkeeping.",
	"document_extractions":   "Derived extraction status; source files and their bytes remain protected.",
	"document_sections":      "Rebuildable extracted-text projection; source files remain protected.",
	"sqlite_sequence":        "SQLite internal auto-increment allocation bookkeeping; committed event/history rows are protected.",
}

const aiCanonicalSnapshotLimit = 32 << 20

type aiCanonicalCell struct {
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
}
type aiCanonicalTable struct {
	Schema  string              `json:"schema"`
	Columns []string            `json:"columns"`
	Rows    [][]aiCanonicalCell `json:"rows"`
}
type aiCanonicalAsset struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Bytes  []byte `json:"bytes"`
}
type aiCanonicalSnapshot struct {
	TestName             string                      `json:"testName"`
	Version              int                         `json:"schemaVersion"`
	Scope                string                      `json:"scope"`
	Tables               map[string]aiCanonicalTable `json:"tables"`
	ExcludedTables       map[string]string           `json:"excludedTables"`
	AssetRoots           map[string]bool             `json:"assetRoots"`
	AssetDirectories     []string                    `json:"assetDirectories"`
	Assets               map[string]aiCanonicalAsset `json:"assets"`
	MutationHistoryCount int                         `json:"mutationHistoryCount"`
}
type aiCanonicalBinding struct {
	SHA256     string         `json:"sha256"`
	Size       int            `json:"size"`
	Path       string         `json:"path,omitempty"`
	Retained   bool           `json:"retained"`
	TableRows  map[string]int `json:"tableRows"`
	AssetFiles int            `json:"assetFiles"`
	AssetBytes int64          `json:"assetBytes"`
}
type aiCanonicalResult struct {
	Version                       int                `json:"schemaVersion"`
	Unchanged                     bool               `json:"unchanged"`
	RowsUnchanged                 bool               `json:"rowsUnchanged"`
	AssetsUnchanged               bool               `json:"assetsUnchanged"`
	MutationHistoryCountUnchanged bool               `json:"mutationHistoryCountUnchanged"`
	Before                        aiCanonicalBinding `json:"before"`
	After                         aiCanonicalBinding `json:"after"`
	Error                         string             `json:"error,omitempty"`
}
type aiCanonicalObservation struct {
	before    aiCanonicalSnapshot
	binding   aiCanonicalBinding
	directory string
}

func aiCanonicalCellValue(v any) (aiCanonicalCell, error) {
	switch v := v.(type) {
	case nil:
		return aiCanonicalCell{Type: "null"}, nil
	case string:
		return aiCanonicalCell{Type: "text", Value: base64.StdEncoding.EncodeToString([]byte(v))}, nil
	case []byte:
		return aiCanonicalCell{Type: "blob", Value: base64.StdEncoding.EncodeToString(v)}, nil
	case int64:
		return aiCanonicalCell{Type: "integer", Value: strconv.FormatInt(v, 10)}, nil
	case float64:
		return aiCanonicalCell{Type: "real-ieee754", Value: fmt.Sprintf("%016x", math.Float64bits(v))}, nil
	default:
		return aiCanonicalCell{}, fmt.Errorf("unreviewed SQLite value type %T", v)
	}
}
func aiCanonicalTableSnapshot(tx *sql.Tx, name, schema string) (aiCanonicalTable, error) {
	out := aiCanonicalTable{Schema: schema, Rows: [][]aiCanonicalCell{}}
	// Names come from sqlite_schema; quote even unusual independently added names.
	rows, err := tx.Query(`SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Columns, err = rows.Columns()
	if err != nil {
		return out, err
	}
	total := 0
	for rows.Next() {
		values := make([]any, len(out.Columns))
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err = rows.Scan(dest...); err != nil {
			return out, err
		}
		row := make([]aiCanonicalCell, len(values))
		for i, v := range values {
			row[i], err = aiCanonicalCellValue(v)
			if err != nil {
				return out, err
			}
			total += len(row[i].Value) + 64
		}
		if total > aiCanonicalSnapshotLimit {
			return out, fmt.Errorf("table %s exceeds canonical audit byte limit", name)
		}
		out.Rows = append(out.Rows, row)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	// A typed row multiset is independent of SQLite query/physical row order and
	// preserves duplicates, NULL/empty, TEXT/BLOB and integer/real distinctions.
	sort.Slice(out.Rows, func(i, j int) bool {
		a, _ := json.Marshal(out.Rows[i])
		b, _ := json.Marshal(out.Rows[j])
		return bytes.Compare(a, b) < 0
	})
	return out, nil
}
func readAICanonicalSnapshot(k *Kernel) (aiCanonicalSnapshot, error) {
	out := aiCanonicalSnapshot{Version: 1, Scope: "All SQLite tables except explicitly listed generated/derived state; all regular bytes under workspace/files and optional workspace/recordings (including raw PCM and recovery journals). SQLite/WAL layout, .staging, diagnostics and lock files are outside this canonical-content audit. No concurrent user mutations or asset writers are allowed during a snapshot.", Tables: map[string]aiCanonicalTable{}, ExcludedTables: aiCanonicalExcludedTables, AssetRoots: map[string]bool{}, AssetDirectories: []string{}, Assets: map[string]aiCanonicalAsset{}}
	tx, err := k.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT name,sql FROM sqlite_schema WHERE type='table' ORDER BY name")
	if err != nil {
		return out, err
	}
	schemas := map[string]string{}
	for rows.Next() {
		var name, ddl string
		if err = rows.Scan(&name, &ddl); err != nil {
			rows.Close()
			return out, err
		}
		if _, excluded := aiCanonicalExcludedTables[name]; !excluded {
			schemas[name] = ddl
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rowBytes := 0
	for name, ddl := range schemas {
		table, err := aiCanonicalTableSnapshot(tx, name, ddl)
		if err != nil {
			return out, fmt.Errorf("snapshot %s: %w", name, err)
		}
		encoded, err := json.Marshal(table)
		if err != nil {
			return out, err
		}
		rowBytes += len(encoded)
		if rowBytes > aiCanonicalSnapshotLimit {
			return out, fmt.Errorf("canonical rows exceed audit byte limit")
		}
		out.Tables[name] = table
	}
	if err = tx.QueryRow("SELECT count(*) FROM mutation_history").Scan(&out.MutationHistoryCount); err != nil {
		return out, err
	}
	var assetBytes int64
	for _, domain := range []string{"files", "recordings"} {
		root := filepath.Join(k.Path(), domain)
		rootInfo, err := os.Lstat(root)
		if os.IsNotExist(err) && domain == "recordings" {
			out.AssetRoots[domain] = false
			continue
		}
		if err != nil {
			return out, err
		}
		if !rootInfo.IsDir() {
			return out, fmt.Errorf("canonical asset root is not a real directory: %s", domain)
		}
		out.AssetRoots[domain] = true
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(k.Path(), path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			st, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if st.IsDir() {
				out.AssetDirectories = append(out.AssetDirectories, rel)
				return nil
			}
			if !st.Mode().IsRegular() {
				return fmt.Errorf("canonical asset is not a regular file: %s", rel)
			}
			if assetBytes+st.Size() > aiCanonicalSnapshotLimit {
				return fmt.Errorf("canonical assets exceed audit byte limit")
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			opened, err := f.Stat()
			if err != nil {
				return err
			}
			if !os.SameFile(st, opened) {
				return fmt.Errorf("asset changed while opening: %s", rel)
			}
			b, err := io.ReadAll(io.LimitReader(f, aiCanonicalSnapshotLimit-assetBytes+1))
			if err != nil {
				return err
			}
			after, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if int64(len(b)) != st.Size() || !os.SameFile(st, after) || st.ModTime() != after.ModTime() || st.Mode() != after.Mode() {
				return fmt.Errorf("asset changed during snapshot: %s", rel)
			}
			sum := sha256.Sum256(b)
			out.Assets[rel] = aiCanonicalAsset{int64(len(b)), hex.EncodeToString(sum[:]), b}
			assetBytes += int64(len(b))
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
func bindAICanonicalSnapshot(s aiCanonicalSnapshot, path string) (aiCanonicalBinding, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return aiCanonicalBinding{}, err
	}
	if len(b) > aiCanonicalSnapshotLimit {
		return aiCanonicalBinding{}, fmt.Errorf("canonical snapshot exceeds audit byte limit")
	}
	sum := sha256.Sum256(b)
	out := aiCanonicalBinding{SHA256: hex.EncodeToString(sum[:]), Size: len(b), TableRows: map[string]int{}, AssetFiles: len(s.Assets)}
	for name, table := range s.Tables {
		out.TableRows[name] = len(table.Rows)
	}
	for _, asset := range s.Assets {
		out.AssetBytes += asset.Size
	}
	if path != "" {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return out, err
		}
		_, writeErr := f.Write(b)
		modeErr := f.Chmod(0400)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil {
			return out, writeErr
		}
		if modeErr != nil {
			return out, modeErr
		}
		if syncErr != nil {
			return out, syncErr
		}
		if closeErr != nil {
			return out, closeErr
		}
		d, err := os.Open(filepath.Dir(path))
		if err != nil {
			return out, err
		}
		syncErr = d.Sync()
		closeErr = d.Close()
		if syncErr != nil {
			return out, syncErr
		}
		if closeErr != nil {
			return out, closeErr
		}
		out.Path = path
		out.Retained = true
	}
	return out, nil
}

var aiCanonicalEvidenceRoots = struct {
	sync.Mutex
	created map[string]bool
}{created: map[string]bool{}}

func aiCanonicalEvidenceDirectory(t *testing.T) string {
	t.Helper()
	root := os.Getenv("TROVE_AI_CANONICAL_EVIDENCE_DIR")
	if root == "" {
		if os.Getenv("TROVE_NATIVE_INTEGRATION") == "1" {
			t.Fatal("native AI canonical audit requires a fresh durable TROVE_AI_CANONICAL_EVIDENCE_DIR")
		}
		return ""
	}
	if !filepath.IsAbs(root) {
		t.Fatal("canonical evidence directory must be absolute")
	}
	aiCanonicalEvidenceRoots.Lock()
	defer aiCanonicalEvidenceRoots.Unlock()
	if !aiCanonicalEvidenceRoots.created[root] {
		// A reused run directory must fail instead of mixing/overwriting attempts.
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal("create fresh canonical evidence directory:", err)
		}
		if err := syncAICanonicalDirectory(filepath.Dir(root)); err != nil {
			t.Fatal(err)
		}
		aiCanonicalEvidenceRoots.created[root] = true
	}
	sum := sha256.Sum256([]byte(t.Name()))
	dir, err := os.MkdirTemp(root, hex.EncodeToString(sum[:8])+"-")
	if err != nil {
		t.Fatal(err)
	}
	if err := syncAICanonicalDirectory(root); err != nil {
		t.Fatal(err)
	}
	return dir
}
func beginAICanonicalObservation(t *testing.T, k *Kernel) *aiCanonicalObservation {
	t.Helper()
	dir := aiCanonicalEvidenceDirectory(t)
	before, err := readAICanonicalSnapshot(k)
	if err != nil {
		t.Fatal("canonical before snapshot:", err)
	}
	before.TestName = t.Name()
	path := ""
	if dir != "" {
		path = filepath.Join(dir, "before.json")
	}
	binding, err := bindAICanonicalSnapshot(before, path)
	if err != nil {
		t.Fatal("retain canonical before snapshot:", err)
	}
	return &aiCanonicalObservation{before, binding, dir}
}
func (o *aiCanonicalObservation) finish(t *testing.T, k *Kernel) aiCanonicalResult {
	t.Helper()
	result := aiCanonicalResult{Version: 1, Before: o.binding}
	after, err := readAICanonicalSnapshot(k)
	after.TestName = o.before.TestName
	if err == nil {
		path := ""
		if o.directory != "" {
			path = filepath.Join(o.directory, "after.json")
		}
		result.After, err = bindAICanonicalSnapshot(after, path)
	}
	if err != nil {
		result.Error = err.Error()
		t.Error("canonical after snapshot:", err)
		return result
	}
	compared := compareAICanonicalSnapshots(o.before, after)
	result.RowsUnchanged, result.AssetsUnchanged = compared.RowsUnchanged, compared.AssetsUnchanged
	result.MutationHistoryCountUnchanged, result.Unchanged = compared.MutationHistoryCountUnchanged, compared.Unchanged
	if !result.Unchanged {
		t.Errorf("canonical row/asset inventory changed: rows=%t assets=%t mutationCount=%t", result.RowsUnchanged, result.AssetsUnchanged, result.MutationHistoryCountUnchanged)
	}
	return result
}

func compareAICanonicalSnapshots(before, after aiCanonicalSnapshot) aiCanonicalResult {
	result := aiCanonicalResult{Version: 1}
	beforeRows, _ := json.Marshal(before.Tables)
	afterRows, _ := json.Marshal(after.Tables)
	beforeAssets, _ := json.Marshal([]any{before.AssetRoots, before.AssetDirectories, before.Assets})
	afterAssets, _ := json.Marshal([]any{after.AssetRoots, after.AssetDirectories, after.Assets})
	result.RowsUnchanged = bytes.Equal(beforeRows, afterRows)
	result.AssetsUnchanged = bytes.Equal(beforeAssets, afterAssets)
	result.MutationHistoryCountUnchanged = before.MutationHistoryCount == after.MutationHistoryCount
	result.Unchanged = result.RowsUnchanged && result.AssetsUnchanged && result.MutationHistoryCountUnchanged
	return result
}

func syncAICanonicalDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
