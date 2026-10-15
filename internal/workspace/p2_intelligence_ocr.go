package workspace

import (
	"context"
	"database/sql"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"trove.local/phase0/contracts"
)

const OCRExtractorVersion = "trove-local-vision-v1"

type OCRLine struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}
type OCRResult struct {
	Lines   []OCRLine `json:"lines"`
	Warning string    `json:"warning,omitempty"`
}
type OCRCapability struct {
	Engine           string   `json:"engine"`
	ExtractorVersion string   `json:"extractorVersion"`
	Languages        []string `json:"languages"`
	Limitations      string   `json:"limitations"`
	ModelSHA256      string   `json:"modelSha256,omitempty"`
}
type OCRProvider func(context.Context, *os.File) (OCRResult, error)
type ocrRuntime struct {
	provider   OCRProvider
	capability OCRCapability
	mu         sync.Mutex
	flight     *ocrFlight
	cancelled  map[string]time.Time
	closed     bool
	suspended  int
}

var ocrProviders sync.Map

func (k *Kernel) SetOCRProvider(p OCRProvider) {
	k.SetOCRProviderWithCapability(p, OCRCapability{Engine: "Apple Vision (local)", ExtractorVersion: OCRExtractorVersion, Languages: []string{"Automatic detection (OS-supported languages)"}})
}
func (k *Kernel) SetOCRProviderWithCapability(p OCRProvider, capability OCRCapability) {
	if old, ok := ocrProviders.LoadAndDelete(k); ok {
		old.(*ocrRuntime).stop()
	}
	if p != nil {
		capability.Languages = append([]string(nil), capability.Languages...)
		ocrProviders.Store(k, &ocrRuntime{provider: p, capability: capability, cancelled: map[string]time.Time{}})
	}
}
func init() {
	RegisterQuery("ocr.status", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		provider, ready := ocrProviders.Load(k)
		capability := OCRCapability{Engine: "Unavailable"}
		if ready {
			capability = provider.(*ocrRuntime).capability
		}
		return map[string]any{"available": ready, "engine": capability.Engine, "languages": capability.Languages, "limitations": capability.Limitations, "extractorVersion": capability.ExtractorVersion, "modelSha256": capability.ModelSHA256, "maxBytes": 20 << 20, "maxPixels": 20000000, "timeoutSeconds": 60, "minimumConfidence": 0.5, "formats": []string{"image/png", "image/jpeg"}, "visionDescriptionAvailable": false}, nil
	})
	RegisterQuery("ocr.run", runOCR)
	RegisterQuery("ocr.cancel", cancelOCR)
}
func runOCR(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if e.Origin != "user" || e.Actor.Kind != "user" {
		return nil, Fail("SCOPE_DENIED", "OCR requires a direct user request")
	}
	fileID := str(r["fileId"])
	f, meta, err := k.ReadFile(ctx, e, fileID)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 20<<20 || meta.MediaType != "image/png" && meta.MediaType != "image/jpeg" {
		return nil, Fail("INVALID_REQUEST", "Local OCR accepts PNG/JPEG images up to 20 MiB")
	}
	config, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, Fail("INVALID_REQUEST", "Image header cannot be decoded")
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 20000000 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Local OCR accepts at most 20 megapixels")
	}
	if _, err = f.Seek(0, 0); err != nil {
		return nil, err
	}
	p, ok := ocrProviders.Load(k)
	if !ok {
		return nil, Fail("WORKER_FAILED", "The compatible local OCR helper is unavailable; install the OCR-enabled native bundle")
	}
	runtime := p.(*ocrRuntime)
	bounded, finish, err := runtime.begin(ctx, e, str(r["requestId"]))
	if err != nil {
		return nil, err
	}
	defer finish()
	ctx = context.WithValue(bounded, commitGrantKey{}, e)
	result, err := runtime.provider(bounded, f)
	if err != nil {
		return nil, err
	}
	current, stillRegistered := ocrProviders.Load(k)
	if !stillRegistered || current != p {
		return nil, Fail("CONFLICT", "OCR engine changed during extraction")
	}
	if err = bounded.Err(); err != nil {
		return nil, err
	}
	if !utf8.ValidString(result.Warning) || len(result.Warning) > 4096 {
		return nil, Fail("WORKER_FAILED", "OCR returned invalid warning")
	}
	if len(result.Lines) > 10000 {
		return nil, Fail("RESOURCE_EXHAUSTED", "OCR result exceeds 10000 lines")
	}
	lines := []string{}
	rejected := 0
	total := 0
	for _, line := range result.Lines {
		if math.IsNaN(line.Confidence) || math.IsInf(line.Confidence, 0) || line.Confidence < 0 || line.Confidence > 1 || !utf8.ValidString(line.Text) {
			return nil, Fail("WORKER_FAILED", "OCR returned invalid text or confidence")
		}
		total += len(line.Text)
		if total > 1<<20 {
			return nil, Fail("RESOURCE_EXHAUSTED", "OCR output exceeds 1 MiB")
		}
		if line.Confidence < 0.5 {
			rejected++
			continue
		}
		if text := strings.TrimSpace(line.Text); text != "" {
			lines = append(lines, text)
		}
	}
	warning := result.Warning
	if rejected > 0 {
		warning = strings.TrimSpace(warning + " Low-confidence lines were omitted. Inspect the source image before using the result.")
	}
	if len(lines) == 0 {
		warning = strings.TrimSpace(warning + " No readable text met the 0.5 confidence threshold.")
	}
	text := strings.Join(lines, "\n")
	state := "ready"
	if warning != "" {
		state = "partial"
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorizeEntity(ctx, tx, e, "file", fileID); err != nil {
			return err
		}
		var rev string
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM files WHERE id=?", fileID).Scan(&rev); err != nil {
			return err
		}
		if rev != string(meta.Revision) {
			return Fail("CONFLICT", "Image changed during OCR")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM document_sections WHERE file_id=?", fileID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO document_extractions VALUES(?,?,?,?,?,?)", fileID, rev, runtime.capability.ExtractorVersion, state, warning, now()); err != nil {
			return err
		}
		for i, chunk := range structuralText(text, 2400) {
			if _, err := tx.ExecContext(ctx, "INSERT INTO document_sections VALUES(?,?,?,?,?)", fileID, i, 1, "Local OCR — "+runtime.capability.Engine, chunk); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO search_dirty SELECT page_id FROM attachments WHERE file_id=?", fileID)
		return err
	})
	if err != nil {
		return nil, err
	}
	// The extraction is durable now. Cancellation may stop the derived index
	// refresh, but cannot make this committed replacement become a cancelled
	// mutation. search_dirty was committed with the extraction for safe retry.
	indexingState := "ready"
	if err = k.FlushSearch(ctx); err != nil {
		indexingState = "pending"
		warning = strings.TrimSpace(warning + " Extracted text was saved. Search indexing is pending and will retry on the next search or index refresh.")
	}
	return map[string]any{"persisted": true, "indexingState": indexingState, "fileId": fileID, "sourceRevision": meta.Revision, "state": state, "text": text, "warning": warning, "acceptedLines": len(lines), "rejectedLines": rejected, "extractorVersion": runtime.capability.ExtractorVersion, "engine": runtime.capability.Engine, "languages": runtime.capability.Languages, "modelSha256": runtime.capability.ModelSHA256}, nil
}
