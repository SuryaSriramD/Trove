package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/build/constraint"
	"go/format"
	"go/scanner"
	"go/token"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

const independentModeControlsSHA256 = "9d23ba556d0cec8a7b54ab91fdf9039cc55d8931791d3023158ee6d23f15eed1"

type independentModeControl struct {
	expandedModeCase
	Title            string `json:"title"`
	Selection        string `json:"selection"`
	UseMemory        bool   `json:"useMemory"`
	SuggestionPolicy string `json:"suggestionPolicy"`
	Counterexamples  []struct {
		Kind       string `json:"kind"`
		Output     string `json:"output"`
		MustReject bool   `json:"mustReject"`
		Reason     string `json:"reason"`
	} `json:"counterexamples"`
	UnselectedBoundary *struct {
		Prefix string `json:"prefix"`
		Suffix string `json:"suffix"`
	} `json:"unselectedBoundary,omitempty"`
}

func independentModeControls(t *testing.T) ([]independentModeControl, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/expanded-evaluation/independent-mode-controls-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	if hash != independentModeControlsSHA256 {
		t.Fatal("independent mode controls bytes changed")
	}
	var corpus struct {
		SchemaVersion int                      `json:"schemaVersion"`
		Family        string                   `json:"family"`
		Cases         []independentModeControl `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil || corpus.SchemaVersion != 1 || corpus.Family != "independent_mode_controls_v1" || len(corpus.Cases) != 16 {
		t.Fatal("invalid locked mode controls", err)
	}
	return corpus.Cases, hash
}

// Reject duplicate object keys before typed JSON decoding; a repeated title or
// property member must not silently overwrite the reviewed expectation.
func modeControlUniqueJSON(raw string) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	var value func() error
	value = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for dec.More() {
					key, err := dec.Token()
					if err != nil {
						return err
					}
					name, ok := key.(string)
					if !ok || seen[name] {
						return fmt.Errorf("duplicate or invalid JSON key")
					}
					seen[name] = true
					if err := value(); err != nil {
						return err
					}
				}
				end, err := dec.Token()
				if err != nil || end != json.Delim('}') {
					return fmt.Errorf("unclosed JSON object")
				}
			case '[':
				for dec.More() {
					if err := value(); err != nil {
						return err
					}
				}
				end, err := dec.Token()
				if err != nil || end != json.Delim(']') {
					return fmt.Errorf("unclosed JSON array")
				}
			default:
				return fmt.Errorf("unexpected JSON delimiter")
			}
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("additional JSON value")
	}
	return nil
}

func modeControlCodeTokens(source string) ([]string, error) {
	var scan scanner.Scanner
	file := token.NewFileSet().AddFile("control.go", -1, len(source))
	errors := []string{}
	scan.Init(file, []byte(source), func(_ token.Position, message string) { errors = append(errors, message) }, scanner.ScanComments)
	result := []string{}
	for {
		_, tok, literal := scan.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT {
			if strings.HasPrefix(literal, "//go:") || strings.HasPrefix(literal, "//line ") || strings.HasPrefix(literal, "/*line ") || constraint.IsGoBuild(literal) || constraint.IsPlusBuild(literal) {
				return nil, fmt.Errorf("compiler-affecting comment is not explanatory commentary")
			}
			continue
		}
		// Go inserts semicolons at layout boundaries. The semantic AST
		// already guards statement/control-flow and explicit empty statements.
		if tok == token.SEMICOLON {
			continue
		}
		result = append(result, tok.String()+":"+literal)
	}
	if len(errors) > 0 {
		return nil, fmt.Errorf("Go tokenization failed: %v", errors)
	}
	return result, nil
}

// This is an automated structural oracle, not a prose/comment meaning judge.
// Mermaid semantics are checked separately with the pinned actual parser.
func modeControlStructuralOracle(c independentModeControl, out string) error {
	switch c.Mode {
	case "database":
		if err := modeControlUniqueJSON(out); err != nil {
			return err
		}
		var draft struct {
			Title      string           `json:"title"`
			Properties []map[string]any `json:"properties"`
			View       string           `json:"view"`
		}
		decoder := json.NewDecoder(strings.NewReader(out))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&draft); err != nil {
			return err
		}
		if draft.Title != c.ExpectedTitle || draft.View != "table" || len(draft.Properties) != len(c.Properties) {
			return fmt.Errorf("exact database title/view/property count differs")
		}
		expected := map[string]map[string]any{}
		for _, property := range c.Properties {
			expected[property["name"].(string)] = property
		}
		for _, property := range draft.Properties {
			name, _ := property["name"].(string)
			want, ok := expected[name]
			if !ok || len(property) != 3 || !reflect.DeepEqual(property, want) {
				return fmt.Errorf("exact property name/type/config differs")
			}
			delete(expected, name)
		}
		if len(expected) != 0 {
			return fmt.Errorf("missing requested property")
		}
	case "code_comment":
		if err := expandedModeOracle(expandedModeCase{Oracle: "code", ExpectedCode: c.ExpectedCode, RequireComment: true}, out); err != nil {
			return err
		}
		want, err := modeControlCodeTokens(c.ExpectedCode)
		if err != nil {
			return err
		}
		got, err := modeControlCodeTokens(out)
		if err != nil || !reflect.DeepEqual(got, want) {
			return fmt.Errorf("non-comment Go token/literal spelling changed: %v", err)
		}
	case "mermaid":
		if strings.TrimSpace(out) == "" || len(out) > 32000 || strings.Contains(out, "```") {
			return fmt.Errorf("invalid diagram source envelope")
		}
	case "expand":
		paragraphs := regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(out), -1)
		if len(paragraphs) != 2 || len(out) <= len(c.Source) || strings.Contains(out, "#") || regexp.MustCompile(`[0-9]`).MatchString(out) {
			return fmt.Errorf("expansion structure or unprovided numeric claim differs")
		}
		labels := regexp.MustCompile(`(?i)\b(?:suggestion|suggested next step)\s*:`).FindAllStringIndex(out, -1)
		if c.SuggestionPolicy == "required-labeled" && len(labels) != 1 {
			return fmt.Errorf("exactly one labeled optional suggestion required")
		}
		if c.SuggestionPolicy == "forbidden" && len(labels) != 0 {
			return fmt.Errorf("suggestion explicitly forbidden")
		}
	default:
		return fmt.Errorf("unknown independent mode")
	}
	return nil
}

