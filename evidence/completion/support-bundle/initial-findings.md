# Initial development checks

The first optional text consent regression failed for two subcases: `imp` + formatted `ortant consented secret` was projected as `imp ortant consented secret`. The new support path was calling the generic projection on a bare run array. It now calls the existing corrected `searchRichText` routine for the declared rich-text field; independent expected text remains unchanged.

The first UI type check found canonical Page.deletedAt/archivedAt are nullable. The component now accepts null, retaining the same live-page filter. First App test attempt was blocked by missing generated frontend/dist assets in this fresh worktree; no test pass was claimed before building them. An initial Vite invocation used a duplicate frontend prefix and was corrected. These are development integration failures, not candidate release evidence.

Independent source review found transcript paragraphs were not excluded by provenance, preparation lacked a final live permission/revision fence, a late successful native preview could reappear after cancellation, and expiry needed checking after waiting for the writer. All were corrected with focused regressions. Root review also corrected the UI's task-text inclusion claim: linked todo properties are excluded; direct toggle-heading text is supported. No acceptance claim was relaxed to make these checks pass.

The first controlled deletion-fence test used a page.trash fixture missing required includeDescendants. The raw validation failure is retained in review-fix-tests.jsonl; the corrected contract-shaped fixture keeps the expected rejection after deletion unchanged.
