package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

func gmailAssetFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	a, err := k.ConnectConnector(ctx, e, connectors.Account{Kind: connectors.Gmail, Identity: connectors.Identity{Provider: "google", Subject: "test-mail-account"}, RegistrationID: NewID()}, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err = k.SetConnectorSelections(ctx, e, a.ID, a.Revision, []connectors.Selection{{ID: "inbox", Kind: "label", Label: "Inbox"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := k.PrepareConnectorAcquired(ctx, e, a, "inbox", connectors.Acquired{Name: "note.txt", MediaType: "text/plain", Bytes: []byte("Mail attachment says launch code is CEDAR-49."), RemoteID: "gmail-attachment:exact-remote-key", RemoteRevision: "message-revision"})
	if err != nil {
		t.Fatal(err)
	}
	var prepared map[string]any
	json.Unmarshal(marshal(raw), &prepared)
	page := createPage(t, k, e, "Mail attachment")
	file, block := attachTestFile(t, k, e, page, prepared, true)
	return page, file, block
}
func externalPolicyGrant(k *Kernel, e contracts.ServiceExecutionContext) contracts.ServiceExecutionContext {
	return k.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(NewID())}, "external", e.Scope)
}
func requireExternalPolicyDenied(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page, file string) {
	t.Helper()
	ctx := context.Background()
	if err := k.authorizePage(ctx, k.db, e, page); err == nil {
		t.Error("Gmail-derived page admitted to external AI", page)
	}
	if file != "" {
		if f, _, err := k.ReadFile(ctx, e, file); err == nil {
			f.Close()
			t.Error("Gmail-derived file admitted to external AI", file)
		}
	}
}
func TestGmailAcquiredAssetPolicyFollowsSharedMovedAndRestoredContent(t *testing.T) {
	k, owner := testKernel(t)
	ctx := context.Background()
	page, file, block := gmailAssetFixture(t, k, owner)
	external := externalPolicyGrant(k, owner)
	defer k.RevokeGrant(string(external.ScopeGrantId))
	requireExternalPolicyDenied(t, k, external, page, file)
	clean := createPage(t, k, owner, "Unrelated allowed page")
	if err := k.authorizePage(ctx, k.db, external, clean); err != nil {
		t.Fatal("unrelated content restricted", err)
	}
	shared := createPage(t, k, owner, "Shared bytes")
	same := stageTestFile(t, k, owner, "Mail attachment says launch code is CEDAR-49.")
	sharedFile, _ := attachTestFile(t, k, owner, shared, same, true)
	if sharedFile != file {
		t.Fatal("deduplication fixture failed")
	}
	requireExternalPolicyDenied(t, k, external, shared, file)
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, "page", page, "block", block))
	requireExternalPolicyDenied(t, k, external, shared, file)
	testOK(t, restoreTest(t, k, owner, page, "2"))
	requireExternalPolicyDenied(t, k, external, page, file)
	// Native backups retain the restriction even though connector activation is inert.
	backup, dest := filepath.Join(t.TempDir(), "backup"), filepath.Join(t.TempDir(), "restored")
	manifest, err := k.CreateBackup(ctx, owner, backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = RestoreBackupReviewed(ctx, backup, dest, manifest); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var hash string
	if err = restored.db.QueryRow("SELECT content_hash FROM files WHERE id=?", file).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	sources, err := readExternalAcquisitionSources(ctx, restored.db, hash)
	if err != nil || len(sources) != 1 || sources[0].RemoteID != "gmail-attachment:exact-remote-key" || sources[0].RemoteRevision != "message-revision" {
		t.Fatal("native restore lost exact source evidence", sources, err)
	}
	re := externalPolicyGrant(restored, restored.OwnerContext())
	requireExternalPolicyDenied(t, restored, re, page, file)
	if f, _, err := restored.ReadFile(ctx, restored.OwnerContext(), file); err != nil {
		t.Fatal("owner access lost", err)
	} else {
		f.Close()
	}
}
func TestGmailDerivedMemoryPageCannotLaunderSourcePolicy(t *testing.T) {
	k, owner := testKernel(t)
	page, _, block := gmailAssetFixture(t, k, owner)
	memoryPage, memory := createPage(t, k, owner, "Approved mail fact"), NewID()
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "memory.propose", "memoryId": memory, "pageId": memoryPage, "scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}, "memoryKind": "preference", "content": "Launch code is CEDAR-49.", "sources": []any{map[string]any{"pageId": page, "sourceRevision": "2"}}}}, "page", memoryPage, "memory", memory))
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "memory.approve", "memoryId": memory}}, "page", memoryPage, "memory", memory))
	external := externalPolicyGrant(k, owner)
	defer k.RevokeGrant(string(external.ScopeGrantId))
	requireExternalPolicyDenied(t, k, external, memoryPage, "")
	entries, err := readMemories(context.Background(), k, k.db, external, false)
	if err != nil || len(entries) != 0 {
		t.Fatal("mail memory leaked into external context", len(entries), err)
	}
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, "page", page, "block", block))
	requireExternalPolicyDenied(t, k, external, memoryPage, "")
	entries, err = readMemories(context.Background(), k, k.db, external, true)
	if err != nil || len(entries) != 0 {
		t.Fatal("source cleanup exposed retained stale memory", len(entries), err)
	}
}