func modeControlSeed(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, c independentModeControl) (string, string) {
	t.Helper()
	page := createPage(t, k, e, c.Title)
	if c.Source == "" {
		return page, ""
	}
	block, typ := NewID(), "paragraph"
	data := map[string]any{"richText": []any{map[string]any{"text": c.Source}}}
	if c.Mode == "code_comment" {
		typ, data = "code", map[string]any{"source": c.Source, "language": "go"}
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": typ, "schemaVersion": 1, "data": data}}}, "page", page, "block", block))
	return page, block
}

func modeControlSourceScope(c independentModeControl, page, block string, sources []AIEvidence) error {
	blocks, numbers := 0, map[int]bool{}
	for _, source := range sources {
		if source.MemoryID != "" || source.Connected != nil || string(source.Location.PageId) != page || source.Title != c.Title || source.Location.SourceRevision == "" || source.Number <= 0 || numbers[source.Number] {
			return fmt.Errorf("unselected or duplicated source identity")
		}
		numbers[source.Number] = true
		if source.Location.BlockId == nil {
			if source.Text != c.Title {
				return fmt.Errorf("title evidence includes unrelated content")
			}
		} else {
			blocks++
			if block == "" || string(*source.Location.BlockId) != block || source.Text != c.Source {
				return fmt.Errorf("selected block evidence differs")
			}
		}
	}
	if (c.Source != "" && blocks != 1) || (c.Source == "" && blocks != 0) {
		return fmt.Errorf("missing or extra selected source")
	}
	return nil
}

func modeControlSelection(c independentModeControl, prepared AIPrepared) error {
	if prepared.Run.Request.Selection != c.Selection {
		return fmt.Errorf("request selection differs")
	}
	if c.Selection == "" {
		return nil
	}
	const marker = "\nSELECTED SOURCE (JSON string; replace only this source)\n"
	_, selected, found := strings.Cut(prepared.Prompt, marker)
	encoded := string(marshal(c.Selection))
	if !found || strings.Count(prepared.Prompt, marker) != 1 || !strings.HasPrefix(selected, encoded) {
		return fmt.Errorf("exact JSON-encoded selected source is missing")
	}
	return nil
}

func modeControlProposal(c independentModeControl, block string, proposal AIProposal, generated string) error {
	if proposal.State != "draft" || proposal.Kind != c.Proposal || proposal.ID == "" || len(proposal.Commands) == 0 {
		return fmt.Errorf("missing unapplied reviewed proposal")
	}
	if c.Mode != "code_comment" {
		return nil
	}
	want := generated
	if c.Selection != "" {
		if c.UnselectedBoundary == nil || c.Source != c.UnselectedBoundary.Prefix+c.Selection+c.UnselectedBoundary.Suffix {
			return fmt.Errorf("invalid locked selection boundary")
		}
		want = c.UnselectedBoundary.Prefix + generated + c.UnselectedBoundary.Suffix
	}
	if proposal.Before != c.Source || proposal.After != want || len(proposal.Commands) != 1 {
		return fmt.Errorf("replacement changed unselected source or duplicated commands")
	}
	var command map[string]any
	if err := json.Unmarshal(marshal(proposal.Commands[0]), &command); err != nil {
		return err
	}
	data, ok := command["data"].(map[string]any)
	if !ok || command["kind"] != "block.update" || command["blockId"] != block || command["type"] != "code" || data["source"] != want || data["language"] != "go" {
		return fmt.Errorf("replacement command differs from exact reviewed code")
	}
	return nil
}

func TestIndependentModeControlsGoldenAndPoisons(t *testing.T) {
	cases, _ := independentModeControls(t)
	counts, seen, poisons := map[string]int{}, map[string]bool{}, 0
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			if seen[c.ID] || c.ID == "" || c.Prompt == "" || c.UseMemory || !c.ManualSemanticReviewRequired || len(c.Counterexamples) != 4 && c.Mode != "expand" {
				t.Fatal("invalid independent control inventory")
			}
			seen[c.ID] = true
			counts[c.Mode]++
			if err := modeControlStructuralOracle(c, c.Golden); err != nil {
				t.Fatal("golden structural failure", err)
			}
			if c.Mode == "code_comment" {
				formatted, err := format.Source([]byte("package fixture\n" + c.Golden))
				if err != nil {
					t.Fatal("golden Go formatting failed", err)
				}
				layout := strings.TrimPrefix(string(formatted), "package fixture\n")
				if err := modeControlStructuralOracle(c, layout); err != nil {
					t.Fatal("equivalent Go layout rejected", err)
				}
			}
			for _, poison := range c.Counterexamples {
				if !poison.MustReject || poison.Output == c.Golden || poison.Kind == "" || poison.Reason == "" {
					t.Fatal("incomplete locked poison")
				}
				poisons++
				manual := c.Mode == "expand" || c.Mode == "code_comment" && poison.Kind == "inaccurate_comment"
				parser := c.Mode == "mermaid"
				if !manual && !parser && modeControlStructuralOracle(c, poison.Output) == nil {
					t.Fatal("structural oracle accepted locked poison", poison.Kind)
				}
				// Semantic poison acceptance is never inferred from an AST or
				// formatting check. The runner retains these required reviews.
				t.Logf("poison=%s manualRequired=%t actualMermaidParserRequired=%t", poison.Kind, manual, parser)
			}
			if c.Mode == "database" {
				for _, poison := range []string{c.Golden + "{}", strings.Replace(c.Golden, `"title":`, `"title":"wrong","title":`, 1)} {
					if modeControlStructuralOracle(c, poison) == nil {
						t.Fatal("database oracle accepted duplicate/trailing JSON")
					}
				}
			}
		})
	}
	if len(seen) != 16 || poisons != 68 || !reflect.DeepEqual(counts, map[string]int{"database": 4, "mermaid": 4, "code_comment": 4, "expand": 4}) {
		t.Fatal("control/poison mode balance differs", counts, poisons)
	}
}

