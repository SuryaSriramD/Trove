package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

const lifecyclePreference = "Use the heading Launch note for answers about Project Marigold."
const lifecycleConstraint = "Do not suggest weekend due dates."
const lifecycleWeekday = "Monday 21 April 2031 is an available due date."

func lifecycleMemory(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, kind, content, target string) (id, page, source string) {
	t.Helper()
	source, _ = workflowParagraph(t, k, e, "Approved memory evidence", content)
	page, id = createPage(t, k, e, "Approved memory"), NewID()
	scope := map[string]any{"kind": "workspace", "workspaceId": k.ID()}
	if kind == "decision" {
		scope = map[string]any{"kind": "pages", "workspaceId": k.ID(), "pageIds": []any{target}}
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.propose", "memoryId": id, "pageId": page, "scope": scope, "memoryKind": kind, "content": content, "sources": []any{map[string]any{"pageId": source, "sourceRevision": "2"}}}}, "page", page, "memory", id))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": id}}, "page", page, "memory", id))
	return
}
func lifecycleOracle(id, raw string) error {
	var draft expandedDraft
	if err := json.Unmarshal([]byte(raw), &draft); err != nil {
		return err
	}
	heading, fact := "", contextFact
	if id == "preference-on" {
		heading = "Launch note"
	}
	if id == "constraint" {
		fact = lifecycleWeekday
	}
	if draft.Title != heading || len(draft.Claims) != 1 || draft.Claims[0].Text != fact || draft.Claims[0].Quote != fact {
		return fmt.Errorf("heading, exact current fact and exclusive supporting quote differ")
	}
	return nil
}
func TestWorkflowMemoryOracleRejectsCounterexamples(t *testing.T) {
	for _, id := range []string{"preference-on", "preference-off", "current-authority", "constraint"} {
		heading, fact := "", contextFact
		if id == "preference-on" {
			heading = "Launch note"
		}
		if id == "constraint" {
			fact = lifecycleWeekday
		}
		raw := string(marshal(map[string]any{"title": heading, "claims": []any{map[string]string{"text": fact, "quote": fact}}}))
		if err := lifecycleOracle(id, raw); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{strings.ReplaceAll(raw, "2031", "2032"), strings.Replace(raw, fact, fact+" Guaranteed success.", 1), strings.Replace(raw, `"title":"`, `"title":"Wrong `, 1)} {
			if lifecycleOracle(id, bad) == nil {
				t.Fatal("accepted counterexample", bad)
			}
		}
	}
}
func TestWorkflowMemoryLifecycleBoundaries(t *testing.T) { runWorkflowMemoryLifecycle(t, nil, NewID()) }
func TestRealLocalWorkflowMemoryLifecycle(t *testing.T) {
	s, p := workflowNative(t, "chat")
	runWorkflowMemoryLifecycle(t, s, p.ID)
}
func runWorkflowMemoryLifecycle(t *testing.T, supervisor *native.Supervisor, pack string) {
	cases, hash := workflowCorpus(t, "memory-lifecycle", 12)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			k, owner := testKernel(t)
			content, kind, sourceText := lifecyclePreference, "preference", contextFact
			if tc.ID == "current-authority" {
				content, kind = "Project Marigold launches on 9 February 2030.", "decision"
			}
			if tc.ID == "constraint" {
				content = lifecycleConstraint
				sourceText = "Saturday 19 April 2031 is an available due date. " + lifecycleWeekday
			}
			primary, _ := workflowParagraph(t, k, owner, "Current launch record", sourceText)
			memory, backing, support := lifecycleMemory(t, k, owner, kind, content, primary)
			e := owner
			req := AIRequest{Mode: "answer", PageID: primary, ModelPackID: pack, UseMemory: tc.ID != "preference-off", Prompt: "When does Project Marigold launch? Reply with one sentence quoting the exact current source sentence. Use no heading unless an approved preference specifies one."}
			if tc.ID == "constraint" {
				req.Prompt = "Suggest one available due date consistent with approved preferences. Reply with the exact source sentence for that date and no heading. Do not create or edit tasks."
			}
			if tc.ID == "scope-revoke" {
				e = relatedScope(k, owner, primary, backing, support)
				defer k.RevokeGrant(string(e.ScopeGrantId))
			}
			checks := map[string]bool{}
			report := map[string]any{"fixture": tc.ID, "family": "memory-lifecycle", "corpusSHA256": hash, "modelPackId": pack, "engineMode": "deterministic", "releaseQualified": false, "assertions": checks, "request": req, "generationAttempts": 0, "memoryId": memory, "memoryPageId": backing, "supportingPageId": support, "memoryContent": content, "outputCap": 512, "contextTokens": 8192}
			if supervisor != nil {
				report["engineMode"] = "native"
			}
			defer func() { report["passed"] = !t.Failed(); t.Log(string(marshal(report))) }()
			check := func(name string, ok bool) {
				checks[name] = ok
				if !ok {
					t.Errorf("memory lifecycle invariant failed: %s", name)
				}
			}
			forget := func() {
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "memory.forget", "memoryId": memory}}, "page", backing, "memory", memory))
			}
			stale := func() {
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.update", "pageId": support, "title": "Owner changed supporting evidence"}}, "page", support))
			}
			switch tc.ID {
			case "source-edit", "restore-stale":
				stale()
			case "source-delete":
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.trash", "pageId": support, "includeDescendants": false}}, "page", support))
			case "forgotten", "restore-forgotten":
				forget()
			}
			if strings.HasPrefix(tc.ID, "restore-") || tc.ID == "older-backup" {
				backup := filepath.Join(t.TempDir(), "snapshot")
				manifest, err := k.CreateBackup(ctx, owner, backup)
				if err != nil {
					t.Fatal(err)
				}
				if tc.ID == "older-backup" {
					forget()
					var state string
					if err = k.db.QueryRow("SELECT status FROM memory_entries WHERE id=?", memory).Scan(&state); err != nil {
						t.Fatal(err)
					}
					report["sourceAfterBackupStatus"] = state
					check("laterForgetRecorded", state == "forgotten")
				}
				dest := filepath.Join(t.TempDir(), "restored")
				result, err := RestoreBackupReviewed(ctx, backup, dest, manifest)
				if err != nil {
					t.Fatal(err)
				}
				restored, err := Open(dest)
				if err != nil {
					t.Fatal(err)
				}
				defer restored.Close()
				k, owner = restored, restored.OwnerContext()
				e = owner
				report["restoredWorkspaceId"], report["backupWorkspaceId"], report["restoredIndexRebuilt"] = k.ID(), manifest.WorkspaceID, result.IndexRebuilt
				check("verifiedRestore", k.ID() == manifest.WorkspaceID && result.IndexRebuilt)
				if tc.ID == "older-backup" {
					report["restoreBoundary"] = "A backup predating Forget retains the earlier approved memory state."
					report["nativeRestoreWarningAcceptancePending"] = true
					report["manualSemanticReviewRequired"] = true
				}
			}
			var status string
			if err := k.db.QueryRow("SELECT status FROM memory_entries WHERE id=?", memory).Scan(&status); err != nil {
				t.Fatal(err)
			}
			report["memoryStatus"] = status
			audit := beginAICanonicalObservation(t, k)
			prepared, err := k.PrepareAIWithBudget(ctx, e, req, workflowBudget(supervisor, pack))
			if err != nil {
				t.Fatal(err)
			}
			report["sources"], report["promptTokens"], report["preparedPrompt"], report["systemPrompt"] = prepared.Run.Sources, prepared.PromptTokens, prepared.Prompt, prepared.System
			check("reservedBudget", prepared.PromptTokens+prepared.OutputTokens+16 <= 8192)
			includes := false
			for _, s := range prepared.Run.Sources {
				includes = includes || s.MemoryID == memory
			}
			expected := tc.ID != "preference-off" && tc.ID != "source-edit" && tc.ID != "source-delete" && tc.ID != "forgotten" && tc.ID != "restore-stale" && tc.ID != "restore-forgotten"
			report["memoryIncluded"] = includes
			check("memoryEligibility", includes == expected)
			if !expected {
				check("memoryContentAbsent", !strings.Contains(prepared.Prompt, content))
			}
			if tc.ID == "older-backup" {
				check("olderStateRecovered", status == "active" && includes)
			}
			if tc.ID == "restore-stale" {
				check("restoredStatePreserved", status == "stale")
			}
			if tc.ID == "restore-forgotten" {
				check("restoredStatePreserved", status == "forgotten")
			}
			if tc.ID == "forgotten" || tc.ID == "source-delete" || tc.ID == "source-edit" || tc.ID == "restore-stale" || tc.ID == "restore-forgotten" {
				_, relatedErr := aiSuggestions(ctx, k, e, map[string]any{"pageId": backing, "semantic": false})
				report["relatedRejected"] = relatedErr != nil
				check("relatedSourceExcluded", relatedErr != nil)
			}
			if tc.ID == "forget-stream" || tc.ID == "scope-revoke" {
				if tc.ID == "forget-stream" {
					forget()
				} else {
					k.RevokeGrant(string(e.ScopeGrantId))
				}
				// The explicit user's Forget is intentional canonical work; observe only the
				// generation finalization after it, not the authorized command itself.
				audit = beginAICanonicalObservation(t, k)
				run, finishErr := k.FinishAI(ctx, e, prepared.Run.ID, "Unreviewed memory-based draft [1].", "completed", "")
				if tc.ID == "scope-revoke" {
					check("publicationRejected", finishErr != nil)
				} else {
					check("publicationRejected", finishErr == nil && run.State == "failed" && run.Text == "")
				}
				// Even a fresh owner grant must not turn a rejected run into an application.
				_, proposalErr := prepareAIAction(ctx, k, owner, map[string]any{"runId": prepared.Run.ID, "kind": "append"})
				check("proposalRejected", proposalErr != nil)
				report["finishState"], report["finishText"], report["finishRejected"], report["proposalRejected"] = run.State, run.Text, finishErr != nil, proposalErr != nil
			}
			if tc.ID == "preference-on" || tc.ID == "preference-off" || tc.ID == "current-authority" || tc.ID == "constraint" {
				attempts := 0
				raw := ""
				completion, generationErr := GeneratePreparedAI(ctx, prepared, func(ctx context.Context, system, prompt string, n int, grammar string, stream func(string)) (string, error) {
					attempts++
					if attempts != 1 {
						return "", fmt.Errorf("one generation only")
					}
					if supervisor != nil {
						var err error
						raw, err = supervisor.GenerateTextWithGrammar(ctx, pack, native.ChatPrompt(system, prompt), n, grammar, stream)
						return raw, err
					}
					heading, fact := "", contextFact
					if tc.ID == "preference-on" {
						heading = "Launch note"
					}
					if tc.ID == "constraint" {
						fact = lifecycleWeekday
					}
					raw = string(marshal(map[string]any{"title": heading, "claims": []any{map[string]string{"text": fact, "quote": fact}}}))
					return raw, nil
				}, nil)
				state, failure := "completed", ""
				if generationErr != nil {
					state, failure = "failed", generationErr.Error()
				}
				run, finishErr := k.FinishAI(ctx, e, prepared.Run.ID, completion.Text, state, failure)
				check("oneGeneration", attempts == 1)
				check("generatedSuccessfully", generationErr == nil && finishErr == nil && run.State == "completed")
				check("independentAnswerOracle", lifecycleOracle(tc.ID, raw) == nil)
				currentFact, allowedCitations := false, true
				for _, c := range run.Citations {
					id := string(c.Location.PageId)
					currentFact = currentFact || (id == primary && c.State == "current")
					allowedCitations = allowedCitations && c.State == "current" && (id == primary || tc.ID == "preference-on" && id == support)
				}
				check("currentCitation", currentFact && allowedCitations)
				report["rawDraft"], report["generationAttempts"], report["generationError"], report["citations"], report["displayed"] = raw, attempts, fmt.Sprint(generationErr), run.Citations, run.Text
			}
			inv := audit.finish(t, k)
			report["canonicalInventory"], report["canonicalUnchanged"], report["mutationHistoryCountUnchanged"] = inv, inv.Unchanged, inv.MutationHistoryCountUnchanged
			check("canonicalPreserved", inv.Unchanged)
		})
	}
}
