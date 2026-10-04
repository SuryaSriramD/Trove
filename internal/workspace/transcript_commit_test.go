package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func leasedTranscriptFixture(t *testing.T) (*Kernel, contracts.ServiceExecutionContext, contracts.ServiceExecutionContext, string, string, string, contracts.JobsJob) {
	t.Helper()
	k, owner := testKernel(t)
	page, meeting, file := meetingFixture(t, k, owner)
	enqueueTestJob(t, k, owner, "TRANSCRIBE_AUDIO", map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()})
	job, err := k.ClaimJob(context.Background(), []string{"TRANSCRIBE_AUDIO"})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := k.JobExecution(context.Background(), string(job.JobId))
	if err != nil {
		t.Fatal(err)
	}
	return k, owner, execution, page, meeting, file, job
}

// This deliberately places the editor commit in the exact previously racy
// window, after the worker reads guards and before it commits a finished unit.
// No scheduler timing or probabilistic loop is required to reproduce the bug.
func TestTranscriptCommitAtomicallyRebasesDestinationAfterGuardRead(t *testing.T) {
	k, owner, execution, page, meeting, _, job := leasedTranscriptFixture(t)
	ctx := context.Background()
	if _, err := k.TranscriptJobGuards(ctx, execution, string(job.JobId), string(*job.LeaseToken)); err != nil {
		t.Fatal(err)
	}
	request := jobTranscriptRequest(t, k, execution, job, meeting)
	block := NewID()
	testOK(t, dbBatch(t, k, owner, []any{
		map[string]any{"kind": "page.update", "pageId": page, "title": "User's edit wins"},
		map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Typed after the transcript guard snapshot"}}}}}}, "page", page, "block", block))
	var beforeAppend string
	if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&beforeAppend); err != nil {
		t.Fatal(err)
	}
	result := k.CommitTranscriptJobBatch(ctx, execution, string(job.JobId), string(*job.LeaseToken), "transcript-v1", request)
	success, ok := result.Value.(contracts.ServiceBatchResponseVariant1)
	if !ok {
		t.Fatalf("completed inference conflicted with typing: %s", marshal(result))
	}
	var pageChange *contracts.ServiceRevisionChange
	for i := range success.Result.Revisions {
		change := &success.Result.Revisions[i]
		if change.Target.EntityType == "page" && string(change.Target.EntityId) == page {
			pageChange = change
		}
	}
	if pageChange == nil || pageChange.Before != beforeAppend {
		t.Fatalf("history did not start from the concurrent user revision: %+v", pageChange)
	}
	body := readPage(t, k, owner, page)
	raw := string(marshal(body))
	if !strings.Contains(raw, "User's edit wins") || !strings.Contains(raw, "Typed after the transcript guard snapshot") || !strings.Contains(raw, "Durable transcript") {
		t.Fatalf("append lost user or transcript content: %s", raw)
	}
	var storedHash string
	if err := k.db.QueryRow("SELECT command_hash FROM mutation_history WHERE transaction_id=?", success.Result.TransactionId).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	effective := request
	effective.Mutation.ExpectedRevisions = append([]contracts.ServiceExpectedRevision{}, request.Mutation.ExpectedRevisions...)
	for i := range effective.Mutation.ExpectedRevisions {
		g := &effective.Mutation.ExpectedRevisions[i]
		if g.Target.EntityType == "page" && string(g.Target.EntityId) == page {
			g.Revision = beforeAppend
		}
	}
	wantHash, err := commandHash(effective, false)
	if err != nil {
		t.Fatal(err)
	}
	if storedHash != wantHash {
		t.Fatal("audit hash omitted the effective page revision")
	}
	replay := k.CommitTranscriptJobBatch(ctx, execution, string(job.JobId), "expired-lease", "transcript-v1", request)
	replayed, ok := replay.Value.(contracts.ServiceBatchResponseVariant1)
	if !ok || !replayed.Result.Replayed {
		t.Fatalf("stale caller guards broke unit replay: %s", marshal(replay))
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM transcript_segments WHERE meeting_id=?", meeting).Scan(&count)
	if count != 1 {
		t.Fatalf("replay duplicated transcript: %d", count)
	}
}

func TestTranscriptAtomicRebaseKeepsInputAndDeletionFences(t *testing.T) {
	for _, change := range []string{"meeting", "file", "deleted_page", "changed_destination"} {
		t.Run(change, func(t *testing.T) {
			k, owner, execution, page, meeting, file, job := leasedTranscriptFixture(t)
			ctx := context.Background()
			if _, err := k.TranscriptJobGuards(ctx, execution, string(job.JobId), string(*job.LeaseToken)); err != nil {
				t.Fatal(err)
			}
			request := jobTranscriptRequest(t, k, execution, job, meeting)
			switch change {
			case "meeting":
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "meeting.finalize", "meetingId": meeting, "durationMs": 0}}, "meeting", meeting, "page", page))
			case "file":
				// Assets are immutable in the public service; simulate a different source
				// revision to prove the dedicated append capability cannot rebase it.
				if err := k.write(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "UPDATE files SET revision=revision+1 WHERE id=?", file)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			case "deleted_page":
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, "page", page))
			case "changed_destination":
				other := createPage(t, k, owner, "Other page")
				if err := k.write(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "UPDATE meetings SET page_id=? WHERE id=?", other, meeting)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			result := k.CommitTranscriptJobBatch(ctx, execution, string(job.JobId), string(*job.LeaseToken), "transcript-v1", request)
			failed, ok := result.Value.(contracts.ServiceBatchResponseVariant2)
			if !ok {
				t.Fatalf("input fence bypassed for %s: %s", change, marshal(result))
			}
			if change != "deleted_page" && failed.Error.Code != "CONFLICT" {
				t.Fatalf("wrong fence error: %+v", failed.Error)
			}
			var count int
			k.db.QueryRow("SELECT count(*) FROM transcript_segments WHERE meeting_id=?", meeting).Scan(&count)
			if count != 0 {
				t.Fatal("conflicting input wrote transcript")
			}
			k.db.QueryRow("SELECT count(*) FROM job_results WHERE job_id=?", job.JobId).Scan(&count)
			if count != 0 {
				t.Fatal("failed input created a unit receipt")
			}
		})
	}
}

func TestTranscriptRebaseCapabilityRejectsUnrelatedCommands(t *testing.T) {
	k, _, execution, page, meeting, _, job := leasedTranscriptFixture(t)
	request := jobTranscriptRequest(t, k, execution, job, meeting)
	title := "An unauthorized replacement"
	request.Commands = []contracts.ServiceCommand{{Value: contracts.ServiceUpdatePage{Kind: "page.update", PageId: contracts.DomainId(page), Title: &title}}}
	result := k.CommitTranscriptJobBatch(context.Background(), execution, string(job.JobId), string(*job.LeaseToken), "not-a-transcript", request)
	var response map[string]any
	json.Unmarshal(marshal(result), &response)
	testCode(t, response, "SCOPE_DENIED")
}
