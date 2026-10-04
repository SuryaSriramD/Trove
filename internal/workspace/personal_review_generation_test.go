package workspace

import (
	"archive/zip"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func generativeReviewFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) (PersonalReviewRun, string, string) {
	t.Helper()
	page, source, block := p2ReviewFixture(t, k, e, "catch_up")
	fact := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": fact, "pageId": source, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "The launch is blocked by the pending security review."}}}}}}, "page", source, "block", fact))
	p, err := readReviewPlan(context.Background(), k.db, block)
	if err != nil {
		t.Fatal(err)
	}
	p.Plan.Generative = &PersonalReviewGenerationConfig{ModelPackID: NewID(), Categories: []string{"summary", "risks"}, AllowedActions: []string{"append_review", "rename_page", "append_page"}, ActionPageIDs: []string{source}}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": string(marshal(p.Plan)), "language": "trove-review"}}, map[string]any{"kind": "review.activate", "pageId": page, "planBlockId": block, "enabled": true}}, "page", page, "block", block))
	p, err = readReviewPlan(context.Background(), k.db, block)
	if err != nil {
		t.Fatal(err)
	}
	r, err := k.preparePersonalReview(context.Background(), e, p, "manual:test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return r, source, block
}
func reviewTestGenerator(actions []PlannedAction) AIGenerator {
	return func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
		return string(marshal(map[string]any{"claims": []any{map[string]any{"category": "risks", "text": "The launch still awaits security review.", "quote": "The launch is blocked by the pending security review."}}, "actions": actions})), nil
	}
}
func reviewTestGenerate(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, r PersonalReviewRun, actions []PlannedAction) PersonalReviewRun {
	t.Helper()
	out, err := k.GeneratePersonalReview(context.Background(), e, r.ID, AIBudget{ContextTokens: 8192, OutputTokens: 1024}, reviewTestGenerator(actions))
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func reviewReq(k *Kernel, id string) map[string]any {
	return map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "runId": id}
}
func TestPersonalReviewGenerationRequiresReviewAndAppliesOnce(t *testing.T) {
	k, e := testKernel(t)
	r, source, _ := generativeReviewFixture(t, k, e)
	r = reviewTestGenerate(t, k, e, r, []PlannedAction{{Kind: "update_page", Target: "Current research evidence", Title: "Reviewed research"}})
	if r.Status != "ready" || !strings.Contains(r.Text, "[1]") && !strings.Contains(r.Text, "[2]") {
		t.Fatal(r)
	}
	var title string
	k.db.QueryRow("SELECT title FROM pages WHERE id=?", source).Scan(&title)
	if title != "Current research evidence" {
		t.Fatal("model wrote canonical content")
	}
	testCode(t, testDispatch(t, k, e, "review.apply", reviewReq(k, r.ID)), "APPROVAL_REQUIRED")
	proposal := testOK(t, testDispatch(t, k, e, "review.propose", reviewReq(k, r.ID)))
	if len(proposal["changes"].([]any)) != 2 {
		t.Fatal(proposal)
	}
	if testOK(t, testDispatch(t, k, e, "review.apply", reviewReq(k, r.ID)))["status"] != "accepted" {
		t.Fatal("missing atomic review receipt")
	}
	var firstCount int
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", r.PageID).Scan(&firstCount)
	testOK(t, testDispatch(t, k, e, "review.apply", reviewReq(k, r.ID)))
	k.db.QueryRow("SELECT title FROM pages WHERE id=?", source).Scan(&title)
	if title != "Reviewed research" {
		t.Fatal(title)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", r.PageID).Scan(&count)
	if count != firstCount || count <= 1 {
		t.Fatalf("approval append repeated: %d", count)
	}
}
func TestPersonalReviewGenerationRejectsUnlinkedProposalAndStaleSource(t *testing.T) {
	k, e := testKernel(t)
	r, source, _ := generativeReviewFixture(t, k, e)
	r = reviewTestGenerate(t, k, e, r, []PlannedAction{})
	other, err := prepareAIAction(context.Background(), k, e, map[string]any{"runId": r.ModelRunID, "kind": "workspace_patch"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyAIAction(context.Background(), k, e, map[string]any{"proposalId": other.(AIProposal).ID}); err == nil {
		t.Fatal("unlinked generic AI proposal bypassed review")
	}
	testOK(t, testDispatch(t, k, e, "review.propose", reviewReq(k, r.ID)))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": source, "title": "Changed"}}, "page", source))
	testCode(t, testDispatch(t, k, e, "review.apply", reviewReq(k, r.ID)), "CONFLICT")
}
func TestPersonalReviewGenerationCancellationFreshnessAndRetry(t *testing.T) {
	for _, mode := range []string{"cancel", "source", "activation", "quote", "destination"} {
		t.Run(mode, func(t *testing.T) {
			k, e := testKernel(t)
			r, source, block := generativeReviewFixture(t, k, e)
			gen := reviewTestGenerator([]PlannedAction{})
			wrap := func(ctx context.Context, system, prompt string, n int, g string, stream func(string)) (string, error) {
				switch mode {
				case "cancel":
					if _, err := k.CancelPersonalReviewGeneration(ctx, e, r.ID); err != nil {
						t.Fatal(err)
					}
				case "source":
					testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": source, "title": "New source"}}, "page", source))
				case "activation":
					testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "review.activate", "pageId": r.PageID, "planBlockId": block, "enabled": false}}, "page", r.PageID))
				case "quote":
					return `{"claims":[{"category":"summary","text":"Unsupported","quote":"Invented evidence"}],"actions":[]}`, nil
				case "destination":
					return reviewTestGenerator([]PlannedAction{{Kind: "update_page", Target: "Review inbox", Title: "Unauthorized"}})(ctx, system, prompt, n, g, stream)
				}
				return gen(ctx, system, prompt, n, g, stream)
			}
			_, err := k.GeneratePersonalReview(context.Background(), e, r.ID, AIBudget{ContextTokens: 8192, OutputTokens: 1024}, wrap)
			if err == nil {
				t.Fatal("unsafe review succeeded")
			}
			current, _ := readPersonalReviewRun(context.Background(), k.db, r.ID)
			if current.Status == "ready" || current.Status == "accepted" {
				t.Fatal(current)
			}
			if mode == "cancel" {
				testOK(t, testDispatch(t, k, e, "review.retry", reviewReq(k, r.ID)))
				current = reviewTestGenerate(t, k, e, current, []PlannedAction{})
				if current.Attempt != 2 {
					t.Fatal(current.Attempt)
				}
			}
		})
	}
}
func TestPersonalReviewGenerationRestartAndActivationBoundaries(t *testing.T) {
	k, e := testKernel(t)
	r, _, block := generativeReviewFixture(t, k, e)
	p, err := readReviewPlan(context.Background(), k.db, block)
	if err != nil {
		t.Fatal(err)
	}
	same, err := k.preparePersonalReview(context.Background(), e, p, r.Occurrence, time.Now())
	if err != nil || same.ID != r.ID {
		t.Fatal("occurrence dedup failed", err)
	}
	k.db.Exec("UPDATE personal_review_runs SET status='generating' WHERE id=?", r.ID)
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	restored, err := readPersonalReviewRun(context.Background(), k.db, r.ID)
	if err != nil || restored.Status != "cancelled" {
		t.Fatal(restored, err)
	}
	testOK(t, testDispatch(t, k, e, "review.retry", reviewReq(k, r.ID)))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "review.activate", "pageId": r.PageID, "planBlockId": block, "enabled": false}}, "page", r.PageID))
	p, _ = readReviewPlan(context.Background(), k.db, block)
	if _, err = k.preparePersonalReview(context.Background(), e, p, "manual:inactive", time.Now()); err == nil {
		t.Fatal("inactive imported plan generated")
	}
}
func TestPersonalReviewDraftCategoriesAndFields(t *testing.T) {
	k, e := testKernel(t)
	r, _, _ := generativeReviewFixture(t, k, e)
	raw := `{"claims":[{"category":"decisions","text":"The supplied evidence does not support this review category.","quote":""}],"actions":[]}`
	if _, _, err := renderPersonalReviewDraft(raw, r.Evidence, *r.Generative); err == nil {
		t.Fatal("unselected category accepted")
	}
	r.Generative.AllowedActions = []string{"set_task_due"}
	if err := k.validateReviewGeneratedActions(context.Background(), k.db, e, r, []PlannedAction{{Kind: "update_task", Target: "Current research evidence", Status: "Done"}}); err == nil {
		t.Fatal("unconfigured task status accepted")
	}
}

