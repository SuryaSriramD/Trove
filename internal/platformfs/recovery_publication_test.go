package platformfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func recoveryFixture(t *testing.T) (string, RecoveryPlan, map[string][]byte) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("full durability protocol requires target-native qualification; see gated Windows tests")
	}
	payload := map[string][]byte{"workspace.db": []byte("independent baseline database bytes\x00\xff"), "assets/nested/a.bin": []byte("asset A\r\n"), "recordings/r.raw": []byte{1, 5, 9, 0}}
	p := RecoveryPlan{Version: 1, OperationID: "0123456789abcdef0123456789abcdef", Kind: "restore", SourceID: "backup-42", SourceSHA256: recoveryHash([]byte("reviewed manifest revision 42"))}
	for _, name := range []string{"workspace.db", "assets/nested/a.bin", "recordings/r.raw"} {
		b := payload[name]
		p.Objects = append(p.Objects, RecoveryObject{name, recoveryHash(b), int64(len(b))})
	}
	return filepath.Join(t.TempDir(), "new-workspace"), p, payload
}
func stageRecovery(t *testing.T, p *RecoveryPublication, payload map[string][]byte) {
	t.Helper()
	for _, o := range p.plan.Objects {
		if err := p.Stage(context.Background(), o.Path, bytes.NewReader(payload[o.Path])); err != nil {
			t.Fatal(err)
		}
	}
}
func completeRecovery(t *testing.T) (string, RecoveryPlan, map[string][]byte) {
	t.Helper()
	dir, plan, payload := recoveryFixture(t)
	p, e := BeginRecoveryPublication(context.Background(), dir, plan)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	stageRecovery(t, p, payload)
	if e = p.Verify(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = p.Commit(context.Background()); e != nil {
		t.Fatal(e)
	}
	return dir, plan, payload
}
func assertRecoveryBytes(t *testing.T, dir string, payload map[string][]byte) {
	t.Helper()
	for n, want := range payload {
		got, e := os.ReadFile(filepath.Join(dir, filepath.FromSlash(n)))
		if e != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: got %q; expected %q (%v)", n, got, want, e)
		}
	}
}

