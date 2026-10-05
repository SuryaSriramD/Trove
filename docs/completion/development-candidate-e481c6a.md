# Development candidate e481c6a

This integrated macOS development candidate passes the recorded regression suites, packaged data journeys and narrow native support-export check. **It is not release-qualified, and the C0–C8 roadmap is not complete.**

Source: `e481c6a7e602313be374785c49bc8ed73252ecad`. Application SHA-256: `41339f1341d037961879f35ea5cc64a8ff3ad680897c5982e690a069b34dac2b`. All 678 application files were hashed before checks and remained unchanged afterward. The [manifest](../../evidence/completion/candidate-e481c6a/manifest.json) records the source archive, exact commands, native resources, raw outcomes and limits. Later documentation and benchmark-harness changes do not change this application.

## Verified scope

- **1,002 Go tests/subtests passed; 45 skipped.** The first full invocation passed 1,001 and failed the real PDF text-layer fixture because no bundled extractor was configured. That failure remains recorded. The unchanged suite then passed with `TROVE_NATIVE_BUNDLE=/private/tmp/trove-native-integrated-e481c6a`; no native device/account integration flags were enabled.
- **113 desktop tests/subtests passed; one skipped.** These overlap the service scope and must not be added together as unique requirements.
- **151 tooling checks passed.** The fresh TypeScript/Vite/native-helper/desktop build and ad-hoc signature verification passed. The later startup harness evidence-preservation correction has its own [seven-check report](../../evidence/completion/startup-trace-ce0973d/harness-evidence-preservation.log).
- The packaged [P0](../../evidence/completion/candidate-e481c6a/packaged-p0/packaged-integration.json), [P1](../../evidence/completion/candidate-e481c6a/packaged-p1/packaged-p1-integration.json) and [P2](../../evidence/completion/candidate-e481c6a/packaged-p2/packaged-p2-integration.json) data journeys passed creation and separate-process reopening with OS network denial and no installed models. These are real packaged service journeys, not complete manual UI or real-model qualification.

## Native support export

The actual Settings interface showed titles and text unchecked by default. An initial native save dialog remained open beyond the five-minute operation lifetime during an automation interruption; the app rejected that attempt with `context deadline exceeded`. A fresh preview saved successfully through the native dialog. This retained failure demonstrates the expiry boundary but also exposes a generic error message; it is not reported as a successful first attempt.

The resulting owner-only ZIP contained exactly `metadata.json` and `manifest.json`, with valid sizes/hashes and no page IDs, titles or text. Private staging was removed. The Go source revision/modified fields matched the frozen clean build; model count was zero and device permissions remained unprobed. Separately, selecting only page text previewed `Preserve café 🦉 and this important phrase.` exactly, without its title. That optional preview was discarded without saving. Read-only inspection after quitting confirmed all page/block values and revisions unchanged. [Native observations and independent archive inspection](../../evidence/completion/candidate-e481c6a/native-ui/observations.json) retain the tool limitations.

## What remains open

This check does not close the 51-item backlog. The existing 8.4-second shell-startup outlier is unexplained; the [separate trace investigation](startup-stage-tracing.md) is scoped to its own earlier binary. No controlled performance claim is made for this candidate. Broad AI quality, representative imports/audio, concurrent workloads and complete native interaction coverage remain open.

Software gaps include Windows recovery durability and other target-native/provider boundaries, Linux full-process PDF containment, screenshot acquisition/qualification and the broader redacted diagnostics system. Linux PDF processing and affected Windows recovery operations stay explicitly unavailable. The missing Notion research report, live authorized integrations, actual devices, independent clean installations, signing/notarization and distribution evidence remain separate prerequisites. Previous failures and platform-specific gaps cannot be closed by this candidate's passing service tests.
