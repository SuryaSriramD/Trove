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
type OCRProvider func(context.Context, *os.File) (OCRResult, error)
type ocrRuntime struct {
	provider OCRProvider
	active   chan struct{}
}

var ocrProviders sync.Map

func (k *Kernel) SetOCRProvider(p OCRProvider) {
	if p == nil {
		ocrProviders.Delete(k)
	} else {
		ocrProviders.Store(k, &ocrRuntime{provider: p, active: make(chan struct{}, 1)})
	}
}
func init() {
	RegisterQuery("ocr.status", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		_, ready := ocrProviders.Load(k)
		return map[string]any{"available": ready, "engine": "Apple Vision (local)", "maxBytes": 20 << 20, "maxPixels": 20000000, "timeoutSeconds": 60, "minimumConfidence": 0.5, "formats": []string{"image/png", "image/jpeg"}, "visionDescriptionAvailable": false}, nil
	})
	RegisterQuery("ocr.run", runOCR)
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
	select {
	case runtime.active <- struct{}{}:
		defer func() { <-runtime.active }()
	default:
		return nil, Fail("RESOURCE_EXHAUSTED", "One local OCR operation is already running")
	}
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result, err := runtime.provider(bounded, f)
	if err != nil {
		return nil, err
	}
	if err = bounded.Err(); err != nil {
		return nil, err
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
		warning = "Low-confidence lines were omitted. Inspect the source image before using the result."
	}
	if len(lines) == 0 {
		warning = "No readable text met the 0.5 confidence threshold."
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
		if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO document_extractions VALUES(?,?,?,?,?,?)", fileID, rev, OCRExtractorVersion, state, warning, now()); err != nil {
			return err
		}
		for i, chunk := range structuralText(text, 2400) {
			if _, err := tx.ExecContext(ctx, "INSERT INTO document_sections VALUES(?,?,?,?,?)", fileID, i, 1, "Local OCR", chunk); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO search_dirty SELECT page_id FROM attachments WHERE file_id=?", fileID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err = k.FlushSearch(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"fileId": fileID, "sourceRevision": meta.Revision, "state": state, "text": text, "warning": warning, "acceptedLines": len(lines), "rejectedLines": rejected, "extractorVersion": OCRExtractorVersion}, nil
}
