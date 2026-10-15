package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func notionDeletionFixture(t *testing.T, k *Kernel, id, status string) ImportPlan {
	t.Helper()
	ctx := context.Background()
	e := k.OwnerContext()
	out, err := startNotionCapture(ctx, k, e, map[string]any{"rootIds": []any{id}, "sourceNamespace": "notion-deletion"})
	if err != nil {
		t.Fatal(err)
	}
	capture := out.(NotionCapture)
	client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/pages/") {
			if status == "403" || status == "404" {
				n := 403
				if status == "404" {
					n = 404
				}
				return notionResponse(n, `{"code":"object_not_found"}`), nil
			}
			return notionResponse(200, fmt.Sprintf(`{"object":"page","id":%q,"in_trash":%s,"properties":{}}`, id, status)), nil
		}
		if status == "true" {
			t.Fatal("archived source enumerated children")
		}
		return notionResponse(200, `{"results":[],"has_more":false}`), nil
	})}
	for len(capture.Pending) > 0 && capture.State != "blocked" {
		capture, err = k.CaptureNotionStep(ctx, e, capture.ID, capture.Revision, "fixture-token", client)
		if err != nil {
			t.Fatal(err)
		}
	}
	out, err = finishNotionCapture(ctx, k, e, map[string]any{"captureId": capture.ID, "revision": float64(capture.Revision), "allowPartial": capture.State == "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	return out.(ImportPlan)
}
func notionImportedPage(t *testing.T, p ImportPlan, id string) string {
	t.Helper()
	for _, item := range p.Items {
		if item.ExternalID == id && item.PageID != "" {
			return item.PageID
		}
	}
	t.Fatal("no source page")
	return ""
}
func TestNotionDirectArchivedPageReviewUndoAndRetry(t *testing.T) {
	k, e := testKernel(t)
	id := strings.Repeat("a", 32)
	first := completeImportFixture(t, k, e, notionDeletionFixture(t, k, id, "false"))
	page := notionImportedPage(t, first, id)
	preview := notionDeletionFixture(t, k, id, "true")
	found := false
	for _, item := range preview.Items {
		if item.Kind == "source_deleted_page" {
			found = true
			if item.PageID != page || item.Merge == nil || item.Merge.BaselineStatus != "retained" || len(item.Merge.Fields) != 1 || !item.Merge.Fields[0].Base.Present {
				t.Fatal("bad retained deletion review", item)
			}
		}
	}
	if !found {
		t.Fatal("successful in_trash evidence did not produce explicit deletion review")
	}
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("unreviewed deletion applied")
	}
	done := completeImportFixture(t, k, e, approveSourcePageDeletions(t, k, preview))
	if !pageDeleted(t, k, page) {
		t.Fatal("not trashed")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Later local title"}}, "page", page))
	schemaUndo(t, k, e, done.PageDeletionOperationID)
	var title string
	k.db.QueryRow("SELECT title FROM pages WHERE id=?", page).Scan(&title)
	if pageDeleted(t, k, page) || title != "Later local title" {
		t.Fatal("undo lost unrelated values")
	}
	retry := notionDeletionFixture(t, k, id, "true")
	for _, item := range retry.Items {
		if item.Kind == "source_deleted_page" {
			t.Fatal("accepted source absence reproposed after metadata undo")
		}
	}
}

func TestNotionDeniedOrOmittedSourceNeverProvesDeletion(t *testing.T) {
	for _, status := range []string{"403", "404", "other_selected_root"} {
		t.Run(status, func(t *testing.T) {
			k, e := testKernel(t)
			id := strings.Repeat("a", 32)
			first := completeImportFixture(t, k, e, notionDeletionFixture(t, k, id, "false"))
			page := notionImportedPage(t, first, id)
			var preview ImportPlan
			if status == "other_selected_root" {
				preview = notionDeletionFixture(t, k, strings.Repeat("b", 32), "false")
			} else {
				preview = notionDeletionFixture(t, k, id, status)
			}
			for _, item := range preview.Items {
				if isSourceDeletion(item.Kind) {
					t.Fatal("inaccessible or unselected object became deletion", item)
				}
			}
			completeImportFixture(t, k, e, preview)
			if pageDeleted(t, k, page) {
				t.Fatal("original source removed")
			}
		})
	}
}
func TestNotionMalformedObjectStatusRetainsCursor(t *testing.T) {
	id := strings.Repeat("a", 32)
	for name, response := range map[string]string{
		"wrong_id":             `{"object":"page","id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","in_trash":true}`,
		"wrong_kind":           fmt.Sprintf(`{"object":"database","id":%q,"in_trash":true}`, id),
		"string":               fmt.Sprintf(`{"object":"page","id":%q,"in_trash":"true"}`, id),
		"null":                 fmt.Sprintf(`{"object":"page","id":%q,"in_trash":null}`, id),
		"missing":              fmt.Sprintf(`{"object":"page","id":%q}`, id),
		"legacy_alias_current": fmt.Sprintf(`{"object":"page","id":%q,"archived":true}`, id),
		"contradictory":        fmt.Sprintf(`{"object":"page","id":%q,"in_trash":false,"archived":true}`, id),
	} {
		t.Run(name, func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			out, err := startNotionCapture(ctx, k, e, map[string]any{"rootIds": []any{id}, "sourceNamespace": "malformed"})
			if err != nil {
				t.Fatal(err)
			}
			original := out.(NotionCapture)
			client := &http.Client{Transport: notionFixtureTransport(func(*http.Request) (*http.Response, error) { return notionResponse(200, response), nil })}
			if _, err = k.CaptureNotionStep(ctx, e, original.ID, original.Revision, "fixture-token", client); err == nil {
				t.Fatal("malformed object accepted")
			}
			stored, err := loadNotionCapture(ctx, k.db, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Revision != original.Revision || len(stored.Pending) != 1 || stored.Completed != 0 {
				t.Fatal("malformed response advanced cursor", stored)
			}
		})
	}
}
func TestNotionDirectDeletionRequiresCompleteLocalClosureAndFreshReview(t *testing.T) {
	for _, which := range []string{"existing_child", "later_child", "later_edit", "namespace"} {
		t.Run(which, func(t *testing.T) {
			k, e := testKernel(t)
			id := strings.Repeat("a", 32)
			page := notionImportedPage(t, completeImportFixture(t, k, e, notionDeletionFixture(t, k, id, "false")), id)
			addChild := func() {
				child := NewID()
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.create", "pageId": child, "parentPageId": page, "title": "Unreviewed child"}}, "page", child, "page", page))
			}
			if which == "existing_child" {
				addChild()
			}
			preview := notionDeletionFixture(t, k, id, "true")
			if which == "namespace" {
				preview.Namespace = "different"
				if _, err := reviewedPageDeletionActions(context.Background(), k.db, &preview); err == nil {
					t.Fatal("changed source namespace accepted")
				}
				return
			}
			preview = approveSourcePageDeletions(t, k, preview)
			if which == "later_child" {
				addChild()
			}
			if which == "later_edit" {
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Concurrent local change"}}, "page", page))
			}
			if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
				t.Fatal("unreviewed graph or edit accepted")
			}
			if pageDeleted(t, k, page) {
				t.Fatal("failed plan partially deleted page")
			}
		})
	}
}
func TestNotionFirstSeenArchivedSourceStaysInert(t *testing.T) {
	k, e := testKernel(t)
	id := strings.Repeat("a", 32)
	p := notionDeletionFixture(t, k, id, "true")
	for _, item := range p.Items {
		if item.PageID != "" {
			t.Fatal("archived object hydrated", item)
		}
	}
	completeImportFixture(t, k, e, p)
	var count int
	k.db.QueryRow("SELECT count(*) FROM import_mappings WHERE identity_value=?", id).Scan(&count)
	if count != 0 {
		t.Fatal("archived source activated")
	}
}

