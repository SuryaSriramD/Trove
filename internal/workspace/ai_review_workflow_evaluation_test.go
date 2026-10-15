package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/internal/native"
)

const reviewFact = "The security review is incomplete. Launch cannot proceed until that review is approved."
const reviewDocumentation = "Documentation is ready."
const reviewSuggestion = "Suggestion: request security review approval."

var reviewModelCases = map[string]bool{"daily-summary": true, "weekly-summary": true, "unresolved-tasks": true, "changed-decision": true, "risk-suggestion": true, "no-owner": true, "categories": true}
var reviewCaseIDs = []string{"daily-summary", "weekly-summary", "unresolved-tasks", "changed-decision", "risk-suggestion", "no-owner", "categories", "stale-destination", "forgotten-memory", "cancel-retry", "restart", "dedup-import"}

// Review run receipts are derived execution state. Keep activation, plans,
// approvals and all canonical sources/approved results inside the audit.
func reviewCanonicalSnapshot(k *Kernel) (aiCanonicalSnapshot, error) {
	s, err := readAICanonicalSnapshot(k)
	if err != nil {
		return s, err
	}
	s.ExcludedTables = map[string]string{}
	for key, value := range aiCanonicalExcludedTables {
		s.ExcludedTables[key] = value
	}
	for _, name := range []string{"personal_review_runs", "personal_review_model_runs"} {
		s.ExcludedTables[name] = "Derived personal-review execution/draft receipt; activation, plan blocks, approvals and applied content remain protected."
		delete(s.Tables, name)
	}
	return s, nil
}

type reviewCanonicalObservation struct {
	before    aiCanonicalSnapshot
	binding   aiCanonicalBinding
	directory string
}

