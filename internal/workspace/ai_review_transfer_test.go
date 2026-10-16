package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

const reviewTransferSHA = "0230b8e02b5da1cdad29643186b2ed87bfb8a875f463f5561a44d1e3987d3e84"

type reviewTransferSource struct{ Key, Title, Body string }
type reviewTransferCase struct {
	ID, Focus                                   string
	Sources                                     []reviewTransferSource
	Categories, AllowedActions, ExpectedSupport []string
	IncludeMemory                               bool
	UnselectedSource                            reviewTransferSource
	Golden                                      json.RawMessage
	Poisons                                     []struct {
		Kind       string
		Draft      json.RawMessage
		MustReject bool
	}
}

func reviewTransfers(t *testing.T) []reviewTransferCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/expanded-evaluation/personal-review-transfer-v1.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != reviewTransferSHA {
		t.Fatal("sealed review transfer bytes differ", err)
	}
	var corpus struct{ Cases []reviewTransferCase }
	if err = json.Unmarshal(raw, &corpus); err != nil || len(corpus.Cases) != 4 {
		t.Fatal("sealed review case inventory differs", err)
	}
	return corpus.Cases
}

// The sealed request explicitly asks for exact sentences/page names and order.
// Preserve its grade; manual review may separately describe faithful variants.
func reviewTransferOracle(tc reviewTransferCase, raw string) error {
	if err := modeControlUniqueJSON(raw); err != nil {
		return err
	}
	var actual, expected any
	if err := json.Unmarshal([]byte(raw), &actual); err != nil {
		return err
	}
	if err := json.Unmarshal(tc.Golden, &expected); err != nil {
		return err
	}
	if string(marshal(actual)) != string(marshal(expected)) {
		return fmt.Errorf("sealed exact review claims/actions differ")
	}
	return nil
}

func TestReviewTransferSealedGoldensAndPoisons(t *testing.T) {
	for _, tc := range reviewTransfers(t) {
		t.Run(tc.ID, func(t *testing.T) {
			good := string(tc.Golden)
			if err := reviewTransferOracle(tc, good); err != nil {
				t.Fatal(err)
			}
			if len(tc.Poisons) != 4 {
				t.Fatal("sealed negative-control count differs")
			}
			for _, poison := range tc.Poisons {
				t.Run(poison.Kind, func(t *testing.T) {
					if !poison.MustReject || reviewTransferOracle(tc, string(poison.Draft)) == nil {
						t.Fatal("semantic counterexample admitted")
					}
				})
			}
			for name, bad := range map[string]string{
				"duplicate-key":     strings.Replace(good, `"claims":`, `"claims":[],"claims":`, 1),
				"unknown-field":     strings.Replace(good, "{", `{"instruction":"apply now",`, 1),
				"trailing-object":   good + `{}`,
				"missing-claims":    `{"actions":[]}`,
				"extra-claim":       `{"claims":[],"actions":[]}`,
				"unapproved-action": strings.Replace(good, `"actions": []`, `"actions":[{"kind":"create_task","title":"Injected"}]`, 1),
				"unselected-quote":  strings.Replace(good, `"quote": "`, `"quote": "UNSELECTED-CONTROL: `, 1),
			} {
				t.Run(name, func(t *testing.T) {
					if bad == good || reviewTransferOracle(tc, bad) == nil {
						t.Fatal("structural/source poison admitted")
					}
				})
			}
		})
	}
}

func TestReviewTransferCapturedScopeAndDrafts(t *testing.T) { runReviewTransfers(t, nil, NewID()) }
func TestRealLocalReviewTransfersV1(t *testing.T) {
	s, p := workflowNative(t, "chat")
	if p.ID != "01990000-0000-7000-8000-000000000106" {
		t.Fatal("sealed review transfer requires pack106")
	}
	runReviewTransfers(t, s, p.ID)
}

