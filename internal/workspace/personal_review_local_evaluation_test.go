package workspace

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/internal/native"
)

// Synthetic local-model check of the actual grammar/tokenizer/durable review
// pipeline. It is deliberately not evidence of broad factual quality.
func TestRealLocalPersonalReviewCitedProposal(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires verified local instruction model and native bundle")
	}
	manager, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), manager)
	if err != nil {
		t.Fatal(err)
	}
	k, e := testKernel(t)
	r, _, block := generativeReviewFixture(t, k, e)
	pack := "01990000-0000-7000-8000-000000000105"
	p, _ := readReviewPlan(context.Background(), k.db, block)
	p.Plan.Generative.ModelPackID = pack
	p.Plan.Generative.Categories = []string{"risks"}
	p.Plan.Generative.AllowedActions = []string{"append_review"}
	p.Plan.Generative.ActionPageIDs = []string{}
	p.Plan.Query = "State the launch blocker as one risk with its exact supporting quote. Propose no optional actions."
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": string(marshal(p.Plan)), "language": "trove-review"}}, map[string]any{"kind": "review.activate", "pageId": r.PageID, "planBlockId": block, "enabled": true}}, "page", r.PageID, "block", block))
	p, _ = readReviewPlan(context.Background(), k.db, block)
	r, err = k.preparePersonalReview(context.Background(), e, p, "manual:real-model", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	size, err := supervisor.GenerationContext(pack)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	started := time.Now()
	r, err = k.GeneratePersonalReview(ctx, e, r.ID, AIBudget{ContextTokens: size, OutputTokens: 512, CountTokens: func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
		texts := make([]string, len(prompts))
		for i, p := range prompts {
			texts[i] = native.ChatPrompt(p.System, p.User)
		}
		return supervisor.CountTokens(ctx, pack, texts)
	}}, func(ctx context.Context, system, prompt string, n int, grammar string, stream func(string)) (string, error) {
		raw, err := supervisor.GenerateTextWithGrammar(ctx, pack, native.ChatPrompt(system, prompt), n, grammar, stream)
		t.Logf("synthetic generated review: %s", raw)
		return raw, err
	})
	if err != nil {
		t.Fatalf("model=%s passed=false releaseQualified=false latency=%s error=%v", pack, time.Since(started), err)
	}
	claim, _, _ := strings.Cut(r.Text, " — “")
	if !strings.Contains(strings.ToLower(claim), "security") || !strings.Contains(strings.ToLower(claim), "launch") || !strings.Contains(r.Text, "[2]") {
		t.Fatalf("factual rubric failed: %s", r.Text)
	}
	var before int
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", r.PageID).Scan(&before)
	if before != 1 {
		t.Fatal("model committed unreviewed content")
	}
	testOK(t, testDispatch(t, k, e, "review.propose", reviewReq(k, r.ID)))
	testOK(t, testDispatch(t, k, e, "review.apply", reviewReq(k, r.ID)))
	t.Logf("model=%s passed=true releaseQualified=false latency=%s exactQuote=true explicitApproval=true", pack, time.Since(started))
}
