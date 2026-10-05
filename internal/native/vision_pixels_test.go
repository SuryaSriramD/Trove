package native

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
)

func pixelTestSupervisor(t *testing.T) (*Supervisor, Pack) {
	t.Helper()
	model, projector := []byte("GGUFmodel"), []byte("GGUFprojector")
	p := visionFixture(model, projector)
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = m.ImportVision(context.Background(), p, bytes.NewReader(model), bytes.NewReader(projector), nil); err != nil {
		t.Fatal(err)
	}
	// This absent executable is intentional: nonuniform images must reach model
	// admission, while measured uniform inputs must not launch any worker.
	return &Supervisor{manager: m, bundle: t.TempDir(), helpers: map[string]Helper{"vision-worker": {Name: "vision-worker"}}}, p
}
func pixelTestFile(t *testing.T, im image.Image) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "pixels-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if err = png.Encode(f, im); err != nil {
		t.Fatal(err)
	}
	return f
}
func pixelWhiteImage(w, h int) *image.NRGBA64 {
	im := image.NewNRGBA64(image.Rect(0, 0, w, h))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	return im
}
func TestVisionExactSourceAbstentionPreservesFaintAndTinyContent(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		point         image.Point
		ink           color.NRGBA64
		lost          bool
	}{
		{"one-eight-bit-level", 40, 30, image.Pt(4, 5), color.NRGBA64{0xfefe, 0xfefe, 0xfefe, 0xffff}, false},
		{"one-sixteen-bit-level", 40, 30, image.Pt(4, 5), color.NRGBA64{0xfffe, 0xffff, 0xffff, 0xffff}, true},
		{"tiny-off-sampling-grid", 2048, 512, image.Pt(1, 1), color.NRGBA64{0, 0, 0, 0xffff}, true},
		{"tiny-on-sampling-grid", 2048, 512, image.Pt(2, 2), color.NRGBA64{0, 0, 0, 0xffff}, false},
		{"faint-transparent-mark", 40, 30, image.Pt(4, 5), color.NRGBA64{0, 0, 0, 0x0101}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, p := pixelTestSupervisor(t)
			im := pixelWhiteImage(test.width, test.height)
			im.SetNRGBA64(test.point.X, test.point.Y, test.ink)
			f := pixelTestFile(t, im)
			before, _ := os.ReadFile(f.Name())
			result, err := s.GenerateImage(context.Background(), p.ID, "What can you identify?", f, nil)
			if result.AnalysisMethod == "pixel_measurement" {
				t.Fatalf("real source content misclassified as blank/uniform: %+v", result)
			}
			if err == nil {
				t.Fatal("unexpected success with no model executable")
			}
			if test.lost && !strings.Contains(err.Error(), "NORMALIZATION_DETAIL_LOST") {
				t.Fatalf("lost source detail not disclosed: %v", err)
			}
			if !test.lost && !os.IsNotExist(err) {
				t.Fatalf("preserved visual detail did not reach the intentionally absent model executable: %v", err)
			}
			after, _ := os.ReadFile(f.Name())
			if !bytes.Equal(before, after) {
				t.Fatal("original source changed")
			}
		})
	}
}
func TestVisionExactUniformColorAndTransparencyMeasurements(t *testing.T) {
	tests := []struct {
		name     string
		pixel    color.NRGBA64
		contains string
		hidden   bool
	}{
		{"white", color.NRGBA64{0xffff, 0xffff, 0xffff, 0xffff}, "uniform white", false},
		{"red", color.NRGBA64{0xffff, 0, 0, 0xffff}, "#ffff00000000", false},
		{"black", color.NRGBA64{0, 0, 0, 0xffff}, "#000000000000", false},
		{"near-white-solid", color.NRGBA64{0xfefe, 0xfefe, 0xfefe, 0xffff}, "#fefefefefefe", false},
		{"partial-alpha-solid", color.NRGBA64{0xffff, 0, 0, 0x8080}, "alpha 32896/65535", false},
		{"transparent-hidden-colors", color.NRGBA64{}, "fully transparent", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, p := pixelTestSupervisor(t)
			im := image.NewNRGBA64(image.Rect(0, 0, 40, 30))
			draw.Draw(im, im.Bounds(), image.NewUniform(test.pixel), image.Point{}, draw.Src)
			if test.hidden {
				im.SetNRGBA64(4, 5, color.NRGBA64{0xffff, 0x1212, 0x7777, 0})
			}
			f := pixelTestFile(t, im)
			before, _ := os.ReadFile(f.Name())
			tokens := 0
			result, err := s.GenerateImage(context.Background(), p.ID, "What objects and text can you identify?", f, func(string) { tokens++ })
			if err != nil {
				t.Fatal(err)
			}
			if result.AnalysisMethod != "pixel_measurement" || result.FinishReason != "measurement" || result.ModelHash != "" || result.ProjectorHash != "" || result.HelperHash != "" || result.InputTokens != 0 || result.OutputTokens != 0 || tokens != 0 {
				t.Fatalf("measurement has false inference provenance: %+v", result)
			}
			if !strings.Contains(result.Text, test.contains) || !strings.Contains(result.Text, "40 × 30") || !strings.Contains(strings.ToLower(result.Text), "source") {
				t.Fatalf("missing source/color evidence: %s", result.Text)
			}
			if test.name != "white" && strings.Contains(result.Text, "blank") {
				t.Fatalf("colored/transparent pixels called blank: %s", result.Text)
			}
			after, _ := os.ReadFile(f.Name())
			if !bytes.Equal(before, after) {
				t.Fatal("original source changed")
			}
		})
	}
}
func TestVisionMeasurementCancellationNeverPublishes(t *testing.T) {
	s, p := pixelTestSupervisor(t)
	f := pixelTestFile(t, pixelWhiteImage(1024, 1024))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := s.GenerateImage(ctx, p.ID, "Describe", f, nil)
	if err != context.Canceled || r.Text != "" {
		t.Fatalf("cancelled image published: %+v %v", r, err)
	}
}
func TestVisionNormalizationKeepsEncodedEXIFOrientationAndOriginal(t *testing.T) {
	// Valid APP1 EXIF orientation 6 requests a 90 degree rotation. This version's
	// stated contract retains encoded orientation rather than pretending to apply it.
	im := image.NewRGBA(image.Rect(0, 0, 40, 20))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(im, image.Rect(0, 0, 10, 20), image.NewUniform(color.Black), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, im, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	size := make([]byte, 2)
	binary.BigEndian.PutUint16(size, uint16(len(exif)+2))
	raw := append([]byte{}, encoded.Bytes()[:2]...)
	raw = append(raw, 0xff, 0xe1)
	raw = append(raw, size...)
	raw = append(raw, exif...)
	raw = append(raw, encoded.Bytes()[2:]...)
	f, err := os.CreateTemp(t.TempDir(), "exif-")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.Write(raw); err != nil {
		t.Fatal(err)
	}
	normalized, w, h, err := NormalizeVisionImage(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	defer normalized.Close()
	defer os.Remove(normalized.Name())
	if w != 40 || h != 20 {
		t.Fatal("unexpected orientation transformation", w, h)
	}
	got, _, err := image.Decode(io.NewSectionReader(normalized, 0, 5<<20))
	if err != nil {
		t.Fatal(err)
	}
	left, _, _, _ := got.At(2, 10).RGBA()
	right, _, _, _ := got.At(38, 10).RGBA()
	if left > 2000 || right < 63000 {
		t.Fatal("encoded spatial structure changed")
	}
	after, _ := os.ReadFile(f.Name())
	if !bytes.Equal(after, raw) {
		t.Fatal("EXIF/source bytes were modified")
	}
}

type cancellingSourceImage struct {
	image.Image
	cancel context.CancelFunc
	reads  int
}

func (im *cancellingSourceImage) At(x, y int) color.Color {
	im.reads++
	if im.reads == 5000 {
		im.cancel()
	}
	return im.Image.At(x, y)
}
func TestVisionSourceMeasurementCancellationDuringWideRow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	im := &cancellingSourceImage{Image: image.NewNRGBA64(image.Rect(0, 0, 20000, 1)), cancel: cancel}
	text, err := measureVisionSource(ctx, im)
	if err != context.Canceled || text != "" || im.reads > 8193 {
		t.Fatalf("source scan did not stop promptly: reads=%d text=%q error=%v", im.reads, text, err)
	}
}
