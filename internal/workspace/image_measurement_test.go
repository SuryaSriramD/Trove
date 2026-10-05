package workspace

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestImageMeasurementKeepsSourceProvenanceAndOriginal(t *testing.T) {
	k, e, page, file := imageDescriptionFixture(t)
	original, _, err := k.ReadFile(context.Background(), e, file)
	if err != nil {
		t.Fatal(err)
	}
	before, err := io.ReadAll(original)
	original.Close()
	if err != nil {
		t.Fatal(err)
	}
	r := startImageDraft(t, k, e, page, file)
	out, err := k.GenerateImageDescription(context.Background(), e, r.ID, func(context.Context, string, string, *os.File) (ImageDescriptionResult, error) {
		return ImageDescriptionResult{Text: "Source pixel measurement: fully transparent; no model interpretation.", AnalysisMethod: "pixel_measurement", FinishReason: "measurement"}, nil
	})
	if err != nil || out.State != "ready" || out.AnalysisMethod != "pixel_measurement" {
		t.Fatal(out, err)
	}
	if out.ContentHash != r.ContentHash || out.PageRevision != r.PageRevision || out.FileRevision != r.FileRevision || out.ModelHash != "" || out.ProjectorHash != "" || out.HelperHash != "" || out.InputTokens != 0 || out.OutputTokens != 0 {
		t.Fatal("measurement lost source or claimed model provenance", out)
	}
	if !strings.Contains(out.Warning, "all decoded source pixels before resizing") || !strings.Contains(out.Warning, "no model interpretation or OCR") {
		t.Fatal("ambiguous measurement basis", out.Warning)
	}
	original, _, err = k.ReadFile(context.Background(), e, file)
	if err != nil {
		t.Fatal(err)
	}
	after, err := io.ReadAll(original)
	original.Close()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("original image changed", err)
	}
	var revision string
	if err = k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&revision); err != nil || revision != r.PageRevision {
		t.Fatal("measurement mutated source page", err)
	}
	if err = k.FlushSearch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(querySearch(t, k, e, "Source pixel measurement", nil)["hits"].([]any)) != 0 {
		t.Fatal("measurement entered canonical search")
	}
}
func TestImageMeasurementRejectsFalseModelProvenance(t *testing.T) {
	for _, kind := range []string{"model", "projector", "helper", "tokens", "memory", "invalid-text"} {
		t.Run(kind, func(t *testing.T) {
			k, e, page, file := imageDescriptionFixture(t)
			r := startImageDraft(t, k, e, page, file)
			result := ImageDescriptionResult{Text: "Source pixel measurement", AnalysisMethod: "pixel_measurement", FinishReason: "measurement"}
			switch kind {
			case "model":
				result.ModelHash = strings.Repeat("a", 64)
			case "projector":
				result.ProjectorHash = strings.Repeat("b", 64)
			case "helper":
				result.HelperHash = strings.Repeat("c", 64)
			case "tokens":
				result.InputTokens = 1
			case "memory":
				result.PeakRSSBytes = 1024
			case "invalid-text":
				result.Text = string([]byte{0xff})
			}
			out, err := k.GenerateImageDescription(context.Background(), e, r.ID, func(context.Context, string, string, *os.File) (ImageDescriptionResult, error) { return result, nil })
			if err != nil || out.State != "failed" || out.Text != "" {
				t.Fatal("invalid measurement published", out, err)
			}
		})
	}
}
func TestImageMeasurementDiscardsCancellationAndChangedSource(t *testing.T) {
	for _, kind := range []string{"cancel", "source-edit", "revocation"} {
		t.Run(kind, func(t *testing.T) {
			k, e, page, file := imageDescriptionFixture(t)
			r := startImageDraft(t, k, e, page, file)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out, err := k.GenerateImageDescription(ctx, e, r.ID, func(context.Context, string, string, *os.File) (ImageDescriptionResult, error) {
				switch kind {
				case "cancel":
					cancel()
				case "source-edit":
					testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Changed while measuring"}}, "page", page))
				case "revocation":
					k.RevokeGrant(string(e.ScopeGrantId))
				}
				return ImageDescriptionResult{Text: "Source measurement must never publish", AnalysisMethod: "pixel_measurement", FinishReason: "measurement"}, nil
			})
			expected := "stale"
			if kind == "cancel" {
				expected = "cancelled"
			}
			if err != nil || out.State != expected || out.Text != "" {
				t.Fatal("stale measurement published", out, err)
			}
		})
	}
}
