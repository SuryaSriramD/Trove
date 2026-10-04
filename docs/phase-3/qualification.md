# P0 release qualification

**Status: P0 release gate OPEN, not approved.** Automated qualification and all
twenty native shell benchmark launches passed. Candidate 2 is installed, but
final native UI qualification is blocked pending the user unlocking the Mac;
computer-use controls cannot unlock it. Earlier native observations remain
historical and do not close those final-candidate checks.

The completed aggregate report records `automated_passed` at 2026-09-30
17:18:23 UTC (22:48:23 IST), with `releaseApproved: false`. All automated stages
passed against the same source and immutable candidate. A runnable desktop
candidate is available; the complete usable-P0 release acceptance is not yet
established.

The candidate under qualification is
`build/phase3-candidate-2/Trove.app`, Trove 0.3.0 for macOS 27 Apple Silicon, built
with an ad-hoc signature. The completed aggregate evidence directory is
[`20260930T171124Z-c451f895`](../../evidence/phase3/runs/20260930T171124Z-c451f895/).
Its recorded source inputs and initial bundle file hashes are in
[`source-before.json`](../../evidence/phase3/runs/20260930T171124Z-c451f895/source-before.json)
and
[`bundle-before.json`](../../evidence/phase3/runs/20260930T171124Z-c451f895/bundle-before.json).
The authoritative result is
[`verification.json`](../../evidence/phase3/runs/20260930T171124Z-c451f895/verification.json),
also published as
[`automated-verification.json`](../../evidence/phase3/automated-verification.json).
No P1 provider or advanced parity capability is a prerequisite for these checks.

| Verified identity | SHA-256 |
| --- | --- |
| Source fingerprint, unchanged before/after | `18ae95a6b632869eddb073cea36629a21220efb00f0e9505c1954e3b7632f97c` |
| Candidate bundle fingerprint, unchanged before/after | `cd0ae875be19f19267a4c864f37f7e7036cf523ba6abac969ae9e51801591d03` |
| `Contents/MacOS/Trove` executable | `5b3ac5c6a5f764bf39b8c28a56e9093c1fb7dd8d5a29f9faf0599fa5829b6e3e` |

Source and bundle fingerprints are the runner's deterministic hashes of their
file/hash maps, not hashes of a release ZIP. Individual file hashes are retained
in the report, including the final source and bundle maps.

The same candidate is installed at
`/Users/suryasriramduvvuri/Applications/Trove.app`.
[`installation.json`](../../evidence/phase3/installation.json) records matching
SHA-256 values for all 293 payload files and strict deep signature verification.
Generated AppleDouble sidecars were removed from the installed APFS copy; the
application payload matches the qualified candidate. This is an ad-hoc signature
and a same-Mac installation, not Developer ID/notarization or a separate clean-Mac
Gatekeeper test.

The planned archive is
`build/releases/Trove-0.3.0-p0-candidate-2-macos27-arm64.zip`. Its final ZIP hash and
other distribution artifact identities will be recorded separately in
`evidence/phase3/release-artifacts.json` after packaging. This checkpoint does not
invent or substitute an archive hash.

## Evidence interpretation

- **Automated pass** means the stated fixture and scope passed; it does not imply
  that the equivalent native interaction was observed.
- **Observed on an earlier package** means the operator exercised that build.
  Source changes and the final immutable candidate still require appropriate
  retesting.
- **Pending** means required evidence is incomplete. **Unverified** identifies
  configurations or distribution checks for which no pass is claimed.

The aggregate uses `scripts/verify-phase3.py`. It records individual test/log
hashes, compares source and candidate fingerprints before and after testing,
uses the candidate's actual native helpers, and separates non-native race tests
from real model/worker tests under macOS `deny network*`. The network-denial
control reported `OS_NETWORK_DENIED`. Packaged data checks passed with
a minimal PATH and an empty model cache; installed packs are supplied separately
for inference. The completed `automated_passed` result records
`releaseApproved: false`; final native interactions remain incomplete because
the Mac is locked. The subsequent shell benchmark closes the performance launch
check described below. The runner also lists clean-machine installation and public signing
as pending; this report treats those as distribution limitations, not additional
user-required blockers by themselves.

## Complete user journey

