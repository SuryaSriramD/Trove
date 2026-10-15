package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func conversationBlockBySource(t *testing.T, item ImportItem, identity string) string {
	t.Helper()
	for id, source := range item.BlockSources {
		if source == identity {
			return id
		}
	}
	t.Fatalf("missing conversation source identity %s", identity)
	return ""
}

func TestConversationReimportPreservesLocalMessageAndStableNavigation(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"conversations.json": chatGPTConversationFixture}))
	item := conversationItem(t, first)
	local := conversationBlockBySource(t, item, "conversation-node:reply-a:text")
	incoming := conversationBlockBySource(t, item, "conversation-node:reply-b:text")
	updateImportFixtureBlock(t, k, local, "Local corrected answer")
	changed := strings.Replace(chatGPTConversationFixture, "Southern light branch beta", "Updated source answer", 1)
	preview := importFixture(t, k, e, map[string]string{"renamed-export.json": changed})
	reviewed := conversationItem(t, preview)
	if reviewed.PageID != item.PageID || conversationBlockBySource(t, reviewed, "conversation-node:reply-b:text") != incoming {
		t.Fatal("conversation reimport changed stable identity")
	}
	preview = reviewDatabasePreview(t, k, preview, nil)
	completeImportFixture(t, k, e, preview)
	for id, want := range map[string]string{local: "Local corrected answer", incoming: "Updated source answer"} {
		var raw string
		if err := k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", id).Scan(&raw); err != nil || !strings.Contains(raw, want) {
			t.Fatal("message merge lost independent changes", id, err)
		}
	}
	var raw string
	if err := k.db.QueryRow("SELECT provenance_json FROM pages WHERE id=?", item.PageID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var provenance struct {
		Raw struct {
			Supplied struct {
				Nodes []conversationNode `json:"nodes"`
			} `json:"supplied"`
		} `json:"raw"`
	}
	if err := json.Unmarshal([]byte(raw), &provenance); err != nil {
		t.Fatal(err)
	}
	for _, node := range provenance.Raw.Supplied.Nodes {
		var count int
		if err := k.db.QueryRow("SELECT count(*) FROM blocks WHERE id=? AND page_id=?", node.BlockID, item.PageID).Scan(&count); err != nil || count != 1 {
			t.Fatal("provenance retained a discarded generated block ID", node.ID, node.BlockID, err)
		}
	}
}

func TestConversationIncompleteEnumerationRetainsOmittedNodeIdentity(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"conversations.json": chatGPTConversationFixture}))
	item := conversationItem(t, first)
	omitted := conversationBlockBySource(t, item, "conversation-node:reply-b:text")
	var entries []map[string]any
	json.Unmarshal([]byte(chatGPTConversationFixture), &entries)
	mapping := entries[0]["mapping"].(map[string]any)
	delete(mapping, "reply-b")
	mapping["question"].(map[string]any)["children"] = []string{"reply-a"}
	entries[0]["current_node"] = "reply-a"
	partial := importFixture(t, k, e, map[string]string{"conversations.json": string(marshal(entries))})
	for _, field := range conversationItem(t, partial).Merge.Fields {
		if strings.HasPrefix(field.Path, "/blocks/"+omitted+"/") && !field.Incoming.Present {
			t.Fatal("incomplete conversation capture inferred node deletion", field)
		}
	}
	partial = reviewDatabasePreview(t, k, partial, nil)
	completeImportFixture(t, k, e, partial)
	base, _, known, err := loadImportBaseline(context.Background(), k.db, item.MappingID)
	if err != nil || !known || base.BlockSources[omitted] != "conversation-node:reply-b:text" {
		t.Fatal("partial capture lost omitted node identity", err)
	}
	full := importFixture(t, k, e, map[string]string{"conversations.json": strings.Replace(chatGPTConversationFixture, "Southern light branch beta", "Recovered complete branch", 1)})
	full = reviewDatabasePreview(t, k, full, nil)
	completeImportFixture(t, k, e, full)
	var raw string
	if err := k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", omitted).Scan(&raw); err != nil || !strings.Contains(raw, "Recovered complete branch") {
		t.Fatal("full capture duplicated omitted node", err)
	}
}

func TestConversationAnonymousClaudeMessagesNeverMatchEditedPositions(t *testing.T) {
	k, e := testKernel(t)
	raw := `{"uuid":"claude-thread","name":"Anonymous","chat_messages":[{"sender":"human","text":"First anonymous message"},{"sender":"assistant","text":"Second anonymous message"}]}`
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"claude.json": raw}))
	item := conversationItem(t, first)
	old := conversationBlockBySource(t, item, "normalizer-position:"+item.Hash+":conversation-node:message-1:text")
	identical := importFixture(t, k, e, map[string]string{"claude.json": raw})
	if conversationItem(t, identical).Choice != "keep_local" || conversationBlockBySource(t, conversationItem(t, identical), "normalizer-position:"+item.Hash+":conversation-node:message-1:text") != old {
		t.Fatal("identical anonymous source lost retained normalizer identity")
	}
	changed := strings.Replace(raw, "First anonymous message", "An unrelated replacement at this position", 1)
	preview := importFixture(t, k, e, map[string]string{"claude.json": changed})
	updated := conversationItem(t, preview)
	if conversationBlockBySource(t, updated, "normalizer-position:"+updated.Hash+":conversation-node:message-1:text") == old {
		t.Fatal("edited anonymous position stole the prior message identity")
	}
	// The exact user-chosen copy path remains usable without fabricating a
	// correspondence between these ambiguous messages.
	out, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash, "choices": []any{map[string]any{"sourceLocator": updated.Locator, "choice": "duplicate"}}})
	if err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, k, e, out.(ImportPlan))
	var original string
	if err := k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", old).Scan(&original); err != nil || !strings.Contains(original, "First anonymous message") {
		t.Fatal("explicit copy changed the original message", err)
	}
}
