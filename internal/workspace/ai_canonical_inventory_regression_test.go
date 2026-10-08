package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func requireAICanonicalSnapshot(t *testing.T, k *Kernel) aiCanonicalSnapshot {
	t.Helper()
	s, err := readAICanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestAICanonicalInventoryDetectsUnloggedRowChanges(t *testing.T) {
	for _, tc := range []struct{ name, sql, table string }{
		{"page", `UPDATE pages SET title='silently changed' WHERE title='Audit source'`, "pages"},
		{"block", `UPDATE blocks SET data_json='{"richText":[{"text":"silently changed"}]}' WHERE page_id IN(SELECT id FROM pages WHERE title='Audit source')`, "blocks"},
		{"approved-memory", `UPDATE memory_entries SET content='silently changed' WHERE status='active'`, "memory_entries"},
		{"memory-provenance", `UPDATE memory_sources SET source_revision='9'`, "memory_sources"},
		{"existing-history", `UPDATE mutation_history SET request_id='silently changed'`, "mutation_history"},
		{"file-metadata", `UPDATE files SET original_name='silently changed'`, "files"},
		{"new-table", `CREATE TABLE future_canonical_content(id TEXT,body BLOB); INSERT INTO future_canonical_content VALUES('new',X'0001')`, "future_canonical_content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, e := testKernel(t)
			page := seedExpandedQA(t, k, e, expandedQACase{Title: "Audit source", Source: "Original canonical text.", Memories: []expandedMemory{{Kind: "preference", Content: "Use complete sentences."}}})
			attachTestFile(t, k, e, page, stageTestFile(t, k, e, "original asset bytes"), false)
			before := requireAICanonicalSnapshot(t, k)
			if before.Tables["memory_entries"].Rows == nil || len(before.Tables["memory_entries"].Rows) != 1 {
				t.Fatal("approved-memory fixture missing")
			}
			if _, err := k.db.Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			after := requireAICanonicalSnapshot(t, k)
			got := compareAICanonicalSnapshots(before, after)
			if !got.MutationHistoryCountUnchanged || got.RowsUnchanged || got.Unchanged || !got.AssetsUnchanged {
				t.Fatalf("unlogged %s change escaped independent audit: %+v", tc.table, got)
			}
		})
	}
}
func TestAICanonicalInventoryDetectsAssetByteAndInventoryChanges(t *testing.T) {
	for _, change := range []string{"same-length-bytes", "deleted", "added", "symlink"} {
		t.Run(change, func(t *testing.T) {
			k, e := testKernel(t)
			page := createPage(t, k, e, "Asset audit")
			id, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, "original"), false)
			var storage string
			if err := k.db.QueryRow("SELECT storage_key FROM files WHERE id=?", id).Scan(&storage); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(k.Path(), storage)
			before := requireAICanonicalSnapshot(t, k)
			var err error
			switch change {
			case "same-length-bytes":
				err = os.Chmod(path, 0600) // deliberate hostile write to an immutable fixture asset
				if err == nil {
					err = os.WriteFile(path, []byte("tampered"), 0600)
				}
			case "deleted":
				err = os.Remove(path)
			case "added":
				err = os.WriteFile(filepath.Join(k.Path(), "files", "unregistered"), []byte("new unregistered bytes"), 0600)
			case "symlink":
				if err = os.Remove(path); err == nil {
					err = os.Symlink(filepath.Join(t.TempDir(), "missing"), path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := readAICanonicalSnapshot(k)
			if change == "symlink" {
				if err == nil {
					t.Fatal("symlink admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := compareAICanonicalSnapshots(before, after)
			if !got.MutationHistoryCountUnchanged || !got.RowsUnchanged || got.AssetsUnchanged || got.Unchanged {
				t.Fatalf("asset %s escaped byte inventory: %+v", change, got)
			}
		})
	}
}
func TestAICanonicalInventoryPermitsDraftsButProtectsUnknownTables(t *testing.T) {
	k, _ := testKernel(t)
	if _, err := k.db.Exec(`CREATE TABLE future_canonical_content(value); INSERT INTO future_canonical_content VALUES(NULL),(''),(X''),(1),(1.0),(X'00ff')`); err != nil {
		t.Fatal(err)
	}
	before := requireAICanonicalSnapshot(t, k)
	if len(before.Tables["future_canonical_content"].Rows) != 6 {
		t.Fatal("typed rows missing")
	}
	if _, err := k.db.Exec(`INSERT INTO ai_runs VALUES('draft','conversation','actor','completed','{}','generated text','[]','[]','','','now'); INSERT INTO ai_proposals VALUES('proposal','draft','draft','{}','now')`); err != nil {
		t.Fatal(err)
	}
	after := requireAICanonicalSnapshot(t, k)
	if !compareAICanonicalSnapshots(before, after).Unchanged {
		t.Fatal("generated draft state changed protected inventory")
	}
	if _, err := k.db.Exec(`DELETE FROM future_canonical_content WHERE typeof(value)='blob' AND length(value)=0; INSERT INTO future_canonical_content VALUES('')`); err != nil {
		t.Fatal(err)
	}
	changed := requireAICanonicalSnapshot(t, k)
	if compareAICanonicalSnapshots(after, changed).Unchanged {
		t.Fatal("equal-count empty BLOB to empty TEXT change escaped typed row audit")
	}
	if _, err := k.db.Exec(`ALTER TABLE future_canonical_content ADD COLUMN extra TEXT`); err != nil {
		t.Fatal(err)
	}
	if compareAICanonicalSnapshots(changed, requireAICanonicalSnapshot(t, k)).RowsUnchanged {
		t.Fatal("schema change escaped audit")
	}
}
func TestAICanonicalInventoryRetainsExactSnapshots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new-evidence")
	t.Setenv("TROVE_AI_CANONICAL_EVIDENCE_DIR", root)
	k, e := testKernel(t)
	page := createPage(t, k, e, "Retained canonical content")
	attachTestFile(t, k, e, page, stageTestFile(t, k, e, "raw\x00asset\xffbytes"), false)
	audit := beginAICanonicalObservation(t, k)
	result := audit.finish(t, k)
	if !result.Unchanged || !result.Before.Retained || !result.After.Retained || result.Before.AssetFiles != 1 {
		t.Fatal(result)
	}
	for _, binding := range []aiCanonicalBinding{result.Before, result.After} {
		raw, err := os.ReadFile(binding.Path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != binding.SHA256 || len(raw) != binding.Size {
			t.Fatal("snapshot binding differs")
		}
		var snapshot aiCanonicalSnapshot
		if err = json.Unmarshal(raw, &snapshot); err != nil {
			t.Fatal(err)
		}
		for _, asset := range snapshot.Assets {
			if !bytes.Equal(asset.Bytes, []byte("raw\x00asset\xffbytes")) {
				t.Fatal("snapshot lacks original bytes")
			}
		}
		if _, err := bindAICanonicalSnapshot(snapshot, binding.Path); err == nil {
			t.Fatal("existing snapshot could be overwritten")
		}
	}
	// Closing/removing the disposable runtime workspace cannot erase the separate
	// exact canonical inventory reports retained under the evidence destination.
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(k.Path()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.Before.Path); err != nil {
		t.Fatal(err)
	}
}

// Produces the actual compact wire proof from populated memory/asset fixtures
// without inference, for independent runner validation against retained bytes.
func TestAICanonicalInventoryReportContract(t *testing.T) {
	k, e := testKernel(t)
	page := seedExpandedQA(t, k, e, expandedQACase{Title: "Audit contract", Source: "Current audited source.", Memories: []expandedMemory{{Kind: "preference", Content: "Write clearly."}}})
	attachTestFile(t, k, e, page, stageTestFile(t, k, e, "exact canonical asset\x00bytes"), false)
	audit := beginAICanonicalObservation(t, k)
	result := audit.finish(t, k)
	report, err := json.Marshal(map[string]any{"fixture": "snapshot-contract", "canonicalUnchanged": true, "mutationHistoryCountUnchanged": true, "canonicalInventory": result, "passed": result.Unchanged})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(report))
}

func TestAICanonicalInventoryDetectsRecordingAndJournalChanges(t *testing.T) {
	for _, name := range []string{"capture.pcm", "capture.json"} {
		t.Run(name, func(t *testing.T) {
			k, _ := testKernel(t)
			root := filepath.Join(k.Path(), "recordings")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			for _, n := range []string{"capture.pcm", "capture.json"} {
				if err := os.WriteFile(filepath.Join(root, n), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := requireAICanonicalSnapshot(t, k)
			if !before.AssetRoots["recordings"] || len(before.Assets) != 2 {
				t.Fatal("recording source fixtures missing")
			}
			if err := os.WriteFile(filepath.Join(root, name), []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
			after := requireAICanonicalSnapshot(t, k)
			got := compareAICanonicalSnapshots(before, after)
			if !got.RowsUnchanged || !got.MutationHistoryCountUnchanged || got.AssetsUnchanged || got.Unchanged {
				t.Fatal("recording bytes escaped audit", got)
			}
		})
	}
	t.Run("missing-to-empty-root", func(t *testing.T) {
		k, _ := testKernel(t)
		before := requireAICanonicalSnapshot(t, k)
		if before.AssetRoots["recordings"] {
			t.Fatal("expected absent recording directory")
		}
		if err := os.Mkdir(filepath.Join(k.Path(), "recordings"), 0700); err != nil {
			t.Fatal(err)
		}
		if compareAICanonicalSnapshots(before, requireAICanonicalSnapshot(t, k)).AssetsUnchanged {
			t.Fatal("recording root creation escaped inventory")
		}
	})
}