func TestIndependentModeControlsScopeAndSelection(t *testing.T) {
	cases, _ := independentModeControls(t)
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			k, e := testKernel(t)
			page, block := modeControlSeed(t, k, e, c)
			foreign := createPage(t, k, e, "Unselected scope control")
			foreignBlock := NewID()
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": foreignBlock, "pageId": foreign, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "This page is outside the explicitly selected control."}}}}}}, "page", foreign, "block", foreignBlock))
			audit := beginAICanonicalObservation(t, k)
			request := AIRequest{PageID: page, BlockID: block, Mode: c.Mode, Prompt: c.Prompt, Selection: c.Selection, ModelPackID: NewID()}
			prepared, err := k.PrepareAIWithBudget(context.Background(), e, request, wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			if err := modeControlSourceScope(c, page, block, prepared.Run.Sources); err != nil {
				t.Fatal(err)
			}
			poison := append(append([]AIEvidence(nil), prepared.Run.Sources...), AIEvidence{Number: 77, Title: "Foreign", Text: "Unselected", Location: contracts.DomainLocation{PageId: contracts.DomainId(NewID())}})
			if modeControlSourceScope(c, page, block, poison) == nil {
				t.Fatal("scope accepted a foreign page")
			}
			if err := modeControlSelection(c, prepared); err != nil {
				t.Fatal("selected code request was not retained", err)
			}
			if c.Selection != "" {
				for _, bad := range []string{
					strings.Replace(prepared.Prompt, string(marshal(c.Selection)), c.Selection, 1),
					strings.Replace(prepared.Prompt, string(marshal(c.Selection)), string(marshal(c.Selection+"\nfunc Extra() {}")), 1),
					strings.Replace(prepared.Prompt, "SELECTED SOURCE", "OTHER SOURCE", 1),
				} {
					poison := prepared
					poison.Prompt = bad
					if modeControlSelection(c, poison) == nil {
						t.Fatal("selection check accepted changed encoding/content/marker")
					}
				}
			}
			if _, err := k.FinishAI(context.Background(), e, prepared.Run.ID, "", "cancelled", ""); err != nil {
				t.Fatal(err)
			}
			if !audit.finish(t, k).Unchanged {
				t.Fatal("preparation/cancellation changed canonical content")
			}
		})
	}
}

