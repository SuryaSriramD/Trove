package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestP2OCRHelperIntegrityAndMissingCapability(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Apple Vision helper is macOS only")
	}
	bundle := t.TempDir()
	if OCRAvailable(bundle) {
		t.Fatal("missing helper advertised available")
	}
	helper := filepath.Join(bundle, "ocr-worker")
	original := []byte("verified fixture executable")
	sum := sha256.Sum256(original)
	if err := os.WriteFile(helper, original, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper+".sha256", []byte(hex.EncodeToString(sum[:])), 0600); err != nil {
		t.Fatal(err)
	}
	if !OCRAvailable(bundle) {
		t.Fatal("valid digest rejected")
	}
	if err := os.WriteFile(helper, []byte("tampered helper"), 0700); err != nil {
		t.Fatal(err)
	}
	if OCRAvailable(bundle) {
		t.Fatal("tampered helper accepted")
	}
}
func TestP2OCRNativeFixture(t *testing.T) {
	bundle, fixture := os.Getenv("TROVE_OCR_BUNDLE"), os.Getenv("TROVE_OCR_FIXTURE")
	if bundle == "" || fixture == "" {
		t.Skip("set TROVE_OCR_BUNDLE and TROVE_OCR_FIXTURE for actual Apple Vision qualification")
	}
	f, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	result, err := RunLocalOCR(context.Background(), f, bundle)
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{}
	for _, line := range result.Lines {
		if line.Confidence >= 0.5 {
			texts = append(texts, line.Text)
		}
	}
	joined := strings.Join(texts, "\n")
	for _, expected := range []string{"TROVE LOCAL OCR 2026", "Review sources before approval"} {
		if !strings.Contains(joined, expected) {
			t.Fatal("missing reference text", expected, joined)
		}
	}
}
