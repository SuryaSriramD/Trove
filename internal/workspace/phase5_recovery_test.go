package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

// These are workspace qualification journeys: every fixture is committed
// through its real service before comparing complete retained rows after a
// clean native restore. Counts alone cannot detect changed content or links.
func phase5RetainedTables(t *testing.T, k *Kernel, tables []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range tables {
		out[table] = retainedTable(t, k, table)
		if out[table] == "" {
			t.Fatalf("qualification fixture did not exercise %s", table)
		}
	}
	return out
}

func phase5AssertRetained(t *testing.T, k *Kernel, expected map[string]string) {
	t.Helper()
	for table, want := range expected {
		if got := retainedTable(t, k, table); got != want {
			t.Fatalf("%s changed across recovery\nwant %s\ngot %s", table, want, got)
		}
	}
}

func TestPhase5RecoveryP1ObjectGraphAndOfflineResume(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "Parent record")
	b, bp := newRecord(t, k, e, f, "Child record")
	number, formula, relation, rollup := NewID(), NewID(), NewID(), NewID()
	for _, p := range []struct {
		id, kind string
		config   any
	}{
		{number, "number", map[string]any{"format": "decimal"}},
		{formula, "formula", derivedConfig("formula", map[string]any{"originalExpression": "source amount * 2", "translatedExpression": fmt.Sprintf("prop(%q) * 2", number), "importedCachedValue": "captured 999"})},
		{relation, "relation", map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": nil}},
		{rollup, "rollup", derivedConfig("rollup", map[string]any{"relationPropertyId": relation, "targetPropertyId": formula, "aggregation": "sum"})},
	} {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": p.id, "name": p.kind, "type": p.kind, "schemaVersion": 1, "config": p.config}}, "collection", f.collection, "property", p.id))
	}
	testOK(t, setValue(t, k, e, f, b, bp, number, "number", map[string]any{"decimal": "12.5", "format": "decimal"}))
	testOK(t, setValue(t, k, e, f, a, ap, f.status, "status", f.open))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.setValue", "recordId": a, "propertyId": relation, "value": map[string]any{"type": "relation", "schemaVersion": 1, "value": []string{b}}}}, "record", a, "record", b, "page", ap, "collection", f.collection, "property", relation))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.configure", "recordId": b, "parentRecordId": a, "dependencyRecordIds": []string{a}}}, "record", a, "record", b, "page", bp, "collection", f.collection))
	linkedPage := createPage(t, k, e, "Linked presentations")
	views := []string{}
	for _, kind := range []string{"gallery", "timeline"} {
		view := NewID()
		views = append(views, view)
		var config any = map[string]any{"coverPropertyId": nil, "cardSize": "medium"}
		if kind == "timeline" {
			config = map[string]any{"datePropertyId": f.date, "scale": "month"}
		}
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": linkedPage, "name": kind, "type": kind, "schemaVersion": 1, "config": config, "query": emptyQuery(f.title, formula, rollup)}}, "view", view, "collection", f.collection, "page", linkedPage))
	}
	// The record body includes P1 canonical content, an offline embed and a
	// synced reference. Native recovery must keep the actual source identities.
	heading, synced, embed := NewID(), NewID(), NewID()
	commands := []any{}
	for _, block := range []struct {
		id, kind string
		data     any
	}{
		{heading, "heading_4", map[string]any{"richText": richImport("PhaseFiveOfflineEvidence नमस्ते")}},
		{synced, "synced_block", map[string]any{"contentSourceId": heading}},
		{embed, "embed", map[string]any{"url": "https://offline.invalid/retained", "provider": "fixture", "sandboxPolicy": "no_scripts_no_network", "offlineText": "Retained external excerpt"}},
	} {
		commands = append(commands, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block.id, "pageId": ap, "type": block.kind, "schemaVersion": 1, "data": block.data}})
	}
	testOK(t, dbBatch(t, k, e, commands, "page", ap, "record", a, "collection", f.collection, "block", heading, "block", synced, "block", embed))
	asset := stageTestFile(t, k, e, "P1 managed asset bytes\x00preserved")
	attachTestFile(t, k, e, linkedPage, asset, false)
	pageRevision := str(readPage(t, k, e, ap)["page"].(map[string]any)["revision"])
	template := testOK(t, testDispatch(t, k, e, "editor.saveTemplate", advancedRequest(k, map[string]any{"pageId": ap, "name": "P1 exact template", "expectedPageRevision": pageRevision})))
	button := testOK(t, testDispatch(t, k, e, "editor.saveButton", advancedRequest(k, map[string]any{"pageId": ap, "label": "Reviewed rename", "commands": []any{map[string]any{"kind": "page.update", "pageId": ap, "title": "Only after review"}}})))
	testOK(t, testDispatch(t, k, e, "editor.setLock", advancedRequest(k, map[string]any{"pageId": ap, "locked": true, "expectedPageRevision": pageRevision, "expectedRevision": "0"})))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": true}}, "collection", f.collection))

	rulesPage := createPage(t, k, e, "Local schedules")
	rule := scheduleRule(rulesPage)
	saveRule(t, k, e, rulesPage, rule)
	occurrence, _ := time.Parse(time.RFC3339, "2026-10-02T04:00:00Z")
	key := "schedule:" + occurrence.Format(time.RFC3339Nano)
	storedRule, err := loadAutomation(ctx, k.db, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = k.executeAutomationOccurrence(ctx, e, storedRule, "", key, occurrence); err != nil {
		t.Fatal(err)
	}
	var runStatus, origin string
	if err = k.db.QueryRow("SELECT status FROM automation_runs WHERE rule_id=?", rule.ID).Scan(&runStatus); err != nil || runStatus != "succeeded" {
		t.Fatal("fixture automation failed", runStatus, err)
	}
	if err = k.db.QueryRow("SELECT origin FROM mutation_history ORDER BY rowid DESC LIMIT 1").Scan(&origin); err != nil || origin != "automation" {
		t.Fatal("automation origin missing", origin, err)
	}
	// A finished assistance run and its unapplied proposal are retained drafts,
	// and can be inspected after recovery without an inference process.
	prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "database", Prompt: "Reading", ModelPackID: NewID()})
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, `{"title":"Reading","properties":[],"view":"gallery"}`, "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "create_database"}); err != nil {
		t.Fatal(err)
	}
	// A resumable API capture is durable evidence, never a stored credential.
	captureValue, err := startNotionCapture(ctx, k, e, map[string]any{"rootIds": []any{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "sourceNamespace": "phase5-recovery"})
	if err != nil {
		t.Fatal(err)
	}
	capture := captureValue.(NotionCapture)
	meetingPage, meeting, _ := meetingFixture(t, k, e)
	segment, transcript := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "meeting.appendSegments", "meetingId": meeting, "segments": []any{map[string]any{"segmentId": segment, "blockId": transcript, "startMs": 1200, "endMs": 3200, "text": "Retain meeting evidence"}}}, map[string]any{"kind": "meeting.finalize", "meetingId": meeting, "durationMs": 4000}}, "meeting", meeting, "page", meetingPage, "block", transcript))
	meetingSource, err := k.MeetingSource(ctx, e, meeting)
	if err != nil {
		t.Fatal(err)
	}
	enqueueTestJob(t, k, e, "SUMMARIZE_MEETING", map[string]any{"kind": "summarize", "meetingId": meeting, "sourceRevision": meetingSource.PageRevision, "modelPackId": NewID()})
	job, err := k.ClaimJob(ctx, []string{"SUMMARIZE_MEETING"})
	if err != nil {
		t.Fatal(err)
	}
	jobContext, err := k.JobExecution(ctx, string(job.JobId))
	if err != nil {
		t.Fatal(err)
	}
	meetingBody := MeetingIntelligence{Summary: []MeetingClaim{{Text: "Retained summary draft", SegmentIDs: []string{segment}}}}
	if err = k.SaveMeetingDraft(ctx, jobContext, string(job.JobId), string(*job.LeaseToken), meetingSource, meetingBody); err != nil {
		t.Fatal(err)
	}
	if err = k.FinishJob(ctx, string(job.JobId), string(*job.LeaseToken), nil, false); err != nil {
		t.Fatal(err)
	}
	expected := phase5RetainedTables(t, k, []string{"pages", "blocks", "collections", "properties", "property_options", "records", "property_values", "views", "files", "attachments", "page_snapshots", "mutation_history", "committed_events", "idempotency_receipts", "advanced_templates", "advanced_button_actions", "advanced_page_locks", "database_settings", "record_structure", "record_dependencies", "relation_edges", "automation_rules", "automation_runs", "ai_runs", "ai_proposals", "notion_captures", "import_runs", "meetings", "transcript_segments", "meeting_intelligence"})
	backup := filepath.Join(t.TempDir(), "native-backup")
	manifest, err := k.CreateBackup(ctx, e, backup)
	if err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(t.TempDir(), "clean-installation")
	if _, err = RestoreBackupReviewed(ctx, backup, restoredPath, manifest); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	phase5AssertRetained(t, restored, expected)
	owner := restored.OwnerContext()
	retainedCapture, err := loadNotionCapture(ctx, restored.db, capture.ID)
	if err != nil || string(marshal(retainedCapture)) != string(marshal(capture)) {
		t.Fatal("API capture lost its resumable request evidence", retainedCapture, err)
	}
	drafts, err := restored.MeetingDrafts(ctx, owner, meeting)
	if err != nil || len(drafts) != 1 || drafts[0].Stale || drafts[0].State != "draft" || string(marshal(drafts[0].Body)) != string(marshal(meetingBody)) {
		t.Fatal("meeting summary lost evidence or activated after recovery", drafts, err)
	}
	for _, view := range views {
		result := testOK(t, queryDB(t, restored, owner, f.collection, view, 200, ""))
		if string(marshal(result["recordIds"])) != string(marshal([]string{a, b})) {
			t.Fatal("linked view changed canonical identities", result)
		}
	}
	derived := testOK(t, testDispatch(t, restored, owner, "databaseDerived", advancedRequest(restored, map[string]any{"collectionId": f.collection, "at": "2026-10-02T04:00:00Z", "timezone": "Asia/Kolkata"})))
	rollupFound := false
	for _, cell := range derived["values"].(map[string]any)[a].([]any) {
		value := cell.(map[string]any)
		if value["propertyId"] == rollup {
			rollupFound = true
			if textValue(value["value"]) != "25" {
				t.Fatal("restored formula/relationship did not compute", value)
			}
		}
	}
	if !rollupFound {
		t.Fatal("restored rollup missing")
	}
	if len(querySearch(t, restored, owner, "PhaseFiveOfflineEvidence", nil)["hits"].([]any)) == 0 {
		t.Fatal("restored P1 body not searchable offline")
	}
	testOK(t, testDispatch(t, restored, owner, "editor.template", advancedRequest(restored, map[string]any{"templateId": template["id"]})))
	testOK(t, testDispatch(t, restored, owner, "editor.button", advancedRequest(restored, map[string]any{"actionDefinitionId": button["actionDefinitionId"]})))
	testCode(t, dbBatch(t, restored, owner, []any{map[string]any{"kind": "page.update", "pageId": ap, "title": "Denied"}}, "page", ap, "record", a, "collection", f.collection), "SCOPE_DENIED")
	testCode(t, setValue(t, restored, owner, f, b, bp, number, "number", map[string]any{"decimal": "30", "format": "decimal"}), "SCOPE_DENIED")
	for _, retained := range manifest.Assets {
		if err = verifyAsset(filepath.Join(restoredPath, filepath.FromSlash(retained.Path)), retained.ContentHash, parseSize(retained.SizeBytes)); err != nil {
			t.Fatal("restored attachment bytes differ", err)
		}
	}
	// A completed occurrence remains deduplicated after restore and restart;
	// the next local occurrence can still create its intended canonical effect.
	storedRule, err = loadAutomation(ctx, restored.db, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.executeAutomationOccurrence(ctx, owner, storedRule, "", key, occurrence); err != nil {
		t.Fatal(err)
	}
	phase5AssertRetained(t, restored, expected)
	if err = restored.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err = Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	phase5AssertRetained(t, restored, expected)
	owner = restored.OwnerContext()
	next := occurrence.AddDate(0, 0, 1)
	if err = restored.executeAutomationOccurrence(ctx, owner, storedRule, "", "schedule:"+next.Format(time.RFC3339Nano), next); err != nil {
		t.Fatal(err)
	}
	var children int
	if err = restored.db.QueryRow("SELECT count(*) FROM pages WHERE parent_page_id=?", rulesPage).Scan(&children); err != nil || children != 2 {
		t.Fatal("offline local schedule failed after recovery", children, err)
	}
	// P0 editing remains usable in the same recovered workspace.
	testOK(t, dbBatch(t, restored, owner, []any{map[string]any{"kind": "page.update", "pageId": linkedPage, "title": "Offline edited presentation"}}, "page", linkedPage))
	if readPage(t, restored, owner, linkedPage)["page"].(map[string]any)["title"] != "Offline edited presentation" {
		t.Fatal("P0 editing failed after P1 restore")
	}
	testOK(t, dbBatch(t, restored, owner, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": false}}, "collection", f.collection))
	testCode(t, dbBatch(t, restored, owner, []any{map[string]any{"kind": "page.update", "pageId": ap, "title": "Page lock still denies"}}, "page", ap, "record", a, "collection", f.collection), "CONFLICT")
}

func TestPhase5RecoveryLinkedViewsMoveReplayAndRestartIdentity(t *testing.T) {
	k, e := testKernel(t)
	f, second := newDatabase(t, k, e), newDatabase(t, k, e)
	record, recordPage := newRecord(t, k, e, f, "Shared record")
	otherRecord, _ := newRecord(t, k, e, second, "Separate source")
	container, placement := NewID(), createPage(t, k, e, "Two source container")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "container.create", "containerId": container, "owningPageId": placement, "containerKind": "database", "label": "Two sources", "parentProjectId": nil}, map[string]any{"kind": "container.linkCollection", "containerId": container, "collectionId": f.collection}, map[string]any{"kind": "container.linkCollection", "containerId": container, "collectionId": second.collection}}, "container", container, "page", placement, "collection", f.collection, "collection", second.collection))
	gallery, timeline := NewID(), NewID()
	saveView := func(view, page, kind string, config any, oldPage string) {
		pairs := []string{"view", view, "collection", f.collection, "page", page}
		if oldPage != "" {
			pairs = append(pairs, "page", oldPage)
		}
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": page, "name": kind, "type": kind, "schemaVersion": 1, "config": config, "query": emptyQuery(f.title, f.status)}}, pairs...))
	}
	saveView(gallery, placement, "gallery", map[string]any{"coverPropertyId": nil, "cardSize": "medium"}, "")
	saveView(timeline, f.page, "timeline", map[string]any{"datePropertyId": f.date, "scale": "month"}, "")
	request := testRequest(k, e, []any{map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": f.status, "value": map[string]any{"type": "status", "schemaVersion": 1, "value": f.done}}}, dbGuards(t, k, "record", record, "page", recordPage, "collection", f.collection, "property", f.status)...)
	first := testOK(t, testDispatch(t, k, e, "batch", request))
	request["requestId"] = NewID()
	replayed := testOK(t, testDispatch(t, k, e, "batch", request))
	if replayed["replayed"] != true || replayed["transactionId"] != first["transactionId"] {
		t.Fatal("linked record edit retry duplicated mutation", replayed)
	}
	saveView(timeline, placement, "timeline", map[string]any{"datePropertyId": f.date, "scale": "month"}, f.page)
	expected := phase5RetainedTables(t, k, []string{"pages", "records", "property_values", "views", "containers", "container_collections", "mutation_history"})
	path := k.Path()
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	phase5AssertRetained(t, reopened, expected)
	for _, view := range []string{gallery, timeline} {
		result := testOK(t, queryDB(t, reopened, reopened.OwnerContext(), f.collection, view, 200, ""))
		if string(marshal(result["recordIds"])) != string(marshal([]string{record})) {
			t.Fatal("linked view duplicated or mixed source records", result)
		}
	}
	other := testOK(t, queryDB(t, reopened, reopened.OwnerContext(), second.collection, "", 200, ""))
	if string(marshal(other["recordIds"])) != string(marshal([]string{otherRecord})) {
		t.Fatal("container mixed canonical source ownership", other)
	}
	var actual, ownerPage string
	if err = reopened.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, f.status).Scan(&actual); err != nil || actual != string(marshal(f.done)) {
		t.Fatal("linked edit lost on move/restart", actual, err)
	}
	if err = reopened.db.QueryRow("SELECT owning_page_id FROM views WHERE id=?", timeline).Scan(&ownerPage); err != nil || ownerPage != placement {
		t.Fatal("linked presentation move lost", ownerPage, err)
	}
}

