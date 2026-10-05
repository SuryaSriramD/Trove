package workspace

import (
	"context"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func TestRelatedGraphRespectsConfiguredModelScopeAndSelfExclusion(t *testing.T) {
	k, owner, source, conceptual, private, lexical, model := relatedFixture(t, true)
	scoped := relatedScope(k, owner, source, conceptual, lexical)
	k.SetEmbeddingProvider(func(_ context.Context, got string, _ []string, _ []contracts.DomainLocation) ([][]float32, error) {
		if got != model {
			t.Errorf("got model %s instead of configured %s", got, model)
		}
		return [][]float32{{1, 0}}, nil
	})
	raw, err := knowledgeGraph(context.Background(), k, scoped, map[string]any{"pageId": source, "includeSemantic": true, "query": "dog", "limit": 3})
	if err != nil {
		t.Fatal(err)
	}
	graph := raw.(KnowledgeGraph)
	found := false
	for _, edge := range graph.Edges {
		if edge.Target == private || edge.Target == source || edge.Kind != "semantic" {
			t.Fatal(edge)
		}
		found = found || edge.Target == conceptual
	}
	if !found {
		t.Fatal("hundreds of nearer self chunks suppressed conceptual neighbor", graph)
	}
}

func TestRelatedGraphDisabledUnavailableAndChangedConsentUsesKeywordEdges(t *testing.T) {
	for _, mode := range []string{"disabled", "unavailable", "disabled_during_embedding", "epoch_changed"} {
		t.Run(mode, func(t *testing.T) {
			k, owner, source, conceptual, _, lexical, model := relatedFixture(t, false)
			scoped := relatedScope(k, owner, source, conceptual, lexical)
			calls := 0
			k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
				calls++
				if mode == "disabled" {
					t.Error("disabled graph embedding invoked")
				}
				if mode == "disabled_during_embedding" || mode == "epoch_changed" {
					testOK(t, testDispatch(t, k, owner, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": false})))
					if mode == "epoch_changed" {
						testOK(t, testDispatch(t, k, owner, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": true, "modelPackId": model, "dimension": 2})))
					}
				}
				return [][]float32{{1, 0}}, nil
			})
			if mode == "disabled" {
				testOK(t, testDispatch(t, k, owner, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": false})))
			} else if mode == "unavailable" {
				k.SetEmbeddingProvider(nil)
			}
			raw, err := knowledgeGraph(context.Background(), k, scoped, map[string]any{"pageId": source, "includeSemantic": true, "query": "dog"})
			if err != nil {
				t.Fatal(err)
			}
			graph := raw.(KnowledgeGraph)
			if len(graph.Edges) != 1 || graph.Edges[0].Target != lexical || graph.Edges[0].Kind != "keyword" || graph.Warning == "" {
				t.Fatal(graph)
			}
			if (mode == "disabled" || mode == "unavailable") && calls != 0 {
				t.Fatal(calls)
			}
			if (mode == "disabled_during_embedding" || mode == "epoch_changed") && (calls != 1 || !strings.Contains(graph.Warning, "changed")) {
				t.Fatal(calls, graph)
			}
		})
	}
}

func TestRelatedGraphRechecksExplicitSnapshotAfterEmbedding(t *testing.T) {
	for _, change := range []string{"source_edit", "source_delete", "candidate_edit", "candidate_delete", "revocation"} {
		t.Run(change, func(t *testing.T) {
			k, owner, source, conceptual, _, lexical, _ := relatedFixture(t, false)
			scoped := relatedScope(k, owner, source, conceptual, lexical)
			block := NewID()
			testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": source, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "linked notes", "link": canonicalPageLink(lexical, "")}}}}}}, "page", source, "block", block))
			k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
				if change == "revocation" {
					k.RevokeGrant(string(scoped.ScopeGrantId))
					return [][]float32{{1, 0}}, nil
				}
				page := source
				if strings.HasPrefix(change, "candidate_") {
					page = lexical
				}
				command := map[string]any{"kind": "page.update", "pageId": page, "title": "Changed during graph retrieval"}
				if strings.HasSuffix(change, "delete") {
					command = map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}
				}
				testOK(t, dbBatch(t, k, owner, []any{command}, "page", page))
				return [][]float32{{1, 0}}, nil
			})
			raw, err := knowledgeGraph(context.Background(), k, scoped, map[string]any{"pageId": source, "includeSemantic": true, "query": "dog"})
			if strings.HasPrefix(change, "source_") || change == "revocation" {
				if err == nil || raw != nil {
					t.Fatal("stale or denied graph published", raw, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			graph := raw.(KnowledgeGraph)
			for _, node := range graph.Nodes {
				if node.ID == lexical {
					t.Fatal("stale explicit node published", node)
				}
			}
			for _, edge := range graph.Edges {
				if edge.Target == lexical || edge.Source == lexical {
					t.Fatal("stale explicit edge published", edge)
				}
			}
			if !graph.Truncated || !strings.Contains(graph.Warning, "omitted") {
				t.Fatal(graph)
			}
		})
	}
}
