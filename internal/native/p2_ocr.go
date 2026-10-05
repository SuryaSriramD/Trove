package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type OCRLine struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}
type OCRResult struct {
	Capability OCRCapability `json:"-"`
	Lines      []OCRLine     `json:"lines"`
	Warning    string        `json:"warning,omitempty"`
}

// OCRAvailable checks the optional helper independently from model workers.
func OCRAvailable(bundle string) bool { _, err := OCRCapabilities(bundle); return err == nil }
func verifyAppleOCRHelper(bundle string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("local OCR helper requires macOS")
	}
	path := filepath.Join(bundle, "ocr-worker")
	if err := regular(path); err != nil {
		return err
	}
	expected, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.TrimSpace(string(expected)) {
		return errors.New("OCR helper digest mismatch")
	}
	return nil
}

type ocrOutput struct{ bytes.Buffer }

func (w *ocrOutput) Write(b []byte) (int, error) {
	if w.Len()+len(b) > 2<<20 {
		return 0, errors.New("OCR worker output exceeds 2 MiB")
	}
	return w.Buffer.Write(b)
}

// RunLocalOCR gives the child only an authorized file descriptor; it cannot
// query or mutate a workspace. The caller owns input validation and persistence.
func RunLocalOCR(ctx context.Context, f *os.File, bundle string) (OCRResult, error) {
	var result OCRResult
	if hasPortableOCR(bundle) {
		return runPortableOCR(ctx, f, bundle)
	}
	if err := verifyAppleOCRHelper(bundle); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(bundle, "ocr-worker"))
	cmd.ExtraFiles = []*os.File{f}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8"}
	var output ocrOutput
	cmd.Stdout = &output
	var diagnostic ringLog
	cmd.Stderr = &diagnostic
	if err := cmd.Run(); err != nil {
		return result, errors.New("local OCR worker failed or exceeded its 60-second deadline")
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return result, err
	}
	result.Capability = OCRCapability{Engine: "Apple Vision (local)", ExtractorVersion: "trove-local-vision-v1", Languages: []string{"Automatic detection (OS-supported languages)"}, Limitations: "Handwriting, columns, low contrast and rotation may be inaccurate; verify against the original."}
	return result, nil
}
