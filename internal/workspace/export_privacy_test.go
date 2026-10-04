package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func exportPrivacyEntries(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, html bool) map[string]string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "export.zip")
	var result PortableExportResult
	var err error
	if html {
		result, err = k.ExportHTML(context.Background(), e, file)
	} else {
		result, err = k.ExportPortable(context.Background(), e, file)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(result.SourceOmissions, " "), "isolated live-item provenance") {
		t.Fatal("evidence omissions were not disclosed", result)
	}
	return portableEntries(t, file)
}

func assertExportAbsent(t *testing.T, entries map[string]string, private ...string) {
	t.Helper()
	for name, body := range entries {
		for _, secret := range private {
			if secret != "" && (strings.Contains(name, secret) || strings.Contains(body, secret)) {
				t.Fatalf("private content %q leaked through %s", secret, name)
			}
		}
	}
}

func assertExportAsset(t *testing.T, entries map[string]string, id, want string) {
	t.Helper()
	for name, body := range entries {
		if strings.HasPrefix(name, "attachments/"+id+"/") {
			if body != want {
				t.Fatal("attachment bytes changed", name)
			}
			return
		}
	}
	t.Fatal("missing live attachment", id)
}

func TestCurrentContentExportsExcludeTrashAndUnreferencedAssets(t *testing.T) {
	k, e := testKernel(t)
	live := createPage(t, k, e, "Live")
	trash := createPage(t, k, e, "Private trashed page")
	private, _ := attachTestFile(t, k, e, trash, stageTestFile(t, k, e, "TRASH_ONLY_PRIVATE_BYTES"), true)
	shared, _ := attachTestFile(t, k, e, trash, stageTestFile(t, k, e, "SHARED_LIVE_BYTES"), true)
	attachTestFile(t, k, e, live, stageTestFile(t, k, e, "SHARED_LIVE_BYTES"), false)
	orphan, block := attachTestFile(t, k, e, live, stageTestFile(t, k, e, "UNREFERENCED_PRIVATE_BYTES"), true)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, "page", live, "block", block))
	cover, _ := attachTestFile(t, k, e, trash, stageTestFile(t, k, e, "LIVE_COVER_BYTES"), false)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": live, "coverFileId": cover}}, "page", live))
	f := newDatabase(t, k, e)
	record, recordPage := newRecord(t, k, e, f, "Live record")
	property := NewID()
	propertyFile, _ := attachTestFile(t, k, e, trash, stageTestFile(t, k, e, "LIVE_PROPERTY_BYTES"), false)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": property, "name": "Files", "type": "files", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", property))
	testOK(t, setValue(t, k, e, f, record, recordPage, property, "files", []any{map[string]any{"fileId": propertyFile, "displayName": "retained.txt"}}))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": trash, "includeDescendants": false}}, "page", trash))
	for _, html := range []bool{false, true} {
		entries := exportPrivacyEntries(t, k, e, html)
		assertExportAbsent(t, entries, private, orphan, "TRASH_ONLY_PRIVATE_BYTES", "UNREFERENCED_PRIVATE_BYTES", "Private trashed page")
		assertExportAsset(t, entries, shared, "SHARED_LIVE_BYTES")
		assertExportAsset(t, entries, cover, "LIVE_COVER_BYTES")
		assertExportAsset(t, entries, propertyFile, "LIVE_PROPERTY_BYTES")
	}
	// Export filtering must not change full native recovery retention.
	backup := filepath.Join(t.TempDir(), "backup")
	m, err := k.CreateBackup(context.Background(), e, backup)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{private, orphan} {
		found := false
		for _, a := range m.Assets {
			if a.FileID == id {
				found = true
			}
		}
		if !found {
			t.Fatal("native backup lost retained evidence", id)
		}
	}
}

func TestCurrentContentExportsExcludeImportContainersAndDeletedCSVRows(t *testing.T) {
	k, e := testKernel(t)
	p := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Live.md": "LIVE_SOURCE_EVIDENCE", "Private.md": "TRASHED_IMPORT_SECRET", "Rows.csv": "Name,Notes\nKeep,public row\nRemove,TRASHED_CSV_SECRET\n"}))
	privateFiles := []string{p.SourceFileID}
	for _, item := range p.Items {
		if item.Title == "Private" || item.Title == "Remove" {
			guards := []string{"page", item.PageID}
			if item.RecordID != "" {
				guards = append(guards, "record", item.RecordID, "collection", item.CollectionID)
			}
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": item.PageID, "includeDescendants": false}}, guards...))
			privateFiles = append(privateFiles, item.RawFileID)
		}
		if item.Kind == "csv" {
			privateFiles = append(privateFiles, item.RawFileID)
		}
	}
	for _, html := range []bool{false, true} {
		entries := exportPrivacyEntries(t, k, e, html)
		assertExportAbsent(t, entries, append(privateFiles, "TRASHED_IMPORT_SECRET", "TRASHED_CSV_SECRET")...)
		for _, item := range p.Items {
			if item.Title == "Live" {
				assertExportAsset(t, entries, item.RawFileID, "LIVE_SOURCE_EVIDENCE")
			}
		}
		k2, e2 := testKernel(t)
		imported := completeImportFixture(t, k2, e2, importFixture(t, k2, e2, entries))
		for _, item := range imported.Items {
			if item.Outcome == "failed" {
				t.Fatal("filtered export cannot be re-imported", item)
			}
		}
	}
}

