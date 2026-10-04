package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"trove.local/phase0/contracts"
	"trove.local/phase0/schemas"
)

func enqueueTestJob(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, kind string, payload map[string]any) contracts.JobsJob {
	t.Helper()
	result := testOK(t, testDispatch(t, k, e, "enqueueJob", recoveryRequest(k, map[string]any{"kind": kind, "payload": payload, "idempotencyKey": NewID()})))
	var j contracts.JobsJob
	if err := json.Unmarshal(marshal(result), &j); err != nil {
		t.Fatal(err)
	}
	return j
}
func meetingFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) (page, meeting, file string) {
	t.Helper()
	page = createPage(t, k, e, "Recording")
	prepared := stageTestFile(t, k, e, "audio bytes for service fixture")
	file, _ = attachTestFile(t, k, e, page, prepared, false)
	meeting = NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "meeting.create", "meetingId": meeting, "pageId": page, "recordingFileId": file, "startedAt": now()}}, "meeting", meeting, "page", page, "file", file))
	return
}
func jobTranscriptRequest(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, j contracts.JobsJob, meeting string) contracts.ServiceBatchRequest {
	t.Helper()
	guards, err := k.JobSourceRevisions(context.Background(), string(j.JobId))
	if err != nil {
		t.Fatal(err)
	}
	id := NewID()
	guards = append(guards, required("block", id, "0"))
	raw := testRequest(k, e, []any{map[string]any{"kind": "meeting.appendSegments", "meetingId": meeting, "segments": []any{map[string]any{"segmentId": NewID(), "blockId": id, "startMs": 0, "endMs": 500, "text": "Durable transcript"}}}, map[string]any{"kind": "meeting.finalize", "meetingId": meeting, "durationMs": 1000}}, guards...)
	r, err := contracts.DecodeBatch(marshal(raw))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestJobUnitReceiptAtomicFenceAndSourceConflict(t *testing.T) {
	k, owner := testKernel(t)
	page, meeting, file := meetingFixture(t, k, owner)
	job := enqueueTestJob(t, k, owner, "TRANSCRIBE_AUDIO", map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()})
	j, err := k.ClaimJob(context.Background(), []string{"TRANSCRIBE_AUDIO"})
	if err != nil {
		t.Fatal(err)
	}
	if j.JobId != job.JobId {
		t.Fatal(j)
	}
	e, err := k.JobExecution(context.Background(), string(j.JobId))
	if err != nil {
		t.Fatal(err)
	}
	r := jobTranscriptRequest(t, k, e, j, meeting)
	out := k.CommitJobBatch(context.Background(), e, string(j.JobId), string(*j.LeaseToken), "transcript-v1", r)
	var parsed map[string]any
	json.Unmarshal(marshal(out), &parsed)
	testOK(t, parsed)
	j2, _ := k.ReadJob(context.Background(), string(j.JobId))
	if j2.State != "succeeded" {
		t.Fatal(j2)
	}
	out = k.CommitJobBatch(context.Background(), e, string(j.JobId), "expired", "transcript-v1", r)
	json.Unmarshal(marshal(out), &parsed)
	if testOK(t, parsed)["replayed"] != true {
		t.Fatal(parsed)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM transcript_segments WHERE meeting_id=?", meeting).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	k.db.QueryRow("SELECT count(*) FROM job_results WHERE job_id=?", j.JobId).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	cancelResult := testOK(t, testDispatch(t, k, owner, "cancelJob", recoveryRequest(k, map[string]any{"jobId": j.JobId, "expectedRevision": j2.Revision})))
	if cancelResult["state"] != "succeeded" {
		t.Fatal(cancelResult)
	}
	next := enqueueTestJob(t, k, owner, "TRANSCRIBE_AUDIO", map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()})
	j, err = k.ClaimJob(context.Background(), nil)
	if err != nil || j.JobId != next.JobId {
		t.Fatal(j, err)
	}
	e, _ = k.JobExecution(context.Background(), string(j.JobId))
	r = jobTranscriptRequest(t, k, e, j, meeting)
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "edited during transcription"}}, "page", page))
	out = k.CommitJobBatch(context.Background(), e, string(j.JobId), string(*j.LeaseToken), "transcript-v1", r)
	json.Unmarshal(marshal(out), &parsed)
	testCode(t, parsed, "CONFLICT")
	k.db.QueryRow("SELECT count(*) FROM transcript_segments WHERE meeting_id=?", meeting).Scan(&count)
	if count != 1 {
		t.Fatal("source conflict wrote transcript")
	}
}
func TestJobCannotBypassApprovalAndDelegationRevocation(t *testing.T) {
	k, owner := testKernel(t)
	for _, principal := range []struct{ kind, origin string }{{"local_ai", "ai"}, {"external_client", "external"}, {"importer", "import"}} {
		e := k.Grant(contracts.ServiceActor{Kind: principal.kind, ActorId: contracts.DomainId(NewID())}, principal.origin, owner.Scope)
		r := recoveryRequest(k, map[string]any{"kind": "BACKUP_WORKSPACE", "payload": map[string]any{"kind": "backup", "destinationHandle": NewID()}, "idempotencyKey": NewID()})
		testCode(t, testDispatch(t, k, e, "enqueueJob", r), "APPROVAL_REQUIRED")
	}
	j := enqueueTestJob(t, k, owner, "BACKUP_WORKSPACE", map[string]any{"kind": "backup", "destinationHandle": NewID()})
	k.RevokeGrant(string(owner.ScopeGrantId))
	state, _ := k.ReadJob(context.Background(), string(j.JobId))
	if state.State != "cancelled" {
		t.Fatal(state)
	}
	if _, err := k.JobExecution(context.Background(), string(j.JobId)); err == nil {
		t.Fatal("revoked delegation recreated")
	}
}
func TestNestedJobLocationsAndProjectScope(t *testing.T) {
	k, owner := testKernel(t)
	projectPage := createPage(t, k, owner, "project")
	member := createPage(t, k, owner, "member")
	child := NewID()
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.create", "pageId": child, "title": "unlisted child", "parentPageId": member}}, "page", child, "page", member))
	project := NewID()
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "container.create", "containerId": project, "owningPageId": projectPage, "label": "project", "containerKind": "project", "parentProjectId": nil}}, "container", project, "page", projectPage))
	testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "project.setScope", "projectId": project, "memberPageIds": []any{member}, "pinnedPageIds": []any{}, "excludedPageIds": []any{}, "inheritAncestorPreferences": false, "inheritWorkspacePreferences": false}}, "container", project))
	e := k.Grant(owner.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant2{Kind: "project", WorkspaceId: contracts.DomainId(k.ID()), ProjectId: contracts.DomainId(project)}})
	if err := k.authorizePage(context.Background(), k.db, e, projectPage); err != nil {
		t.Fatal(err)
	}
	if err := k.authorizePage(context.Background(), k.db, e, member); err != nil {
		t.Fatal(err)
	}
	if err := k.authorizePage(context.Background(), k.db, e, child); err == nil {
		t.Fatal("page tree ancestry widened scope")
	}
	r := recoveryRequest(k, map[string]any{"kind": "GENERATE_EMBEDDINGS", "payload": map[string]any{"kind": "embedding", "locations": []any{map[string]any{"pageId": child, "sourceRevision": "1"}}, "modelPackId": NewID(), "dimension": 384}, "idempotencyKey": NewID()})
	testCode(t, testDispatch(t, k, e, "enqueueJob", r), "SCOPE_DENIED")
	r = recoveryRequest(k, map[string]any{"kind": "BACKUP_WORKSPACE", "payload": map[string]any{"kind": "backup", "destinationHandle": NewID()}, "idempotencyKey": NewID()})
	testCode(t, testDispatch(t, k, e, "enqueueJob", r), "SCOPE_DENIED")
}
func TestCancellationCommitLinearizationAndReplay(t *testing.T) {
	k, e := testKernel(t)
	for i := 0; i < 20; i++ {
		id, cancelID := NewID(), NewID()
		r := testRequest(k, e, []any{map[string]any{"kind": "page.create", "pageId": id, "title": "race"}}, required("page", id, "0"))
		r["cancellationId"] = cancelID
		var mutation, cancellation map[string]any
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); mutation = testDispatch(t, k, e, "batch", r) }()
		go func() {
			defer wg.Done()
			cancellation = testDispatch(t, k, e, "cancel", recoveryRequest(k, map[string]any{"cancellationId": cancelID}))
		}()
		wg.Wait()
		cancelResult := testOK(t, cancellation)
		var count int
		k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", id).Scan(&count)
		if count == 1 {
			if cancelResult["state"] != "already_committed" || mutation["ok"] != true {
				t.Fatal(mutation, cancellation)
			}
			testOK(t, testDispatch(t, k, e, "batch", r))
		} else {
			testCode(t, mutation, "CANCELLED")
			if cancelResult["state"] != "cancelled" {
				t.Fatal(cancellation)
			}
		}
	}
}
func TestMigrationFailureRollsBackWholeSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fault.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	base, _ := schemas.Files.ReadFile("workspace.sql")
	services, _ := schemas.Files.ReadFile("services.sql")
	err = applySchemaScripts(db, string(base), string(services), kernelMigration, "CREATE TABLE injected_invalid (")
	if err == nil {
		t.Fatal("fault accepted")
	}
	db.Close()
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, version int
	db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&count)
	db.QueryRow("PRAGMA user_version").Scan(&version)
	if count != 0 || version != 0 {
		t.Fatal("partial schema survived", count, version)
	}
	if err = applySchemaScripts(db, string(base), string(services), kernelMigration); err != nil {
		t.Fatal(err)
	}
}
func TestRunJobsContinuesAfterFailureAndStopsCleanly(t *testing.T) {
	k, e := testKernel(t)
	one := enqueueTestJob(t, k, e, "BACKUP_WORKSPACE", map[string]any{"kind": "backup", "destinationHandle": NewID()})
	two := enqueueTestJob(t, k, e, "BACKUP_WORKSPACE", map[string]any{"kind": "backup", "destinationHandle": NewID()})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- k.RunJobs(ctx, map[string]func(context.Context, contracts.JobsJob) error{"BACKUP_WORKSPACE": func(ctx context.Context, j contracts.JobsJob) error {
			if j.JobId == one.JobId {
				return Fail("INVALID_REQUEST", "fixture invalid input")
			}
			return nil
		}})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		j, _ := k.ReadJob(context.Background(), string(two.JobId))
		if j.State == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("next job stalled")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	j, _ := k.ReadJob(context.Background(), string(one.JobId))
	if j.State != "failed" || j.Attempt != 1 {
		t.Fatal(j)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM job_events WHERE job_id=?", one.JobId).Scan(&count)
	if count == 0 {
		t.Fatal("terminal failure not durable")
	}
}

func TestBackupDestinationJobAndLostAcknowledgement(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "backup job fixture")
	prepared := stageTestFile(t, k, e, "retained backup asset")
	attachTestFile(t, k, e, page, prepared, true)
	destination := filepath.Join(t.TempDir(), "backup")
	handle, err := k.RegisterBackupDestination(context.Background(), e, destination)
	if err != nil {
		t.Fatal(err)
	}
	j := enqueueTestJob(t, k, e, "BACKUP_WORKSPACE", map[string]any{"kind": "backup", "destinationHandle": handle})
	claimed, err := k.ClaimJob(context.Background(), []string{"BACKUP_WORKSPACE"})
	if err != nil || claimed.JobId != j.JobId {
		t.Fatal(err)
	}
	if err = runBackupJob(context.Background(), k, claimed); err != nil {
		t.Fatal(err)
	}
	manifest, err := VerifyBackup(context.Background(), destination)
	if err != nil || len(manifest.Assets) != 1 {
		t.Fatal(manifest, err)
	}
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	claimed, err = k.ClaimJob(context.Background(), []string{"BACKUP_WORKSPACE"})
	if err != nil {
		t.Fatal(err)
	}
	if err = runBackupJob(context.Background(), k, claimed); err != nil {
		t.Fatal("completed backup did not reconcile", err)
	}
	if err = k.FinishJob(context.Background(), string(claimed.JobId), string(*claimed.LeaseToken), nil, false); err != nil {
		t.Fatal(err)
	}
}
func TestJobInputRevisionAndCustomRequestSchemas(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "source")
	r := recoveryRequest(k, map[string]any{"kind": "INDEX_CONTENT", "payload": map[string]any{"kind": "index", "location": map[string]any{"pageId": page, "sourceRevision": "99"}, "generationId": NewID()}, "idempotencyKey": NewID()})
	testCode(t, testDispatch(t, k, e, "enqueueJob", r), "CONFLICT")
	for method, extra := range map[string]map[string]any{"workspace.snapshot": {"unknown": true}, "links.backlinks": {"pageId": page, "sql": "SELECT *"}, "jobs.list": {"limit": 1000000}} {
		testCode(t, testDispatch(t, k, e, method, recoveryRequest(k, extra)), "INVALID_REQUEST")
	}
}