| Journey step | Evidence available | Current acceptance |
| --- | --- | --- |
| Install and launch without an account or separately installed Node, Python, Ollama, Docker or database server | Production Wails build and all three packaged process steps passed with bundled native helpers, embedded SQLite and license notices under OS network denial and `PATH=/usr/bin:/bin:/usr/sbin:/sbin`. The two data steps had no installed weights. No product account was configured. | **Automated packaged pass and matching installation on this Mac.** All 293 installed payload files match the qualified candidate and strict signature verification passed. Separate clean-Mac installation remains an **unverified distribution limit**; build/test tools are not application runtime requirements. |
| Configure or import model packs | Acquisition tests cover exact digest/container checks, incompatible architecture/runtime/format/identity/hash/memory/role, explicit local import, cancellation, resumed HTTP transfer, corrupt-file retry and low disk. Pack catalogue and installed file hashes are recorded. After the user requested agent completion, Manager.Import verified existing local BGE, Whisper and Qwen 1.5B files into the normal cache. | **Automated pass; service-assisted setup passed for the earlier native session.** This does not qualify native picker interaction. Final-candidate Settings status/selection requires confirmation. Low disk is deterministically injected, not produced by filling the user's volume. |
| Import Notion | Inventory, hierarchy, internal links, CSV types/fallback, retained originals, identity reconciliation, explicit conflict review, stale-review rejection and durable import cursors pass. Hostile ZIP and cyclic-manifest tests pass. The operator reports a successful **user-assisted ZIP selection** in the native picker on the earlier package. | **Automated pass; native picker success observed with user assistance.** An earlier CUA attempt could not activate Open; this does not establish an application picker defect or an unattended automation pass. Final-candidate packaged import/reconciliation passed; interactive review/progress remain pending. |
| Edit pages and databases | Current aggregate: 24 editor/browser tests, 3 database browser tests and 10 codec/database unit tests pass, with TypeScript clean. Browser cases cover views, record identity, canonical writes, imported content, clipboard, nested blocks, background changes, draft conflicts, a 10,000-page hierarchy/body search and Blob audio playback/lifecycle. | **Automated pass; earlier native typed/structural save and database Estimate edit passed.** Exact clipboard fixture fidelity was unverified. Final native save/reopen, clipboard and keyboard confirmation pending. |
| Search | Model-free keyword indexing/query/edit/delete/rebuild and authorization scopes pass. The public P0 journey validates exact imported content retrieval. Defined keyword benchmarks pass through 10,000 pages. | **Automated pass for stated profiles; earlier native model-free ORCHID-472 search passed.** Final packaged UI navigation to an exact hit remains pending. Hybrid timing is qualified only for the measured 100-page profile. |
| Ask local AI | Final-candidate helpers passed actual Qwen instruction, native BGE retrieval, root application AI workflow and reviewed meeting-summary tests under OS network denial. The separately launched app passed runtime generation/embedding/transcription smoke checks. Missing packs fail explicitly; no implicit acquisition/cloud fallback is used. | **Automated final-candidate native and packaged pass.** The packaged smoke generation uses TinyStories as a runtime fixture; Qwen assistant behavior is covered by the actual application/helper tests. Earlier native Qwen 1.5B UI answered ORCHID-472 with an explicit missing-citations disclosure and seven provided sources. Final-candidate UI answer/source review pending. |
| Record and transcribe | Earlier packaged AVAudioEngine capture produced growing PCM/checkpoints and a saved 69.589-second WAV. Final-candidate native Whisper completed all 60 minute units of the one-hour fixture with 694 segments and 46 concurrent same-page edits. Final-candidate cancellation/parent-exit tests and packaged synthetic capture pass. | **Earlier actual capture observed; final-candidate automated lifecycle/transcription pass.** Final-candidate microphone permission, live byte/duration display, stop/save, playable audio and visible transcription progress remain pending. The audio fixtures do not establish natural-meeting recognition quality. |
| Manage memory | Approval, forgetting during generation, supersession and changed/deleted-source invalidation pass. The public data journey verifies forgotten and stale evidence do not enter prepared AI context; memory state is checked after reopen/restore. | **Automated pass; earlier native proposed → active → forgotten controls passed.** Final-candidate UI approval/forget/source-invalidation observation pending. |
| Restart | Actual SIGKILL recovery retains committed canonical content and durable recordings. A separate final-candidate desktop process reopened and verified the complete model-free data fixture. The selected-workspace regression passes and rejects an unavailable saved path without recreating an empty workspace. | **Automated packaged restart pass.** Final-candidate quit/reopen must confirm the selected workspace and committed edits/audio. |
| Back up and restore | Native backup/staged restore checks cover canonical snapshots, history/import evidence, recordings, revoked approvals, checksums, unsafe paths and unsupported versions. The final packaged P0 journey verified a backup, restored to an unused workspace, matched canonical snapshots, rechecked search/memory and finalized the complete recovered audio tail. | **Automated packaged pass without weights.** Interactive selection/review/restore into a fresh directory remains pending. |