func TestCurrentContentExportIsolatesConversationSiblingEvidence(t *testing.T) {
	k, e := testKernel(t)
	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(chatGPTConversationFixture), &entries); err != nil {
		t.Fatal(err)
	}
	private := strings.ReplaceAll(string(entries[0]), "conversation-42", "private-conversation")
	private = strings.ReplaceAll(private, "All branches", "Private conversation")
	private = strings.ReplaceAll(private, "must not enter text projections", "TRASHED_CONVERSATION_NON_TEXT_SECRET")
	raw := "[" + string(entries[0]) + "," + private + "]"
	p := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"conversations.json": raw}))
	var live ImportItem
	for _, item := range p.Items {
		if item.Title == "Private conversation" {
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": item.PageID, "includeDescendants": false}}, "page", item.PageID))
		}
		if item.Title == "All branches" {
			live = item
		}
	}
	for _, html := range []bool{false, true} {
		archive := exportPrivacyEntries(t, k, e, html)
		assertExportAbsent(t, archive, p.SourceFileID, live.RawFileID, "private-conversation", "TRASHED_CONVERSATION_NON_TEXT_SECRET", "Private conversation")
		found := false
		for name, body := range archive {
			if strings.HasPrefix(name, "attachments/") && strings.Contains(body, "hidden_secret") {
				found = true
				var got, want any
				if err := json.Unmarshal([]byte(body), &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(entries[0], &want); err != nil {
					t.Fatal(err)
				}
				if string(marshal(got)) != string(marshal(want)) {
					t.Fatal("live conversation's non-text source fields changed")
				}
			}
		}
		if !found {
			t.Fatal("isolated live conversation evidence was lost")
		}
		k2, e2 := testKernel(t)
		imported := completeImportFixture(t, k2, e2, importFixture(t, k2, e2, archive))
		count := 0
		for _, item := range imported.Items {
			if item.Title == "All branches" && item.PageID != "" {
				count++
			}
		}
		if count != 1 {
			t.Fatal("isolated evidence duplicated or lost conversation", count)
		}
	}
	original, err := k.readImportBytes(context.Background(), live.RawFileID)
	if err != nil || string(original) != raw {
		t.Fatal("export changed original retained evidence", err)
	}
}

func TestPortableRoundTripDoesNotExportOldInventoryAsContent(t *testing.T) {
	k, e := testKernel(t)
	completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Keep.md": "Keep current content", "Private.md": "SECOND_EXPORT_PRIVATE_SECRET"}))
	first := exportPrivacyEntries(t, k, e, false)
	k2, e2 := testKernel(t)
	p := completeImportFixture(t, k2, e2, importFixture(t, k2, e2, first))
	var manifest ImportItem
	for _, item := range p.Items {
		if item.Locator == "trove-manifest.json" {
			manifest = item
			if item.PageID != "" {
				t.Fatal("portable inventory became a content page")
			}
		}
		if item.Title == "Private" && item.PageID != "" {
			testOK(t, dbBatch(t, k2, e2, []any{map[string]any{"kind": "page.trash", "pageId": item.PageID, "includeDescendants": false}}, "page", item.PageID))
		}
	}
	// Recreate the automatic unsupported page emitted by older importers.
	legacy := createPage(t, k2, e2, "trove-manifest")
	block := NewID()
	testOK(t, dbBatch(t, k2, e2, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": legacy, "type": "unsupported_external", "schemaVersion": 1, "data": map[string]any{"sourceType": "unsupported", "sourceVersion": 1, "rawFileId": manifest.RawFileID, "readableText": first["trove-manifest.json"], "reason": "unsupported_behavior"}}}}, "page", legacy, "block", block))
	provenance := map[string]any{"sourceSystem": "notion", "raw": map[string]any{"sourceLocator": "trove-manifest.json", "rawFileId": manifest.RawFileID}}
	if _, err := k2.db.Exec("UPDATE pages SET provenance_json=? WHERE id=?", string(marshal(provenance)), legacy); err != nil {
		t.Fatal(err)
	}
	for _, html := range []bool{false, true} {
		archive := exportPrivacyEntries(t, k2, e2, html)
		assertExportAbsent(t, archive, "SECOND_EXPORT_PRIVATE_SECRET", manifest.RawFileID, legacy)
	}
}