func TestRealLocalIndependentModeControls(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("explicit native integration required")
	}
	pack := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if pack != "01990000-0000-7000-8000-000000000106" {
		t.Fatal("locked pack106 required")
	}
	runRealLocalIndependentModeControls(t, pack)
}

func runRealLocalIndependentModeControls(t *testing.T, pack string) {
	cases, hash := independentModeControls(t)
	manager, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), manager)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			k, e := testKernel(t)
			page, block := modeControlSeed(t, k, e, c)
			audit := beginAICanonicalObservation(t, k)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			request := AIRequest{PageID: page, BlockID: block, Mode: c.Mode, Prompt: c.Prompt, Selection: c.Selection, UseMemory: false, ModelPackID: pack}
			budget := AIBudget{ContextTokens: 8192, OutputTokens: 512, CountTokens: func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
				texts := make([]string, len(prompts))
				for i, prompt := range prompts {
					texts[i] = native.ChatPrompt(prompt.System, prompt.User)
				}
				return supervisor.CountTokens(ctx, pack, texts)
			}}
			started := time.Now()
			prepared, prepareErr := k.PrepareAIWithBudget(ctx, e, request, budget)
			raw, system, prompt, grammar, decoded := "", "", "", "", ""
			attempts, outputTokens := 0, 0
			nativeCompleted := false
			var nativeError error
			metrics := &native.RunMetrics{}
			var sourceErr, generationErr, finishErr, oracleErr, proposalErr error
			var run AIRun
			var proposal *AIProposal
			if prepareErr == nil {
				sourceErr = modeControlSourceScope(c, page, block, prepared.Run.Sources)
				if sourceErr == nil {
					sourceErr = modeControlSelection(c, prepared)
				}
				if sourceErr == nil {
					completion, err := GeneratePreparedAI(ctx, prepared, func(ctx context.Context, s, u string, tokens int, g string, stream func(string)) (string, error) {
						attempts++
						if attempts != 1 {
							return "", fmt.Errorf("only one generation allowed")
						}
						system, prompt, grammar, outputTokens = s, u, g, tokens
						out, err := supervisor.GenerateTextWithGrammarMetrics(ctx, pack, native.ChatPrompt(s, u), tokens, g, metrics, stream)
						raw, nativeCompleted, nativeError = out, true, err
						return out, err
					}, nil)
					decoded, generationErr = completion.Text, err
					state, failure := "completed", ""
					if err != nil {
						state, failure = "failed", err.Error()
					}
					run, finishErr = k.FinishAI(context.Background(), e, prepared.Run.ID, completion.Text, state, failure)
					oracleErr = modeControlStructuralOracle(c, run.Text)
					if generationErr == nil && finishErr == nil && c.Proposal != "" {
						value, err := prepareAIAction(context.Background(), k, e, map[string]any{"runId": run.ID, "kind": c.Proposal})
						proposalErr = err
						if err == nil {
							p, ok := value.(AIProposal)
							if !ok {
								proposalErr = fmt.Errorf("unexpected proposal response")
							} else {
								proposal = &p
								proposalErr = modeControlProposal(c, block, p, run.Text)
							}
						}
					}
				}
			}
			latency := time.Since(started).Milliseconds()
			canonical := audit.finish(t, k)
			executed := prepareErr == nil && sourceErr == nil && generationErr == nil && finishErr == nil && attempts == 1 && run.State == "completed" && canonical.Unchanged
			automated := executed && oracleErr == nil && proposalErr == nil
			report := map[string]any{"fixture": c.ID, "mode": c.Mode, "useMemory": false, "corpusSHA256": hash, "modelPackId": pack, "request": request, "sourceTitle": c.Title, "source": c.Source, "selection": c.Selection, "prompt": c.Prompt, "contextTokens": 8192, "promptTokens": prepared.PromptTokens, "outputCap": prepared.OutputTokens, "generationAttempts": attempts, "nativeCallbackCompleted": nativeCompleted, "nativeCallbackError": fmt.Sprint(nativeError), "nativeCompletionContract": "GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos", "peakWorkerRSSBytes": metrics.PeakRSSBytes.Load(), "generatorSystem": system, "generatorPrompt": prompt, "generatorOutputCap": outputTokens, "generatorGrammar": grammar, "generatorGrammarSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte(grammar))), "preparedSystem": prepared.System, "preparedPrompt": prepared.Prompt, "sources": prepared.Run.Sources, "rawDraft": raw, "decodedBody": decoded, "displayed": run.Text, "citations": run.Citations, "proposal": proposal, "prepareError": fmt.Sprint(prepareErr), "sourceScopeError": fmt.Sprint(sourceErr), "generationError": fmt.Sprint(generationErr), "finishError": fmt.Sprint(finishErr), "structuralOracleError": fmt.Sprint(oracleErr), "proposalError": fmt.Sprint(proposalErr), "executionPassed": executed, "automatedTaskPassed": automated, "actualMermaidParserPending": c.Mode == "mermaid", "manualSemanticReviewRequired": true, "semanticTaskPassed": nil, "canonicalInventory": canonical, "canonicalUnchanged": canonical.Unchanged, "mutationHistoryCountUnchanged": canonical.MutationHistoryCountUnchanged, "latencyMs": latency, "releaseQualified": false}
			t.Log(string(marshal(report)))
			if !automated {
				t.Error("mode control execution, structure, proposal or canonical preservation failed; semantic review remains separate")
			}
		})
	}
}