func TestRecoveryPositiveCompletionAndActivation(t *testing.T) {
	ctx := context.Background()
	dir, plan, payload := completeRecovery(t)
	status, e := InspectRecoveryAdmission(dir, plan.OperationID)
	if e != nil || status.State != "complete" {
		t.Fatalf("%+v %v", status, e)
	}
	assertRecoveryBytes(t, dir, payload)
	if e = ActivateRecoveryAdmission(dir, plan.OperationID, nil); !errors.Is(e, ErrLocked) {
		t.Fatalf("missing owner: %v", e)
	}
	p, e := openRecoveryPublication(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if e = ActivateRecoveryAdmission(dir, plan.OperationID, p.owner); e != nil {
		t.Fatal(e)
	}
	// Ordinary edits after activation must not be compared to restoration bytes.
	if e = os.WriteFile(filepath.Join(dir, "workspace.db"), []byte("later ordinary database revision"), 0600); e != nil {
		t.Fatal(e)
	}
	status, e = InspectRecoveryAdmission(dir, plan.OperationID)
	if e != nil || status.State != "active" {
		t.Fatalf("ordinary edit rejected: %+v %v", status, e)
	}
	if e = RecoverPublication(ctx, dir, plan.OperationID); !errors.Is(e, ErrLocked) {
		t.Fatalf("second recovery owner: %v", e)
	}
}
func TestRecoveryFaultBoundariesRequirePositiveEvidence(t *testing.T) {
	boundaries := []string{"intent", "stage:workspace.db", "stage:assets/nested/a.bin", "stage:recordings/r.raw", "verified", "publish:workspace.db", "publish:assets/nested/a.bin", "publish:recordings/r.raw", "before_complete", "complete"}
	for index, boundary := range boundaries {
		t.Run(boundary, func(t *testing.T) {
			ctx := context.Background()
			dir, plan, payload := recoveryFixture(t)
			fault := errors.New("independent simulated process stop")
			p, e := beginRecoveryPublication(ctx, dir, plan, func(stage string) error {
				if stage == boundary {
					return fault
				}
				return nil
			})
			if e == nil {
				for _, o := range plan.Objects {
					e = p.Stage(ctx, o.Path, bytes.NewReader(payload[o.Path]))
					if e != nil {
						break
					}
				}
				if e == nil {
					e = p.Verify(ctx)
				}
				if e == nil {
					e = p.Commit(ctx)
				}
				p.Close()
			}
			if !errors.Is(e, fault) {
				t.Fatalf("did not reach fault: %v", e)
			}
			probe, lockErr := openRecoveryPublication(dir)
			if lockErr != nil {
				t.Fatal("interrupted owner was not released", lockErr)
			}
			probe.Close()
			status, e := InspectRecoveryAdmission(dir, plan.OperationID)
			if boundary == "complete" {
				if e != nil || status.State != "complete" {
					t.Fatalf("completed evidence rejected: %+v %v", status, e)
				}
			} else if !errors.Is(e, ErrRecoveryIncomplete) {
				t.Fatalf("interruption admitted: %+v %v", status, e)
			}
			e = RecoverPublication(ctx, dir, plan.OperationID)
			if index < 4 {
				if e == nil {
					t.Fatal("unverified staging was recovered as completed")
				}
				return
			}
			if e != nil {
				t.Fatal("verified publication could not resume", e)
			}
			assertRecoveryBytes(t, dir, payload)
			status, e = InspectRecoveryAdmission(dir, plan.OperationID)
			if e != nil || status.State != "complete" {
				t.Fatalf("recovered receipt: %+v %v", status, e)
			}
		})
	}
}
func TestRecoveryRejectsWrongBytesAndCancellation(t *testing.T) {
	for _, kind := range []string{"short", "long", "same-size-corrupt", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			dir, plan, payload := recoveryFixture(t)
			p, e := BeginRecoveryPublication(context.Background(), dir, plan)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			b := append([]byte(nil), payload["workspace.db"]...)
			ctx := context.Background()
			switch kind {
			case "short":
				b = b[:len(b)-1]
			case "long":
				b = append(b, 1)
			case "same-size-corrupt":
				b[0] ^= 1
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if e = p.Stage(ctx, "workspace.db", bytes.NewReader(b)); e == nil {
				t.Fatal("invalid stage accepted")
			}
			if e = p.Verify(context.Background()); e == nil {
				t.Fatal("invalid bytes verified")
			}
			if e = p.Commit(context.Background()); e == nil {
				t.Fatal("unverified bytes committed")
			}
			if _, e = os.Stat(filepath.Join(dir, recoveryComplete)); !os.IsNotExist(e) {
				t.Fatal("completion record created", e)
			}
		})
	}
}
func TestRecoveryDoesNotOverwriteUnexpectedTargetOrStage(t *testing.T) {
	dir, plan, payload := recoveryFixture(t)
	p, e := BeginRecoveryPublication(context.Background(), dir, plan)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	stageRecovery(t, p, payload)
	if e = p.Verify(context.Background()); e != nil {
		t.Fatal(e)
	}
	sentinel := []byte("unrelated existing target")
	if e = os.WriteFile(filepath.Join(dir, "workspace.db"), sentinel, 0600); e != nil {
		t.Fatal(e)
	}
	if e = p.Commit(context.Background()); e == nil {
		t.Fatal("ambiguous target and stage accepted")
	}
	got, e := os.ReadFile(filepath.Join(dir, "workspace.db"))
	if e != nil || !bytes.Equal(got, sentinel) {
		t.Fatal("target overwritten", e)
	}
	got, e = os.ReadFile(filepath.Join(dir, stagedRecoveryName(0)))
	if e != nil || !bytes.Equal(got, payload["workspace.db"]) {
		t.Fatal("stage lost", e)
	}
	if _, e = BeginRecoveryPublication(context.Background(), dir, plan); !os.IsExist(e) {
		t.Fatalf("existing reservation replaced: %v", e)
	}
}
func TestRecoveryStartupRejectsMissingReplayedOrCorruptEvidence(t *testing.T) {
	for _, kind := range []string{"missing-all", "missing-complete", "wrong-operation", "changed-intent", "duplicate-key", "corrupt-payload", "unexpected-wal", "interrupted-active", "extra-stage"} {
		t.Run(kind, func(t *testing.T) {
			dir, plan, _ := completeRecovery(t)
			id := plan.OperationID
			switch kind {
			case "missing-all":
				entries, _ := os.ReadDir(dir)
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), recoveryPrefix) {
						os.RemoveAll(filepath.Join(dir, e.Name()))
					}
				}
			case "missing-complete":
				os.Remove(filepath.Join(dir, recoveryComplete))
			case "wrong-operation":
				id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "changed-intent":
				b, _ := os.ReadFile(filepath.Join(dir, recoveryIntent))
				b = bytes.Replace(b, []byte("backup-42"), []byte("backup-43"), 1)
				os.WriteFile(filepath.Join(dir, recoveryIntent), b, 0600)
			case "duplicate-key":
				b, _ := os.ReadFile(filepath.Join(dir, recoveryComplete))
				b = append([]byte(`{"version":1,`), b[1:]...)
				os.WriteFile(filepath.Join(dir, recoveryComplete), b, 0600)
			case "corrupt-payload":
				os.WriteFile(filepath.Join(dir, "workspace.db"), []byte("changed"), 0600)
			case "unexpected-wal":
				os.WriteFile(filepath.Join(dir, "workspace.db-wal"), []byte("unverified WAL"), 0600)
			case "interrupted-active":
				os.WriteFile(filepath.Join(dir, recoveryActive+".staged"), []byte("partial"), 0600)
			case "extra-stage":
				os.WriteFile(filepath.Join(dir, recoveryStage, "999999"), []byte("unknown operation"), 0600)
			}
			if _, e := InspectRecoveryAdmission(dir, id); !errors.Is(e, ErrRecoveryIncomplete) {
				t.Fatalf("bad evidence admitted: %v", e)
			}
		})
	}
}
func TestRecoveryRejectsCopiedReceiptButAllowsWholeDirectoryMove(t *testing.T) {
	dir, plan, payload := completeRecovery(t)
	copyRoot := filepath.Join(t.TempDir(), "copy")
	if e := os.Mkdir(copyRoot, 0700); e != nil {
		t.Fatal(e)
	}
	e := filepath.WalkDir(dir, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, name)
		target := filepath.Join(copyRoot, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		b, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0600)
	})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = InspectRecoveryAdmission(copyRoot, plan.OperationID); !errors.Is(e, ErrRecoveryIncomplete) {
		t.Fatalf("copied receipt trusted: %v", e)
	}
	if e = RelocateRecoveryPublication(context.Background(), dir, copyRoot, plan.OperationID); e == nil {
		t.Fatal("relocation overwrote an existing directory")
	}
	destination := filepath.Join(t.TempDir(), "moved")
	if e = RelocateRecoveryPublication(context.Background(), dir, destination, plan.OperationID); e != nil {
		t.Fatal(e)
	}
	if _, e = InspectRecoveryAdmission(destination, plan.OperationID); e != nil {
		t.Fatal(e)
	}
	assertRecoveryBytes(t, destination, payload)
	if _, e = os.Stat(dir); !os.IsNotExist(e) {
		t.Fatal("original still exists", e)
	}
}
func TestRecoveryPlanCannotEscapeOrAliasAndIsCloned(t *testing.T) {
	for _, name := range []string{"../escaped", "/absolute", "assets\\bad", ".owner.lock", "INCOMPLETE", recoveryComplete} {
		t.Run(name, func(t *testing.T) {
			dir, plan, _ := recoveryFixture(t)
			plan.Objects[0].Path = name
			if _, e := BeginRecoveryPublication(context.Background(), dir, plan); e == nil {
				t.Fatal("unsafe plan accepted")
			}
			if _, e := os.Stat(dir); !os.IsNotExist(e) {
				t.Fatal("unsafe plan reserved a directory")
			}
		})
	}
	dir, plan, payload := recoveryFixture(t)
	p, e := BeginRecoveryPublication(context.Background(), dir, plan)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	plan.Objects[0].SHA256 = recoveryHash([]byte("different"))
	stageRecovery(t, p, payload)
	if e = p.Verify(context.Background()); e != nil {
		t.Fatal("caller mutation changed immutable plan", e)
	}
}
func TestRecoveryStageCorruptionAfterVerificationCannotCommit(t *testing.T) {
	dir, plan, payload := recoveryFixture(t)
	p, e := BeginRecoveryPublication(context.Background(), dir, plan)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	stageRecovery(t, p, payload)
	if e = p.Verify(context.Background()); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(dir, stagedRecoveryName(0)), os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteAt([]byte("X"), 0)
	f.Close()
	if e = p.Commit(context.Background()); e == nil {
		t.Fatal("corrupted staged file committed")
	}
	if _, e = os.Stat(filepath.Join(dir, "workspace.db")); !os.IsNotExist(e) {
		t.Fatal("bad staged file published", e)
	}
}
func TestRecoveryCancellationDuringVerification(t *testing.T) {
	dir, plan, payload := recoveryFixture(t)
	p, e := BeginRecoveryPublication(context.Background(), dir, plan)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	stageRecovery(t, p, payload)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = p.verifyFile(ctx, stagedRecoveryName(0), plan.Objects[0]); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

var _ io.Reader = (*recoveryReader)(nil)

func TestRecoveryHostileAncestorCannotEscapePinnedRoot(t *testing.T) {
	dir, plan, payload := recoveryFixture(t)
	p, e := BeginRecoveryPublication(context.Background(), dir, plan)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	stageRecovery(t, p, payload)
	if e = p.Verify(context.Background()); e != nil {
		t.Fatal(e)
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep")
	os.WriteFile(sentinel, []byte("external sentinel"), 0600)
	if e = os.Symlink(outside, filepath.Join(dir, "assets")); e != nil {
		t.Fatal(e)
	}
	if e = p.Commit(context.Background()); e == nil {
		t.Fatal("ancestor symlink accepted")
	}
	entries, e := os.ReadDir(outside)
	if e != nil || len(entries) != 1 || entries[0].Name() != "keep" {
		t.Fatal("outside tree mutated", entries, e)
	}
	got, e := os.ReadFile(filepath.Join(dir, stagedRecoveryName(1)))
	if e != nil || !bytes.Equal(got, payload["assets/nested/a.bin"]) {
		t.Fatal("confined staged bytes lost", e)
	}
	got, e = os.ReadFile(sentinel)
	if e != nil || string(got) != "external sentinel" {
		t.Fatal("outside sentinel changed", e)
	}
}
func TestRecoveryUnexpectedPayloadIsNotCompleted(t *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir, plan, payload := recoveryFixture(t)
			p, e := BeginRecoveryPublication(context.Background(), dir, plan)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			stageRecovery(t, p, payload)
			if e = p.Verify(context.Background()); e != nil {
				t.Fatal(e)
			}
			if kind == "file" {
				os.WriteFile(filepath.Join(dir, "unknown.bin"), []byte("not in inventory"), 0600)
			} else {
				os.Mkdir(filepath.Join(dir, "unknown-dir"), 0700)
			}
			if e = p.Commit(context.Background()); e == nil {
				t.Fatal("undeclared payload completed")
			}
			if _, e = os.Stat(filepath.Join(dir, recoveryComplete)); !os.IsNotExist(e) {
				t.Fatal("completion exists for unexpected payload", e)
			}
		})
	}
}

