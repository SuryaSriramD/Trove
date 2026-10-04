# Reviewed workspace actions

AI-001 and EXT-003 now share a typed action-plan resolver. The authoritative `ActionPlan` service schema accepts human names and proposed values, never model-supplied permissions, revisions or identities. The application resolves uniquely named accessible destinations, allocates canonical IDs, reads current revisions and validates commands without changing content. Ambiguous or unavailable names stop preparation.

Supported plans create pages, canonical tasks, databases with simple typed properties and records; rename/append pages; and update supported record values or bound task status/due fields. A maximum of 32 actions and 200 canonical mutations bounds each atomic proposal. Task property bindings survive renamed columns. Unsupported property semantics require the regular editor. References to newly created objects within the same plan are not supported.

The local workspace-agent mode uses bounded structured generation and shows a readable review instead of internal command JSON. The user explicitly approves an exact proposal through the kernel approval service. Generated commits retain `local_ai` / `ai` provenance. A final transaction fence checks both the executing grant and original approving grant. Changed evidence, stale destinations and revoked grants prevent all effects. Repeated preparation/approval reuses the stored proposal and does not create duplicate content. This is one atomic review, not arbitrary shell execution or an autonomous unbounded agent loop.

Paired MCP clients explicitly opt into `proposeActions`. The same resolver provides readable changes and exact guarded commands. Repeated requests must retain their idempotency key and plan; changed payloads conflict. Creating a child in a selected destination does not automatically share that child's content with the client. Disconnect/revocation prevents pending approval. Legacy existing-object proposals retain their stricter creation prohibition. Discovery exposes the authoritative closed schema.

## Development evidence

- `internal/workspace/action_plan_test.go`: scoped creation without expanded reads, duplicate names, stale evidence, invalid dates, option-label resolution, renamed task bindings and database review details.
- `internal/workspace/provider_actions_test.go`: page/task creation after approval, stable retries, stale parents, revoked grants, MCP discovery/call/disconnect, AI origin and revocation during a transaction.
- Existing provider tests retain cancellation, approval receipts, disconnected clients and crash-after-commit recovery checks.
- `frontend/tests/proposals.browser.spec.ts`: two passing visible-interface scenarios, with no application call before the explicit approval button.
- `TestRealLocalNamedWorkspacePlan`: actual local Qwen 1.5B produced the requested page, destination and body, with no effects until approval and exactly one page after repeat approval. Log: `evidence/completion/c4-real-named-plan-rerun.jsonl`. This synthetic task is a narrow functional pass; it is not broad model-quality or release qualification.

Official SDK integration, real authorized client interoperability, provider runtime containment and native packaged qualification remain separate C5 requirements. Broader action/mode quality and corrected-memory evaluations remain open; see [AI context evidence](ai-context.md).
