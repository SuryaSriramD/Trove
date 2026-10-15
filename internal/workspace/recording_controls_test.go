package workspace

import (
	"context"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func TestRecordingDisclosurePauseGapsAndRecovery(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	if _, _, err := k.BeginDisclosedRecording(ctx, e, "test", "chosen-input", false); err == nil {
		t.Fatal("missing disclosure accepted")
	}
	draft, f, err := k.BeginDisclosedRecording(ctx, e, "test", "chosen-input", true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if draft.Version != 1 || draft.InputDeviceID != "chosen-input" || draft.DisclosureAcknowledgedAt == "" {
		t.Fatal(draft)
	}
	if err := contracts.Validate("service", "NativeRecordingDraft", marshal(draft)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(make([]byte, 64000)); err != nil {
		t.Fatal(err)
	}
	f.Sync()
	if _, err = k.CheckpointRecording(ctx, e, draft.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	controlID := NewID()
	paused, err := k.AcknowledgeRecordingControl(ctx, e, draft.ID, controlID, "pause", 16000)
	if err != nil {
		t.Fatal(err)
	}
	again, err := k.AcknowledgeRecordingControl(ctx, e, draft.ID, controlID, "pause", 16000)
	if err != nil || len(again.Gaps) != 1 {
		t.Fatal(again, err)
	}
	if paused.State != "paused" || paused.DurationMS != 1000 || paused.Gaps[0].SampleOffset != 16000 {
		t.Fatal(paused)
	}
	if err := contracts.Validate("service", "NativeRecordingDraft", marshal(paused)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	resumed, err := k.AcknowledgeRecordingControl(ctx, e, draft.ID, NewID(), "resume", 16000)
	if err != nil || resumed.DurationMS != 1000 || resumed.Gaps[0].DurationMS == nil {
		t.Fatal(resumed, err)
	}
	if _, err = k.AcknowledgeRecordingControl(ctx, e, draft.ID, NewID(), "pause", 16001); err == nil {
		t.Fatal("unflushed sample acknowledged")
	}
	denied := k.Grant(contracts.ServiceActor{Kind: "local_ai", ActorId: contracts.DomainId(NewID())}, "ai", e.Scope)
	if _, err = k.AcknowledgeRecordingControl(ctx, denied, draft.ID, NewID(), "pause", 16000); err == nil {
		t.Fatal("non-owner controlled capture")
	}
	if _, err = k.AcknowledgeRecordingControl(ctx, e, draft.ID, NewID(), "pause", 16000); err != nil {
		t.Fatal(err)
	}
	recovered, err := k.CheckpointRecording(ctx, e, draft.ID, true, "LOW_DISK: recover flushed audio")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != "interrupted" || recovered.DurationMS != 1000 || len(recovered.Gaps) != 3 || recovered.Gaps[1].DurationMS != nil || recovered.Gaps[2].Kind != "interruption" {
		t.Fatal(recovered)
	}
	saved, err := k.FinalizeRecording(ctx, e, draft.ID)
	if err != nil || saved.DurationMS != 1000 || len(saved.Gaps) != 3 {
		t.Fatal(saved, err)
	}
}

func TestPausedRecordingBackupAndRestartStayInert(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	draft, f, err := k.BeginDisclosedRecording(ctx, e, "paused", "", true)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(make([]byte, 6400))
	f.Sync()
	k.CheckpointRecording(ctx, e, draft.ID, false, "")
	if _, err = k.AcknowledgeRecordingControl(ctx, e, draft.ID, NewID(), "pause", 1600); err != nil {
		t.Fatal(err)
	}
	sources, err := k.captureBackupRecordings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	closeBackupRecordings(sources)
	f.Close()
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	drafts, err := k.ListRecordings(ctx, k.OwnerContext())
	if err != nil || len(drafts) != 1 {
		t.Fatal(drafts, err)
	}
	if drafts[0].State != "interrupted" || len(drafts[0].Gaps) != 2 || drafts[0].Gaps[0].DurationMS != nil {
		t.Fatal(drafts)
	}
}
