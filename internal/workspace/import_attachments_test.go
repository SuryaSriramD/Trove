package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func importedPageNamed(t *testing.T, p ImportPlan, title string) ImportItem {
	t.Helper()
	for _, item := range p.Items {
		if item.Title == title && item.PageID != "" {
			return item
		}
	}
	t.Fatalf("missing imported page %s", title)
	return ImportItem{}
}

func assertExplicitAttachment(t *testing.T, k *Kernel, page string, r ImportAttachmentRelationship, want int) {
	t.Helper()
	kind, source := attachmentReference(r)
	var count int
	if err := k.db.QueryRow("SELECT count(DISTINCT file_id) FROM file_references WHERE page_id=? AND source_kind IN (?,?) AND source_id=? AND file_id=?", page, kind, attachmentLegacyKind(r), source, r.FileID).Scan(&count); err != nil || count != want {
		t.Fatalf("relationship %s: count=%d want=%d err=%v", marshal(r), count, want, err)
	}
}

func TestImportAttachmentExplicitSameBytesSurviveEvidenceDeduplicationAndHistory(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "separate evidence reference"
		if legacy {
			name = "legacy ambiguous reference"
		}
		t.Run(name, func(t *testing.T) {
			k, e := testKernel(t)
			body := `<p id="one">Same bytes deliberately attached by the user</p>`
			first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"A.html": body}))
			item := first.Items[0]
			if legacy {
				if _, err := k.db.Exec("UPDATE file_references SET source_kind='explicit' WHERE source_kind='import_evidence'"); err != nil {
					t.Fatal(err)
				}
			}
			file, _ := attachTestFile(t, k, e, item.PageID, stageTestFile(t, k, e, body), false)
			if file != item.RawFileID {
				t.Fatal("fixture did not exercise byte deduplication")
			}
			var revision string
			if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", item.PageID).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": item.PageID, "title": "Changed"}}, "page", item.PageID))
			testOK(t, restoreTest(t, k, e, item.PageID, revision))
			var userReference int
			if err := k.db.QueryRow("SELECT count(*) FROM file_references WHERE page_id=? AND source_kind='explicit_user' AND file_id=?", item.PageID, file).Scan(&userReference); err != nil || userReference != 1 {
				t.Fatal("history lost explicit user intent", userReference, err)
			}
			entries := exportPrivacyEntries(t, k, e, false)
			assertExportAsset(t, entries, file, body)
			var manifest portableHierarchy
			json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest)
			count := 0
			for _, p := range manifest.Pages {
				if string(p.CanonicalPage.Id) == item.PageID {
					for _, r := range p.AttachmentRelationships {
						if r.FileID == file {
							count++
						}
					}
				}
			}
			if count != 1 {
				t.Fatal("user relationship omitted or duplicated by evidence inference", count)
			}
			target, te := testKernel(t)
			roundtrip := completeImportFixture(t, target, te, importFixture(t, target, te, entries))
			imported := importedPageNamed(t, roundtrip, item.Title)
			if len(imported.AttachmentRelationships) != 1 {
				t.Fatal("same-bytes relationship lost in round trip")
			}
			for _, r := range imported.AttachmentRelationships {
				assertExplicitAttachment(t, target, imported.PageID, r, 1)
			}
		})
	}
}

func TestImportAttachmentRelationshipsRoundtripAndIndependentLocalAddition(t *testing.T) {
	source, se := testKernel(t)
	page := createPage(t, source, se, "Attached")
	file, _ := attachTestFile(t, source, se, page, stageTestFile(t, source, se, "page attachment"), false)
	blockFile, sourceBlock := attachTestFile(t, source, se, page, stageTestFile(t, source, se, "block attachment"), true)
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	item := importedPageNamed(t, first, "Attached")
	if !item.AttachmentsComplete || len(item.AttachmentRelationships) != 2 {
		t.Fatal("relationships not retained", item.AttachmentRelationships)
	}
	for _, r := range item.AttachmentRelationships {
		assertExplicitAttachment(t, target, item.PageID, r, 1)
	}
	blockPath := attachmentFieldPath(ImportAttachmentRelationship{FileID: blockFile, BlockID: sourceBlock})
	if item.AttachmentRelationships[blockPath].BlockID == sourceBlock || item.AttachmentRelationships[blockPath].BlockID == "" {
		t.Fatal("block relationship was not remapped")
	}
	localFile, _ := attachTestFile(t, target, te, item.PageID, stageTestFile(t, target, te, "independent local attachment"), false)
	added, _ := attachTestFile(t, source, se, page, stageTestFile(t, source, se, "incoming additional attachment"), false)
	preview := portableDatabasePreview(t, source, target)
	updated := importedPageNamed(t, preview, "Attached")
	if updated.MappingID != item.MappingID {
		t.Fatal("attachment import changed page identity")
	}
	preview = reviewDatabasePreview(t, target, preview, nil)
	completeImportFixture(t, target, te, preview)
	assertExplicitAttachment(t, target, item.PageID, ImportAttachmentRelationship{FileID: localFile}, 1)
	for _, r := range updated.AttachmentRelationships {
		assertExplicitAttachment(t, target, item.PageID, r, 1)
	}
	if updated.AttachmentRelationships[attachmentFieldPath(ImportAttachmentRelationship{FileID: file})].FileID != item.AttachmentRelationships[attachmentFieldPath(ImportAttachmentRelationship{FileID: file})].FileID || updated.AttachmentRelationships[attachmentFieldPath(ImportAttachmentRelationship{FileID: added})].FileID == "" {
		t.Fatal("immutable files did not retain local identity")
	}
}

