package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/internal/native"
)

type expandedModeCase struct {
	ID                           string           `json:"id"`
	Mode                         string           `json:"mode"`
	Source                       string           `json:"source"`
	Prompt                       string           `json:"prompt"`
	Oracle                       string           `json:"oracle"`
	Required                     []string         `json:"required"`
	Forbidden                    []string         `json:"forbidden"`
	AllowedNumbers               []string         `json:"allowedNumbers"`
	ExpectedTitle                string           `json:"expectedTitle"`
	Properties                   []map[string]any `json:"properties"`
	Edges                        [][]string       `json:"edges"`
	ExpectedCode                 string           `json:"expectedCode"`
	RequireComment               bool             `json:"requireComment"`
	Golden                       string           `json:"golden"`
	Proposal                     string           `json:"proposal"`
	ManualSemanticReviewRequired bool             `json:"manualSemanticReviewRequired"`
}

func expandedModes(t *testing.T) []expandedModeCase {
	t.Helper()
	b, err := os.ReadFile("testdata/expanded-evaluation/heldout-modes.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Cases []expandedModeCase `json:"cases"`
	}
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) != 42 {
		t.Fatal("expected42predeclaredmodecases")
	}
	return v.Cases
}
func expandedModeOracle(tc expandedModeCase, out string) error {
	claims := []string{}
	for _, line := range strings.Split(out, "\n") {
		claim, _, _ := strings.Cut(line, " — “")
		claims = append(claims, claim)
	}
	factsText := regexp.MustCompile(`\[\d+\]`).ReplaceAllString(strings.Join(claims, "\n"), "")
	numbers := map[string]bool{}
	if tc.Oracle == "writing" {
		parsed, err := expandedWritingNumbers(factsText)
		if err != nil {
			return err
		}
		for _, n := range parsed {
			numbers[n] = true
		}
	}
	has := func(fact string) bool {
		if tc.Oracle == "writing" {
			if n, ok := expandedCanonicalNumber(fact); ok {
				return numbers[n]
			}
		}
		return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(fact) + `\b`).MatchString(factsText)
	}
	switch tc.Oracle {
	case "writing", "code_explain", "organize":
		for _, f := range tc.Required {
			if !has(f) {
				return fmt.Errorf("required claimed fact missing: %q", f)
			}
		}
		for _, f := range tc.Forbidden {
			if has(f) {
				return fmt.Errorf("forbidden fact: %q", f)
			}
		}
		if tc.Oracle == "writing" {
			allowed := map[string]bool{}
			for _, n := range tc.AllowedNumbers {
				canonical, ok := expandedCanonicalNumber(n)
				if !ok {
					return fmt.Errorf("invalid numeric expectation: %q", n)
				}
				allowed[canonical] = true
			}
			for n := range numbers {
				if !allowed[n] {
					return fmt.Errorf("unprovided numeric claim: %s", n)
				}
			}
			switch tc.Mode {
			case "shorten":
				if len(out) >= len(tc.Source) {
					return fmt.Errorf("not shortened")
				}
			case "expand":
				if len(out) <= len(tc.Source) || !strings.Contains(out, "\n\n") {
					return fmt.Errorf("not expanded into paragraphs")
				}
			case "outline":
				if !strings.Contains(out, "#") || !(strings.Contains(out, "- ") || strings.Contains(out, "* ")) {
					return fmt.Errorf("outline heading/bullets missing")
				}
			case "generate":
				if !strings.Contains(out, "#") {
					return fmt.Errorf("page heading missing")
				}
			}
		}
		if tc.Oracle == "organize" {
			if !regexp.MustCompile(`(?i)\b(because|helps|allows|reason|so that)\b`).MatchString(out) {
				return fmt.Errorf("organization rationale missing")
			}
			if regexp.MustCompile(`(?i)\b(I have moved|I created|I deleted|I updated)\b`).MatchString(out) {
				return fmt.Errorf("unapplied suggestion claims an effect")
			}
		}
	case "database":
		var d struct {
			Title      string           `json:"title"`
			Properties []map[string]any `json:"properties"`
			View       string           `json:"view"`
		}
		if err := json.Unmarshal([]byte(out), &d); err != nil {
			return err
		}
		if d.Title != tc.ExpectedTitle || d.View != "table" || len(d.Properties) != len(tc.Properties) {
			return fmt.Errorf("schema title/view/arity mismatch")
		}
		expected := map[string]map[string]any{}
		for _, p := range tc.Properties {
			expected[p["name"].(string)] = p
		}
		for _, p := range d.Properties {
			name, _ := p["name"].(string)
			want, ok := expected[name]
			if !ok || p["type"] != want["type"] || !reflect.DeepEqual(p["config"], want["config"]) {
				return fmt.Errorf("typed property mismatch: %s", name)
			}
			delete(expected, name)
		}
		if len(expected) != 0 {
			return fmt.Errorf("missing required property")
		}
	case "mermaid":
		normalized := strings.TrimSpace(strings.ReplaceAll(out, ";", "\n"))
		lines := strings.Split(normalized, "\n")
		if len(lines) != len(tc.Edges)+1 || strings.Join(strings.Fields(lines[0]), " ") != "flowchart LR" {
			return fmt.Errorf("diagram header/edge count mismatch")
		}
		got := []string{}
		for _, line := range lines[1:] {
			got = append(got, strings.Join(strings.Fields(line), ""))
		}
		want := []string{}
		for _, edge := range tc.Edges {
			want = append(want, edge[0]+"-->"+edge[1])
		}
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("diagram edges changed")
		}
	case "code":
		canonical := func(s string) (*ast.File, error) {
			// Positions describe source layout, not syntax. Skip parser-only
			// object links so identifier bindings cannot retain position-bearing
			// pointers outside the syntax tree. Preserve whether a token exists:
			// CallExpr.Ellipsis and TypeSpec.Assign carry syntax through Pos.IsValid.
			// Keep every non-position AST field.
			tree, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package fixture\n"+s, parser.SkipObjectResolution)
			if err != nil {
				return nil, err
			}
			position := reflect.TypeOf(token.NoPos)
			ast.Inspect(tree, func(node ast.Node) bool {
				if node != nil {
					fields := reflect.ValueOf(node).Elem()
					for i := 0; i < fields.NumField(); i++ {
						if field := fields.Field(i); field.Type() == position {
							if field.Int() != 0 {
								field.SetInt(1)
							}
						}
					}
				}
				return true
			})
			return tree, nil
		}
		got, err := canonical(out)
		if err != nil {
			return err
		}
		want, err := canonical(tc.ExpectedCode)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("function syntax/signature/logic changed")
		}
		if tc.RequireComment {
			tree, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package fixture\n"+out, parser.ParseComments)
			if err != nil || len(tree.Comments) == 0 {
				return fmt.Errorf("requested code comment absent")
			}
		}
	default:
		return fmt.Errorf("unknown independent oracle %q", tc.Oracle)
	}
	return nil
}
func TestExpandedModeOracles(t *testing.T) {
	counts := map[string]int{}
	for _, tc := range expandedModes(t) {
		t.Run(tc.ID, func(t *testing.T) {
			counts[tc.Mode]++
			if err := expandedModeOracle(tc, tc.Golden); err != nil {
				t.Fatal("golden", err)
			}
			if expandedModeOracle(tc, "This answer has no requested result.") == nil {
				t.Fatal("oracle accepts missing outcome")
			}
			switch tc.Oracle {
			case "writing":
				bad := strings.ReplaceAll(tc.Golden, tc.Required[0], "99999")
				if expandedModeOracle(tc, bad) == nil {
					t.Fatal("oracle accepts changed amount")
				}
			case "code":
				if expandedModeOracle(tc, tc.Golden+"\nfunc Unrequested() {}") == nil {
					t.Fatal("oracle accepts extra executable code")
				}
			case "database":
				if expandedModeOracle(tc, strings.ReplaceAll(tc.Golden, "decimal", "percent")) == nil {
					t.Fatal("oracle accepts changed property semantics")
				}
			case "mermaid":
				if expandedModeOracle(tc, tc.Golden+"\nUnauthorized --> Save") == nil {
					t.Fatal("oracle accepts extra edge")
				}
			}
		})
	}
	if len(counts) != 14 {
		t.Fatal("missing exposed mode")
	}
	for mode, n := range counts {
		if n != 3 {
			t.Fatalf("%s has%dheldoutcases", mode, n)
		}
	}
}
func TestRealLocalExpandedModes(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("explicit verified native bundle/model required")
	}
	pack := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if pack == "" {
		t.Fatal("select evaluation model explicitly")
	}
	manager, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), manager)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range expandedModes(t) {
		t.Run(tc.ID, func(t *testing.T) {
			k, e := testKernel(t)
			page := createPage(t, k, e, "Held-out source")
			block := ""
			if tc.Source != "" {
				block = NewID()
				kind := "paragraph"
				data := map[string]any{"richText": []any{map[string]any{"text": tc.Source}}}
				if strings.HasPrefix(tc.Mode, "code_") {
					kind = "code"
					data = map[string]any{"source": tc.Source, "language": "go"}
				}
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": kind, "schemaVersion": 1, "data": data}}}, "page", page, "block", block))
			}
			canonicalAudit := beginAICanonicalObservation(t, k)
			var before int
			if err = k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&before); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			started := time.Now()
			budget := AIBudget{ContextTokens: 8192, OutputTokens: 512, CountTokens: func(ctx context.Context, p []AIPrompt) ([]int, error) {
				texts := make([]string, len(p))
				for i, v := range p {
					texts[i] = native.ChatPrompt(v.System, v.User)
				}
				return supervisor.CountTokens(ctx, pack, texts)
			}}
			p, prepareErr := k.PrepareAIWithBudget(ctx, e, AIRequest{PageID: page, BlockID: block, Mode: tc.Mode, Prompt: tc.Prompt, ModelPackID: pack}, budget)
			raw := ""
			metrics := &native.RunMetrics{}
			var generationErr, finishErr, oracleErr, proposalErr error
			var run AIRun
			if prepareErr == nil {
				completion, err := GeneratePreparedAI(ctx, p, func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
					out, err := supervisor.GenerateTextWithGrammarMetrics(ctx, pack, native.ChatPrompt(system, prompt), tokens, grammar, metrics, stream)
					raw = out
					return out, err
				}, nil)
				generationErr = err
				state, failure := "completed", ""
				if err != nil {
					state, failure = "failed", err.Error()
				}
				run, finishErr = k.FinishAI(context.Background(), e, p.Run.ID, completion.Text, state, failure)
				oracleErr = expandedModeOracle(tc, run.Text)
				if generationErr == nil && finishErr == nil && tc.Proposal != "" {
					_, proposalErr = prepareAIAction(context.Background(), k, e, map[string]any{"runId": run.ID, "kind": tc.Proposal})
				}
			}
			var after int
			if err = k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&after); err != nil {
				t.Fatal(err)
			}
			latencyMS := time.Since(started).Milliseconds() // exclude canonical evidence capture/fsync
			canonicalInventory := canonicalAudit.finish(t, k)
			passed := prepareErr == nil && generationErr == nil && finishErr == nil && oracleErr == nil && proposalErr == nil && run.State == "completed" && before == after && canonicalInventory.Unchanged
			var rss any
			if n := metrics.PeakRSSBytes.Load(); n > 0 {
				rss = n
			}
			report, _ := json.Marshal(map[string]any{"fixture": tc.ID, "mode": tc.Mode, "modelPackId": pack, "automatedTaskPassed": passed, "manualSemanticReviewRequired": tc.ManualSemanticReviewRequired, "canonicalUnchanged": before == after, "mutationHistoryCountUnchanged": before == after, "canonicalInventory": canonicalInventory, "latencyMs": latencyMS, "promptTokens": p.PromptTokens, "outputCap": p.OutputTokens, "peakWorkerRSSBytes": rss, "prepareError": fmt.Sprint(prepareErr), "generationError": fmt.Sprint(generationErr), "finishError": fmt.Sprint(finishErr), "rubricError": fmt.Sprint(oracleErr), "proposalError": fmt.Sprint(proposalErr), "rawDraft": raw, "displayed": run.Text, "releaseQualified": false})
			t.Log(string(report))
			if !passed {
				t.Error("independent mode outcome or no-write boundary failed")
			}
		})
	}
}
