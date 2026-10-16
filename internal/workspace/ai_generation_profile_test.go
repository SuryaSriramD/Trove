package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func preparedGenerationFixture(t *testing.T, mode string) AIPrepared {
	t.Helper()
	p, err := prepareAIContext(context.Background(), AIRequest{Mode: mode, Prompt: "Preserve the source."}, []AIEvidence{{Title: "Transport fixture", Text: "The meeting is Friday."}}, nil, "", wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func profileDigest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func assertGenerationProfile(t *testing.T, p *contracts.ServiceAIGenerationProfile, system, grammar string, n int) {
	t.Helper()
	if p == nil || p.SchemaVersion != 1 || string(p.SystemSHA256) != profileDigest(system) || string(p.GrammarSHA256) != profileDigest(grammar) || p.OutputTokens != int64(n) {
		t.Fatalf("stored profile differs from callback: %+v", p)
	}
	if err := contracts.Validate("service", "AIGenerationProfile", marshal(p)); err != nil {
		t.Fatal(err)
	}
}

func TestAIGenerationProfileStoredBeforeNormalCallback(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page, _ := workflowParagraph(t, k, e, "Profile record", "The meeting is Friday.")
	for _, mode := range []string{"answer", "code_comment", "database", "workspace_agent", "expand", "mermaid"} {
		t.Run(mode, func(t *testing.T) {
			p, err := k.PrepareAIWithBudget(ctx, e, AIRequest{PageID: page, Mode: mode, Prompt: "Preserve the source.", ModelPackID: NewID()}, wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			stop := errors.New("captured")
			_, err = GeneratePreparedAI(ctx, p, func(_ context.Context, s, u string, n int, g string, _ func(string)) (string, error) {
				calls++
				stored, er := k.AIRun(ctx, e, p.Run.ID)
				if er != nil {
					t.Fatal(er)
				}
				assertGenerationProfile(t, stored.GenerationProfile, s, g, n)
				if !reflect.DeepEqual(stored.GenerationProfile, p.Run.GenerationProfile) || u != p.Prompt {
					t.Fatal("stored preparation changed")
				}
				return "", stop
			}, nil)
			if calls != 1 || !errors.Is(err, stop) {
				t.Fatal(calls, err)
			}
		})
	}
}
func TestAIGenerationProfileIncludesTailWithoutPersistingPrivateInput(t *testing.T) {
	first := preparedGenerationFixture(t, "answer")
	p, err := prepareAIContext(context.Background(), AIRequest{Mode: "answer", Prompt: "PRIVATE_REQUEST"}, []AIEvidence{{Title: "PRIVATE_TITLE", Text: "PRIVATE_SOURCE is current."}}, nil, "", wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	expected := profileDigest(string(marshal([]string{p.System, "", aiCitedResponseContract})))
	if string(p.Run.GenerationProfile.TrustedGuidanceSHA256) != expected || string(first.Run.GenerationProfile.TrustedGuidanceSHA256) != expected {
		t.Fatal("trusted USER-tail guidance absent or private data included")
	}
	if p.Run.GenerationProfile.GrammarSHA256 == first.Run.GenerationProfile.GrammarSHA256 {
		t.Fatal("actual source-dependent grammar not bound")
	}
	if strings.Contains(string(marshal(p.Run.GenerationProfile)), "PRIVATE_") {
		t.Fatal("private input in durable profile")
	}
	if strings.Contains(string(marshal(p)), "generationSeal") || strings.Contains(string(marshal(p)), `"Grammar"`) {
		t.Fatal("private preparation fields serialized")
	}
}
func TestAIGenerationProfileRejectsChangedPreparationBeforeCallback(t *testing.T) {
	original := preparedGenerationFixture(t, "answer")
	changes := map[string]func(*AIPrepared){
		"unprepared":      func(p *AIPrepared) { *p = AIPrepared{} },
		"missing-profile": func(p *AIPrepared) { p.Run.GenerationProfile = nil },
		"system":          func(p *AIPrepared) { p.System += "changed" }, "prompt": func(p *AIPrepared) { p.Prompt += "changed" },
		"grammar": func(p *AIPrepared) { p.Grammar += "changed" }, "output-cap": func(p *AIPrepared) { p.OutputTokens-- },
		"prompt-count": func(p *AIPrepared) { p.PromptTokens-- }, "run": func(p *AIPrepared) { p.Run.ID = NewID() },
		"mode": func(p *AIPrepared) { p.Run.Request.Mode = "mermaid" }, "model": func(p *AIPrepared) { p.Run.Request.ModelPackID = NewID() },
		"source":  func(p *AIPrepared) { p.Run.Sources[0].Text = "Changed source." },
		"version": func(p *AIPrepared) { p.Run.GenerationProfile.ProfileVersion = "forged-v1" },
		"guidance": func(p *AIPrepared) {
			p.Run.GenerationProfile.TrustedGuidanceSHA256 = contracts.DomainHash(strings.Repeat("0", 64))
		},
		"matching-forged-hashes": func(p *AIPrepared) {
			p.System = "changed"
			p.Run.GenerationProfile.SystemSHA256 = contracts.DomainHash(profileDigest(p.System))
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			p := original
			p.Run.Sources = append([]AIEvidence(nil), original.Run.Sources...)
			profile := *original.Run.GenerationProfile
			p.Run.GenerationProfile = &profile
			change(&p)
			calls := 0
			_, err := GeneratePreparedAI(context.Background(), p, func(context.Context, string, string, int, string, func(string)) (string, error) {
				calls++
				return "bad", nil
			}, nil)
			if err == nil || calls != 0 {
				t.Fatal("tampered generation reached callback", err, calls)
			}
		})
	}
}
func TestAIGenerationProfileFailureCancellationAndReopen(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page, _ := workflowParagraph(t, k, e, "Profile record", "The meeting is Friday.")
	expected := map[string]string{}
	for _, state := range []string{"failed", "cancelled", "running"} {
		p, err := k.PrepareAI(ctx, e, AIRequest{PageID: page, Mode: "answer", Prompt: "When?"})
		if err != nil {
			t.Fatal(err)
		}
		expected[p.Run.ID] = string(marshal(p.Run.GenerationProfile))
		if state != "running" {
			r, err := k.FinishAI(ctx, e, p.Run.ID, "", state, "deliberate fixture failure")
			if err != nil || r.State != state || string(marshal(r.GenerationProfile)) != expected[p.Run.ID] {
				t.Fatal(r, err)
			}
		}
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for id, profile := range expected {
		r, err := reopened.AIRun(ctx, reopened.OwnerContext(), id)
		if err != nil || r.State == "running" || string(marshal(r.GenerationProfile)) != profile {
			t.Fatal(r, err)
		}
	}
}
func TestPersonalReviewGenerationProfileMatchesActualCallback(t *testing.T) {
	for _, mode := range []string{"success", "failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			r, _, _ := generativeReviewFixture(t, k, e)
			calls := 0
			modelID, profile := "", ""
			stop := errors.New("fixture generator failed")
			_, err := k.GeneratePersonalReview(ctx, e, r.ID, AIBudget{ContextTokens: 8192, OutputTokens: 1024}, func(ctx context.Context, s, p string, n int, g string, stream func(string)) (string, error) {
				calls++
				current, er := readPersonalReviewRun(ctx, k.db, r.ID)
				if er != nil {
					t.Fatal(er)
				}
				modelID = current.ModelRunID
				run, er := k.AIRun(ctx, e, modelID)
				if er != nil {
					t.Fatal(er)
				}
				assertGenerationProfile(t, run.GenerationProfile, s, g, n)
				if run.GenerationProfile.ProfileVersion != "personal-review-claim-guidance-v2" || g != personalReviewGrammar(*r.Generative) {
					t.Fatal("review mislabeled as generic workspace action")
				}
				profile = string(marshal(run.GenerationProfile))
				switch mode {
				case "failure":
					return "", stop
				case "cancel":
					if _, er := k.CancelPersonalReviewGeneration(ctx, e, r.ID); er != nil {
						t.Fatal(er)
					}
					return "", context.Canceled
				}
				return reviewTestGenerator([]PlannedAction{})(ctx, s, p, n, g, stream)
			})
			if calls != 1 || (mode == "success") != (err == nil) {
				t.Fatal(calls, err)
			}
			run, er := k.AIRun(ctx, e, modelID)
			if er != nil || string(marshal(run.GenerationProfile)) != profile {
				t.Fatal("review profile lost on terminal outcome", run, er)
			}
		})
	}
}
func removeGenerationProfileForLegacyFixture(t *testing.T, k *Kernel) {
	t.Helper()
	if _, err := k.db.Exec("ALTER TABLE ai_runs DROP COLUMN generation_profile_json; DELETE FROM service_runtime_migrations WHERE version=21; UPDATE kernel_meta SET value='1' WHERE key='storageGeneration'"); err != nil {
		t.Fatal(err)
	}
}
func TestAIGenerationProfileLegacyMigrationAndRestore(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "current"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			page, _ := workflowParagraph(t, k, e, "Restore profile", "The meeting is Friday.")
			p, err := k.PrepareAI(ctx, e, AIRequest{PageID: page, Mode: "answer", Prompt: "When?"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = k.FinishAI(ctx, e, p.Run.ID, "Friday [1]", "completed", ""); err != nil {
				t.Fatal(err)
			}
			expected := string(marshal(p.Run.GenerationProfile))
			if legacy {
				removeGenerationProfileForLegacyFixture(t, k)
				expected = "null"
			}
			backup := filepath.Join(t.TempDir(), "backup")
			manifest, err := k.CreateBackup(ctx, e, backup)
			if err != nil {
				t.Fatal(err)
			}
			rollback := filepath.Join(t.TempDir(), "rollback")
			_, rollbackErr := RestoreBackupCompatible(ctx, backup, rollback, manifest, CompatibilityDescriptor{UserVersion: 3, StorageGeneration: 1})
			if legacy {
				if rollbackErr != nil {
					t.Fatal(rollbackErr)
				}
				assertLegacyGenerationProfileStorage(t, rollback)
			} else if rollbackErr == nil {
				t.Fatal("generation2 backup admitted as generation1 rollback")
			}
			if !legacy {
				paired := filepath.Join(t.TempDir(), "paired-generation2")
				if _, er := RestoreBackupCompatible(ctx, backup, paired, manifest, CurrentCompatibility()); er != nil {
					t.Fatal(er)
				}
				db, er := openMigrationDatabase(paired)
				if er != nil {
					t.Fatal(er)
				}
				var retained string
				er = db.QueryRow("SELECT generation_profile_json FROM ai_runs WHERE id=?", p.Run.ID).Scan(&retained)
				db.Close()
				if er != nil || retained != expected {
					t.Fatal("paired rollback changed generation profile", er, retained)
				}
			}
			// Both a direct reopen and a portable native restore execute the migration.
			path := k.Path()
			k.Close()
			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			restoredPath := filepath.Join(t.TempDir(), "restored")
			if _, err = RestoreBackup(ctx, backup, restoredPath); err != nil {
				t.Fatal(err)
			}
			restored, err := Open(restoredPath)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			for _, current := range []*Kernel{reopened, restored} {
				if legacy {
					status, er := ReadMigrationStatus(ctx, current.Path())
					if er != nil || status == nil || status.State != "complete" || status.Before.StorageGeneration != 1 || status.Target.StorageGeneration != 2 {
						t.Fatal("pre-migration snapshot missing", status, er)
					}
					snapshot, er := VerifyMigrationSnapshot(ctx, status.SnapshotPath)
					if er != nil || snapshot.Before.StorageGeneration != 1 {
						t.Fatal("legacy recovery snapshot invalid", snapshot, er)
					}
					recovery := filepath.Join(t.TempDir(), "recovery")
					if _, er = RestoreMigrationSnapshot(ctx, status.SnapshotPath, recovery); er != nil {
						t.Fatal(er)
					}
					assertLegacyGenerationProfileStorage(t, recovery)
				}
				run, er := current.AIRun(ctx, current.OwnerContext(), p.Run.ID)
				if er != nil || string(marshal(run.GenerationProfile)) != expected {
					t.Fatal("migration changed historical provenance", run, er)
				}
				var count int
				if er = current.db.QueryRow("SELECT count(*) FROM service_runtime_migrations WHERE version=21").Scan(&count); er != nil || count != 1 {
					t.Fatal("migration not versioned", er, count)
				}
				if legacy && strings.Contains(string(marshal(run)), "generationProfile") {
					t.Fatal("legacy profile was fabricated")
				}
			}
		})
	}
}
func TestAIGenerationProfileRejectsCorruptPersistedMetadata(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"schemaVersion":99}`, `{"prompt":"private"}`} {
		if _, err := decodeAIGenerationProfile(sql.NullString{String: raw, Valid: true}); err == nil {
			t.Fatal("corrupt profile accepted", raw)
		}
	}
	if p, err := decodeAIGenerationProfile(sql.NullString{}); p != nil || err != nil {
		t.Fatal(p, err)
	}
}

type generationInputBinding struct {
	SystemSHA256, PromptSHA256, GrammarSHA256 string
	OutputTokens                              int
}

func TestAIGenerationProfilePreservesGenerationInputs(t *testing.T) {
	raw, err := os.ReadFile("testdata/generation-input-parity-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SourceCommit string
		Cases        map[string]generationInputBinding
	}
	if err = json.Unmarshal(raw, &fixture); err != nil || fixture.SourceCommit != "4065a2c9d18e50583d9717337d4579190bd15860" || len(fixture.Cases) != 23 {
		t.Fatal("baseline binding changed", err)
	}
	rows := captureGenerationInputs(t)
	// This versioned experiment intentionally changes only grounded expansion.
	// Keep the immutable 23-case baseline fixture; all other callback bytes stay
	// bound to it. Expansion has its own transport/budget/scope regressions.
	if rows["expand-sources"] == fixture.Cases["expand-sources"] {
		t.Fatal("structured expansion protocol was not applied")
	}
	delete(rows, "expand-sources")
	delete(fixture.Cases, "expand-sources")
	if !reflect.DeepEqual(rows, fixture.Cases) {
		for id, want := range fixture.Cases {
			if rows[id] != want {
				t.Errorf("generation input changed for %s: got%+v want%+v", id, rows[id], want)
			}
		}
		if len(rows) != len(fixture.Cases) {
			t.Fatal("case inventory changed")
		}
	}
}
func captureGenerationInputs(t *testing.T) map[string]generationInputBinding {
	t.Helper()
	rows := map[string]generationInputBinding{}
	stop := errors.New("deterministic capture only; no inference")
	capture := func(key string) AIGenerator {
		return func(_ context.Context, s, p string, n int, g string, _ func(string)) (string, error) {
			if _, exists := rows[key]; exists {
				t.Fatal("duplicate callback", key)
			}
			rows[key] = generationInputBinding{profileDigest(s), profileDigest(p), profileDigest(g), n}
			return "", stop
		}
	}
	modes := []string{"answer", "rewrite", "summarize", "explain", "shorten", "expand", "outline", "generate", "database", "schema", "mermaid", "code_explain", "code_refactor", "code_comment", "organize", "workspace_agent"}
	for _, mode := range modes {
		for _, withSources := range []bool{true, false} {
			if !withSources && !aiSourceOptional(mode) {
				continue
			}
			key := mode
			if withSources {
				key += "-sources"
			} else {
				key += "-empty"
			}
			sources := []AIEvidence{}
			if withSources {
				sources = []AIEvidence{{Title: "Fixture record", Text: "The meeting is Friday.", Location: contracts.DomainLocation{PageId: "01990000-0000-7000-8000-000000000201", SourceRevision: "2"}}, {Title: "Approved display", Text: "Use a concise heading.", MemoryID: "01990000-0000-7000-8000-000000000202", MemoryKind: "preference"}}
			}
			p, err := prepareAIContext(context.Background(), AIRequest{Mode: mode, Prompt: "Preserve the selected facts.", UseMemory: true, ModelPackID: "01990000-0000-7000-8000-000000000106"}, sources, nil, "", wordBudget())
			if err != nil {
				t.Fatal(key, err)
			}
			_, err = GeneratePreparedAI(context.Background(), p, capture(key), nil)
			if !errors.Is(err, stop) {
				t.Fatal(key, err)
			}
		}
	}
	k, e := testKernel(t)
	r, _, _ := generativeReviewFixture(t, k, e)
	_, err := k.GeneratePersonalReview(context.Background(), e, r.ID, AIBudget{ContextTokens: 8192, OutputTokens: 1024}, capture("personal-review"))
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	return rows
}

func assertLegacyGenerationProfileStorage(t *testing.T, path string) {
	t.Helper()
	d, err := ReadWorkspaceCompatibility(context.Background(), path)
	if err != nil || d.UserVersion != 3 || d.StorageGeneration != 1 {
		t.Fatal("rollback format changed", d, err)
	}
	db, err := openMigrationDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var columns, rows int
	if err = db.QueryRow("SELECT count(*) FROM pragma_table_info('ai_runs') WHERE name='generation_profile_json'").Scan(&columns); err != nil || columns != 0 {
		t.Fatal("rollback schema migrated", columns, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM ai_runs").Scan(&rows); err != nil || rows < 1 {
		t.Fatal("historical runs lost", rows, err)
	}
}
