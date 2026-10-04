package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func TestAdvancedAIDatabaseDraftReviewAndIdempotency(t *testing.T) {
	for _, view := range []string{"table", "list", "gallery"} {
		t.Run(view, func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "database", Prompt: "Reading list", ModelPackID: NewID()})
			if err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"title":"Reading","properties":[{"name":"Pages","type":"number","config":{"format":"decimal"}}],"view":%q}`, view)
			run, err := k.FinishAI(ctx, e, prepared.Run.ID, body, "completed", "")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "create_database"})
			if err != nil {
				t.Fatal(err)
			}
			p := raw.(AIProposal)
			var count int
			k.db.QueryRow("SELECT count(*) FROM collections").Scan(&count)
			if count != 0 {
				t.Fatal("draft wrote canonical data")
			}
			if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": p.ID}); err != nil {
				t.Fatal(err)
			}
			if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": p.ID}); err != nil {
				t.Fatal(err)
			}
			k.db.QueryRow("SELECT count(*) FROM collections").Scan(&count)
			if count != 1 {
				t.Fatal("duplicate database", count)
			}
			var origin string
			k.db.QueryRow("SELECT origin FROM mutation_history ORDER BY rowid DESC LIMIT 1").Scan(&origin)
			if origin != "ai" {
				t.Fatal("AI lost origin", origin)
			}
		})
	}
}
func TestAdvancedAICodeLiteralAndStaleReview(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Code")
	block := NewID()
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": "return values[1]", "language": "python"}}}}, required("page", page, "1"), required("block", block, "0"))))
	prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "code_refactor", Prompt: "Refactor", PageID: page, BlockID: block, ModelPackID: NewID()})
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, "return values[2]", "completed", "")
	if err != nil || run.Text != "return values[2]" {
		t.Fatal("code rewritten as citation", run, err)
	}
	raw, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "replace_code"})
	if err != nil {
		t.Fatal(err)
	}
	p := raw.(AIProposal)
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Changed"}}, required("page", page, "2"))))
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": p.ID}); err == nil {
		t.Fatal("stale draft accepted")
	}
}
func TestAdvancedAIStructureValidation(t *testing.T) {
	for _, source := range []string{"flowchart LR\n A-->B", "sequenceDiagram\n A->>B: hello"} {
		if err := safeMermaid(source); err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []string{"%%{init:{}}%%\nflowchart LR", "flowchart LR\nclick A javascript:alert(1)", "<script>alert(1)</script>", "hello"} {
		if err := safeMermaid(source); err == nil {
			t.Fatal("unsafe diagram accepted")
		}
	}
	k, e := testKernel(t)
	ctx := context.Background()
	prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "database", Prompt: "Test", ModelPackID: NewID()})
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, `{"title":"Bad","properties":[{"name":"X","type":"number","config":{}}],"view":"table"}`, "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "create_database"}); err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatal("invalid structure accepted", err)
	}
}

func TestAdvancedAICodeSelectionPreservesUnselectedFunctions(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Code selection")
	block := NewID()
	selected := "def first():\n    return 1"
	original := selected + "\n\ndef second():\n    return 2\n"
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": original, "language": "python", "filename": "functions.py"}}}}, "page", page, "block", block))
	prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "code_refactor", Prompt: "Refactor only the selection", PageID: page, BlockID: block, Selection: selected, ModelPackID: NewID()})
	if err != nil {
		t.Fatal(err)
	}
	replacement := "def first():\n    return 42"
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, replacement, "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "replace_code"})
	if err != nil {
		t.Fatal(err)
	}
	proposal := raw.(AIProposal)
	want := replacement + "\n\ndef second():\n    return 2\n"
	if proposal.Before != original || proposal.After != want {
		t.Fatalf("review does not describe precise selection edit: %#v", proposal)
	}
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err != nil {
		t.Fatal(err)
	}
	data := readPage(t, k, e, page)["blocks"].([]any)[0].(map[string]any)["data"].(map[string]any)
	if data["source"] != want || data["filename"] != "functions.py" {
		t.Fatal("unselected code or block metadata was lost", data)
	}
}

func TestAdvancedAICodeRejectsAmbiguousSelectionsAndTruncatedWholeBlock(t *testing.T) {
	for _, kind := range []string{"ambiguous", "truncated", "missing_evidence"} {
		t.Run(kind, func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			page := createPage(t, k, e, "Preserve code")
			block := NewID()
			source := strings.Repeat("# keep this complete line\n", 90) + "return 7"
			selection := ""
			if kind == "ambiguous" {
				source = "return 1\nreturn 1"
				selection = "return 1"
			}
			if kind == "missing_evidence" {
				source = "def first():\n    return 1\n\ndef second():\n    return 2"
			}
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": source, "language": "python"}}}}, "page", page, "block", block))
			prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "code_refactor", Prompt: "Refactor", PageID: page, BlockID: block, Selection: selection, ModelPackID: NewID()})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "missing_evidence" {
				// Explicitly remove the recorded evidence. Canonical code is now
				// supplied verbatim, so whitespace no longer creates this condition.
				if _, err := k.db.Exec("UPDATE ai_runs SET sources_json='[]' WHERE id=?", prepared.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			run, err := k.FinishAI(ctx, e, prepared.Run.ID, "return 42", "completed", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "replace_code"}); err == nil {
				t.Fatal("unsafe full-block replacement received a preview")
			}
			data := readPage(t, k, e, page)["blocks"].([]any)[0].(map[string]any)["data"].(map[string]any)
			if data["source"] != source {
				t.Fatal("rejected draft changed source")
			}
		})
	}
}

func TestAdvancedAISuggestionsScopeFilteringPrecedesCandidateLimit(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	for offset := 0; offset < 1001; offset += 200 {
		commands := []any{}
		guards := []contracts.ServiceExpectedRevision{}
		for i := offset; i < offset+200 && i < 1001; i++ {
			id := fmt.Sprintf("01000000-0000-7000-8000-%012d", i)
			commands = append(commands, map[string]any{"kind": "page.create", "pageId": id, "title": "Unauthorized earlier page"})
			guards = append(guards, required("page", id, "0"))
		}
		testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, commands, guards...)))
	}
	first, second := createPage(t, k, e, "Scoped shared notebook"), createPage(t, k, e, "Scoped shared notebook")
	scope := contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(first), contracts.DomainId(second)}}}
	scoped := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", scope)
	raw, err := aiSuggestions(ctx, k, scoped, map[string]any{"pageId": first})
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(map[string]any)
	found := result["suggestions"].([]map[string]any)
	if result["partial"] != false || len(found) != 1 || found[0]["pageId"] != second {
		t.Fatalf("authorized candidates hidden by earlier denied pages: %s", marshal(result))
	}
	raw, err = aiSuggestions(ctx, k, e, map[string]any{"pageId": first})
	if err != nil {
		t.Fatal(err)
	}
	if raw.(map[string]any)["partial"] != true {
		t.Fatal("workspace candidate truncation was not reported")
	}
}