## Acceptance requirements and failure cases

| Requirement | Evidence and result | Boundary still to verify |
| --- | --- | --- |
| Editing, import and keyword search need no model weights | `TestP0PublicServiceJourney` and model-free search/editor fixtures pass. Final candidate `p0-create` and `p0-verify` both pass with `installedWeightsAvailable: false`. | The third packaged `native` step deliberately has installed weights; its inference result is separate from the model-free data proof. |
| All P0 AI/transcription work offline; no silent cloud fallback | All eight actual native tests (ten including subtests) passed under OS network denial using final-candidate helpers, including root application AI and one-hour transcription. All three packaged steps passed under the same network policy. `TestMissingModelNeverAcquiresOrFallsBack` and the OS denial control pass. | Exact helper/model hashes are retained in the completed aggregate; final interactive answer/source review remains pending. |
| Import reconciliation and hostile-input tests pass | Stable Notion external IDs survive rename/reimport; stale review is rejected. Unsafe paths, collisions, special files, CRC/size/ratio abuse and non-UTF-8 items are accounted for and original evidence retained; cyclic hierarchy rolls back atomically. | These deterministic regressions are not an exhaustive security audit or maximum-size archive benchmark. |
| Crash recovery preserves committed content and recordings | `TestP0SIGKILLPreservesCommittedJourneyAndRestores` kills a separate process with an open uncommitted transaction and audio descriptor; committed data and synced audio survive, uncommitted changes do not, and backup/restore retains recoverable audio. | Physical power failure and external-drive removal are unverified. |
| Model acquisition handles invalid hashes, incompatible packs, interruption and low disk | Race tests verify rejection before publication, visible corrupt state and successful retry, retained partial bytes/ETag, Range/If-Range continuation, resumed full-download admission and mid-transfer disk exhaustion. | Local imports require explicit file reselection to retry; the app does not retain arbitrary source access. Final UI progress/status review pending. |
| Memory forgetting and source invalidation | Existing memory suites plus the public data journey reject forgotten, superseded and stale evidence, including during prepared generation and after restore. | Final native control interactions pending. |
| Native permissions, clipboard, worker lifecycle, audio, restart and restore | Earlier actual microphone capture and user-assisted file selection observed; final-candidate worker lifecycle, packaged restart and service restore pass. Clipboard/browser regressions pass. Native issues found during this journey have fixes with regressions. | Final interactive permissions, clipboard, actual audio, selected-workspace restart and restore controls require observations; service/browser fixtures alone cannot close those interactions. |
| Dependency/model hashes, licenses, migrations and rollback recorded | Current run's offline inventory reports `hashVerificationPassed: true`; notices are embedded in the built app. Exact source/license evidence and migration/rollback boundaries are documented. | Final executable and stable source/bundle fingerprints are recorded above. Installed payload equality and strict ad-hoc signature verification pass. Final ZIP digest will be in `release-artifacts.json`; public signing/notarization is an unverified distribution limit. |

Detailed proofs and limits are in [data-journey.md](data-journey.md),
[models-workers.md](models-workers.md), and
[release-provenance.md](release-provenance.md). The inventory removes the
unlicensed Go binding dependency from the product; the replacement registration
adapter uses the verified, byte-identical MIT sqlite-vec runtime. Workspace
format remains 1, SQLite `user_version` remains 2, and no in-place downgrade is
supported. Rollback restores a complete compatible backup into a new directory.

## Native findings and regressions

**Current native UI status: blocked pending Mac unlock.** Candidate 2 has not
completed the following interactive checks on its installed matching payload:

