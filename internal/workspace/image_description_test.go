package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func imageDescriptionFixture(t *testing.T) (*Kernel, contracts.ServiceExecutionContext, string, string) {
	t.Helper()
	k, e := testKernel(t)
	page := createPage(t, k, e, "Image source")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 20, 20))); err != nil {
		t.Fatal(err)
	}
	file, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, b.String()), false)
	return k, e, page, file
}
func startImageDraft(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page, file string) ImageDescription {
	t.Helper()
	out := testOK(t, testDispatch(t, k, e, "image.start", recoveryRequest(k, map[string]any{"pageId": page, "fileId": file, "modelPackId": NewID(), "question": "Describe the image"})))
	r, err := readImageDescription(context.Background(), k.db, str(out["id"]))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func imageResult() ImageDescriptionResult {
	return ImageDescriptionResult{Text: "A red square. UniqueDerivedVisionPhrase", ModelHash: strings.Repeat("a", 64), ProjectorHash: strings.Repeat("b", 64), HelperHash: strings.Repeat("c", 64), FinishReason: "eos", AnalysisMethod: "local_vision_model", InputTokens: 176, OutputTokens: 6}
}
func TestImageDescriptionDraftIsScopedFreshAndInert(t *testing.T) {
	k, e, page, file := imageDescriptionFixture(t)
	ctx := context.Background()
	r := startImageDraft(t, k, e, page, file)
	var pageRev string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&pageRev)
	result, err := k.GenerateImageDescription(ctx, e, r.ID, func(ctx context.Context, model, question string, f *os.File) (ImageDescriptionResult, error) {
		if model != r.ModelPackID || question != r.Question {
			t.Fatal("request changed")
		}
		if _, _, err := image.DecodeConfig(f); err != nil {
			t.Fatal(err)
		}
		return imageResult(), nil
	})
	if err != nil || result.State != "ready" || result.AnalysisMethod != "local_vision_model" {
		t.Fatal(result, err)
	}
	var after string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&after)
	if after != pageRev {
		t.Fatal("description mutated source")
	}
	if err = k.FlushSearch(ctx); err != nil {
		t.Fatal(err)
	}
	if len(querySearch(t, k, e, "UniqueDerivedVisionPhrase", nil)["hits"].([]any)) != 0 {
		t.Fatal("derived hallucination entered search")
	}
	other := createPage(t, k, e, "Other")
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(other)}}})
	testCode(t, testDispatch(t, k, scoped, "image.list", recoveryRequest(k, map[string]any{"pageId": page})), "SCOPE_DENIED")
	testCode(t, testDispatch(t, k, e, "image.start", recoveryRequest(k, map[string]any{"pageId": other, "fileId": file, "modelPackId": NewID(), "question": "Hidden attachment"})), "NOT_FOUND")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Changed source"}}, "page", page))
	list := testOK(t, testDispatch(t, k, e, "image.list", recoveryRequest(k, map[string]any{"pageId": page})))
	if list["runs"].([]any)[0].(map[string]any)["stale"] != true {
		t.Fatal("stale source not labeled", list)
	}
}
func TestImageDescriptionCancelAndMidFlightSourceChange(t *testing.T) {
	for _, mode := range []string{"cancel", "edit", "revoke"} {
		t.Run(mode, func(t *testing.T) {
			k, e, page, file := imageDescriptionFixture(t)
			r := startImageDraft(t, k, e, page, file)
			out, err := k.GenerateImageDescription(context.Background(), e, r.ID, func(context.Context, string, string, *os.File) (ImageDescriptionResult, error) {
				switch mode {
				case "cancel":
					testOK(t, testDispatch(t, k, e, "image.cancel", recoveryRequest(k, map[string]any{"runId": r.ID})))
				case "edit":
					testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Changed during model call"}}, "page", page))
				case "revoke":
					k.RevokeGrant(string(e.ScopeGrantId))
				}
				return imageResult(), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			want := "stale"
			if mode == "cancel" {
				want = "cancelled"
			}
			if out.State != want || out.Text != "" {
				t.Fatal(out)
			}
		})
	}
}
func TestImageDescriptionContractAndMissingCapabilityFailures(t *testing.T) {
	k, e, page, file := imageDescriptionFixture(t)
	request := recoveryRequest(k, map[string]any{"pageId": page, "fileId": file, "modelPackId": NewID(), "question": "Q", "path": "/private/secret"})
	testCode(t, testDispatch(t, k, e, "image.start", request), "INVALID_REQUEST")
	delete(request, "path")
	request["question"] = strings.Repeat("x", 2049)
	testCode(t, testDispatch(t, k, e, "image.start", request), "INVALID_REQUEST")
	r := startImageDraft(t, k, e, page, file)
	out, err := k.GenerateImageDescription(context.Background(), e, r.ID, func(context.Context, string, string, *os.File) (ImageDescriptionResult, error) {
		return ImageDescriptionResult{}, Fail("MODEL_NOT_INSTALLED", "Choose a verified pair")
	})
	if err != nil || out.State != "failed" || out.Text != "" {
		t.Fatal(out, err)
	}
	r = startImageDraft(t, k, e, page, file)
	out, err = k.GenerateImageDescription(context.Background(), e, r.ID, func(context.Context, string, string, *os.File) (ImageDescriptionResult, error) {
		return ImageDescriptionResult{Text: "Measured uniform pixels; no model run.", AnalysisMethod: "pixel_measurement", FinishReason: "measurement"}, nil
	})
	if err != nil || out.State != "ready" || out.ModelHash != "" || out.AnalysisMethod != "pixel_measurement" {
		t.Fatal(out, err)
	}
}

func TestImageDescriptionRestartAndPortableImportDoNotReplay(t *testing.T) {
	k, e, page, file := imageDescriptionFixture(t)
	r := startImageDraft(t, k, e, page, file)
	original, _, err := k.ReadFile(context.Background(), e, file)
	if err != nil {
		t.Fatal(err)
	}
	originalBytes, err := io.ReadAll(original)
	original.Close()
	if err != nil {
		t.Fatal(err)
	}
	path := k.Path()
	k.Close()
	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	owner := opened.OwnerContext()
	saved, err := readImageDescription(context.Background(), opened.db, r.ID)
	if err != nil || saved.State != "cancelled" {
		t.Fatal(saved, err)
	}
	dest := filepath.Join(t.TempDir(), "images.zip")
	if _, err = opened.ExportPortable(context.Background(), owner, dest); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := map[string]string{}
	for _, item := range archive.File {
		f, err := item.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[item.Name] = string(raw)
	}
	restored, restoredOwner := testKernel(t)
	plan := importFixture(t, restored, restoredOwner, entries)
	completeImportFixture(t, restored, restoredOwner, plan)
	var count int
	if err = restored.db.QueryRow("SELECT count(*) FROM image_descriptions").Scan(&count); err != nil || count != 0 {
		t.Fatal("portable import replayed model drafts", count, err)
	}
	// Import also preserves archive/source evidence, so the total file count is
	// not the image-preservation oracle. Check the actual original digest/bytes.
	var restoredFile string
	if err = restored.db.QueryRow("SELECT id FROM files WHERE content_hash=?", r.ContentHash).Scan(&restoredFile); err != nil {
		t.Fatal("original image digest lost", err)
	}
	imageFile, _, err := restored.ReadFile(context.Background(), restoredOwner, restoredFile)
	if err != nil {
		t.Fatal(err)
	}
	defer imageFile.Close()
	restoredBytes, err := io.ReadAll(imageFile)
	if err != nil || !bytes.Equal(originalBytes, restoredBytes) {
		t.Fatal("original image bytes changed", err)
	}
}