func TestRunJobsShutdownFencesUnfinishedAttempt(t *testing.T) {
	k, e := testKernel(t)
	j := enqueueTestJob(t, k, e, "BACKUP_WORKSPACE", map[string]any{"kind": "backup", "destinationHandle": NewID()})
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- k.RunJobs(ctx, map[string]func(context.Context, contracts.JobsJob) error{"BACKUP_WORKSPACE": func(ctx context.Context, j contracts.JobsJob) error { close(started); <-ctx.Done(); return ctx.Err() }})
	}()
	<-started
	before, _ := k.ReadJob(context.Background(), string(j.JobId))
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, _ := k.ReadJob(context.Background(), string(j.JobId))
	if after.State != "retry_wait" || after.LeaseToken != nil {
		t.Fatal(after)
	}
	if err := k.FinishJob(context.Background(), string(j.JobId), string(*before.LeaseToken), nil, false); err == nil {
		t.Fatal("stopped runner retained lease")
	}
}
func TestTransientRetriesAreBoundedAndDurable(t *testing.T) {
	k, e := testKernel(t)
	j := enqueueTestJob(t, k, e, "BACKUP_WORKSPACE", map[string]any{"kind": "backup", "destinationHandle": NewID()})
	for attempt := int64(1); attempt <= 3; attempt++ {
		claimed, err := k.ClaimJob(context.Background(), []string{"BACKUP_WORKSPACE"})
		if err != nil {
			t.Fatal(err)
		}
		if claimed.Attempt != attempt {
			t.Fatal(claimed)
		}
		if err = k.FinishJob(context.Background(), string(j.JobId), string(*claimed.LeaseToken), Fail("WORKER_FAILED", "fixture temporary failure"), true); err != nil {
			t.Fatal(err)
		}
		state, _ := k.ReadJob(context.Background(), string(j.JobId))
		if attempt < 3 {
			if state.State != "retry_wait" || state.NextAttemptAt == nil {
				t.Fatal(state)
			}
			if _, err = k.ClaimJob(context.Background(), nil); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("retry ran before due time", err)
			}
			if err = k.write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec("UPDATE jobs SET next_attempt_at=? WHERE job_id=?", time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), j.JobId)
				return err
			}); err != nil {
				t.Fatal(err)
			}
		} else if state.State != "failed" {
			t.Fatal(state)
		}
	}
	if _, err := k.ClaimJob(context.Background(), nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("retry exceeded budget", err)
	}
}
func TestFileAndMeetingJobsCannotEscapePageScope(t *testing.T) {
	k, e := testKernel(t)
	allowed := createPage(t, k, e, "allowed")
	_, meeting, file := meetingFixture(t, k, e)
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(allowed)}}})
	for kind, payload := range map[string]map[string]any{"EXTRACT_DOCUMENT": {"kind": "file", "fileId": file, "extractorVersion": "1"}, "TRANSCRIBE_AUDIO": {"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()}} {
		testCode(t, testDispatch(t, k, scoped, "enqueueJob", recoveryRequest(k, map[string]any{"kind": kind, "payload": payload, "idempotencyKey": NewID()})), "SCOPE_DENIED")
	}
}

