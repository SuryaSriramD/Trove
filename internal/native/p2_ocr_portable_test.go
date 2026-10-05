package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func ocrPixelFile(t *testing.T, img image.Image) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "image-*.png")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if err = png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Seek(0, 0)
	return f
}
func TestPortableOCRFullResolutionAlphaAndOriginal(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4000, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4000; x++ {
			img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
		}
	}
	img.SetNRGBA(3999, 1, color.NRGBA{0, 0, 0, 255})
	img.SetNRGBA(3, 0, color.NRGBA{0, 0, 0, 0})
	img.SetNRGBA(4, 0, color.NRGBA{0, 0, 0, 128})
	img.SetNRGBA(5, 0, color.NRGBA{254, 254, 254, 255})
	input := ocrPixelFile(t, img)
	original, _ := os.ReadFile(input.Name())
	out, _ := os.CreateTemp(t.TempDir(), "pixels")
	defer out.Close()
	if err := prepareOCRPixels(context.Background(), input, out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out.Name())
	if string(raw[:4]) != "TOCR" || binary.BigEndian.Uint32(raw[4:8]) != 4000 || binary.BigEndian.Uint32(raw[8:12]) != 2 || len(raw) != 8012 {
		t.Fatal("dimensions lost")
	}
	if raw[12+7999] != 0 || raw[12+3] != 255 || raw[12+4] != 127 || raw[12+5] != 254 {
		t.Fatal("tiny, alpha or faint pixels changed", raw[12+7999], raw[15:18])
	}
	current, _ := os.ReadFile(input.Name())
	if !bytes.Equal(original, current) {
		t.Fatal("source changed")
	}
}
func TestPortableOCRInputBoundsAndCancellation(t *testing.T) {
	f := ocrPixelFile(t, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	out, _ := os.CreateTemp(t.TempDir(), "pixels")
	defer out.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := prepareOCRPixels(ctx, f, out); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if st, _ := out.Stat(); st.Size() != 0 {
		t.Fatal("cancelled preparation wrote pixels")
	}
	f.Truncate(0)
	f.Seek(0, 0)
	f.WriteString("not a PNG or JPEG")
	if err := prepareOCRPixels(context.Background(), f, out); err == nil {
		t.Fatal("accepted malformed input")
	}
	f.Truncate((20 << 20) + 1)
	if err := prepareOCRPixels(context.Background(), f, out); err == nil {
		t.Fatal("accepted oversized input")
	}
}
func TestPortableOCRManifestRejectsIdentityAndDowngrade(t *testing.T) {
	m := ocrManifest{SchemaVersion: 1, Name: "ocr-worker", OS: runtime.GOOS, Architecture: runtime.GOARCH, Runtime: "tesseract-5.5.1-leptonica-1.85.0-eng-v1", SHA256: strings.Repeat("a", 64), ModelSHA256: ocrModelSHA256}
	for _, kind := range []string{"os", "arch", "runtime", "model", "test-only", "digest"} {
		t.Run(kind, func(t *testing.T) {
			bad := m
			switch kind {
			case "os":
				bad.OS = "invalid"
			case "arch":
				bad.Architecture = "invalid"
			case "runtime":
				bad.Runtime = "latest"
			case "model":
				bad.ModelSHA256 = strings.Repeat("b", 64)
			case "test-only":
				bad.TestOnly = true
			case "digest":
				bad.SHA256 = strings.Repeat("z", 64)
			}
			bundle := t.TempDir()
			data, _ := json.Marshal(bad)
			os.WriteFile(filepath.Join(bundle, "ocr-worker.json"), data, 0600)
			if _, err := readOCRManifest(bundle); err == nil {
				t.Fatal("accepted incompatible identity")
			}
			if OCRAvailable(bundle) {
				t.Fatal("bad portable manifest advertised ready")
			}
		})
	}
	bundle := t.TempDir()
	os.WriteFile(filepath.Join(bundle, "ocr-worker.json"), []byte("{"), 0600)
	original := []byte("legacy helper")
	digest := sha256.Sum256(original)
	os.WriteFile(filepath.Join(bundle, "ocr-worker"), original, 0700)
	os.WriteFile(filepath.Join(bundle, "ocr-worker.sha256"), []byte(hex.EncodeToString(digest[:])), 0600)
	if OCRAvailable(bundle) {
		t.Fatal("invalid portable manifest silently downgraded to Apple engine")
	}
}
func TestPortableOCRNativeEnglishProvenanceCancellation(t *testing.T) {
	bundle, fixture := os.Getenv("TROVE_PORTABLE_OCR_BUNDLE"), os.Getenv("TROVE_OCR_FIXTURE")
	if bundle == "" || fixture == "" {
		t.Skip("requires built portable OCR and original printed-text fixture")
	}
	capability, err := OCRCapabilities(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if capability.Engine != "Tesseract 5.5.1 (local)" || capability.ModelSHA256 != ocrModelSHA256 || strings.Join(capability.Languages, ",") != "English" {
		t.Fatal(capability)
	}
	f, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	original, _ := os.ReadFile(fixture)
	result, err := RunLocalOCR(context.Background(), f, bundle)
	if err != nil {
		t.Fatal(err)
	}
	var accepted []string
	for _, line := range result.Lines {
		if line.Confidence >= 0.5 {
			accepted = append(accepted, line.Text)
		}
	}
	text := strings.Join(accepted, "\n")
	for _, expected := range []string{"TROVE LOCAL OCR 2026", "Review sources before approval"} {
		if !strings.Contains(text, expected) {
			t.Fatal("missing independent expected phrase", expected, text)
		}
	}
	if result.Capability.ExtractorVersion != portableOCRVersion || !strings.Contains(result.Warning, "English printed text only") {
		t.Fatal("incorrect provenance", result)
	}
	t.Logf("engine=%s model=%s text=%q", result.Capability.Engine, result.Capability.ModelSHA256, text)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err = RunLocalOCR(ctx, f, bundle); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancellation not preserved", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancel did not bound worker")
	}
	tampered := t.TempDir()
	for _, name := range []string{portableOCRName(), "ocr-worker.json", "eng.traineddata"} {
		raw, err := os.ReadFile(filepath.Join(bundle, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "eng.traineddata" {
			raw[0] ^= 1
		}
		if err = os.WriteFile(filepath.Join(tampered, name), raw, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if OCRAvailable(tampered) {
		t.Fatal("modified English model advertised available")
	}
	if _, err = RunLocalOCR(context.Background(), f, tampered); err == nil {
		t.Fatal("modified model reached worker")
	}
	current, _ := os.ReadFile(fixture)
	if !bytes.Equal(original, current) {
		t.Fatal("original changed")
	}
}

func TestOCRBlockedProcessCancellation(t *testing.T) {
	if os.Getenv("TROVE_OCR_TEST_CHILD") == "1" {
		var start [1]byte
		os.Stdin.Read(start[:])
		os.WriteFile("started", []byte("yes"), 0600)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	private, err := privateWorkerDirectory("trove-ocr-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(private)
	f, err := os.Create(filepath.Join(private, "input"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOCRBlockedProcessCancellation$")
	cmd.Dir = private
	cmd.Env = append(workerEnvironment(private), "TROVE_OCR_TEST_CHILD=1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reached := make(chan bool, 1)
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(filepath.Join(private, "started")); err == nil {
				reached <- true
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		reached <- false
	}()
	started := time.Now()
	if _, err = runOCRProcess(ctx, cmd, []*os.File{f, f}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !<-reached || time.Since(started) > 3*time.Second {
		t.Fatal("active process was not promptly cancelled")
	}
}