func beginReviewCanonical(t *testing.T, k *Kernel) *reviewCanonicalObservation {
	t.Helper()
	dir := aiCanonicalEvidenceDirectory(t)
	s, err := reviewCanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	s.TestName = t.Name()
	path := ""
	if dir != "" {
		path = filepath.Join(dir, "before.json")
	}
	b, err := bindAICanonicalSnapshot(s, path)
	if err != nil {
		t.Fatal(err)
	}
	return &reviewCanonicalObservation{s, b, dir}
}
func (o *reviewCanonicalObservation) finish(t *testing.T, k *Kernel) aiCanonicalResult {
	t.Helper()
	s, err := reviewCanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	s.TestName = o.before.TestName
	path := ""
	if o.directory != "" {
		path = filepath.Join(o.directory, "after.json")
	}
	b, err := bindAICanonicalSnapshot(s, path)
	if err != nil {
		t.Fatal(err)
	}
	r := compareAICanonicalSnapshots(o.before, s)
	r.Before, r.After = o.binding, b
	if !r.Unchanged {
		t.Error("review analysis changed canonical rows, approvals or assets")
	}
	return r
}
func reviewExpectedDraft(id string) string {
	category := "summary"
	if id == "risk-suggestion" {
		category = "risks"
	}
	if id == "categories" {
		category = "decisions"
	}
	claims := []any{map[string]string{"category": category, "text": reviewFact, "quote": reviewFact}}
	actions := []PlannedAction{}
	if id == "weekly-summary" {
		claims = append(claims, map[string]string{"category": "summary", "text": reviewDocumentation, "quote": reviewDocumentation})
	}
	if id == "unresolved-tasks" {
		claims = []any{map[string]string{"category": "summary", "text": "Security review is open.", "quote": "Status: To do"}}
	}
	if id == "risk-suggestion" {
		actions = append(actions, PlannedAction{Kind: "update_page", Target: "Current security decision", Body: reviewSuggestion})
	}
	return string(marshal(map[string]any{"claims": claims, "actions": actions}))
}
func reviewAnswerOracle(id, raw string) error {
	var actual, expected any
	if err := json.Unmarshal([]byte(raw), &actual); err != nil {
		return err
	}
	json.Unmarshal([]byte(reviewExpectedDraft(id)), &expected)
	if string(marshal(actual)) != string(marshal(expected)) {
		return fmt.Errorf("review facts, category, exact source quotes or suggestion-only action differ from the independent fixture oracle")
	}
	return nil
}
func TestWorkflowReviewOracleRejectsCounterexamples(t *testing.T) {
	for _, id := range reviewCaseIDs {
		good := reviewExpectedDraft(id)
		if err := reviewAnswerOracle(id, good); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{strings.ReplaceAll(good, "incomplete", "complete"), strings.Replace(good, "Security review is open.", "Publish notes is open.", 1), strings.Replace(good, `"text":"`, `"text":"Alex owns this. `, 1), strings.Replace(good, `"category":"`, `"category":"unselected-`, 1), strings.Replace(good, "Suggestion:", "Already completed:", 1)} {
			if bad != good && reviewAnswerOracle(id, bad) == nil {
				t.Fatal("review oracle accepted unsupported output", id, bad)
			}
		}
	}
}
func TestWorkflowReviewBoundaries(t *testing.T) { runWorkflowReviews(t, nil, NewID()) }
func TestRealLocalWorkflowReviews(t *testing.T) {
	s, p := workflowNative(t, "chat")
	runWorkflowReviews(t, s, p.ID)
}
func runWorkflowReviews(t *testing.T, supervisor *native.Supervisor, pack string) {
	cases, hash := workflowCorpus(t, "reviews", 12)
	for index, tc := range cases {
		if tc.ID != reviewCaseIDs[index] || tc.Source != reviewFact {
			t.Fatal("review scenario inventory changed")
		}
		t.Run(tc.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			k, owner := testKernel(t)
			source, _ := workflowParagraph(t, k, owner, "Current security decision", reviewFact)
			private, _ := workflowParagraph(t, k, owner, "Unselected private evidence", "PRIVATE-REVIEW-73: a secret unrelated owner is Alex.")
			page := createPage(t, k, owner, "Review inbox")
			block := NewID()
			config := PersonalReviewGenerationConfig{ModelPackID: pack, Categories: []string{"summary"}, AllowedActions: []string{"append_review"}, ActionPageIDs: []string{source}}
			focus := "Return one summary claim with the complete two-sentence current security decision as both exact text and quote. No headings, people, owners, task assignments, extra conclusions or optional actions."
			plan := PersonalReviewPlan{Type: "trove.personal-review/v1", Name: "Security review", Enabled: true, SourcePageIDs: []string{source}, Query: focus, Schedule: AutomationTrigger{Type: "schedule", Timezone: "UTC", Frequency: "daily", Hour: 9, MissedPolicy: "catch_up", CatchUpLimit: 1}}
			bindings := map[string]string{"source": source, "destination": page, "private": private}
			memory, memoryPage := "", ""
			if tc.ID == "weekly-summary" {
				other, _ := workflowParagraph(t, k, owner, "Documentation evidence", reviewDocumentation)
				plan.SourcePageIDs = append(plan.SourcePageIDs, other)
				bindings["documentation"] = other
				plan.Schedule.Frequency = "weekly"
				plan.Schedule.Weekday = 1
				plan.Query = "Return exactly two summary claims: the complete current security decision and the documentation sentence. Use each complete source sentence(s) as both exact text and quote. No optional actions."
				for n := 0; n < 6; n++ {
					id := NewID()
					testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": id, "pageId": other, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": fmt.Sprintf("Unrelated archived inventory section %d.", n)}}}}}}, "page", other, "block", id))
				}
			}
			if tc.ID == "unresolved-tasks" {
				f := ensureTestTasks(t, k, owner)
				for i, title := range []string{"Security review", "Publish notes"} {
					record, taskPage := NewID(), NewID()
					testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "task.create", "recordId": record, "pageId": taskPage, "title": title}}, "record", record, "page", taskPage, "collection", f.collection, "property", f.status))
					if i == 1 {
						testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "task.setStatus", "recordId": record, "statusOptionId": f.done}}, "record", record, "page", taskPage, "collection", f.collection, "property", f.status))
						bindings["completedTask"] = taskPage
					} else {
						bindings["openTask"] = taskPage
					}
					plan.SourcePageIDs = append(plan.SourcePageIDs, taskPage)
				}
				plan.Query = `Identify the open canonical task only. Return one summary claim with text "Security review is open." and quote the exact current open status field. Do not describe the completed Publish notes task as open. Return no actions.`
			}
			if tc.ID == "changed-decision" || tc.ID == "forgotten-memory" {
				memory, memoryPage, _ = lifecycleMemory(t, k, owner, "decision", "The security review is complete. Launch can proceed.", source)
				plan.IncludeMemory = true
			}
			if tc.ID == "risk-suggestion" {
				config.Categories = []string{"risks"}
				config.AllowedActions = append(config.AllowedActions, "append_page")
				plan.Query = `Return one risks claim with the complete security decision as exact text and quote. Propose one update_page action for Current security decision with body "Suggestion: request security review approval." Do not claim it has happened, assign a person, or propose another action.`
			}
			if tc.ID == "categories" {
				config.Categories = []string{"decisions"}
				plan.Query = "Return one decisions claim with the complete current security decision as exact text and quote. Return no optional actions and no other categories."
			}
			plan.Generative = &config
			testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": string(marshal(plan)), "language": "trove-review"}}}, map[string]any{"kind": "review.activate", "pageId": page, "planBlockId": block, "enabled": true}}, "page", page, "block", block))
			pv, err := readReviewPlan(ctx, k.db, block)
			if err != nil {
				t.Fatal(err)
			}
			r, err := k.preparePersonalReview(ctx, owner, pv, "workflow:"+tc.ID, time.Date(2031, 4, 14, 9, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			check := map[string]bool{}
			report := map[string]any{"fixture": tc.ID, "family": "reviews", "corpusSHA256": hash, "modelPackId": pack, "engineMode": "deterministic", "releaseQualified": false, "assertions": check, "generationAttempts": 0, "generatorInvocations": 0, "sourceBindings": bindings, "plan": plan, "capturedSources": r.Evidence, "capturedWarning": r.Warning, "runId": r.ID, "occurrence": r.Occurrence, "serviceBoundaryOnly": !reviewModelCases[tc.ID], "outputCap": 1024, "contextTokens": 8192}
			if supervisor != nil {
				report["engineMode"] = "native"
			}
			defer func() { report["passed"] = !t.Failed(); t.Log(string(marshal(report))) }()
			assert := func(name string, ok bool) {
				check[name] = ok
				if !ok {
					t.Errorf("review invariant failed: %s", name)
				}
			}
			audit := beginReviewCanonical(t, k)
			saveAudit := func() {
				inventory := audit.finish(t, k)
				report["canonicalInventory"] = inventory
				report["canonicalUnchanged"] = inventory.Unchanged
				report["mutationHistoryCountUnchanged"] = inventory.MutationHistoryCountUnchanged
				assert("canonicalPreserved", inventory.Unchanged)
			}
			budget := workflowBudget(supervisor, pack)
			budget.OutputTokens = 1024
			attempts, invocations := 0, 0
			generator := func(ctx context.Context, system, prompt string, n int, grammar string, stream func(string)) (string, error) {
				invocations++
				report["generatorInvocations"] = invocations
				report["preparedPrompt"] = prompt
				counts, err := budget.CountTokens(ctx, []AIPrompt{{System: system, User: prompt}})
				if err != nil {
					return "", err
				}
				report["promptTokens"] = counts[0]
				assert("reservedBudget", counts[0] >= 0 && counts[0]+n+16 <= 8192 && n == 1024)
				if tc.ID == "forgotten-memory" {
					before := audit.finish(t, k)
					report["beforeExplicitMutationInventory"] = before
					testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "memory.forget", "memoryId": memory}}, "memory", memory, "page", memoryPage))
					audit = beginReviewCanonical(t, k)
				}
				if tc.ID == "cancel-retry" && invocations == 1 {
					if _, err := k.CancelPersonalReviewGeneration(ctx, owner, r.ID); err != nil {
						return "", err
					}
					return "", context.Canceled
				}
				raw := reviewExpectedDraft(tc.ID)
				if supervisor != nil && reviewModelCases[tc.ID] {
					attempts++
					report["generationAttempts"] = attempts
					raw, err = supervisor.GenerateTextWithGrammar(ctx, pack, native.ChatPrompt(system, prompt), n, grammar, stream)
				}
				report["rawDraft"] = raw
				return raw, err
			}
			if tc.ID == "restart" {
				if _, err = k.db.Exec("UPDATE personal_review_runs SET status='generating' WHERE id=?", r.ID); err != nil {
					t.Fatal(err)
				}
				path := k.Path()
				k.Close()
				k, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer k.Close()
				owner = k.OwnerContext()
				r, err = readPersonalReviewRun(ctx, k.db, r.ID)
				if err != nil {
					t.Fatal(err)
				}
				next, err := k.NextPersonalReviewGeneration(ctx, owner)
				if err != nil {
					t.Fatal(err)
				}
				report["finalStatus"], report["nextRunId"] = r.Status, next.ID
				assert("restartInert", r.Status == "cancelled" && next.ID == "")
				saveAudit()
				return
			}
			r, err = k.GeneratePersonalReview(ctx, owner, r.ID, budget, generator)
			if tc.ID == "forgotten-memory" {
				report["finishRejected"], report["finalStatus"] = err != nil, r.Status
				_, proposalErr := preparePersonalReviewProposal(ctx, k, owner, reviewReq(k, r.ID))
				report["proposalRejected"] = proposalErr != nil
				assert("forgetInvalidates", err != nil && r.Status != "ready" && proposalErr != nil)
				saveAudit()
				return
			}
			if tc.ID == "cancel-retry" {
				report["cancelStatus"] = r.Status
				assert("cancelledInert", err != nil && r.Status == "cancelled")
				testOK(t, testDispatch(t, k, owner, "review.retry", reviewReq(k, r.ID)))
				r, err = k.GeneratePersonalReview(ctx, owner, r.ID, budget, generator)
				assert("boundedRetry", r.Attempt == 2 && invocations == 2)
			}
			if err != nil {
				saveAudit()
				t.Fatal(err)
			}
			report["finalStatus"], report["warning"], report["citations"], report["actions"] = r.Status, r.Warning, r.Citations, r.Actions
			model, err := k.AIRun(ctx, owner, r.ModelRunID)
			if err != nil {
				t.Fatal(err)
			}
			report["sources"] = model.Sources
			assert("sourceScope", !strings.Contains(report["preparedPrompt"].(string), "PRIVATE-REVIEW-73"))
			assert("independentAnswerOracle", reviewAnswerOracle(tc.ID, report["rawDraft"].(string)) == nil)
			current := len(r.Citations) > 0
			for _, citation := range r.Citations {
				current = current && citation.State == "current" && string(citation.Location.PageId) != private
			}
			assert("currentCitations", current)
			assert("readyUnapplied", r.Status == "ready")
			if tc.ID == "weekly-summary" {
				assert("omissionsDisclosed", strings.Contains(r.Warning, "omitted"))
			}
			if tc.ID == "changed-decision" {
				assert("memoryIncluded", strings.Contains(report["preparedPrompt"].(string), "The security review is complete. Launch can proceed."))
			}
			saveAudit()
			// Explicit user review is outside the read-only analysis observation. Verify
			// the expected destination effect independently, then repeat the exact apply.
			denied := testDispatch(t, k, owner, "review.apply", reviewReq(k, r.ID))
			report["unapprovedApplyRejected"] = denied["ok"] != true
			assert("approvalRequired", denied["ok"] != true)
			testOK(t, testDispatch(t, k, owner, "review.propose", reviewReq(k, r.ID)))
			if tc.ID == "stale-destination" {
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Owner edited review inbox"}}, "page", page))
				before := beginReviewCanonical(t, k)
				denied = testDispatch(t, k, owner, "review.apply", reviewReq(k, r.ID))
				report["staleApplyRejected"] = denied["ok"] != true
				report["staleApplyInventory"] = before.finish(t, k)
				assert("staleApplyRejected", denied["ok"] != true)
				return
			}
			var beforeBlocks, afterBlocks, retryBlocks int
			k.db.QueryRow("SELECT COUNT(*) FROM blocks WHERE page_id=?", page).Scan(&beforeBlocks)
			testOK(t, testDispatch(t, k, owner, "review.apply", reviewReq(k, r.ID)))
			k.db.QueryRow("SELECT COUNT(*) FROM blocks WHERE page_id=?", page).Scan(&afterBlocks)
			testOK(t, testDispatch(t, k, owner, "review.apply", reviewReq(k, r.ID)))
			k.db.QueryRow("SELECT COUNT(*) FROM blocks WHERE page_id=?", page).Scan(&retryBlocks)
			var destinationText, sourceText string
			for target, out := range map[string]*string{page: &destinationText, source: &sourceText} {
				rows, er := k.db.Query("SELECT data_json FROM blocks WHERE page_id=? AND type!='code' ORDER BY sort_key", target)
				if er != nil {
					t.Fatal(er)
				}
				parts := []string{}
				for rows.Next() {
					var raw string
					if er = rows.Scan(&raw); er != nil {
						t.Fatal(er)
					}
					var data struct {
						RichText []struct {
							Text string `json:"text"`
						} `json:"richText"`
					}
					if er = json.Unmarshal([]byte(raw), &data); er != nil {
						t.Fatal(er)
					}
					text := ""
					for _, span := range data.RichText {
						text += span.Text
					}
					parts = append(parts, text)
				}
				rows.Close()
				*out = strings.Join(parts, "\n\n")
			}
			report["reviewedEffect"] = map[string]any{"destinationPageId": page, "beforeBlocks": beforeBlocks, "afterBlocks": afterBlocks, "retryBlocks": retryBlocks, "status": "accepted", "destinationText": destinationText, "sourceText": sourceText}
			assert("reviewedContentPresent", strings.Contains(destinationText, r.Text))
			if tc.ID == "risk-suggestion" {
				assert("suggestionAppliedExactly", strings.Contains(sourceText, reviewSuggestion))
			}
			assert("reviewedOnce", afterBlocks > beforeBlocks && retryBlocks == afterBlocks)
			same, err := k.preparePersonalReview(ctx, owner, pv, r.Occurrence, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			report["repeatedOccurrenceRunId"] = same.ID
			assert("occurrenceDedup", same.ID == r.ID)
			if tc.ID == "dedup-import" {
				entries := exportPrivacyEntries(t, k, owner, false)
				target, te := testKernel(t)
				completeImportFixture(t, target, te, importFixture(t, target, te, entries))
				counts := map[string]int{}
				for _, table := range []string{"personal_review_activation", "personal_review_runs", "personal_review_model_runs", "ai_proposals"} {
					var n int
					if err = target.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
						t.Fatal(err)
					}
					counts[table] = n
					assert("portableInert-"+table, n == 0)
				}
				report["portableExecutionCounts"] = counts
				if err = target.RunPersonalReviewTick(ctx, te, time.Now().Add(72*time.Hour)); err != nil {
					t.Fatal(err)
				}
				next, err := target.NextPersonalReviewGeneration(ctx, te)
				if err != nil {
					t.Fatal(err)
				}
				report["portableNextRunId"] = next.ID
				assert("portableNoGeneration", next.ID == "")
			}
		})
	}
}

