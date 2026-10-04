package workspace

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

const chatGPTConversationFixture = `[{"id":"conversation-42","title":"All branches","create_time":1700000000,"current_node":"reply-b","mapping":{"root":{"parent":null,"children":["question"],"message":null},"question":{"parent":"root","children":["reply-a","reply-b"],"message":{"id":"message-u","author":{"role":"user"},"create_time":1700000001,"content":{"content_type":"text","parts":["Explain the aurora"]}}},"reply-a":{"parent":"question","children":[],"message":{"id":"message-a","author":{"role":"assistant"},"create_time":1700000002,"content":{"parts":["Northern light branch alpha"]}}},"reply-b":{"parent":"question","children":[],"message":{"id":"message-b","author":{"role":"assistant"},"create_time":1700000003,"content":{"parts":["Southern light branch beta",{"image_asset_pointer":"image://original"}]},"metadata":{"hidden_secret":"must not enter text projections"}}}}}]`

func conversationItem(t *testing.T, p ImportPlan) ImportItem {
	t.Helper()
	for _, item := range p.Items {
		if item.Kind == "conversation" {
			return item
		}
	}
	t.Fatal("conversation was not parsed")
	return ImportItem{}
}

func TestConversationExportCanonicalBranchesProvenanceSearchAndRoundTrip(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{"conversations.json": chatGPTConversationFixture})
	item := conversationItem(t, p)
	if item.ExternalID != "chatgpt:conversation-42" || item.IdentityKind != "external_id" {
		t.Fatal(item)
	}
	if !strings.Contains(string(marshal(item.Provenance)), `"currentNode":"reply-b"`) {
		t.Fatal("selected branch lost")
	}
	if len(item.Provenance.(map[string]any)["nodes"].([]conversationNode)) != 4 {
		t.Fatal("branch nodes dropped")
	}
	p = completeImportFixture(t, k, e, p)
	page := readPage(t, k, e, item.PageID)
	if !strings.Contains(string(marshal(page["page"])), `"sourceSystem":"chatgpt"`) {
		t.Fatal("conversation provenance misidentifies its provider")
	}
	text := string(marshal(page["blocks"]))
	for _, want := range []string{"Northern light branch alpha", "Southern light branch beta", "Parent: question", "Reply: reply-b"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing branch evidence %s", want)
		}
	}
	if strings.Contains(text, "hidden_secret") || strings.Contains(text, "image://original") {
		t.Fatal("non-text metadata was projected")
	}
	if err := k.FlushSearch(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM search_chunks WHERE page_id=? AND body LIKE '%branch beta%'", item.PageID).Scan(&count); err != nil || count == 0 {
		t.Fatal("message text not searchable", count, err)
	}
	bytes, err := k.readImportBytes(context.Background(), item.RawFileID)
	if err != nil || string(bytes) != chatGPTConversationFixture {
		t.Fatal("original conversation source changed", err)
	}
	// Re-import reconciles by provider conversation identity and explicit review.
	second := importFixture(t, k, e, map[string]string{"renamed-export.json": chatGPTConversationFixture})
	again := conversationItem(t, second)
	if again.PageID != item.PageID || again.Choice != "keep_local" {
		t.Fatal("stable conversation identity not reconciled", again)
	}
	// Portable export keeps canonical navigation, message provenance and exact source.
	archive := filepath.Join(t.TempDir(), "conversation.zip")
	if _, err = k.ExportPortable(context.Background(), e, archive); err != nil {
		t.Fatal(err)
	}
	entries := portableEntries(t, archive)
	if !strings.Contains(entries["trove-manifest.json"], `"conversationId": "conversation-42"`) && !strings.Contains(entries["trove-manifest.json"], `"conversationId":"conversation-42"`) {
		t.Fatal("conversation provenance absent from manifest")
	}
	k2, e2 := testKernel(t)
	imported := completeImportFixture(t, k2, e2, importFixture(t, k2, e2, entries))
	var roundTrip string
	conversationPages := 0
	for _, entry := range imported.Items {
		if entry.Title == "All branches" && entry.PageID != "" {
			roundTrip = entry.PageID
			conversationPages++
		}
	}
	if conversationPages != 1 {
		t.Fatalf("portable evidence was re-imported as duplicate conversations: %d", conversationPages)
	}
	if roundTrip == "" || !strings.Contains(string(marshal(readPage(t, k2, e2, roundTrip))), "Southern light branch beta") {
		t.Fatal("canonical conversation round trip failed")
	}
	roundTripBlocks := string(marshal(readPage(t, k2, e2, roundTrip)["blocks"]))
	if strings.Contains(roundTripBlocks, "trove://page/"+item.PageID) || !strings.Contains(roundTripBlocks, "trove://page/"+roundTrip+"/block/") {
		t.Fatal("conversation navigation did not remap into the destination workspace")
	}
	backup := filepath.Join(t.TempDir(), "conversation-backup")
	if _, err = k.CreateBackup(context.Background(), e, backup); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyBackup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	retained, err := restored.readImportBytes(context.Background(), item.RawFileID)
	if err != nil || string(retained) != chatGPTConversationFixture {
		t.Fatal("conversation evidence lost in backup", err)
	}
}

func portableEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	out := map[string]string{}
	for _, f := range reader.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(b)
	}
	return out
}

func TestConversationExportClaudeAndInvalidGraphsRetainOriginal(t *testing.T) {
	claude := `[{"uuid":"claude-c","name":"Claude import","created_at":"2026-09-30T00:00:00Z","chat_messages":[{"uuid":"human-1","sender":"human","text":"A Claude question","created_at":"2026-09-30T00:00:01Z"},{"uuid":"assistant-1","sender":"assistant","content":[{"type":"text","text":"Claude answer"},{"type":"tool_use","name":"external-operation","input":{"text":"do not execute"}}]}]}]`
	items, recognized, err := ParseConversationExport([]byte(claude), ImportItem{Locator: "claude.json", RawFileID: NewID()})
	if err != nil || !recognized || len(items) != 1 {
		t.Fatal(recognized, err)
	}
	if !strings.Contains(string(marshal(items[0])), "Parent: human-1") || strings.Contains(string(marshal(items[0].Blocks)), "do not execute") {
		t.Fatal("Claude structure or inert tool payload changed")
	}
	k, e := testKernel(t)
	broken := strings.Replace(chatGPTConversationFixture, `"parent":"root"`, `"parent":"reply-a"`, 1)
	p := importFixture(t, k, e, map[string]string{"broken.json": broken})
	for _, item := range p.Items {
		if item.Kind == "conversation" {
			t.Fatal("invalid graph silently adapted")
		}
		if item.Locator == "broken.json" && (!strings.Contains(item.Reason, "Conversation export was not committed") || item.Outcome != "unsupported_but_retained") {
			t.Fatal(item)
		}
	}
	p = completeImportFixture(t, k, e, p)
	if len(p.Items) == 0 {
		t.Fatal("source inventory lost")
	}
}

func TestPageDisplayPersistenceScopeHistoryPortableAndBackup(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Presentation")
	get := func(k *Kernel, e contracts.ServiceExecutionContext, page string) map[string]any {
		return testOK(t, testDispatch(t, k, e, "pageDisplay.get", advancedRequest(k, map[string]any{"pageId": page})))
	}
	if get(k, e, page)["fullWidth"] != false {
		t.Fatal("incorrect default")
	}
	command := map[string]any{"kind": "page.display.set", "pageId": page, "fullWidth": true, "presentation": true}
	testOK(t, dbBatch(t, k, e, []any{command}, "page", page))
	if got := get(k, e, page); got["fullWidth"] != true || got["presentation"] != true || got["revision"] != "2" {
		t.Fatal(got)
	}
	outsider := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(NewID())}}})
	testCode(t, testDispatch(t, k, outsider, "pageDisplay.get", advancedRequest(k, map[string]any{"pageId": page})), "SCOPE_DENIED")
	testCode(t, dbBatch(t, k, outsider, []any{command}, "page", page), "SCOPE_DENIED")
	// History applies display settings from the reviewed revision.
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.display.set", "pageId": page, "fullWidth": false, "presentation": false}}, "page", page))
	testOK(t, restoreTest(t, k, e, page, "2"))
	if get(k, e, page)["presentation"] != true {
		t.Fatal("history lost preferences")
	}
	archive := filepath.Join(t.TempDir(), "portable.zip")
	if _, err := k.ExportPortable(context.Background(), e, archive); err != nil {
		t.Fatal(err)
	}
	k2, e2 := testKernel(t)
	p := completeImportFixture(t, k2, e2, importFixture(t, k2, e2, portableEntries(t, archive)))
	for _, item := range p.Items {
		if item.Title == "Presentation" {
			if get(k2, e2, item.PageID)["fullWidth"] != true {
				t.Fatal("portable import lost preferences")
			}
		}
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err := k.CreateBackup(context.Background(), e, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBackup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if get(restored, restored.OwnerContext(), page)["presentation"] != true {
		t.Fatal("native backup lost preferences")
	}
	location := k.Path()
	k.Close()
	reopened, err := Open(location)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if get(reopened, reopened.OwnerContext(), page)["fullWidth"] != true {
		t.Fatal("restart lost preferences")
	}
	var stored map[string]any
	if err = json.Unmarshal(marshal(readPage(t, reopened, reopened.OwnerContext(), page)), &stored); err != nil {
		t.Fatal(err)
	}
}
