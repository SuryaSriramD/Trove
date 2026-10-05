package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func splitSearchText() []any {
	return []any{map[string]any{"text": "Preserve café 🦉 and this imp"}, map[string]any{"text": "ortant phrase.", "marks": []any{"bold"}, "link": "https://example.invalid/never-index-this"}}
}

func TestSearchFormattingWithinWordPreservesCanonicalText(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Native rich text regression")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": splitSearchText()}}}}, "page", page, "block", block))
	before, err := readPageTx(context.Background(), k.db, page)
	if err != nil {
		t.Fatal(err)
	}
	out := querySearch(t, k, e, "important phrase", nil)
	hits := out["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("formatting hid complete word: %v", out)
	}
	loc := hits[0].(map[string]any)["location"].(map[string]any)
	citation := testOK(t, testDispatch(t, k, e, "resolveCitation", recoveryRequest(k, map[string]any{"location": loc})))
	if citation["excerpt"] != "Preserve café 🦉 and this important phrase." {
		t.Fatal(citation)
	}
	if len(querySearch(t, k, e, "never-index-this", nil)["hits"].([]any)) != 0 {
		t.Fatal("formatting link leaked into text projection")
	}
	after, err := readPageTx(context.Background(), k.db, page)
	if err != nil {
		t.Fatal(err)
	}
	if string(marshal(before.Page)) != string(marshal(after.Page)) || string(marshal(before.Blocks)) != string(marshal(after.Blocks)) {
		t.Fatal("index changed canonical rich text")
	}
}

func TestSearchRichTextBoundariesRetainWhitespaceAndSeparateCells(t *testing.T) {
	var value any
	if err := json.Unmarshal([]byte(`{"cells":[{"richText":[{"text":"Andro"},{"text":"meda","marks":["bold"]}]},{"richText":[{"text":"tele"},{"text":"scope"}]}],"caption":[{"text":" café"},{"text":"\n🦉"}]}`), &value); err != nil {
		t.Fatal(err)
	}
	if got := searchReadable(value); got != " café\n🦉 Andromeda telescope" {
		t.Fatalf("lost rich text or structural boundary: %q", got)
	}
	if got := searchReadable([]any{"alpha", "beta"}); got != "alpha beta" {
		t.Fatal("unrelated list items joined", got)
	}
}

func TestSearchTextPropertyFormattingPreservesWholeWord(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Separate record")
	property := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": property, "name": "Notes", "type": "text", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", property, "page", f.page))
	testOK(t, setValue(t, k, e, f, r, p, property, "text", splitSearchText()))
	hits := querySearch(t, k, e, "important phrase", nil)["hits"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["location"].(map[string]any)["pageId"] != p {
		t.Fatal("formatted database property missing", hits)
	}
	fields, err := personalReviewRecordFields(context.Background(), k.db, p)
	if err != nil || fields != "Notes: Preserve café 🦉 and this important phrase." {
		t.Fatal("review evidence split the formatted property", fields, err)
	}
}

func TestSearchProjectionUpgradeReindexesPersistedOldWordsOnce(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Legacy index")
	block := NewID()
	ctx := context.Background()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": splitSearchText()}}}}, "page", page, "block", block))
	if err := k.FlushSearch(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := readPageTx(ctx, k.db, page)
	if err != nil {
		t.Fatal(err)
	}
	// Persist the old derived bytes without changing canonical content/revisions.
	old := "Preserve café 🦉 and this imp ortant phrase."
	if _, err = k.db.Exec("UPDATE search_chunks SET body=?,content_hash=? WHERE block_id=?", old, searchDigest(old), block); err != nil {
		t.Fatal(err)
	}
	if _, err = k.db.Exec("DELETE FROM search_meta WHERE key='readable_projection'"); err != nil {
		t.Fatal(err)
	}
	var generation string
	if err = k.db.QueryRow("SELECT value FROM search_meta WHERE key='generation'").Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if len(querySearch(t, k, e, "important", nil)["hits"].([]any)) != 0 {
		t.Fatal("fixture did not retain old broken index")
	}
	path := k.path
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	owner := reopened.OwnerContext()
	hits := querySearch(t, reopened, owner, "important phrase", nil)
	if len(hits["hits"].([]any)) != 1 || hits["indexGeneration"] == generation {
		t.Fatal("old projection was not rebuilt", hits)
	}
	after, err := readPageTx(ctx, reopened.db, page)
	if err != nil {
		t.Fatal(err)
	}
	if string(marshal(before.Page)) != string(marshal(after.Page)) || string(marshal(before.Blocks)) != string(marshal(after.Blocks)) {
		t.Fatal("projection upgrade changed canonical state")
	}
	var body string
	if err = reopened.db.QueryRow("SELECT body FROM search_chunks WHERE block_id=?", block).Scan(&body); err != nil || strings.Contains(body, "imp ortant") {
		t.Fatal(body, err)
	}
	stableGeneration := hits["indexGeneration"]
	if err = reopened.InitSearch(ctx); err != nil {
		t.Fatal(err)
	}
	var dirty int
	if err = reopened.db.QueryRow("SELECT count(*) FROM search_dirty").Scan(&dirty); err != nil || dirty != 0 {
		t.Fatal("current projection queued repeated rebuild", dirty, err)
	}
	if querySearch(t, reopened, owner, "important", nil)["indexGeneration"] != stableGeneration {
		t.Fatal("current generation changed on repeated initialization")
	}
}

func TestSearchProjectionUpgradeRemovesStaleVectorCoordinates(t *testing.T) {
	k, _, page, _, _ := aiSemanticFixture(t)
	ctx := context.Background()
	before, err := readPageTx(ctx, k.db, page)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = k.db.QueryRow("SELECT count(*) FROM search_embeddings").Scan(&count); err != nil || count == 0 {
		t.Fatal("fixture needs vectors", count, err)
	}
	if _, err = k.db.Exec("DELETE FROM search_meta WHERE key='readable_projection'"); err != nil {
		t.Fatal(err)
	}
	if err = k.InitSearch(ctx); err != nil {
		t.Fatal(err)
	}
	if err = k.FlushSearch(ctx); err != nil {
		t.Fatal(err)
	}
	if err = k.db.QueryRow("SELECT count(*) FROM search_embeddings").Scan(&count); err != nil || count != 0 {
		t.Fatal("old embedding survived readable projection upgrade", count, err)
	}
	rows, err := k.db.Query("SELECT table_name FROM search_vector_models")
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{}
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if err = k.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("sqlite-vec retained stale coordinates", count, err)
		}
	}
	after, err := readPageTx(ctx, k.db, page)
	if err != nil {
		t.Fatal(err)
	}
	if string(marshal(before.Page)) != string(marshal(after.Page)) || string(marshal(before.Blocks)) != string(marshal(after.Blocks)) {
		t.Fatal("vector invalidation changed canonical state")
	}
}