func TestPersonalReviewMemoryForgettingDuringGeneration(t *testing.T) {
	k, e := testKernel(t)
	r, _, block := generativeReviewFixture(t, k, e)
	evidence := createPage(t, k, e, "Reviewed preference source")
	memory, memoryPage := memoryFixture(t, k, e, evidence)
	p, _ := readReviewPlan(context.Background(), k.db, block)
	p.Plan.IncludeMemory = true
	p.Plan.SourcePageIDs = append(p.Plan.SourcePageIDs, evidence)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": string(marshal(p.Plan)), "language": "trove-review"}}, map[string]any{"kind": "review.activate", "pageId": r.PageID, "planBlockId": block, "enabled": true}}, "page", r.PageID, "block", block))
	p, _ = readReviewPlan(context.Background(), k.db, block)
	r, err := k.preparePersonalReview(context.Background(), e, p, "manual:memory", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Memories) != 1 {
		t.Fatal("missing approved memory", r.Memories)
	}
	_, err = k.GeneratePersonalReview(context.Background(), e, r.ID, AIBudget{ContextTokens: 8192, OutputTokens: 1024}, func(ctx context.Context, system, prompt string, n int, g string, stream func(string)) (string, error) {
		if !strings.Contains(prompt, "Use concise prose") {
			t.Fatal("approved memory omitted")
		}
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.forget", "memoryId": memory}}, "memory", memory, "page", memoryPage))
		return reviewTestGenerator([]PlannedAction{})(ctx, system, prompt, n, g, stream)
	})
	if err == nil {
		t.Fatal("forgotten memory survived generation")
	}
	testCode(t, testDispatch(t, k, e, "review.retry", reviewReq(k, r.ID)), "CONFLICT")
}
func TestPersonalReviewTaskFieldsUseSharedCanonicalBindings(t *testing.T) {
	k, e := testKernel(t)
	r, source, block := generativeReviewFixture(t, k, e)
	f := ensureTestTasks(t, k, e)
	record, taskPage := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.create", "recordId": record, "pageId": taskPage, "title": "Security review"}}, "record", record, "page", taskPage, "collection", f.collection, "property", f.status))
	p, _ := readReviewPlan(context.Background(), k.db, block)
	p.Plan.SourcePageIDs = []string{source, taskPage}
	p.Plan.Generative.AllowedActions = []string{"set_task_status", "set_task_due"}
	p.Plan.Generative.ActionPageIDs = []string{taskPage}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": string(marshal(p.Plan)), "language": "trove-review"}}, map[string]any{"kind": "review.activate", "pageId": r.PageID, "planBlockId": block, "enabled": true}}, "page", r.PageID, "block", block))
	p, _ = readReviewPlan(context.Background(), k.db, block)
	r, err := k.preparePersonalReview(context.Background(), e, p, "manual:tasks", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r = reviewTestGenerate(t, k, e, r, []PlannedAction{{Kind: "update_task", Target: "Security review", Status: "Done", Due: "2026-10-22"}})
	result := testOK(t, testDispatch(t, k, e, "review.propose", reviewReq(k, r.ID)))
	if len(result["changes"].([]any)) != 2 {
		t.Fatal(result)
	}
	var complete bool
	k.db.QueryRow("SELECT completed FROM task_state WHERE record_id=?", record).Scan(&complete)
	if complete {
		t.Fatal("unreviewed task mutation")
	}
	testOK(t, testDispatch(t, k, e, "review.apply", reviewReq(k, r.ID)))
	k.db.QueryRow("SELECT completed FROM task_state WHERE record_id=?", record).Scan(&complete)
	if !complete {
		t.Fatal("approved task status missing")
	}
	var due string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, f.date).Scan(&due)
	if !strings.Contains(due, "2026-10-22") {
		t.Fatal(due)
	}
}

