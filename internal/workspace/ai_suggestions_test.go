package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"trove.local/phase0/contracts"
)

func relatedFixture(t *testing.T, longSource bool) (*Kernel, contracts.ServiceExecutionContext, string, string, string, string, string) {
	t.Helper()
	k, owner, conceptual, private, model := aiSemanticFixture(t)
	source := createPage(t, k, owner, "Notebook")
	lexical := createPage(t, k, owner, "Dog movement journal")
	count := 1
	if longSource {
		count = 225
	}
	for start := 0; start < count; start += 75 {
		commands := []any{}
		guards := []string{"page", source}
		for i := start; i < min(start+75, count); i++ {
			block := NewID()
			commands = append(commands, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": source, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "How can a dog stay healthy through movement? తెలుగు 世界 "}}}}})
			guards = append(guards, "block", block)
		}
		testOK(t, dbBatch(t, k, owner, commands, guards...))
	}
	if err := k.FlushSearch(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Explicit fixture coordinates establish an independent expected neighbor:
	// denied/private and self chunks are closer than the allowed conceptual page.
	err := k.write(context.Background(), func(tx *sql.Tx) error {
		var table string
		if err := tx.QueryRow("SELECT table_name FROM search_vector_models WHERE model_pack_id=?", model).Scan(&table); err != nil {
			return err
		}
		rows, err := tx.Query("SELECT id,page_id,content_hash FROM search_chunks")
		if err != nil {
			return err
		}
		type item struct {
			hit  SearchHit
			page string
		}
		items := []item{}
		for rows.Next() {
			var x item
			if err = rows.Scan(&x.hit.ChunkID, &x.page, &x.hit.ContentHash); err != nil {
				rows.Close()
				return err
			}
			items = append(items, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, x := range items {
			v := []float32{1, 0}
			if x.page == conceptual {
				v = []float32{.9, .1}
			}
			if x.page == lexical {
				v = []float32{0, 1}
			}
			if err = storeSearchVector(context.Background(), tx, table, x.hit, model, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return k, owner, source, conceptual, private, lexical, model
}
func relatedScope(k *Kernel, e contracts.ServiceExecutionContext, pages ...string) contracts.ServiceExecutionContext {
	ids := []contracts.DomainId{}
	for _, page := range pages {
		ids = append(ids, contracts.DomainId(page))
	}
	return k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: ids}})
}
func relatedResult(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page string) map[string]any {
	t.Helper()
	raw, err := aiSuggestions(context.Background(), k, e, map[string]any{"pageId": page, "semantic": true})
	if err != nil {
		t.Fatal(err)
	}
	return raw.(map[string]any)
}
func TestRelatedSemanticUsesBodyScopeAndExcludesSelfBeforeLimit(t *testing.T) {
	k, owner, source, conceptual, private, lexical, model := relatedFixture(t, true)
	scoped := relatedScope(k, owner, source, conceptual, lexical)
	calls := 0
	k.SetEmbeddingProvider(func(_ context.Context, got string, texts []string, _ []contracts.DomainLocation) ([][]float32, error) {
		calls++
		if got != model || len(texts) != 1 || !strings.Contains(texts[0], "dog stay healthy") || !utf8.ValidString(texts[0]) || len(texts[0]) > 4096 {
			t.Errorf("wrong bounded query %q", texts)
		}
		return [][]float32{{1, 0}}, nil
	})
	result := relatedResult(t, k, scoped, source)
	search := result["semanticBacklinks"].(SearchResults)
	if calls != 1 || search.Mode != "semantic" || len(search.Hits) == 0 || string(search.Hits[0].Location.PageId) != conceptual {
		t.Fatalf("conceptual neighbor missing: %s", marshal(result))
	}
	seen := map[string]bool{}
	for _, hit := range search.Hits {
		id := string(hit.Location.PageId)
		if id == source || id == private || seen[id] || hit.Location.SourceRevision == "" || hit.Excerpt == "" {
			t.Fatal(hit)
		}
		seen[id] = true
	}
	if len(result["omissions"].([]string)) == 0 {
		t.Fatal("bounded query omission missing")
	}
	// The selector is also applied before the keyword result limit and validates
	// malformed selectors rather than silently widening a restricted query.
	keyword := querySearch(t, k, scoped, "dog", map[string]any{"limit": 1, "excludePageId": source})
	if hits := keyword["hits"].([]any); len(hits) != 1 || hits[0].(map[string]any)["location"].(map[string]any)["pageId"] != lexical {
		t.Fatal(keyword)
	}
	for _, bad := range []any{"", "x' OR 1=1 --", []any{source}} {
		if _, err := searchQuery(context.Background(), k, scoped, map[string]any{"query": "dog", "excludePageId": bad}); err == nil {
			t.Fatal("invalid exclusion broadened search")
		}
	}
}
func TestRelatedDisabledOrUnavailableModelUsesExplicitKeywordFallback(t *testing.T) {
	for _, mode := range []string{"disabled", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			k, owner, source, conceptual, _, lexical, _ := relatedFixture(t, false)
			// A duplicate contains all literal keyword terms, independently of embeddings.
			testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.update", "pageId": lexical, "title": "Notebook"}}, "page", lexical))
			block := NewID()
			testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": lexical, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "How can a dog stay healthy through movement? తెలుగు 世界 "}}}}}}, "page", lexical, "block", block))
			scoped := relatedScope(k, owner, source, conceptual, lexical)
			if mode == "disabled" {
				testOK(t, testDispatch(t, k, owner, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": false})))
				k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
					t.Error("disabled model invoked")
					return nil, fmt.Errorf("disabled")
				})
			} else {
				k.SetEmbeddingProvider(nil)
			}
			result := relatedResult(t, k, scoped, source)
			search := result["semanticBacklinks"].(SearchResults)
			if search.Mode != "keyword" || search.Warning == "" || len(search.Hits) != 1 || string(search.Hits[0].Location.PageId) != lexical {
				t.Fatal(string(marshal(result)))
			}
		})
	}
}
func TestRelatedRechecksSourceGrantCandidatesAndConsentAfterEmbedding(t *testing.T) {
	for _, change := range []string{"source_edit", "source_delete", "revocation", "candidate_edit", "candidate_delete", "disable"} {
		t.Run(change, func(t *testing.T) {
			k, owner, source, conceptual, _, lexical, _ := relatedFixture(t, false)
			scoped := relatedScope(k, owner, source, conceptual, lexical)
			// This is a vocabulary/duplicate candidate captured before the worker runs.
			testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.update", "pageId": lexical, "title": "Notebook"}}, "page", lexical))
			before, err := aiSuggestions(context.Background(), k, scoped, map[string]any{"pageId": source})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, candidate := range before.(map[string]any)["suggestions"].([]map[string]any) {
				found = found || candidate["pageId"] == lexical
			}
			if !found {
				t.Fatal("fixture did not establish the lexical candidate before embedding")
			}
			k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
				switch change {
				case "source_edit":
					testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.update", "pageId": source, "title": "New subject"}}, "page", source))
				case "source_delete":
					testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.trash", "pageId": source, "includeDescendants": false}}, "page", source))
				case "revocation":
					k.RevokeGrant(string(scoped.ScopeGrantId))
				case "candidate_edit":
					testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.update", "pageId": lexical, "title": "Changed during retrieval"}}, "page", lexical))
				case "candidate_delete":
					testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.trash", "pageId": lexical, "includeDescendants": false}}, "page", lexical))
				case "disable":
					testOK(t, testDispatch(t, k, owner, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": false})))
				}
				return [][]float32{{1, 0}}, nil
			})
			raw, err := aiSuggestions(context.Background(), k, scoped, map[string]any{"pageId": source, "semantic": true})
			if change == "source_edit" || change == "source_delete" || change == "revocation" {
				if err == nil || raw != nil {
					t.Fatalf("stale/denied result published: %s %v", marshal(raw), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := raw.(map[string]any)
			search := result["semanticBacklinks"].(SearchResults)
			if change == "disable" {
				if search.Mode != "keyword" || !strings.Contains(search.Warning, "changed") {
					t.Fatal(search)
				}
				return
			}
			for _, candidate := range result["suggestions"].([]map[string]any) {
				if candidate["pageId"] == lexical {
					t.Fatal("stale lexical candidate published", candidate)
				}
			}
			for _, candidate := range search.Hits {
				if string(candidate.Location.PageId) == lexical {
					t.Fatal("stale retrieved candidate published", candidate)
				}
			}
		})
	}
}

func TestRelatedNeverEmbedsForgottenOrStaleMemorySource(t *testing.T) {
	for _, state := range []string{"forgotten", "stale"} {
		t.Run(state, func(t *testing.T) {
			k, owner, _, _, _, _, _ := relatedFixture(t, false)
			evidence := createPage(t, k, owner, "Memory evidence")
			memory, page := memoryFixture(t, k, owner, evidence)
			if state == "forgotten" {
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "memory.forget", "memoryId": memory}}, "memory", memory, "page", page))
			} else {
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.update", "pageId": evidence, "title": "New evidence"}}, "page", evidence))
			}
			k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
				t.Error("inadmissible memory was sent to the embedding worker")
				return [][]float32{{1, 0}}, nil
			})
			if result, err := aiSuggestions(context.Background(), k, owner, map[string]any{"pageId": page, "semantic": true}); err == nil || result != nil {
				t.Fatalf("hidden memory yielded suggestions: %s %v", marshal(result), err)
			}
		})
	}
}