func TestGmailPolicyPortableRoundtripAndReviewedDerivedPage(t *testing.T) {
	k, owner := testKernel(t)
	ctx := context.Background()
	page, file, _ := gmailAssetFixture(t, k, owner)
	fact := NewID()
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": fact, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Launch code is CEDAR-49."}}}}}}, "page", page, "block", fact))
	prepared, err := k.PrepareAIWithBudget(ctx, owner, AIRequest{Mode: "answer", PageID: page, Prompt: "What is the launch code?"}, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.FinishAI(ctx, owner, prepared.Run.ID, "Launch code is CEDAR-49. [1]", "completed", ""); err != nil {
		t.Fatal(err)
	}
	raw, err := prepareAIAction(ctx, k, owner, map[string]any{"runId": prepared.Run.ID, "kind": "create_page", "title": "Derived mail fact", "body": "Launch code is CEDAR-49."})
	if err != nil {
		t.Fatal(err)
	}
	proposal := raw.(AIProposal)
	if _, err = applyAIAction(ctx, k, owner, map[string]any{"proposalId": proposal.ID}); err != nil {
		t.Fatal(err)
	}
	external := externalPolicyGrant(k, owner)
	requireExternalPolicyDenied(t, k, external, proposal.PageID, "")
	// A later source edit cannot clear the derived copy's provenance.
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, "page", page))
	requireExternalPolicyDenied(t, k, external, proposal.PageID, "")
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.update", "pageId": proposal.PageID, "title": "Edited derived page"}}, "page", proposal.PageID))
	testOK(t, restoreTest(t, k, owner, proposal.PageID, "1"))
	requireExternalPolicyDenied(t, k, external, proposal.PageID, "")
	// Keep an independent visible relationship to the same acquired content.
	shared := createPage(t, k, owner, "Shared exported attachment")
	attachTestFile(t, k, owner, shared, stageTestFile(t, k, owner, "Mail attachment says launch code is CEDAR-49."), true)
	meta, err := readFileMetadata(ctx, k.db, file)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		entries := exportPrivacyEntries(t, k, owner, false)
		var manifest portableHierarchy
		if err = json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest); err != nil {
			t.Fatal(err)
		}
		policies := 0
		for _, a := range manifest.Attachments {
			if a.SHA256 == string(meta.ContentHash) && a.ExternalContextPolicy != nil {
				policies++
			}
		}
		sources, sourceErr := readExternalAcquisitionSources(ctx, k.db, string(meta.ContentHash))
		if sourceErr != nil || len(sources) != 1 || sources[0].RemoteID != "gmail-attachment:exact-remote-key" || sources[0].RemoteRevision != "message-revision" {
			t.Fatal("source evidence lost before portable roundtrip", sources, sourceErr)
		}
		if policies != 1 {
			t.Fatal("attachment policy omitted or duplicated", policies)
		}
		target, te := testKernel(t)
		plan := completeImportFixture(t, target, te, importFixture(t, target, te, entries))
		derived := importedPageNamed(t, plan, "Derived mail fact")
		shared := importedPageNamed(t, plan, "Shared exported attachment")
		var importedFile string
		if err = target.db.QueryRow("SELECT id FROM files WHERE content_hash=?", meta.ContentHash).Scan(&importedFile); err != nil {
			t.Fatal(err)
		}
		ext := externalPolicyGrant(target, te)
		requireExternalPolicyDenied(t, target, ext, derived.PageID, "")
		requireExternalPolicyDenied(t, target, ext, shared.PageID, importedFile)
		k, owner = target, te
	}
}
func TestGmailPolicyCanonicalAcquisitionAndImportContainer(t *testing.T) {
	k, owner := testKernel(t)
	ctx := context.Background()
	a, err := k.ConnectConnector(ctx, owner, connectors.Account{Kind: connectors.Gmail, Identity: connectors.Identity{Provider: "google", Subject: "mail"}, RegistrationID: NewID()}, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err = k.SetConnectorSelections(ctx, owner, a.ID, a.Revision, []connectors.Selection{{ID: "inbox", Kind: "label", Label: "Inbox"}})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, _ := zw.Create("Mail.md")
	w.Write([]byte("# Imported mail\n\nMail-derived launch fact."))
	w, _ = zw.Create("Dates.csv")
	w.Write([]byte("Name,When\nMail task,2031-04-21\n"))
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	copyAccount := a
	copyAccount.Kind = connectors.GoogleDrive
	raw, err := k.PrepareConnectorAcquired(ctx, owner, copyAccount, "inbox", connectors.Acquired{Name: "mail.zip", Bytes: archive.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	var prepared map[string]any
	json.Unmarshal(marshal(raw), &prepared)
	page := createPage(t, k, owner, "Downloaded container")
	file, _ := attachTestFile(t, k, owner, page, prepared, true)
	meta, err := readFileMetadata(ctx, k.db, file)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := k.RegisterSelection(filepath.Join(k.Path(), meta.StorageKey))
	if err != nil {
		t.Fatal(err)
	}
	// Native selection preserves identity by open handle; its basename is the
	// content hash, so name the selected archive as the trusted original name.
	state := k.recoveryState()
	state.mu.Lock()
	sel := state.selections[handle]
	sel.name = "mail.zip"
	state.selections[handle] = sel
	state.mu.Unlock()
	preview, err := previewPortableImport(ctx, k, owner, map[string]any{"sourceHandle": handle, "sourceNamespace": "mail-archive"})
	if err != nil {
		t.Fatal(err)
	}
	plan := completeImportFixture(t, k, owner, preview.(ImportPlan))
	external := externalPolicyGrant(k, owner)
	checked := 0
	for _, item := range plan.Items {
		if item.PageID != "" {
			requireExternalPolicyDenied(t, k, external, item.PageID, item.RawFileID)
			checked++
		}
	}
	if checked < 3 {
		t.Fatal("container/CSV derived fixtures absent", checked)
	}
	snapshot, err := k.StoreConnectorPreview(ctx, owner, a, "inbox", connectors.Snapshot{RemoteID: "thread", Title: "Mail thread", Text: "Canonical mail fact", RemoteRevision: "rev", Content: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := k.PreviewConnectorImport(ctx, owner, snapshot.ID, snapshot.RemoteRevision)
	if err != nil {
		t.Fatal(err)
	}
	imported = completeImportFixture(t, k, owner, imported)
	requireExternalPolicyDenied(t, k, external, imported.Items[0].PageID, imported.SourceFileID)
}
func TestGmailPolicyUnknownPortableVersionAndHashRejected(t *testing.T) {
	for _, kind := range []string{"version", "hash", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			k, owner := testKernel(t)
			gmailAssetFixture(t, k, owner)
			entries := exportPrivacyEntries(t, k, owner, false)
			var manifest map[string]any
			json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest)
			asset := manifest["attachments"].([]any)[0].(map[string]any)
			if kind == "version" {
				asset["externalContextPolicy"].(map[string]any)["version"] = 2
			} else {
				asset["sha256"] = strings.Repeat("0", 64)
			}
			entries["trove-manifest.json"] = string(marshal(manifest))
			if kind == "malformed" {
				entries["trove-manifest.json"] = `{"format":"trove-portable",`
			}
			var archive bytes.Buffer
			zw := zip.NewWriter(&archive)
			for name, body := range entries {
				w, err := zw.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				w.Write([]byte(body))
			}
			zw.Close()
			target, te := testKernel(t)
			path := filepath.Join(t.TempDir(), "bad.zip")
			if err := os.WriteFile(path, archive.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			handle, err := target.RegisterSelection(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = previewPortableImport(context.Background(), target, te, map[string]any{"sourceHandle": handle, "sourceNamespace": "bad"}); err == nil {
				t.Fatal("restricted archive metadata silently weakened")
			}
		})
	}
}

func TestGmailAutofillAndMeetingTaskRetainRestrictedSources(t *testing.T) {
	k, owner := testKernel(t)
	ctx := context.Background()
	_, file, _ := gmailAssetFixture(t, k, owner)
	attach := func(page, record string) string {
		block := NewID()
		guards := []string{"page", page, "block", block}
		if record != "" {
			var collection string
			if err := k.db.QueryRow("SELECT collection_id FROM records WHERE id=?", record).Scan(&collection); err != nil {
				t.Fatal(err)
			}
			guards = append(guards, "record", record, "collection", collection)
		}
		testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "file", "schemaVersion": 1, "data": map[string]any{"fileId": file, "caption": []any{}, "displayName": "mail.txt"}}}}, guards...))
		return block
	}
	f := newDatabase(t, k, owner)
	record, page := newRecord(t, k, owner, f, "Due 2026-10-20")
	block := attach(page, record)
	b := autofillFixture(t, k, owner, f, []string{record}, []string{f.date}, false)
	b = autofillGenerate(t, k, owner, b.ID, autofillDateGenerator)
	testOK(t, autofillApplyFixture(t, k, owner, b, []string{b.Cells[0].ID}, []string{}))
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, "page", page, "record", record, "collection", f.collection, "block", block))
	external := externalPolicyGrant(k, owner)
	requireExternalPolicyDenied(t, k, external, page, "")
	meeting, source := extractionFixture(t, k, owner)
	block = attach(source.PageID, "")
	x, err := k.prepareMeetingExtraction(ctx, owner, recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "task"}))
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, owner, "meetings.createFromTranscript", extractionApproval(k, x, "Mail-derived task")))
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, "page", source.PageID, "block", block))
	requireExternalPolicyDenied(t, k, external, x.PageID, "")
	var collectionPage string
	if err = k.db.QueryRow("SELECT c.owning_page_id FROM records r JOIN collections c ON c.id=r.collection_id WHERE r.id=?", x.RecordID).Scan(&collectionPage); err != nil {
		t.Fatal(err)
	}
	if err = k.authorizePage(ctx, k.db, external, collectionPage); err != nil {
		t.Fatal("unrelated collection metadata restricted", err)
	}
}
func TestGmailAcquisitionGuardFailureIsAtomic(t *testing.T) {
	k, owner := testKernel(t)
	ctx := context.Background()
	data := []byte("a revoked acquired file")
	path := filepath.Join(t.TempDir(), "revoked.txt")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := k.RegisterSelection(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = prepareFileWithPolicy(ctx, k, owner, map[string]any{"selectionHandle": handle}, &gmailExternalContextPolicy, func(tx *sql.Tx, hash string) error {
		if err := retainExternalAcquisitionSource(ctx, tx, hash, externalAcquisitionSource{Version: 1, Provider: "gmail", AccountID: NewID(), SelectionID: "inbox"}); err != nil {
			return err
		}
		return Fail("SCOPE_DENIED", "Acquisition revoked during I/O")
	})
	if err == nil {
		t.Fatal("revoked acquisition succeeded")
	}
	for _, table := range []string{"file_external_context_policy", "file_external_context_sources", "file_ingests"} {
		var n int
		if err = k.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("failed acquisition left metadata", table, n, err)
		}
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	if _, err = os.Stat(filepath.Join(k.Path(), "files", hash[:2], hash)); !os.IsNotExist(err) {
		t.Fatal("failed acquisition published content", err)
	}
}
func TestGmailRecoveryMetadataVersionAndPolicyValidation(t *testing.T) {
	for _, version := range []int{6, 7, 8, -1} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			k, owner := testKernel(t)
			page, _, _ := gmailAssetFixture(t, k, owner)
			ctx := context.Background()
			var raw string
			if err := k.db.QueryRow("SELECT snapshot_json FROM page_snapshots WHERE page_id=? AND revision='2'", page).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var snap map[string]any
			if err := json.Unmarshal([]byte(raw), &snap); err != nil {
				t.Fatal(err)
			}
			if int(snap["recoveryMetadataVersion"].(float64)) != currentRecoveryMetadataVersion {
				t.Fatal("current snapshot version not emitted")
			}
			snap["recoveryMetadataVersion"] = version
			if _, err := k.db.Exec("UPDATE page_snapshots SET snapshot_json=? WHERE page_id=? AND revision='2'", string(marshal(snap)), page); err != nil {
				t.Fatal(err)
			}
			_, _, err := loadRecoverySnapshot(ctx, k.db, page, "2")
			if (version < 0 || version > currentRecoveryMetadataVersion) != (err != nil) {
				t.Fatal("unsupported history metadata admitted or compatible metadata denied", version, err)
			}
		})
	}
}
func TestGmailRestoreRejectsUnknownPolicyShapesAndRows(t *testing.T) {
	for _, scenario := range []string{"shape", "trigger", "version", "policy", "row_bounds", "source_digest", "source_without_policy", "missing_optional"} {
		t.Run(scenario, func(t *testing.T) {
			k, _ := testKernel(t)
			ctx := context.Background()
			err := k.write(ctx, func(tx *sql.Tx) error {
				var err error
				switch scenario {
				case "shape":
					_, err = tx.Exec("DROP TABLE file_external_context_policy; CREATE TABLE file_external_context_policy(content_hash TEXT)")
				case "trigger":
					_, err = tx.Exec("CREATE TRIGGER policy_trigger AFTER INSERT ON file_external_context_policy BEGIN DELETE FROM page_external_context_policy; END")
				case "version":
					_, err = tx.Exec("UPDATE kernel_meta SET value='2' WHERE key='externalContextPolicyVersion'")
				case "policy":
					_, err = tx.Exec("INSERT INTO file_external_context_policy VALUES(?,?,?,?)", strings.Repeat("a", 64), "gmail", 1, `{"version":2,"provider":"gmail","reason":"external_ai_unqualified"}`)
				case "row_bounds":
					_, err = tx.Exec("INSERT INTO file_external_context_policy VALUES(?,?,?,?)", strings.Repeat("a", 64), "gmail", 1, string(marshal(map[string]any{"version": 1, "provider": "gmail", "reason": "external_ai_unqualified", "extra": strings.Repeat("x", 2048)})))
				case "source_without_policy":
					raw := string(marshal(externalAcquisitionSource{Version: 1, Provider: "gmail", AccountID: NewID(), SelectionID: "inbox"}))
					digest := sha256.Sum256([]byte(raw))
					_, err = tx.Exec("INSERT INTO file_external_context_sources VALUES(?,?,?)", strings.Repeat("a", 64), hex.EncodeToString(digest[:]), raw)
				case "source_digest":
					_, err = tx.Exec("INSERT INTO file_external_context_sources VALUES(?,?,?)", strings.Repeat("a", 64), strings.Repeat("b", 64), string(marshal(externalAcquisitionSource{Version: 1, Provider: "gmail", AccountID: NewID(), SelectionID: "inbox"})))
				case "missing_optional":
					for name := range externalContextTables {
						if _, err = tx.Exec("DROP TABLE " + name); err != nil {
							return err
						}
					}
				}
				if err != nil {
					return err
				}
				err = validateExternalContextRestore(ctx, tx)
				if scenario == "missing_optional" {
					if err != nil {
						t.Error("legacy backup rejected", err)
					}
				} else if err == nil {
					t.Error("unsafe policy restore accepted", scenario)
				}
				return Fail("CANCELLED", "discard test mutation")
			})
			if err == nil {
				t.Fatal("fixture did not roll back")
			}
		})
	}
}

