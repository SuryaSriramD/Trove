package workspace

import (
	"encoding/json"
	"testing"
)

func TestHistoryRestoresExactAttachmentMembershipAndLegacyInventory(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "exact relationships"
		if legacy {
			name = "legacy aggregate inventory"
		}
		t.Run(name, func(t *testing.T) {
			k, e := testKernel(t)
			page := createPage(t, k, e, "Attachment history")
			file, block := attachTestFile(t, k, e, page, stageTestFile(t, k, e, "history bytes"), true)
			if legacy {
				var raw string
				if err := k.db.QueryRow("SELECT snapshot_json FROM page_snapshots WHERE page_id=? AND revision='2'", page).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var snap map[string]any
				json.Unmarshal([]byte(raw), &snap)
				delete(snap, "attachmentRelationships")
				snap["recoveryMetadataVersion"] = 5
				if _, err := k.db.Exec("UPDATE page_snapshots SET snapshot_json=? WHERE page_id=? AND revision='2'", string(marshal(snap)), page); err != nil {
					t.Fatal(err)
				}
			}
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, "page", page, "block", block))
			testOK(t, restoreTest(t, k, e, page, "2"))
			if legacy {
				assertExplicitAttachment(t, k, page, ImportAttachmentRelationship{FileID: file}, 1)
			} else {
				assertExplicitAttachment(t, k, page, ImportAttachmentRelationship{FileID: file, BlockID: block}, 1)
				assertExplicitAttachment(t, k, page, ImportAttachmentRelationship{FileID: file}, 0)
			}
		})
	}
}

func TestHistoryDoesNotPromoteDerivedCoverReferenceIntoExplicitAttachment(t *testing.T) {
	k, e := testKernel(t)
	source := createPage(t, k, e, "File owner")
	file, _ := attachTestFile(t, k, e, source, stageTestFile(t, k, e, "cover bytes"), false)
	page := createPage(t, k, e, "Cover history")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "coverFileId": file}}, "page", page))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "coverFileId": nil}}, "page", page))
	testOK(t, restoreTest(t, k, e, page, "2"))
	assertExplicitAttachment(t, k, page, ImportAttachmentRelationship{FileID: file}, 0)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "coverFileId": nil}}, "page", page))
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM attachments WHERE page_id=?", page).Scan(&count); err != nil || count != 0 {
		t.Fatal("cleared restored cover retained a fabricated attachment", count, err)
	}
}