- Exact native clipboard copy/paste, formatted content save and subsequent
  reopen.
- Actual audio metadata, playback/seek, microphone permission, capture progress
  and stop/save behavior after the Blob playback change.
- Visible public-speech transcription progress and editing during that work.
- Native quit/restart with the selected workspace and persisted edited content,
  database state and recordings.
- Backup/restore controls and inspection of the restored workspace in the native
  UI. The prepared helper has not yet been run for this UI workspace.

Final-candidate UI search, model status/selection, assistant/source review and
memory controls also remain pending where indicated in the journey matrix.
The automated packaged restart/restore, offline inference and data-integrity
passes remain valid; they do not replace these specific interactive observations.

The first candidate's
[`native observations`](../../evidence/phase3/first-candidate/native-observations.json)
are retained against its original executable SHA-256
`0b7f3a3e5de1f2a0b9dedc790ee258c3a729de9af9a8a229ee9a3a90bc525705`.
These observations do not substitute for native UI confirmation on candidate 2:

| First-candidate native observation | Result and scope |
| --- | --- |
| Restarted imported content | Release notebook and Milestones survived the earlier quit/launch. |
| Page and database edits | Structural save no longer raised the missing-guard error. A typed `P0 native save checkpoint ORCHID-472` persisted; Milestones → Restore rehearsal → Estimate changed from 2 to 4. |
| Model-free keyword search | Native Search returned the original launch-code paragraph and newly typed checkpoint for `ORCHID-472`. |
| Model setup | CUA selected an unintended row and Open remained disabled. After the user said “you do it,” the agent used Trove's `Manager.Import` to import and verify the existing local BGE small English F16, Whisper tiny English and Qwen2.5 1.5B Q5 files into the normal model cache. This is service-assisted acquisition, not a native picker pass. |
| Local assistant | Native UI selected Qwen 1.5B and answered the Release notebook launch-code question with `ORCHID-472`. The UI disclosed absent model-supplied citations and displayed seven provided sources. This UI session was not network-denied; the separate actual-model tests establish offline execution. |
| Memory controls | Native controls moved the test preference through proposed, active and forgotten states. |
| Clipboard fidelity | Different clipboard content appeared instead of the intended CUA paste fixture, so exact fixture fidelity remained unverified. The paste was undone and saved; unrelated clipboard content is not retained in evidence. |
| Saved audio playback | A valid existing 69.59-second WAV displayed zero duration; Play changed to Pause but time did not advance. This was a real first-candidate failure. |

The verified pack hashes are retained in
[`local-model-import.log`](../../evidence/phase3/local-model-import.log).
Candidate 2 replaces the authorized recording fetch with a managed Blob URL for
the browser media element, with cancellation, error reporting and old-URL
release. Two browser tests verify metadata, play/seek, selection changes and
failed authorized fetches; both passed in its 24-test suite. Actual WKWebView
playback on candidate 2 remains pending.

The earlier native clipboard journey exposed a real save failure after splitting
an imported internal-link paragraph and pasting two formatted paragraphs:
`Missing revision guard for block:…`. Reordering a block list touches every
sibling in the canonical service, but the editor guarded only inserted IDs and
direct command targets. Unchanged siblings were omitted. The editor now derives
guards for every structurally touched sibling, parent and moved subtree using
accepted revisions. A browser regression performs repeated real document splits
and HTML clipboard pastes with bold text and Telugu, Chinese, Arabic and emoji,
saves twice, verifies unchanged imported neighbors and link text, checks advancing
guards, and reloads. It passed in the original 21-test suite and the second candidate's 24-test
browser stage. The earlier live draft was reverted by Undo and visibly saved
before the app was closed. **Final native clipboard/save/restart confirmation is
still pending.**

Restart investigation also found that selecting a workspace needed durable
application preference persistence. The candidate records the successful
selection atomically. `TestSelectedWorkspaceSurvivesRestartAndMissingDriveIsNotRecreated`
passed in the aggregate's non-native race suite, including preserved workspace
identity and an unavailable-drive case. This regression proves preference/service
behavior; the native quit/relaunch selection remains to be observed.