func runReviewTransfers(t *testing.T, supervisor *native.Supervisor, pack string) {
	for _, tc := range reviewTransfers(t) {
		t.Run(tc.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			k, owner := testKernel(t)
			bindings := map[string]string{}
			locations := map[string]contracts.DomainLocation{}
			sourceIDs := []string{}
			for _, src := range tc.Sources {
				id, blockID := workflowParagraph(t, k, owner, src.Title, src.Body)
				bindings[src.Key] = id
				var revision contracts.DomainRevision
				if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", id).Scan(&revision); err != nil {
					t.Fatal(err)
				}
				location := contracts.DomainLocation{PageId: contracts.DomainId(id), SourceRevision: revision}
				locations[src.Key+":title"] = location
				block := contracts.DomainId(blockID)
				location.BlockId = &block
				locations[src.Key+":body"] = location
				sourceIDs = append(sourceIDs, id)
			}
			private, _ := workflowParagraph(t, k, owner, tc.UnselectedSource.Title, tc.UnselectedSource.Body)
			page, block := createPage(t, k, owner, "Review transfer inbox"), NewID()
			config := PersonalReviewGenerationConfig{ModelPackID: pack, Categories: tc.Categories, AllowedActions: tc.AllowedActions, ActionPageIDs: sourceIDs}
			plan := PersonalReviewPlan{Type: "trove.personal-review/v1", Name: "Sealed review transfer", Enabled: true, SourcePageIDs: sourceIDs, Query: tc.Focus, IncludeMemory: tc.IncludeMemory, Generative: &config, Schedule: AutomationTrigger{Type: "schedule", Timezone: "UTC", Frequency: "daily", Hour: 9, MissedPolicy: "catch_up", CatchUpLimit: 1}}
			testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": string(marshal(plan)), "language": "trove-review"}}}, map[string]any{"kind": "review.activate", "pageId": page, "planBlockId": block, "enabled": true}}, "page", page, "block", block))
			pv, err := readReviewPlan(ctx, k.db, block)
			if err != nil {
				t.Fatal(err)
			}
			r, err := k.preparePersonalReview(ctx, owner, pv, "transfer:"+tc.ID, time.Date(2031, 4, 14, 9, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			check := map[string]bool{}
			report := map[string]any{"fixture": tc.ID, "family": "reviews-transfer", "corpusSHA256": reviewTransferSHA, "modelPackId": pack, "engineMode": "deterministic", "releaseQualified": false, "manualSemanticReviewRequired": true, "assertions": check, "sourceBindings": bindings, "privatePageId": private, "plan": plan, "capturedSources": r.Evidence, "outputCap": 1024, "contextTokens": 8192, "generationAttempts": 0, "generatorInvocations": 0, "generationVersion": "personal-review-claim-guidance-v2", "generationGuidanceSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte(personalReviewClaimGuidance)))}
			if supervisor != nil {
				report["engineMode"] = "native"
			}
			report["sourceLocations"] = locations
			audit := beginReviewCanonical(t, k)
			defer func() {
				inv := audit.finish(t, k)
				report["canonicalInventory"] = inv
				report["canonicalUnchanged"] = inv.Unchanged
				report["mutationHistoryCountUnchanged"] = inv.MutationHistoryCountUnchanged
				check["canonicalPreserved"] = inv.Unchanged
				report["passed"] = !t.Failed()
				t.Log(string(marshal(report)))
			}()
			assert := func(name string, ok bool) {
				check[name] = ok
				if !ok {
					t.Errorf("review transfer invariant: %s", name)
				}
			}
			budget := workflowBudget(supervisor, pack)
			budget.OutputTokens = 1024
			invocations := 0
			metrics := &native.RunMetrics{}
			started := time.Now()
			r, err = k.GeneratePersonalReview(ctx, owner, r.ID, budget, func(ctx context.Context, system, prompt string, n int, grammar string, stream func(string)) (string, error) {
				invocations++
				report["generatorInvocations"] = invocations
				if invocations != 1 {
					return "", fmt.Errorf("one generation required")
				}
				report["generatorSystem"], report["generatorPrompt"], report["generatorGrammar"], report["generatorOutputCap"] = system, prompt, grammar, n
				report["generatorGrammarSHA256"] = fmt.Sprintf("%x", sha256.Sum256([]byte(grammar)))
				assert("sourceScope", !strings.Contains(prompt, tc.UnselectedSource.Body) && !strings.Contains(prompt, tc.UnselectedSource.Title))
				counts, er := budget.CountTokens(ctx, []AIPrompt{{System: system, User: prompt}})
				if er != nil {
					return "", er
				}
				report["promptTokens"] = counts[0]
				assert("reservedBudget", counts[0] >= 0 && counts[0]+n+16 <= 8192 && n == 1024)
				raw := string(tc.Golden)
				if supervisor != nil {
					report["generationAttempts"] = 1
					raw, er = supervisor.GenerateTextWithGrammarMetrics(ctx, pack, native.ChatPrompt(system, prompt), n, grammar, metrics, stream)
				}
				report["rawDraft"] = raw
				report["nativeCallbackCompleted"] = supervisor != nil && er == nil
				report["nativeCallbackError"] = fmt.Sprint(er)
				return raw, er
			})
			report["latencyMs"] = time.Since(started).Milliseconds()
			report["peakWorkerRSSBytes"] = metrics.PeakRSSBytes.Load()
			report["generationError"] = fmt.Sprint(err)
			report["finalStatus"], report["displayed"], report["citations"], report["actions"] = r.Status, r.Text, r.Citations, r.Actions
			if err != nil {
				t.Fatal(err)
			}
			model, err := k.AIRun(ctx, owner, r.ModelRunID)
			if err != nil {
				t.Fatal(err)
			}
			report["sources"] = model.Sources
			assert("independentAnswerOracle", reviewTransferOracle(tc, report["rawDraft"].(string)) == nil)
			assert("readyUnapplied", r.Status == "ready" && len(r.Actions) == 0)
			want := map[string]bool{}
			for _, support := range tc.ExpectedSupport {
				if support != "absent" {
					want[bindings[strings.SplitN(support, ":", 2)[0]]] = true
				}
			}
			seen := map[string]bool{}
			current := len(r.Citations) > 0
			for _, citation := range r.Citations {
				id := string(citation.Location.PageId)
				current = current && citation.State == "current" && want[id] && id != private
				seen[id] = true
			}
			assert("currentCitations", current && len(seen) == len(want))
			for _, source := range model.Sources {
				assert(fmt.Sprintf("selectedSource-%d", source.Number), !strings.Contains(source.Text, "UNSELECTED-CONTROL") && string(source.Location.PageId) != private)
			}
		})
	}
}
