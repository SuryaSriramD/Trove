package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
)

// This separate product-path evaluation does not alter TestVisionRealRubric.
// Its four PNG hashes and answer predicates match the original raw-model rubric.
// Only the blank product case may use an explicitly labeled source measurement;
// all nonblank cases must still return complete answers from the real local model.
func TestVisionProductionRubric(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_VISION_INTEGRATION") != "1" {
		t.Skip("explicit real vision product qualification")
	}
	m, err := NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), m)
	if err != nil {
		t.Fatal(err)
	}
	scenes := []struct {
		name, question, hash string
		draw                 func(*image.RGBA)
		accept               func(string) bool
	}{
		{"red_square", "What color is the square? Answer briefly.", "11abcd871cec8f5b572c55831211de12a54b5792147382f04f5c153b71334424", func(im *image.RGBA) {
			draw.Draw(im, image.Rect(64, 64, 192, 192), image.NewUniform(color.RGBA{255, 0, 0, 255}), image.Point{}, draw.Src)
		}, func(s string) bool { return strings.Contains(s, "red") }},
		{"spatial", "Which side is the blue circle on relative to the red square?", "c300c685c0da8e08eac984ba04ffb4a72e32ebf2989ef7be2dd3799ab40bf4b6", func(im *image.RGBA) {
			draw.Draw(im, image.Rect(25, 85, 100, 160), image.NewUniform(color.RGBA{255, 0, 0, 255}), image.Point{}, draw.Src)
			for y := 75; y < 170; y++ {
				for x := 155; x < 245; x++ {
					if (x-200)*(x-200)+(y-122)*(y-122) < 42*42 {
						im.SetRGBA(x, y, color.RGBA{0, 0, 255, 255})
					}
				}
			}
		}, func(s string) bool { return strings.Contains(s, "right") }},
		{"labeled_chart", "In the chart, what are the values for A and B?", "cbacae11c3cf49e4346deedce27bd3e05d04069ffa1c3ed4b62cfabf116ce6cb", visionChartFixture, func(s string) bool { return strings.Contains(s, "2") && strings.Contains(s, "4") }},
		{"blank_uncertainty", "What objects and text can you identify?", "d79623d7050280a496c43f0a3acd51c7a13e09f3d3bc2ba1dcf390ea1893a1a5", func(*image.RGBA) {}, func(s string) bool {
			return (strings.Contains(s, "blank") || strings.Contains(s, "no object") || strings.Contains(s, "no text")) && !strings.Contains(s, "black text") && !strings.Contains(s, "text reads")
		}},
	}
	rows := []map[string]any{}
	for _, scene := range scenes {
		im := image.NewRGBA(image.Rect(0, 0, 256, 256))
		draw.Draw(im, im.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		scene.draw(im)
		f, err := os.CreateTemp(t.TempDir(), scene.name)
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, im); err != nil {
			t.Fatal(err)
		}
		original, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(original)
		hash := hex.EncodeToString(sum[:])
		if hash != scene.hash {
			t.Fatal("fixture changed from unchanged raw rubric", scene.name, hash)
		}
		started := time.Now()
		result, runErr := s.GenerateImage(context.Background(), "01990000-0000-7000-8000-000000000107", scene.question, f, nil)
		f.Close()
		provenance := result.AnalysisMethod == "local_vision_model" && result.FinishReason == "eos" && result.ModelHash != "" && result.ProjectorHash != "" && result.HelperHash != ""
		if scene.name == "blank_uncertainty" {
			provenance = result.AnalysisMethod == "pixel_measurement" && result.FinishReason == "measurement" && result.ModelHash == "" && result.ProjectorHash == "" && result.HelperHash == "" && result.InputTokens == 0 && result.OutputTokens == 0
		}
		passed := runErr == nil && provenance && scene.accept(strings.ToLower(result.Text))
		row := map[string]any{"case": scene.name, "question": scene.question, "passed": passed, "result": result, "seconds": time.Since(started).Seconds(), "fixtureSha256": hash, "scope": "production-path behavior; separate from raw model quality"}
		if runErr != nil {
			row["error"] = runErr.Error()
		}
		rows = append(rows, row)
		t.Logf("%s passed=%v method=%s answer=%s error=%v", scene.name, passed, result.AnalysisMethod, result.Text, runErr)
	}
	raw, _ := json.MarshalIndent(rows, "", "  ")
	if path := os.Getenv("TROVE_VISION_PRODUCT_REPORT"); path != "" {
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range rows {
		if !row["passed"].(bool) {
			t.Errorf("production rubric failed: %s", row["case"])
		}
	}
}
