package workspace

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestImageProfileProvenancePersistsWithoutBackfillingHistory(t *testing.T) {
	k, e, page, file := imageDescriptionFixture(t)
	historic := startImageDraft(t, k, e, page, file)
	old, err := k.GenerateImageDescription(context.Background(), e, historic.ID, func(context.Context, string, string, *os.File) (ImageDescriptionResult, error) {
		return imageResult(), nil
	})
	if err != nil || old.State != "ready" {
		t.Fatal(old, err)
	}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "preprocessingProfile") {
		t.Fatal("historical provenance fabricated", string(raw))
	}
	current := startImageDraft(t, k, e, page, file)
	input := imageResult()
	input.PreprocessingProfileID = "smolvlm-500m-idefics3-bounded-v1"
	input.PreprocessingProfileHash = strings.Repeat("d", 64)
	result, err := k.GenerateImageDescription(context.Background(), e, current.ID, func(context.Context, string, string, *os.File) (ImageDescriptionResult, error) { return input, nil })
	if err != nil || result.State != "ready" || result.PreprocessingProfileID != input.PreprocessingProfileID || result.PreprocessingProfileHash != input.PreprocessingProfileHash {
		t.Fatal(result, err)
	}
	stored, err := readImageDescription(context.Background(), k.db, current.ID)
	if err != nil || stored.PreprocessingProfileHash != input.PreprocessingProfileHash {
		t.Fatal(stored, err)
	}
	old, err = readImageDescription(context.Background(), k.db, historic.ID)
	if err != nil || old.PreprocessingProfileID != "" || old.PreprocessingProfileHash != "" {
		t.Fatal("historic record changed", old, err)
	}
}
func TestImageProfileRejectsPartialOrMeasurementProvenance(t *testing.T) {
	for _, mode := range []string{"id-only", "hash-only", "bad-id", "bad-hash", "measurement"} {
		t.Run(mode, func(t *testing.T) {
			k, e, page, file := imageDescriptionFixture(t)
			draft := startImageDraft(t, k, e, page, file)
			input := imageResult()
			input.PreprocessingProfileID = "bounded-v1"
			input.PreprocessingProfileHash = strings.Repeat("d", 64)
			switch mode {
			case "id-only":
				input.PreprocessingProfileHash = ""
			case "hash-only":
				input.PreprocessingProfileID = ""
			case "bad-id":
				input.PreprocessingProfileID = "<not-code>"
			case "bad-hash":
				input.PreprocessingProfileHash = "wrong"
			case "measurement":
				input.AnalysisMethod = "pixel_measurement"
				input.FinishReason = "measurement"
				input.ModelHash = ""
				input.ProjectorHash = ""
				input.HelperHash = ""
				input.InputTokens = 0
				input.OutputTokens = 0
			}
			result, err := k.GenerateImageDescription(context.Background(), e, draft.ID, func(context.Context, string, string, *os.File) (ImageDescriptionResult, error) { return input, nil })
			if err != nil || result.State != "failed" || result.Text != "" || result.PreprocessingProfileID != "" || result.PreprocessingProfileHash != "" {
				t.Fatal(result, err)
			}
		})
	}
}