func TestWorkflowReviewCanonicalAuditProtectsPlanAndApprovals(t *testing.T) {
	k, e := testKernel(t)
	r, _, _ := generativeReviewFixture(t, k, e)
	before, err := reviewCanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.db.Exec("UPDATE personal_review_runs SET status='generating' WHERE id=?", r.ID); err != nil {
		t.Fatal(err)
	}
	after, err := reviewCanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	if !compareAICanonicalSnapshots(before, after).Unchanged {
		t.Fatal("derived execution receipt counted as canonical source mutation")
	}
	for _, table := range []string{"personal_review_activation", "personal_review_schedule", "blocks", "approvals", "memory_entries", "file_external_context_policy"} {
		if _, ok := after.Tables[table]; !ok {
			t.Fatal("protected table excluded", table)
		}
	}
	if _, err = k.db.Exec("UPDATE pages SET title='Unapproved mutation' WHERE id=?", r.PageID); err != nil {
		t.Fatal(err)
	}
	changed, err := reviewCanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	if compareAICanonicalSnapshots(before, changed).Unchanged {
		t.Fatal("canonical source mutation escaped audit")
	}
	ordinary, err := readAICanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ordinary.Tables["personal_review_runs"]; !ok {
		t.Fatal("review audit weakened another family's scope")
	}
}
