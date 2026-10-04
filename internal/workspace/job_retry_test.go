package workspace

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"trove.local/phase0/contracts"
)

func TestReviewedJobRetryPreservesInputsUnitsAndFencesOldLease(t *testing.T) {
	k, owner := testKernel(t)
	_, meeting, file := meetingFixture(t, k, owner)
	job := enqueueTestJob(t, k, owner, "TRANSCRIBE_AUDIO", map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()})
	j, err := k.ClaimJob(context.Background(), []string{"TRANSCRIBE_AUDIO"})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := k.ReserveJobIDs(context.Background(), string(j.JobId), string(*j.LeaseToken), "minute-0", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err = k.FinishJob(context.Background(), string(j.JobId), string(*j.LeaseToken), Fail("WORKER_FAILED", "interrupted decoder"), false); err != nil {
		t.Fatal(err)
	}
	failed, _ := k.ReadJob(context.Background(), string(j.JobId))
	request := recoveryRequest(k, map[string]any{"jobId": j.JobId, "expectedRevision": failed.Revision})
	other := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", owner.Scope)
	testCode(t, testDispatch(t, k, other, "jobs.retry", request), "SCOPE_DENIED")
	stale := recoveryRequest(k, map[string]any{"jobId": j.JobId, "expectedRevision": "1"})
	testCode(t, testDispatch(t, k, owner, "jobs.retry", stale), "CONFLICT")
	testOK(t, testDispatch(t, k, owner, "jobs.retry", request))
	retried, err := k.ClaimJob(context.Background(), []string{"TRANSCRIBE_AUDIO"})
	if err != nil {
		t.Fatal(err)
	}
	if retried.JobId != job.JobId || string(marshal(retried.Payload)) != string(marshal(job.Payload)) || *retried.LeaseToken == *j.LeaseToken {
		t.Fatal("retry changed saved inputs or reused old lease")
	}
	again, err := k.ReserveJobIDs(context.Background(), string(retried.JobId), string(*retried.LeaseToken), "minute-0", 2)
	if err != nil || string(marshal(again)) != string(marshal(ids)) {
		t.Fatal("retry lost stable unit IDs", err)
	}
	if err = k.ProgressJob(context.Background(), string(j.JobId), string(*j.LeaseToken), contracts.JobsProgress{Sequence: 0, Completed: 1, Unit: "items"}); err == nil {
		t.Fatal("old worker retained authority")
	}
	if err = contracts.Validate("jobs", "Job", marshal(retried)); err != nil {
		t.Fatal(err)
	}
}

func TestImportPreviewEmitsCorrelatedInventoryProgress(t *testing.T) {
	k, e := testKernel(t)
	channel, stop := k.SubscribeChanges()
	defer stop()
	path := t.TempDir() + "/notes.md"
	if err := os.WriteFile(path, []byte("# Progress\nReadable content"), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := k.RegisterSelection(path)
	if err != nil {
		t.Fatal(err)
	}
	id := NewID()
	if _, err = previewPortableImport(context.Background(), k, e, map[string]any{"requestId": id, "sourceNamespace": "progress", "sourceHandle": handle}); err != nil {
		t.Fatal(err)
	}
	stages := map[string]bool{}
	for len(channel) > 0 {
		var event map[string]any
		if err := json.Unmarshal(<-channel, &event); err != nil {
			t.Fatal(err)
		}
		if event["eventType"] == "imports.previewProgress" {
			if event["requestId"] != id || event["workspaceId"] != k.ID() {
				t.Fatal("uncorrelated progress")
			}
			stages[str(event["stage"])] = true
		}
	}
	if !stages["reading"] || !stages["parsing"] || !stages["ready"] {
		t.Fatal(stages)
	}
}