func TestNotionMovedStablePageAndOmittedChildPreserveIdentity(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, b, child := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	captureTree := func(parent string) ImportPlan {
		out, err := startNotionCapture(ctx, k, e, map[string]any{"rootIds": []any{a, b}, "sourceNamespace": "notion-tree"})
		if err != nil {
			t.Fatal(err)
		}
		capture := out.(NotionCapture)
		client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Path, "/pages/") {
				id := strings.TrimPrefix(r.URL.Path, "/v1/pages/")
				if id != a && id != b && id != child {
					t.Fatal("unselected object read", id)
				}
				return notionResponse(200, fmt.Sprintf(`{"object":"page","id":%q,"in_trash":false,"properties":{}}`, id)), nil
			}
			values := []any{}
			if r.URL.Path == "/v1/blocks/"+parent+"/children" {
				values = append(values, map[string]any{"id": child, "type": "child_page", "child_page": map[string]any{"title": "Stable child"}})
			}
			return notionResponse(200, string(marshal(map[string]any{"results": values, "has_more": false}))), nil
		})}
		for len(capture.Pending) > 0 {
			capture, err = k.CaptureNotionStep(ctx, e, capture.ID, capture.Revision, "fixture-token", client)
			if err != nil {
				t.Fatal(err)
			}
		}
		out, err = finishNotionCapture(ctx, k, e, map[string]any{"captureId": capture.ID, "revision": float64(capture.Revision)})
		if err != nil {
			t.Fatal(err)
		}
		return out.(ImportPlan)
	}
	first := completeImportFixture(t, k, e, captureTree(a))
	page := notionImportedPage(t, first, child)
	nextParent := notionImportedPage(t, first, b)
	moved := captureTree(b)
	for _, item := range moved.Items {
		if isSourceDeletion(item.Kind) {
			t.Fatal("move became deletion")
		}
		if item.ExternalID == child && item.Kind == "notion_page" && item.PageID != page {
			t.Fatal("stable source page duplicated")
		}
	}
	moved = completeImportFixture(t, k, e, reviewDatabasePreview(t, k, moved, nil))
	var actualParent string
	k.db.QueryRow("SELECT parent_page_id FROM pages WHERE id=?", page).Scan(&actualParent)
	if actualParent != nextParent {
		t.Fatal("incoming move not applied", actualParent, nextParent)
	}
	omitted := captureTree("")
	for _, item := range omitted.Items {
		if isSourceDeletion(item.Kind) {
			t.Fatal("omitted child inferred deleted")
		}
	}
	completeImportFixture(t, k, e, reviewDatabasePreview(t, k, omitted, nil))
	if pageDeleted(t, k, page) {
		t.Fatal("omitted child lost")
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM import_mappings WHERE source_namespace='notion-tree' AND external_kind='page' AND identity_value=?", child).Scan(&count)
	if count != 1 {
		t.Fatal("stable identity changed", count)
	}
}
func TestNotionHistoricalUnknownExistenceNeedsExplicitReview(t *testing.T) {
	k, e := testKernel(t)
	id := strings.Repeat("a", 32)
	first := completeImportFixture(t, k, e, notionDeletionFixture(t, k, id, "false"))
	page := notionImportedPage(t, first, id)
	// Simulate a retained v1 snapshot written before explicit object-presence evidence.
	if _, err := k.db.Exec(`UPDATE import_baselines SET snapshot_json=json_remove(snapshot_json,'$.pageSourceScope','$.fields."/exists"') WHERE mapping_id=(SELECT id FROM import_mappings WHERE local_id=? AND external_kind='page')`, page); err != nil {
		t.Fatal(err)
	}
	preview := notionDeletionFixture(t, k, id, "true")
	found := false
	for _, item := range preview.Items {
		if item.Kind == "source_deleted_page" {
			found = true
			if item.Merge.BaselineStatus != "unavailable" || item.Merge.Fields[0].Base.Present {
				t.Fatal("historical presence invented", item)
			}
		}
	}
	if !found {
		t.Fatal("missing unavailable-baseline review")
	}
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("unavailable baseline bypassed review")
	}
	completeImportFixture(t, k, e, approveSourcePageDeletions(t, k, preview))
	if !pageDeleted(t, k, page) {
		t.Fatal("explicit deletion not applied")
	}
}