func TestPersonalReviewPortableImportNeverActivatesModelOrReplaysRuns(t *testing.T) {
	k, e := testKernel(t)
	r, _, _ := generativeReviewFixture(t, k, e)
	r = reviewTestGenerate(t, k, e, r, []PlannedAction{})
	dest := filepath.Join(t.TempDir(), "personal-review.zip")
	if _, err := k.ExportPortable(context.Background(), e, dest); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := map[string]string{}
	for _, f := range archive.File {
		reader, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = string(raw)
	}
	restored, owner := testKernel(t)
	plan := importFixture(t, restored, owner, entries)
	completeImportFixture(t, restored, owner, plan)
	var source string
	if err = restored.db.QueryRow("SELECT json_extract(data_json,'$.source') FROM blocks WHERE type='code' AND json_extract(data_json,'$.language')='trove-review' LIMIT 1").Scan(&source); err != nil {
		t.Fatal("portable review config lost", err)
	}
	if !strings.Contains(source, "modelPackId") || !strings.Contains(source, "allowedActions") {
		t.Fatal("model configuration dropped", source)
	}
	for _, table := range []string{"personal_review_activation", "personal_review_runs", "personal_review_model_runs", "ai_proposals"} {
		var count int
		if err = restored.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("imported review activated or replayed", table, count, err)
		}
	}
	if err = restored.RunPersonalReviewTick(context.Background(), owner, time.Now().Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	next, err := restored.NextPersonalReviewGeneration(context.Background(), owner)
	if err != nil || next.ID != "" {
		t.Fatal("import started model analysis", next, err)
	}
}
func TestPersonalReviewRejectsStaleTokenizerPreparation(t *testing.T) {
	k, e := testKernel(t)
	r, source, _ := generativeReviewFixture(t, k, e)
	called := false
	_, err := k.GeneratePersonalReview(context.Background(), e, r.ID, AIBudget{ContextTokens: 8192, OutputTokens: 1024, CountTokens: func(ctx context.Context, p []AIPrompt) ([]int, error) {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": source, "title": "Changed during tokenization"}}, "page", source))
		counts := make([]int, len(p))
		for i := range counts {
			counts[i] = 1000
		}
		return counts, nil
	}}, func(context.Context, string, string, int, string, func(string)) (string, error) {
		called = true
		return "", nil
	})
	if err == nil || called {
		t.Fatal("stale prepared evidence reached model", err, called)
	}
}