Earlier microphone evidence is retained in
[`native-microphone-metadata.json`](../../evidence/phase3/native-microphone-metadata.json).
It records actual growing capture, 4,453,756 journaled float32 bytes and 69 journal
segments, correct app identity for the OS permission request, and a valid PCM16
WAV header. No private audio samples were read for that metadata check. The
operator observed stale renderer progress during investigation; subsequent
duration/MIME/error-observation changes are in the candidate. That older evidence
does not prove the new renderer behavior, playback or final-candidate permission
flow. Ad-hoc rebuilds also change code requirements and may cause new macOS
permission prompts.

For the next native transcription interaction, an authorized disposable helper
imported a ten-minute repetition of the public JFK speech fixture through
`RegisterSelection` and `CreateMeetingFromSelection` into the normal test
workspace. The canonical meeting is titled `P0 public speech transcription`,
has duration 600,000 ms, and is captured but not yet transcribed at this
checkpoint. All preexisting canonical rows were verified unchanged and the
kernel was closed. See
[`native-public-audio-setup.json`](../../evidence/phase3/native-public-audio-setup.json).
This is service-assisted input setup, not a file-picker or native transcription
pass. No private microphone samples were read or transcribed. A separate
backup/restore helper is prepared but has **not been run** on this UI workspace.

## Performance acceptance

Hardware, storage, caches, workspace sizes, model files and raw samples are
defined in [performance.md](performance.md). The measured host is an Apple M4
Mac16,12 with 24 GiB unified memory, macOS 27.0, on AC power. Keyword fixtures have
100, 1,000 and 10,000 pages with two paragraphs per page and no model dependency.
Hybrid uses the verified BGE small English F16 pack, two CPU threads, 512-token
context and no reranker.

The first candidate's
[`shell benchmark`](../../evidence/phase3/first-candidate/shell-benchmark.json)
failed at 10,000 pages: three completed launches measured 3,089.61–3,370.18 ms,
and a fourth produced no usable-shell event. Empty, 100-page and 1,000-page
profiles stayed below one second. This failure is retained even though its
automated functional aggregate passed. Candidate 2 removes repeated full-page
and full-block scans during rendering: page relationships and block ownership
are indexed once per snapshot, the full page tree is memoized, and quick-switcher
body text is computed only for an open nonempty query. The new browser regression
preserves all eligible pages, record-page search, archived/deleted exclusion,
late body-only matches, navigation and refreshed content at 10,000 pages/20,000
blocks. The second candidate's native shell benchmark now passes all twenty
launches; the browser regression is additional coverage rather than its timing
substitute.

[`shell-benchmark-candidate-2.json`](../../evidence/phase3/shell-benchmark-candidate-2.json)
records five fresh application processes for each profile, no installed model
packs, OS network denial, an unchanged executable and no failed/missing readiness
events. OS filesystem caches were not flushed. The endpoint is Go process
initialization through the canonical snapshot being rendered plus two animation
frames, including native validation and Wails initialization. The harness defined
“approximately two seconds” as at most 2.5 seconds before this measurement.

| Pages / paragraph blocks | Launches | Median | Maximum |
| --- | ---: | ---: | ---: |
| 0 / 0 | 5 | 745.398 ms | 1,003.112 ms |
| 100 / 200 | 5 | 840.270 ms | 954.433 ms |
| 1,000 / 2,000 | 5 | 911.342 ms | 969.399 ms |
| 10,000 / 20,000 | 5 | 2,238.231 ms | 2,315.355 ms |

These measurements exclude installation/Gatekeeper and do not establish visible
editing responsiveness during background work.

| TRD target | Evidence at this checkpoint | Acceptance |
| --- | --- | --- |
| Usable application shell approximately 2 seconds | All 20 final-candidate native launches passed across 0/100/1,000/10,000 pages; 10,000-page median 2,238.231 ms and maximum 2,315.355 ms. Endpoint includes Go/native validation/Wails startup through snapshot render plus two animation frames. | **Pass under the predefined approximate threshold of 2,500 ms**, with exact samples retained; no strict 2,000 ms claim for the 10,000-page profile. |
| Common keyword queries under 300 ms | All 100 measured samples in each defined profile passed; maximum 38.31 ms at 10,000 pages. | **Pass for measured profiles**, not a universal latency guarantee. |
| Hybrid retrieval under 1 second, excluding optional reranking | Ten native BGE queries at 100 pages: maximum 239.45 ms with no reranker. | **Pass at 100 pages**; 1,000/10,000-page hybrid unmeasured. |
| No visible editing blocking during background work | Background append/conflict browser regressions pass. Final-candidate one-hour transcription committed 46 concurrent same-page edits; maximum service mutation 195.99 ms. | **Pending native visual responsiveness**; service timing does not measure keyboard-to-paint delay. |
| Transcription/import visible progress and recoverable interruption | Durable units/cursors and recovery pass; earlier capture produced growing persisted bytes. | **Recovery automated pass; final packaged progress UI pending.** |

