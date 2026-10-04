package workspace

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/internal/native"
)

// Fixed, synthetic rubric: approved formatting should improve the answer;
// an older memory must not replace current budget/launch facts. This is one
// task family, not a general model-quality or packaged-application claim.
func TestRealLocalMemoryPreferenceAndCurrentAuthority(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires supported local model and native helpers")
	}
	m, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), m)
	if err != nil {
		t.Fatal(err)
	}
	pack := "01990000-0000-7000-8000-000000000105"
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Current delivery plan")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Current authoritative plan: the budget is 12000 USD. The current launch month is December. These current values supersede earlier plans."}}}}}}, "page", page, "block", block))
	addMemory := func(kind, content string) (string, string) {
		source := createPage(t, k, e, "Previously reviewed context: "+content)
		memoryPage := createPage(t, k, e, "Approved "+kind)
		id := NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.propose", "memoryId": id, "pageId": memoryPage, "scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}, "memoryKind": kind, "content": content, "sources": []any{map[string]any{"pageId": source, "sourceRevision": "1"}}}}, "page", memoryPage, "memory", id))
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": id}}, "page", memoryPage, "memory", id))
		return id, memoryPage
	}
	preference, preferencePage := addMemory("preference", "The preferred heading for project updates is exactly: Status for review.")
	addMemory("decision", "Earlier plan: the launch month was July and the budget was 9000 USD. This is historical context.")
	budget := AIBudget{ContextTokens: 8192, OutputTokens: 512, CountTokens: func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
		texts := make([]string, len(prompts))
		for i, p := range prompts {
			texts[i] = native.ChatPrompt(p.System, p.User)
		}
		return s.CountTokens(ctx, pack, texts)
	}}
	req := AIRequest{PageID: page, Mode: "answer", ModelPackID: pack, Prompt: "Write a project update as exactly three lines. Heading: use my approved preferred heading if available, otherwise Update. Budget: state the current amount in USD and cite it. Launch: state the current launch month and cite it. Current source facts override historical memories."}
	for _, useMemory := range []bool{false, true} {
		t.Run("memory="+strconv.FormatBool(useMemory), func(t *testing.T) {
			req.UseMemory = useMemory
			start := time.Now()
			prepared, err := k.PrepareAIWithBudget(ctx, e, req, budget)
			if err != nil {
				t.Fatal(err)
			}
			completion, err := GeneratePreparedAI(ctx, prepared, func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
				text, err := s.GenerateTextWithGrammar(ctx, pack, native.ChatPrompt(system, prompt), tokens, grammar, stream)
				t.Logf("synthetic fixture generated claims: %s", text)
				return text, err
			}, nil)
			answer := completion.Text
			if err != nil {
				report, _ := json.Marshal(map[string]any{"modelPackId": pack, "useMemory": useMemory, "passed": false, "error": err.Error(), "promptTokens": prepared.PromptTokens, "latencyMs": time.Since(start).Milliseconds(), "releaseQualified": false})
				t.Fatal(string(report))
			}
			run, err := k.FinishAI(ctx, e, prepared.Run.ID, answer, "completed", "")
			if err != nil {
				t.Fatal(err)
			}
			claimLines := []string{}
			for _, line := range strings.Split(run.Text, "\n") {
				claim, _, _ := strings.Cut(line, " — “")
				claimLines = append(claimLines, claim)
			}
			claims := strings.Join(claimLines, "\n")
			current := strings.Contains(strings.ReplaceAll(claims, ",", ""), "12000") && strings.Contains(strings.ToLower(claims), "december")
			preferenceUsed := strings.Contains(strings.ToLower(run.Text), "status for review")
			support := true
			for _, line := range strings.Split(run.Text, "\n") {
				required := []string{}
				lower := strings.ToLower(strings.ReplaceAll(line, ",", ""))
				if useMemory && strings.Contains(lower, "status for review") {
					required = append(required, "Status for review")
				}
				if strings.Contains(lower, "12000") {
					required = append(required, "12000")
				}
				if strings.Contains(lower, "december") {
					required = append(required, "December")
				}
				for _, fact := range required {
					matched := false
					for _, label := range aiCitationPattern.FindAllStringSubmatch(line, -1) {
						for _, source := range prepared.Run.Sources {
							if strconv.Itoa(source.Number) == label[1] && strings.Contains(source.Text, fact) {
								matched = true
							}
						}
					}
					if !matched {
						support = false
					}
				}
			}

			passed := support && current && len(run.Citations) > 0 && run.State == "completed" && preferenceUsed == useMemory
			report, _ := json.Marshal(map[string]any{"modelPackId": pack, "useMemory": useMemory, "passed": passed, "currentFactsCorrect": current, "fixtureCitationSupportCorrect": support, "preferredHeadingUsed": preferenceUsed, "validCitations": len(run.Citations), "promptTokens": prepared.PromptTokens, "outputTokenCap": prepared.OutputTokens, "citationStrategy": "exact supporting quote", "latencyMs": time.Since(start).Milliseconds(), "answer": run.Text, "peakWorkerRSS": nil, "releaseQualified": false})
			t.Log(string(report))
			if !passed {
				t.Errorf("fixed memory/current-authority rubric failed: %s", report)
			}
		})
	}
	// Forget while tokens are streaming. Even valid-looking output cannot be
	// published after its approved memory has ceased to be current.
	t.Run("forget-during-stream", func(t *testing.T) {
		req.UseMemory = true
		prepared, err := k.PrepareAIWithBudget(ctx, e, req, budget)
		if err != nil {
			t.Fatal(err)
		}
		forgotten := false
		answer, err := s.GenerateText(ctx, pack, native.ChatPrompt(prepared.System, prepared.Prompt), 512, func(string) {
			if !forgotten {
				forgotten = true
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.forget", "memoryId": preference}}, "page", preferencePage, "memory", preference))
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		run, err := k.FinishAI(ctx, e, prepared.Run.ID, answer, "completed", "")
		if err != nil {
			t.Fatal(err)
		}
		if !forgotten || run.State != "failed" || run.Text != "" {
			t.Fatal("forgotten memory leaked from real streamed answer", run)
		}
		next, err := k.PrepareAIWithBudget(ctx, e, req, budget)
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range next.Run.Sources {
			if source.MemoryID == preference {
				t.Fatal("forgotten preference was retrieved")
			}
		}
	})
}