func TestGmailTranscriptAndMeetingSummarySurviveAudioCleanupPolicy(t *testing.T) {
	for _, kind := range []string{"transcript", "summary"} {
		t.Run(kind, func(t *testing.T) {
			k, owner := testKernel(t)
			ctx := context.Background()
			page, meeting, file := meetingFixture(t, k, owner)
			restrict := func() {
				t.Helper()
				if err := k.write(ctx, func(tx *sql.Tx) error {
					var hash string
					if err := tx.QueryRow("SELECT content_hash FROM files WHERE id=?", file).Scan(&hash); err != nil {
						return err
					}
					return retainFileExternalContextPolicy(ctx, tx, hash, gmailExternalContextPolicy)
				}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "transcript" {
				restrict()
			}
			enqueueTestJob(t, k, owner, "TRANSCRIBE_AUDIO", map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()})
			job, err := k.ClaimJob(ctx, []string{"TRANSCRIBE_AUDIO"})
			if err != nil {
				t.Fatal(err)
			}
			execution, err := k.JobExecution(ctx, string(job.JobId))
			if err != nil {
				t.Fatal(err)
			}
			result := k.CommitJobBatch(ctx, execution, string(job.JobId), string(*job.LeaseToken), "mail-transcript", jobTranscriptRequest(t, k, execution, job, meeting))
			if failure, ok := result.Value.(contracts.ServiceBatchResponseVariant2); ok {
				t.Fatal(failure.Error)
			}
			finishMeetingFixtureJobs(t, k)
			if kind == "summary" {
				restrict()
				source, err := k.MeetingSource(ctx, owner, meeting)
				if err != nil {
					t.Fatal(err)
				}
				enqueueTestJob(t, k, owner, "SUMMARIZE_MEETING", map[string]any{"kind": "summarize", "meetingId": meeting, "sourceRevision": source.PageRevision, "modelPackId": NewID()})
				job, err = k.ClaimJob(ctx, []string{"SUMMARIZE_MEETING"})
				if err != nil {
					t.Fatal(err)
				}
				execution, err = k.JobExecution(ctx, string(job.JobId))
				if err != nil {
					t.Fatal(err)
				}
				claim := MeetingClaim{Text: "Mail-derived summary", SegmentIDs: []string{source.Segments[0].SegmentID}}
				if err = k.SaveMeetingDraft(ctx, execution, string(job.JobId), string(*job.LeaseToken), source, MeetingIntelligence{Summary: []MeetingClaim{claim}}); err != nil {
					t.Fatal(err)
				}
				finishMeetingFixtureJobs(t, k)
				drafts, err := k.MeetingDrafts(ctx, owner, meeting)
				if err != nil || len(drafts) != 1 {
					t.Fatal(drafts, err)
				}
				if _, err = k.ApplyMeetingDraft(ctx, owner, drafts[0].ID); err != nil {
					t.Fatal(err)
				}
			}
			preview := retentionPreviewTest(t, k, owner, meeting, "audio")
			if !preview.CanApply {
				t.Fatal("audio cleanup fixture blocked", preview)
			}
			testOK(t, testDispatch(t, k, owner, "batch", testRequest(k, owner, []any{preview.Command}, preview.ExpectedRevisions...)))
			var n int
			if err = k.db.QueryRow("SELECT COUNT(*) FROM file_references WHERE page_id=? AND file_id=?", page, file).Scan(&n); err != nil || n != 0 {
				t.Fatal("audio reference was not removed", n, err)
			}
			requireExternalPolicyDenied(t, k, externalPolicyGrant(k, owner), page, "")
		})
	}
}