func TestRecoveryRepeatedCommitDoesNotPoisonCompletion(t *testing.T) {
	dir, plan, payload := recoveryFixture(t)
	p, e := BeginRecoveryPublication(context.Background(), dir, plan)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	stageRecovery(t, p, payload)
	if e = p.Verify(context.Background()); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = p.Commit(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = InspectRecoveryAdmission(dir, plan.OperationID); e != nil {
		t.Fatal("repeat commit poisoned completion", e)
	}
}
func TestRecoveryLostOwnerNameCannotStageOrPublish(t *testing.T) {
	dir, plan, payload := recoveryFixture(t)
	p, e := BeginRecoveryPublication(context.Background(), dir, plan)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if e = os.Rename(filepath.Join(dir, ".owner.lock"), filepath.Join(dir, "lost-owner")); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, ".owner.lock"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e = p.Stage(context.Background(), "workspace.db", bytes.NewReader(payload["workspace.db"])); !errors.Is(e, ErrLocked) {
		t.Fatal("lost owner authority accepted", e)
	}
	if _, e = os.Stat(filepath.Join(dir, stagedRecoveryName(0))); !os.IsNotExist(e) {
		t.Fatal("lost owner wrote stage", e)
	}
}

func TestRecoveryUndeclaredStagedBytesCannotComplete(t *testing.T) {
	dir, plan, payload := recoveryFixture(t)
	p, e := BeginRecoveryPublication(context.Background(), dir, plan)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	stageRecovery(t, p, payload)
	if e = p.Verify(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, recoveryStage, "999999"), []byte("unexpected staged bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = p.Commit(context.Background()); !errors.Is(e, ErrRecoveryIncomplete) {
		t.Fatal("undeclared stage reported complete", e)
	}
	if _, e = os.Stat(filepath.Join(dir, recoveryComplete)); !os.IsNotExist(e) {
		t.Fatal("positive completion exists for ambiguous stage", e)
	}
}
func TestRecoveryRootSwapAfterIntentCannotCreateOutsideStage(t *testing.T) {
	dir, plan, _ := recoveryFixture(t)
	outside := t.TempDir()
	moved := dir + "-moved"
	_, e := beginRecoveryPublication(context.Background(), dir, plan, func(boundary string) error {
		if boundary != "intent" {
			return nil
		}
		if err := os.Rename(dir, moved); err != nil {
			return err
		}
		return os.Symlink(outside, dir)
	})
	if e == nil {
		t.Fatal("root substitution was accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside tree changed after root substitution", entries, err)
	}
	if _, err = os.Stat(filepath.Join(moved, recoveryStage)); !os.IsNotExist(err) {
		t.Fatal("stage unexpectedly created after authority loss", err)
	}
}