func TestPhase5RecoveryExternalAuthorityRequiresFreshConsent(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	page := createPage(t, k, e, "Referenced local source")
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "reference.md"), []byte("OutsideWorkspaceEvidence"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := k.ApproveIndexRoot(ctx, e, folder, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	var sourceID string
	if err = k.db.QueryRow("SELECT id FROM external_sources WHERE root_id=?", root.ID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = referenceExternalSource(ctx, k, e, map[string]any{"sourceId": sourceID, "pageId": page}); err != nil {
		t.Fatal(err)
	}
	bridge := NewProviderBridge(k, e)
	defer bridge.Close()
	connection, err := bridge.Connect(ctx, "mcp", []string{page}, []string{"readPage", "proposeChanges"})
	if err != nil {
		t.Fatal(err)
	}
	proposalRaw, err := bridge.Call(ctx, connection.ID, connection.Token, "proposeChanges", map[string]any{"idempotencyKey": "restore-pending", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Must stay pending"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, "1")}})
	if err != nil {
		t.Fatal(err)
	}
	var proposal struct {
		Result ProviderProposal `json:"result"`
	}
	if err = json.Unmarshal(proposalRaw, &proposal); err != nil {
		t.Fatal(err)
	}
	expected := phase5RetainedTables(t, k, []string{"external_sources", "external_page_references", "pages"})
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err = k.CreateBackup(ctx, e, backup); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackup(ctx, backup, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	phase5AssertRetained(t, restored, expected)
	owner := restored.OwnerContext()
	if _, err = scanExternalRoot(ctx, restored, owner, map[string]any{"rootId": root.ID}); err == nil {
		t.Fatal("clean restore inherited permission to read external filesystem")
	}
	hits, err := searchExternalSources(ctx, restored, owner, map[string]any{"query": "OutsideWorkspaceEvidence"})
	if err != nil || len(hits.([]any)) != 0 {
		t.Fatal("restored external snippets accessible without renewed consent", hits, err)
	}
	if snippets := retainedTable(t, restored, "external_chunks"); snippets != "" {
		t.Fatal("external snippet cache was not removed")
	}
	var state string
	if err = restored.db.QueryRow("SELECT state FROM provider_connections WHERE id=?", connection.ID).Scan(&state); err != nil || state != "disconnected" {
		t.Fatal("provider connection activated after restore", state, err)
	}
	if err = restored.db.QueryRow("SELECT state FROM provider_proposals WHERE id=?", proposal.Result.ID).Scan(&state); err != nil || state != "interrupted" {
		t.Fatal("pending proposal activated after restore", state, err)
	}
	restoredBridge := NewProviderBridge(restored, owner)
	defer restoredBridge.Close()
	if _, err = restoredBridge.Call(ctx, connection.ID, connection.Token, "readPage", map[string]any{"pageId": page}); err == nil {
		t.Fatal("source bearer credential authorized restored installation")
	}
	for _, table := range []string{"provider_connections", "provider_proposals", "provider_audit"} {
		if strings.Contains(retainedTable(t, restored, table), connection.Token) {
			t.Fatalf("bearer credential persisted in %s", table)
		}
	}
	// Fresh native selection reuses the reference identity and restores search.
	renewed, err := restored.ApproveIndexRoot(ctx, owner, folder, false)
	if err != nil || renewed.ID != root.ID {
		t.Fatal("renewal changed root identity", renewed, err)
	}
	if _, err = scanExternalRoot(ctx, restored, owner, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	hits, err = searchExternalSources(ctx, restored, owner, map[string]any{"query": "OutsideWorkspaceEvidence"})
	if err != nil || len(hits.([]any)) != 1 {
		t.Fatal("reapproved source did not become searchable", hits, err)
	}
	if got := retainedTable(t, restored, "external_page_references"); got != expected["external_page_references"] {
		t.Fatal("reapproval lost page/source reference")
	}
}

func TestPhase5ProvenanceSuppliedMetadataAndOpaqueEvidenceSurviveRestore(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	author := map[string]any{"sourceSystem": "notion", "externalKind": "person", "externalId": "supplied-person", "displayName": "Asha", "raw": map[string]any{"avatar": nil}}
	supplied := map[string]any{"sourceSystem": "notion", "sourceCreatedAt": "2020-02-29T10:20:30Z", "sourceAuthor": author, "raw": map[string]any{"comments": []any{map[string]any{"id": "comment-1", "anchor": "missing-source-block", "anchorState": "unresolved", "author": author, "createdAt": "2020-03-01T00:00:00Z", "resolved": false, "text": "Preserve source comment"}}, "location": map[string]any{"label": "Hyderabad", "latitude": 17.385, "longitude": 78.4867}}}
	manifest := map[string]any{"pages": []any{map[string]any{"path": "Source.html", "provenance": supplied}}, "sourceOmissions": []string{"No source editor or updated time supplied"}}
	plan := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Source.html": "<h4>SourceMetadataEvidence</h4><p>Retained body</p>", "trove-manifest.json": string(marshal(manifest)), "Unknown.widget": "opaque source bytes"}))
	var page string
	for _, item := range plan.Items {
		if item.Locator == "Source.html" {
			page = item.PageID
		}
	}
	if page == "" {
		t.Fatal("HTML source not imported")
	}
	assertProvenance := func(k *Kernel) {
		t.Helper()
		p := readPage(t, k, k.OwnerContext(), page)["page"].(map[string]any)
		provenance := p["provenance"].(map[string]any)
		actual := provenance["raw"].(map[string]any)["supplied"]
		if string(marshal(actual)) != string(marshal(supplied)) {
			t.Fatal("source people/timestamps/place/comment evidence changed", actual)
		}
		if provenance["sourceEditor"] != nil || provenance["sourceUpdatedAt"] != nil {
			t.Fatal("missing source metadata was invented", provenance)
		}
	}
	assertProvenance(k)
	f := newDatabase(t, k, e)
	record, recordPage := newRecord(t, k, e, f, "Provenance record")
	for _, field := range []struct {
		kind   string
		config any
		value  any
	}{
		{"identity", map[string]any{}, []any{author}},
		{"created_time", map[string]any{}, "2020-02-29T10:20:30Z"},
		{"place", map[string]any{}, map[string]any{"label": "Hyderabad", "latitude": 17.385, "longitude": 78.4867, "raw": map[string]any{"placeId": "source-location"}}},
		{"unsupported_external", derivedConfig("opaque-config", map[string]any{"nativeType": "button-vendor"}), derivedConfig("opaque-value", map[string]any{"vendorValue": []any{"literal", nil, 42}})},
	} {
		id := NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": id, "name": field.kind, "type": field.kind, "schemaVersion": 1, "config": field.config}}, "collection", f.collection, "property", id))
		testOK(t, setValue(t, k, e, f, record, recordPage, id, field.kind, field.value))
	}
	expected := phase5RetainedTables(t, k, []string{"pages", "blocks", "properties", "property_values", "import_runs", "import_mappings", "import_mapping_state", "files", "mutation_history", "page_snapshots"})
	backup := filepath.Join(t.TempDir(), "backup")
	m, err := k.CreateBackup(ctx, e, backup)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackupReviewed(ctx, backup, destination, m); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	phase5AssertRetained(t, restored, expected)
	assertProvenance(restored)
	for _, item := range plan.Items {
		if item.Locator != "Unknown.widget" {
			continue
		}
		reader, _, err := restored.ReadFile(ctx, restored.OwnerContext(), item.RawFileID)
		if err != nil {
			t.Fatal(err)
		}
		bytes, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || string(bytes) != "opaque source bytes" {
			t.Fatal("uninterpreted source bytes lost", string(bytes), err)
		}
	}
	if len(querySearch(t, restored, restored.OwnerContext(), "SourceMetadataEvidence", nil)["hits"].([]any)) == 0 {
		t.Fatal("HTML imported evidence lost from rebuilt search")
	}
}
