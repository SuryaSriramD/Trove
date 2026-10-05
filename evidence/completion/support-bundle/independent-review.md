# Independent support bundle review

Reviewed 2026-10-05 by the database/recovery agent, independently of the implementation owner. Review covered the service, native bindings, installed-manifest inventory, frontend consent/cancellation, focused fixtures and raw test reports. Reviewed source commit: `2e2a6b9c8e2d05391255261bf9ce3a5fc3a1361b` (base `19f1dcd86edddc7a8ee5beda1c790ab8d1ea9f6a`); the source hashes below identify the reviewed files. Root integration is recorded separately. This review did not modify implementation or rerun its tests.

## Findings and corrections

- **Transcript exclusion:** meeting transcript segments are stored as ordinary paragraph blocks. Filtering only by block type exposed them in optional page text despite the stated exclusion. The query now checks `transcript_segments.block_id` and omits those bytes before JSON projection. A fixture creates an actual meeting segment and a separate ordinary paragraph; only the ordinary note is included.
- **Preview publication:** authentication before the snapshot did not prevent a grant revocation or page deletion during collection from publishing a preview. Preparation now closes its historical snapshot and uses the shared writer/revision/grant/closed-state fence before returning content. Controlled revocation, deletion and kernel-close fixtures reject publication.
- **Expiry while queued:** an export could pass its initial expiry check, wait for the writer and publish after expiry. The final publication callback now checks expiry again. The queued-export fixture confirms no archive or staging remains.
- **Late UI responses:** cancelling cleared the preview but a previously completed native reply could restore it. Operation generations now invalidate preview, success and error settlement on cancellation, discard and workspace replacement. The delayed-success browser fixture verifies that cancelled or replaced content stays absent.
- **Damaged diagnostic rows:** malformed error JSON aborted support preparation. Bounded, validity-checked projection now maps it to `OTHER`; a deliberately damaged row fixture verifies that its secret text is not included.

## Boundary assessment

No remaining concrete blocker was found in the reviewed scope. Default output uses fixed metadata categories and does not collect workspace/account/model names or identifiers, paths, page content, raw errors or logs. Optional content requires separate category/page consent, stays bounded and does not recursively expand attachments, database properties, linked blocks, OCR or transcripts. The archive preserves the exact preview file bytes. Native bindings use opaque one-use review IDs, bind the current kernel and guard pending rollback before dialogs or I/O. Export rechecks canonical page revisions and the canonical in-memory grant under the publication fence, honors cancellation before publication, uses private staging and no-replace filesystem publication, and reports cleanup failures. The support UI makes no automatic-redaction promise for explicitly selected text.

## Observed verification

- `final-affected-tests.jsonl`: **21 passing test/subtest events, zero failures, zero skips** across workspace, native and App packages. This includes transcript provenance, revocation/deletion/close, expiry, damaged JSON, picker cancellation and pending-rollback guards.
- `browser-final.json`: **4 expected passes, zero unexpected failures, skips or flaky tests**, including delayed response cancellation and workspace replacement.
- `typescript-final.txt`: successful type-check report with no output, as recorded by the implementation owner. An empty file alone is not independent process-exit evidence.
- Earlier failed attempts remain in the evidence directory. In particular, the first deletion-fence fixture omitted the required `includeDescendants` command field; the corrected fixture passed in the final report.

These are source, service and browser-fixture results. They do not establish packaged native dialog behavior, Windows/Linux execution, installation, release qualification or full TRD diagnostics/support-bundle parity. Root's integrated checks remain separate.

## Reviewed identities

| Source or report | SHA-256 |
| --- | --- |
| `internal/workspace/support_bundle.go` | `711e46656dd323face14aaa8bfeb075749c1f5f9309816b66185011a3dd1b535` |
| `internal/workspace/support_bundle_test.go` | `8d2f7eff85f3a111375f8bd0258f93729174265815407e6c12b586212a0e687d` |
| `app_support_bundle.go` | `8c62eeb4aa3bcc84c6ed743540b90fbc8e79ae80618f452f85f3238230fc7fa8` |
| `app_support_bundle_test.go` | `80d5fe685a43c8d4697a1b575b4a5e0b97f1ce732a964dc0b5dd10b1015120d3` |
| `internal/native/support_inventory.go` | `5980995e59de1204dda8bfc091221a073c548aee5361552d2d787740a79fc5f5` |
| `frontend/src/SupportBundlePanel.tsx` | `02023249a3758763ed8511573c15a8193f011765755ac17818f14b301a52b700` |
| `frontend/tests/support.browser.spec.ts` | `69205427fcb5f83a6d06d126bd3684ce95ecec516684d83c0b67a170fb41b6f2` |
| `final-affected-tests.jsonl` | `3b8b5bae51a629bc06d8f5265915b62e4953503ddf270c78ad7912921d1802bf` |
| `browser-final.json` | `1ee470f1faa84d5c042f03e5cc8438fa15eb20efef5ba6390548840fd025d863` |
| `typescript-final.txt` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
