package workspace

import (
	"context"
	"encoding/json"
	"testing"

	"trove.local/phase0/contracts"
)

func TestMeetingRestoreExistingEditedAndDeletedSegments(t *testing.T) {
	for _, change := range []string{"rename", "edit", "delete"} {
		t.Run(change, func(t *testing.T) {
			k, e := testKernel(t)
			page, meeting, file := meetingFixture(t, k, e)
			block, segment := NewID(), NewID()
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "meeting.appendSegments", "meetingId": meeting, "segments": []any{map[string]any{"segmentId": segment, "blockId": block, "startMs": 1200, "endMs": 3400, "text": "Original transcript", "speakerLabel": "Speaker 1"}}}, map[string]any{"kind": "meeting.finalize", "meetingId": meeting, "durationMs": 4000}}, "page", page, "meeting", meeting, "block", block))
			var revision string
			if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			other, _, _ := meetingFixture(t, k, e)
			beforeOther := string(marshal(readPage(t, k, e, other)["page"]))
			switch change {
			case "rename":
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Renamed"}}, "page", page))
			case "edit":
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Edited transcript")}}}, "page", page, "block", block))
			case "delete":
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, "page", page, "block", block))
			}
			scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page)}}})
			if change != "delete" {
				if err := k.authorizeEntity(context.Background(), k.db, scoped, "transcript_segment", segment); err != nil {
					t.Fatal(err)
				}
			}
			result := testOK(t, restoreTest(t, k, scoped, page, revision))
			var gotBlock, text, speaker, gotFile, segmentRevision string
			var start, end int
			if err := k.db.QueryRow("SELECT s.block_id,s.text,s.speaker_label,s.start_ms,s.end_ms,s.revision,m.recording_file_id FROM transcript_segments s JOIN meetings m ON m.id=s.meeting_id WHERE s.id=?", segment).Scan(&gotBlock, &text, &speaker, &start, &end, &segmentRevision, &gotFile); err != nil {
				t.Fatal(err)
			}
			if gotBlock != block || text != "Original transcript" || speaker != "Speaker 1" || start != 1200 || end != 3400 || gotFile != file {
				t.Fatalf("restored segment differs: %s %q %q %d %d %s", gotBlock, text, speaker, start, end, gotFile)
			}
			matched := false
			for _, raw := range result["revisions"].([]any) {
				r := raw.(map[string]any)
				if r["target"].(map[string]any)["entityId"] == segment {
					matched = true
					if r["after"] != segmentRevision {
						t.Fatal("event revision differs from segment", r, segmentRevision)
					}
				}
			}
			if !matched {
				t.Fatal("missing segment revision")
			}
			if got := string(marshal(readPage(t, k, e, other)["page"])); got != beforeOther {
				t.Fatal("unrelated meeting changed")
			}
		})
	}
}

func TestMeetingRestoreScopeAndStaleSegmentAreAtomic(t *testing.T) {
	k, e := testKernel(t)
	page, meeting, _ := meetingFixture(t, k, e)
	block, segment := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "meeting.appendSegments", "meetingId": meeting, "segments": []any{map[string]any{"segmentId": segment, "blockId": block, "startMs": 0, "endMs": 500, "text": "Before"}}}}, "page", page, "meeting", meeting, "block", block))
	var revision string
	if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	other := createPage(t, k, e, "Unrelated")
	denied := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(other)}}})
	if err := k.authorizeEntity(context.Background(), k.db, denied, "transcript_segment", segment); err == nil {
		t.Fatal("segment authorization escaped meeting page")
	}
	testCode(t, testDispatch(t, k, denied, "history.previewRestore", recoveryRequest(k, map[string]any{"pageId": page, "restoreRevision": revision})), "SCOPE_DENIED")
	preview := testOK(t, testDispatch(t, k, e, "history.previewRestore", recoveryRequest(k, map[string]any{"pageId": page, "restoreRevision": revision})))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Concurrent transcript correction")}}}, "page", page, "block", block))
	before := string(marshal(readPage(t, k, e, page)["page"]))
	var guards []contracts.ServiceExpectedRevision
	if err := json.Unmarshal(marshal(preview["expectedRevisions"]), &guards); err != nil {
		t.Fatal(err)
	}
	testCode(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "history.restore", "pageId": page, "restoreRevision": revision, "previewId": preview["previewId"]}}, guards...)), "CONFLICT")
	if got := string(marshal(readPage(t, k, e, page)["page"])); got != before {
		t.Fatal("stale restore partially changed page")
	}
	var text string
	if err := k.db.QueryRow("SELECT text FROM transcript_segments WHERE id=?", segment).Scan(&text); err != nil || text != "Concurrent transcript correction" {
		t.Fatal("stale restore changed segment", text, err)
	}
}