func TestImportAttachmentRemovalRequiresExplicitReviewAndRetainsBytes(t *testing.T) {
	source, se := testKernel(t)
	page := createPage(t, source, se, "Attachment removal")
	file, _ := attachTestFile(t, source, se, page, stageTestFile(t, source, se, "retained original attachment bytes"), false)
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	item := importedPageNamed(t, first, "Attachment removal")
	path := attachmentFieldPath(ImportAttachmentRelationship{FileID: file})
	r := item.AttachmentRelationships[path]
	// Model a complete upstream relationship removal, not a missing file in a
	// partial capture. Bytes remain immutable in the source store too.
	if _, err := source.db.Exec("DELETE FROM file_references WHERE page_id=? AND file_id=?", page, file); err != nil {
		t.Fatal(err)
	}
	preview := portableDatabasePreview(t, source, target)
	changed := importedPageNamed(t, preview, "Attachment removal")
	found := false
	for _, field := range changed.Merge.Fields {
		if field.Path == path {
			found = true
			if field.Outcome != "conflict" || field.Incoming.Present {
				t.Fatal("removal was not an explicit review", field)
			}
		}
	}
	if !found {
		t.Fatal("attachment removal absent from review")
	}
	if _, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("unreviewed removal committed")
	}
	preview = reviewDatabasePreview(t, target, preview, map[string]string{path: "take_incoming"})
	completeImportFixture(t, target, te, preview)
	assertExplicitAttachment(t, target, item.PageID, r, 0)
	bytes, err := target.readImportBytes(context.Background(), r.FileID)
	if err != nil || string(bytes) != "retained original attachment bytes" {
		t.Fatal("removed relationship destroyed immutable evidence", err)
	}
	var historical int
	if err = target.db.QueryRow("SELECT count(*) FROM page_snapshots WHERE page_id=? AND snapshot_json LIKE ?", item.PageID, "%"+r.FileID+"%").Scan(&historical); err != nil || historical == 0 {
		t.Fatal("removed file lost history evidence", err)
	}
}

func TestImportAttachmentUnknownInventoryCannotDeleteAndEvidenceIsNotExportedAsUserRelationship(t *testing.T) {
	source, se := testKernel(t)
	page := createPage(t, source, se, "Legacy attachment inventory")
	attachTestFile(t, source, se, page, stageTestFile(t, source, se, "live explicit attachment"), false)
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	item := importedPageNamed(t, first, "Legacy attachment inventory")
	archive := filepath.Join(t.TempDir(), "legacy.zip")
	if _, err := source.ExportPortable(context.Background(), se, archive); err != nil {
		t.Fatal(err)
	}
	entries := portableEntries(t, archive)
	var manifest map[string]any
	if err := json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, raw := range manifest["pages"].([]any) {
		page := raw.(map[string]any)
		delete(page, "attachmentRelationships")
		delete(page, "attachmentRelationshipsVersion")
	}
	entries["trove-manifest.json"] = string(marshal(manifest))
	preview := importFixture(t, target, te, entries)
	// Reuse the exact source namespace for the externally modified archive.
	preview.Namespace = first.Namespace
	for i := range preview.Items {
		candidate := &preview.Items[i]
		if candidate.Title != item.Title {
			continue
		}
		candidate.PageID, candidate.MappingID, candidate.ExpectedRevision = item.PageID, item.MappingID, "1"
		if err := prepareImportMerge(context.Background(), target.db, &preview, candidate); err != nil {
			t.Fatal(err)
		}
		for _, field := range candidate.Merge.Fields {
			if strings.HasPrefix(field.Path, "/attachments/") && (!field.Incoming.Present || field.Outcome != "unchanged") {
				t.Fatal("unknown inventory inferred deletion", field)
			}
		}
	}
	result := exportPrivacyEntries(t, target, te, false)
	var exported portableHierarchy
	if err := json.Unmarshal([]byte(result["trove-manifest.json"]), &exported); err != nil {
		t.Fatal(err)
	}
	for _, page := range exported.Pages {
		for _, r := range page.AttachmentRelationships {
			if r.FileID == item.RawFileID {
				t.Fatal("automatic source evidence became user relationship")
			}
		}
	}
}

func TestImportAttachmentMalformedManifestRejected(t *testing.T) {
	for _, mode := range []string{"missing file", "missing block", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			source, se := testKernel(t)
			page := createPage(t, source, se, "Attachment validation")
			attachTestFile(t, source, se, page, stageTestFile(t, source, se, "attachment"), false)
			entries := exportPrivacyEntries(t, source, se, false)
			var manifest map[string]any
			json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest)
			p := manifest["pages"].([]any)[0].(map[string]any)
			relations := p["attachmentRelationships"].([]any)
			switch mode {
			case "missing file":
				relations[0].(map[string]any)["fileId"] = NewID()
			case "missing block":
				relations[0].(map[string]any)["blockId"] = NewID()
			case "duplicate":
				p["attachmentRelationships"] = append(relations, relations[0])
			}
			var parsed portableHierarchy
			json.Unmarshal(marshal(manifest), &parsed)
			plan := ImportPlan{}
			for _, page := range parsed.Pages {
				plan.Items = append(plan.Items, ImportItem{Locator: page.Path, PageID: NewID(), Kind: "markdown"})
			}
			for _, file := range parsed.Attachments {
				plan.Items = append(plan.Items, ImportItem{Locator: file.Path, RawFileID: NewID(), Kind: "asset"})
			}
			if err := restorePortableAttachments(&plan, parsed); err == nil {
				t.Fatal("invalid relationship accepted")
			}
		})
	}
}