func TestTranscriptCitationValidatesMeetingSegmentAndTime(t *testing.T) {
	k, owner := testKernel(t)
	page, meeting, file := meetingFixture(t, k, owner)
	job := enqueueTestJob(t, k, owner, "TRANSCRIBE_AUDIO", map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()})
	j, err := k.ClaimJob(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := k.JobExecution(context.Background(), string(job.JobId))
	r := jobTranscriptRequest(t, k, e, j, meeting)
	out := k.CommitJobBatch(context.Background(), e, string(j.JobId), string(*j.LeaseToken), "transcript-v1", r)
	if _, ok := out.Value.(contracts.ServiceBatchResponseVariant1); !ok {
		t.Fatal(out)
	}
	var segment, block, rev string
	k.db.QueryRow("SELECT id,block_id FROM transcript_segments WHERE meeting_id=?", meeting).Scan(&segment, &block)
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&rev)
	loc := map[string]any{"pageId": page, "blockId": block, "sourceRevision": rev, "transcript": map[string]any{"meetingId": meeting, "segmentId": segment, "startMs": 0, "endMs": 500}}
	result := testOK(t, testDispatch(t, k, owner, "resolveCitation", recoveryRequest(k, map[string]any{"location": loc})))
	if result["state"] != "current" {
		t.Fatal(result)
	}
	loc["transcript"].(map[string]any)["endMs"] = 10000
	result = testOK(t, testDispatch(t, k, owner, "resolveCitation", recoveryRequest(k, map[string]any{"location": loc})))
	if result["state"] != "unavailable" {
		t.Fatal("forged transcript timestamp considered current", result)
	}
}