## Completed automated aggregate

The second aggregate ran from 17:11:24 to 17:18:23 UTC on 2026-09-30. Every check passed,
with no source or candidate file changes. Completed stages are recorded in the
run directory:

- Contract/schema/ledger checks: **49 passed**.
- Non-native Go race suite: **122 top-level tests passed** (172 pass events
  including subtests), seven tested packages, no failed tests or skipped test
  cases. Two packages without tests are reported as package-level skips. Real
  native `TestReal*` cases are intentionally run in the separate offline stage.
- TypeScript: **passed**; codec/database unit tests: **10 passed**.
- Editor/browser suite: **24 passed**, including large-workspace navigation and
  two audio playback/lifecycle regressions; database browser suite: **3 passed**.
- Release inventory: **hash verification passed**; production candidate build:
  **completed**; OS network-denial control: **passed**.
- Actual final-candidate native race suite under OS network denial: **8
  top-level tests / 10 including subtests passed**, no unsuccessful or missing
  required tests. This includes `TestRealLocalAIWorkflow`, actual instruction
  generation, native runtime qualification, microphone cancellation and parent
  exit, worker cancellation/crash/Metal, durable transcription, reviewed meeting
  summaries and the one-hour same-page-edit fixture.
- Packaged `p0-create`, separate-process `p0-verify`, and `native`: **3 passed**,
  each process exited successfully under OS network denial and minimal PATH.
- Final source stability and candidate bundle stability: **passed**, both with
  zero changed paths and matching before/after fingerprints.

The final
[`packaged-integration.json`](../../evidence/phase3/runs/20260930T171124Z-c451f895/packaged-integration.json)
records three separate application launches, each exiting with code 0. Only the
first two are model-free:

| Packaged step | Model weights | Verified behavior |
| --- | --- | --- |
| `p0-create` | Separate empty cache; `installedWeightsAvailable: false` | Notion ZIP inventory and five-outcome reconciliation, imported page and database record edits, exact keyword search, forgotten/stale memory exclusion and retained interrupted recording. |
| `p0-verify` | Same empty cache; `installedWeightsAvailable: false` | Separate-process restart preserves the fixture; verified native backup restores into an unused workspace; restored canonical snapshot matches its source; restored search/memory remain correct and interrupted recording finalizes with its complete audio tail. |
| `native` | Verified installed packs; `installedWeightsAvailable: true` | Actual bundled runtime generation, one 384-dimensional embedding, public English speech transcription and 6,400 bytes of synthetic 16 kHz mono float32 capture. Synthetic capture does not exercise a physical microphone or permissions. |

The separate real-model test stage verifies Qwen instruction behavior and the
root application AI workflow; the packaged smoke step's TinyStories generation
checks runtime operation only. The
[`final one-hour result`](../../evidence/phase3/runs/20260930T171124Z-c451f895/native-one-hour-native.json)
records 117.77 seconds for all sixty durable transcription units, 694 segments
and 46 successful same-page edits. The source is repeated public speech, so this
is duration/lifecycle coverage rather than a natural-meeting accuracy claim.

## Remaining release decision

The automated, installation-integrity and shell timing checks are complete.
The **P0 release gate remains OPEN and unapproved** because the locked Mac
prevents completion of the final-candidate native UI journey. The pending unlock
request does not count as a completed check. Resume the remaining interactions
on the installed matching candidate when the user unlocks the Mac, and record
each result individually, including any assistance. The ZIP digest belongs in
`release-artifacts.json` once the archive is produced.

A separate clean-Mac installation and Developer ID signing/notarization remain
unverified distribution limitations, not additional user-required blockers by
themselves. Neither those limitations nor earlier candidate observations should
be confused with the specific unfinished native acceptance listed above. This
document does not approve a complete usable P0 release.
