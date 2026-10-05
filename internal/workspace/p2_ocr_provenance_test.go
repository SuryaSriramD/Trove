package workspace

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
)

func TestOCRPortableProvenanceAndRevocationBeforePublish(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "OCR source")
	var data bytes.Buffer
	png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	file, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, data.String()), true)
	capability := OCRCapability{Engine: "Tesseract 5.5.1 (local)", ExtractorVersion: "test-tesseract-English-sha256", Languages: []string{"English"}, Limitations: "English printed text", ModelSHA256: strings.Repeat("a", 64)}
	provider := func(context.Context, *os.File) (OCRResult, error) {
		return OCRResult{Lines: []OCRLine{{Text: "Independent printed witness", Confidence: .98}}, Warning: "English only"}, nil
	}
	k.SetOCRProviderWithCapability(provider, capability)
	status := testOK(t, testDispatch(t, k, e, "ocr.status", recoveryRequest(k, nil)))
	if status["engine"] != capability.Engine || status["extractorVersion"] != capability.ExtractorVersion {
		t.Fatal(status)
	}
	out := testOK(t, testDispatch(t, k, e, "ocr.run", recoveryRequest(k, map[string]any{"fileId": file})))
	if out["engine"] != capability.Engine || out["modelSha256"] != capability.ModelSHA256 || out["extractorVersion"] != capability.ExtractorVersion {
		t.Fatal(out)
	}
	var version, heading string
	k.db.QueryRow("SELECT extractor_version FROM document_extractions WHERE file_id=?", file).Scan(&version)
	k.db.QueryRow("SELECT heading FROM document_sections WHERE file_id=?", file).Scan(&heading)
	if version != capability.ExtractorVersion || !strings.Contains(heading, capability.Engine) {
		t.Fatal("persisted provenance lost", version, heading)
	}
	k.SetOCRProviderWithCapability(func(context.Context, *os.File) (OCRResult, error) {
		k.RevokeGrant(string(e.ScopeGrantId))
		return OCRResult{Lines: []OCRLine{{Text: "Must not publish after revoke", Confidence: 1}}}, nil
	}, capability)
	result := testDispatch(t, k, e, "ocr.run", recoveryRequest(k, map[string]any{"fileId": file}))
	if result["ok"] != false {
		t.Fatal("revoked scope published OCR")
	}
	var text string
	if err := k.db.QueryRow("SELECT body FROM document_sections WHERE file_id=?", file).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != "Independent printed witness" {
		t.Fatal("revoked output persisted")
	}
}
func TestOCRProviderChangeDoesNotPublish(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Image")
	var data bytes.Buffer
	png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	file, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, data.String()), true)
	k.SetOCRProvider(func(context.Context, *os.File) (OCRResult, error) {
		k.SetOCRProvider(nil)
		return OCRResult{Lines: []OCRLine{{Text: "stale engine", Confidence: 1}}}, nil
	})
	testCode(t, testDispatch(t, k, e, "ocr.run", recoveryRequest(k, map[string]any{"fileId": file})), "CONFLICT")
	var count int
	k.db.QueryRow("SELECT count(*) FROM document_sections WHERE file_id=?", file).Scan(&count)
	if count != 0 {
		t.Fatal("stale extraction persisted")
	}
}

func TestOCRCancelledProviderDoesNotPublish(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Image")
	var data bytes.Buffer
	png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	file, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, data.String()), true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k.SetOCRProvider(func(context.Context, *os.File) (OCRResult, error) {
		cancel()
		return OCRResult{Lines: []OCRLine{{Text: "cancelled output", Confidence: 1}}}, nil
	})
	if _, err := runOCR(ctx, k, e, map[string]any{"fileId": file}); err != context.Canceled {
		t.Fatal(err)
	}
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM document_sections WHERE file_id=?", file).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("cancelled extraction persisted")
	}
}