func TestNotionDeletionProofSurvivesNativeRestore(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	id := strings.Repeat("a", 32)
	page := notionImportedPage(t, completeImportFixture(t, k, e, notionDeletionFixture(t, k, id, "false")), id)
	preview := notionDeletionFixture(t, k, id, "true")
	backup := filepath.Join(t.TempDir(), "backup")
	manifest, err := k.CreateBackup(ctx, e, backup)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackupReviewed(ctx, backup, dest, manifest); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	retained, err := loadImportPlan(ctx, restored.db, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := completeImportFixture(t, restored, restored.OwnerContext(), approveSourcePageDeletions(t, restored, retained))
	if !pageDeleted(t, restored, page) {
		t.Fatal("restored direct evidence did not apply")
	}
	schemaUndo(t, restored, restored.OwnerContext(), done.PageDeletionOperationID)
	if pageDeleted(t, restored, page) {
		t.Fatal("restored deletion did not undo")
	}
}

func TestNotionResumedLegacyArchivedAliasIsVersionBound(t *testing.T) {
	// archived was the documented in_trash alias before2026-03-11. Historical
	// cursors retain their negotiated version; current captures reject this shape.
	for _, version := range []string{"", "2025-09-03"} {
		t.Run("version_"+version, func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			id := strings.Repeat("a", 32)
			first := completeImportFixture(t, k, e, notionDeletionFixture(t, k, id, "false"))
			page := notionImportedPage(t, first, id)
			out, err := startNotionCapture(ctx, k, e, map[string]any{"rootIds": []any{id}, "sourceNamespace": "notion-deletion"})
			if err != nil {
				t.Fatal(err)
			}
			capture := out.(NotionCapture)
			capture.APIVersion = version
			if err = k.write(ctx, func(tx *sql.Tx) error { return saveNotionCapture(ctx, tx, capture, capture.Revision) }); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Notion-Version") != "2025-09-03" || r.URL.Path != "/v1/pages/"+id {
					t.Fatal("legacy capture changed version or queried descendants", r.Header, r.URL.Path)
				}
				return notionResponse(200, fmt.Sprintf(`{"object":"page","id":%q,"archived":true}`, id)), nil
			})}
			capture, err = k.CaptureNotionStep(ctx, e, capture.ID, capture.Revision, "fixture-token", client)
			if err != nil {
				t.Fatal(err)
			}
			if len(capture.Pending) != 0 {
				t.Fatal("legacy archived page queued children")
			}
			out, err = finishNotionCapture(ctx, k, e, map[string]any{"captureId": capture.ID, "revision": float64(capture.Revision)})
			if err != nil {
				t.Fatal(err)
			}
			preview := out.(ImportPlan)
			found := false
			for _, item := range preview.Items {
				if item.Kind == "source_deleted_page" {
					found = true
					if item.PageID != page || item.SourceDeletionProof == nil || item.Merge.BaselineStatus != "retained" {
						t.Fatal("legacy direct proof mismatched", item)
					}
				}
			}
			if !found {
				t.Fatal("known legacy alias did not produce explicit review")
			}
			completeImportFixture(t, k, e, approveSourcePageDeletions(t, k, preview))
			if !pageDeleted(t, k, page) {
				t.Fatal("reviewed legacy proof did not apply")
			}
		})
	}
}
