package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// These independent synthetic fixtures expose transport geometry and pixel identity.
// They do not invoke a model or assert general vision quality. Optional retained
// outputs are for comparison with the exact native processor outside this test.
func TestVisionPreprocessingTransportFixtures(t *testing.T) {
	fixtures := []struct {
		name    string
		w, h    int
		pattern string
	}{
		{"landscape-769x257", 769, 257, "geometry"},
		{"portrait-257x769", 257, 769, "geometry"},
		{"square-512", 512, 512, "geometry"},
		{"grid-boundary-1023x513", 1023, 513, "geometry"},
		{"large-2049x1025", 2049, 1025, "geometry"},
		{"red-channel", 512, 512, "red"},
		{"green-channel", 512, 512, "green"},
		{"blue-channel", 512, 512, "blue"},
		{"rgb-bars", 765, 255, "bars"},
		{"impulse-513", 513, 257, "impulse"},
		{"off-grid-impulse-1537", 1537, 513, "impulse"},
		{"alpha-composite", 257, 129, "alpha"},
	}
	output := os.Getenv("TROVE_VISION_PREPROCESS_OUT")
	if output != "" {
		if err := os.Mkdir(output, 0700); err != nil {
			t.Fatalf("fresh output directory required: %v", err)
		}
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			src := image.NewNRGBA(image.Rect(0, 0, fixture.w, fixture.h))
			for y := 0; y < fixture.h; y++ {
				for x := 0; x < fixture.w; x++ {
					c := color.NRGBA{240, 240, 240, 255}
					switch fixture.pattern {
					case "red":
						c = color.NRGBA{255, 0, 0, 255}
					case "green":
						c = color.NRGBA{0, 255, 0, 255}
					case "blue":
						c = color.NRGBA{0, 0, 255, 255}
					case "bars":
						c = []color.NRGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}}[min(2, x*3/fixture.w)]
					case "alpha":
						c = color.NRGBA{17, 91, 203, 128}
					case "geometry":
						if x < fixture.w/3 {
							c = color.NRGBA{220, 30, 20, 255}
						} else if y < fixture.h/2 {
							c = color.NRGBA{30, 210, 40, 255}
						} else {
							c = color.NRGBA{20, 40, 230, 255}
						}
						if x == fixture.w/2 || y == fixture.h/2 {
							c = color.NRGBA{0, 0, 0, 255}
						}
					case "impulse":
						if x == 2 && y == 2 {
							c = color.NRGBA{255, 0, 0, 255}
						}
					}
					src.SetNRGBA(x, y, c)
				}
			}
			path := filepath.Join(t.TempDir(), "source.png")
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err = png.Encode(f, src); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(before)
			normalized, w, h, err := NormalizeVisionImage(context.Background(), f)
			if err != nil {
				t.Fatal(err)
			}
			defer normalized.Close()
			defer os.Remove(normalized.Name())
			ew, eh := fixture.w, fixture.h
			if max(ew, eh) > 1024 {
				if ew >= eh {
					eh = max(1, eh*1024/ew)
					ew = 1024
				} else {
					ew = max(1, ew*1024/eh)
					eh = 1024
				}
			}
			if w != ew || h != eh {
				t.Fatalf("transport geometry got %dx%d want %dx%d", w, h, ew, eh)
			}
			st, err := normalized.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().Perm() != 0600 {
				t.Fatalf("private output mode %v", st.Mode())
			}
			actual, _, err := image.Decode(io.NewSectionReader(normalized, 0, st.Size()))
			if err != nil {
				t.Fatal(err)
			}
			if fixture.pattern == "red" || fixture.pattern == "green" || fixture.pattern == "blue" {
				got := color.NRGBAModel.Convert(actual.At(w/2, h/2)).(color.NRGBA)
				want := src.NRGBAAt(fixture.w/2, fixture.h/2)
				if got != want {
					t.Fatalf("channel mismatch got %v want %v", got, want)
				}
			}
			if fixture.pattern == "alpha" {
				// Independent integer white-composition expectation for this exact input.
				if got := color.NRGBAModel.Convert(actual.At(0, 0)).(color.NRGBA); got != (color.NRGBA{135, 172, 228, 255}) {
					t.Fatalf("alpha pixel %v", got)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if sha256.Sum256(after) != hash {
				t.Fatal("original bytes changed")
			}
			if output != "" {
				dir := filepath.Join(output, fixture.name)
				if err = os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, "source.png"), before, 0600); err != nil {
					t.Fatal(err)
				}
				b, err := os.ReadFile(normalized.Name())
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, "transport.png"), b, 0600); err != nil {
					t.Fatal(err)
				}
				nh := sha256.Sum256(b)
				meta := map[string]any{"id": fixture.name, "pattern": fixture.pattern, "sourceWidth": fixture.w, "sourceHeight": fixture.h, "transportWidth": w, "transportHeight": h, "sourceSHA256": hex.EncodeToString(hash[:]), "transportSHA256": hex.EncodeToString(nh[:]), "sourceUnchanged": true, "modelInvoked": false}
				m, err := json.MarshalIndent(meta, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, "manifest.json"), append(m, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			fmt.Printf("preprocessing fixture=%s original=%dx%d transport=%dx%d sourceUnchanged=true modelInvoked=false\n", fixture.name, fixture.w, fixture.h, w, h)
		})
	}
}
