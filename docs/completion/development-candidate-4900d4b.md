# Development candidate 4900d4b

This is an identified development checkpoint, **not a release approval**. Source: `4900d4bfbdb7a324ebc6e828544ca7204f72a709`. The packaged application SHA-256 is `ec8b5ed605ef45ef0154eb5be55740048f9534343b780ac20c0dd4a7896c03bd`.

The [candidate manifest](../../evidence/completion/candidate-4900d4b/manifest.json) records the complete app resource hashes, source archive identity, local model identities, hardware, test reports and limitations. This candidate predates the later related-content, platform credential, native portability and migration packages. Their evidence does not change this binary's identity.

## Passed checks

- Full Go race suite: **620 passing tests/subtests, 27 explicit skips**, no failures, with ephemeral local fixture networking allowed. [Raw result](../../evidence/completion/c6-go-4900d4b-local-network.jsonl). An earlier sandboxed run failed because localhost listeners were denied; that [failure is retained](../../evidence/completion/c6-go-4900d4b.jsonl).
- TypeScript and production frontend build, native helper build, packaged executable/helper dependency checks, and strict ad-hoc signature verification. [Build result](../../evidence/completion/c6-packaged-build-4900d4b.txt).
- Packaged offline [P0](../../evidence/completion/candidate-4900d4b/p0/packaged-integration.json), [P1](../../evidence/completion/candidate-4900d4b/p1/packaged-p1-integration.json), and [P2](../../evidence/completion/candidate-4900d4b/p2/packaged-p2-integration.json) service journeys. Each creates content, reopens it in a separate packaged process, and exercises its declared recovery/portability assertions under an OS-enforced network denial.
- Packaged [native worker journey](../../evidence/completion/candidate-4900d4b/native/packaged-integration.json) with installed local packs and OS network denial. This is worker integration evidence, not a clean-machine model acquisition or a real microphone test.

The new P2 journey independently checks required form validation, exact decimal chart totals, confirmed-only row actions, current graph citations, display persistence, original conversation bytes and branch selection, backup/reopen, and inactive portable imports for forms, actions and scheduled reviews.

## Open qualification

The factual/memory model rubric still fails: the constrained model can omit a required supported claim. The image rubric still fails blank-image abstention. Successful worker execution does not establish answer quality. The previous missing-startup-readiness incident remains unexplained; this checkpoint does not close startup performance. No new representative database or hybrid retrieval performance qualification was run on this candidate.

Signing/notarization, independent clean installation and restore, physical microphone/permission tests, live provider/Notion accounts, hosted HTTPS interoperability, and Windows/Linux execution remain unqualified. The research report remains unavailable. All 51 backlog criteria retain their original scope and independent release gates.
