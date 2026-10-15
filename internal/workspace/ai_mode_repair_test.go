package workspace

import (
	"context"
	"strings"
	"testing"
)

// These tests inject completed drafts to exercise proposal safety, not model
// quality. Their code is independent of the retained native-mode corpus.
func TestAIModeCommentProposalGuard(t *testing.T) {
	original := "func Remaining(limit, used int) int { return limit - used }"
	cases := []struct {
		name, before, draft, language, mode, selection string
		allowed                                        bool
	}{
		{name: "line_comment_newline", before: original, draft: "func Remaining(limit, used int) int {\n// Subtract usage.\nreturn limit - used\n}", allowed: true},
		{name: "block_comment", before: original, draft: "func Remaining(limit, used int) int { /* Subtract usage. */ return limit - used }", allowed: true},
		{name: "complete_file_layout", before: "package quota\n" + original, draft: "package quota\n\n// Remaining subtracts usage.\nfunc Remaining(\nlimit, used int,\n) int {\nreturn limit - used\n}\n", allowed: true},
		{name: "literal_comment_markers", before: "func Marker() string { return \"//go:build linux\" }", draft: "// Marker returns literal text.\nfunc Marker() string { return \"//go:build linux\" }", allowed: true},
		{name: "crlf_comments", before: original, draft: "// Remaining subtracts usage.\r\nfunc Remaining(limit, used int) int {\r\nreturn limit - used\r\n}", allowed: true},
		{name: "raw_string_directive_text", before: "func Marker() string { return `//go:noinline\n/*line sample.go:4*/` }", draft: "// Marker returns literal text.\nfunc Marker() string { return `//go:noinline\n/*line sample.go:4*/` }", allowed: true},
		{name: "existing_ordinary_comments", before: "// Reports remaining capacity.\n" + original, draft: "// Remaining subtracts recorded usage.\nfunc Remaining(limit, used int) int { /* Preserve arithmetic. */ return limit - used }", allowed: true},
		{name: "directive_removal", before: "//go:noinline\n" + original, draft: "// Remaining subtracts usage.\n" + original},
		{name: "inline_comment_swallows_code", before: original, draft: "func Remaining(limit, used int) int { // Subtract usage. return limit - used }"},
		{name: "changed_operator", before: original, draft: "// Remaining reports capacity.\nfunc Remaining(limit, used int) int { return limit + used }"},
		{name: "changed_signature", before: original, draft: "// Remaining subtracts usage.\nfunc Remaining(limit, used int64) int64 { return limit - used }"},
		{name: "changed_name", before: original, draft: "// Capacity subtracts usage.\nfunc Capacity(limit, used int) int { return limit - used }"},
		{name: "added_declaration", before: original, draft: "// Remaining subtracts usage.\n" + original + "\nfunc Reset() {}"},
		{name: "added_control_flow", before: original, draft: "// Remaining subtracts usage.\nfunc Remaining(limit, used int) int { if used > limit { return 0 }; return limit - used }"},
		{name: "equivalent_expression_is_code_change", before: original, draft: "// Remaining subtracts usage.\nfunc Remaining(limit, used int) int { return limit + -used }"},
		{name: "changed_literal", before: "const Limit = 48", draft: "// Limit bounds usage.\nconst Limit = 49"},
		{name: "variadic_call_presence", before: "func Relay(xs []int) { sink(xs...) }", draft: "// Relay forwards values.\nfunc Relay(xs []int) { sink(xs) }"},
		{name: "type_alias_presence", before: "type Offset = int", draft: "// Offset measures position.\ntype Offset int"},
		{name: "compiler_directive_added", before: original, draft: "//go:noinline\n" + original},
		{name: "line_directive_added", before: original, draft: "//line other.go:12\n" + original},
		{name: "build_directive_added", before: "package quota\n" + original, draft: "//go:build linux\n\npackage quota\n" + original},
		{name: "legacy_build_directive_no_space", before: "package quota\n" + original, draft: "//+build linux\n\npackage quota\n" + original},
		{name: "legacy_build_directive_tab_space", before: "package quota\n" + original, draft: "//\t+build linux\n\npackage quota\n" + original},
		{name: "existing_directive_manual_boundary", before: "//go:noinline\n" + original, draft: "// Subtract usage.\n//go:noinline\n" + original},
		{name: "cgo_manual_boundary", before: "package quota\n/* int quota = 4; */\nimport \"C\"\n" + original, draft: "package quota\n/* int quota = 4; */\nimport \"C\"\n// Subtract usage.\n" + original},
		{name: "escaped_cgo_import", before: "package quota\nimport \"\\x43\"\n" + original, draft: "package quota\nimport \"\\x43\"\n// Subtract usage.\n" + original},
		{name: "selected_fragment_valid", before: original + "\nfunc Untouched() {}", selection: "return limit - used", draft: "/* Subtract usage. */ return limit - used", allowed: true},
		{name: "selected_fragment_invalid", before: original + "\nfunc Untouched() {}", selection: "return limit - used", draft: "// Subtract usage. return limit - used"},
		{name: "golang_language_alias", before: original, language: "golang", draft: "// Remaining subtracts usage.\n" + original, allowed: true},
		{name: "non_go_unchanged_review_path", before: original, language: "text", draft: "This is a literal code sample, not a Go block.", allowed: true},
		{name: "incomplete_original_unchanged_review_path", before: "func Pending(", draft: "// Pending is incomplete.\nfunc Pending(", allowed: true},
		{name: "statement_original_unchanged_review_path", before: "return remaining", draft: "// Return the remainder.\nreturn remaining", allowed: true},
		{name: "refactor_keeps_distinct_contract", before: original, mode: "code_refactor", draft: "func Remaining(limit, used int) int { return limit + -used }", allowed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			page, block := createPage(t, k, e, "Independent comment review"), NewID()
			language, mode := tc.language, tc.mode
			if language == "" {
				language = "go"
			}
			if mode == "" {
				mode = "code_comment"
			}
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": tc.before, "language": language, "filename": "quota.go"}}}}, "page", page, "block", block))
			prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: mode, Prompt: "Add useful comments only", PageID: page, BlockID: block, Selection: tc.selection, ModelPackID: NewID()})
			if err != nil {
				t.Fatal(err)
			}
			run, err := k.FinishAI(ctx, e, prepared.Run.ID, tc.draft, "completed", "")
			if err != nil {
				t.Fatal(err)
			}
			var historyBefore int
			if err = k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&historyBefore); err != nil {
				t.Fatal(err)
			}
			raw, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "replace_code"})
			if tc.allowed {
				if err != nil {
					t.Fatalf("valid or explicitly unsupported-boundary draft rejected: %v", err)
				}
				proposal := raw.(AIProposal)
				want := tc.draft
				if tc.selection != "" {
					want = strings.Replace(tc.before, tc.selection, tc.draft, 1)
				}
				if proposal.Before != tc.before || proposal.After != want {
					t.Fatal("review rewrote source or unselected code", proposal)
				}
				if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err != nil {
					t.Fatal(err)
				}
				data := readPage(t, k, e, page)["blocks"].([]any)[0].(map[string]any)["data"].(map[string]any)
				if data["source"] != want || data["language"] != language || data["filename"] != "quota.go" {
					t.Fatal("approved exact source or metadata changed", data)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "Go") {
				t.Fatalf("unsafe Go comment draft received a proposal: %v", err)
			}
			var proposals, historyAfter int
			if err = k.db.QueryRow("SELECT count(*) FROM ai_proposals").Scan(&proposals); err != nil {
				t.Fatal(err)
			}
			if err = k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&historyAfter); err != nil {
				t.Fatal(err)
			}
			data := readPage(t, k, e, page)["blocks"].([]any)[0].(map[string]any)["data"].(map[string]any)
			retained, err := k.AIRun(ctx, e, run.ID)
			if err != nil || retained.Text != tc.draft || data["source"] != tc.before || proposals != 0 || historyBefore != historyAfter {
				t.Fatalf("rejected draft, source or canonical history changed: proposals=%d history=%d/%d error=%v", proposals, historyBefore, historyAfter, err)
			}
		})
	}
}
