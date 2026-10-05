package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"golang.org/x/net/html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
	"unicode/utf8"
)

const documentExtractorVersion = "trove-document-v1"
const maxDocumentBytes = 128 << 20
const maxExtractedBytes = 16 << 20

type DocumentSection struct {
	Page    int    `json:"page"`
	Heading string `json:"heading"`
	Text    string `json:"text"`
}
type DocumentExtraction struct {
	Sections []DocumentSection `json:"sections"`
	Warning  string            `json:"warning,omitempty"`
}

func init() {
	RegisterJobHandler("EXTRACT_DOCUMENT", extractDocumentJob)
	RegisterQuery("documents.status", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		rows, err := k.db.QueryContext(ctx, `SELECT f.id,f.original_name,COALESCE(d.state,'pending'),COALESCE(d.warning,''),COALESCE(d.extractor_version,'') FROM files f LEFT JOIN document_extractions d ON d.file_id=f.id AND d.file_revision=f.revision ORDER BY f.original_name`)
		if err != nil {
			return nil, err
		}
		type status struct {
			FileID  string `json:"fileId"`
			Name    string `json:"name"`
			State   string `json:"state"`
			Warning string `json:"warning"`
			Version string `json:"version"`
		}
		all := []status{}
		for rows.Next() {
			var s status
			if err = rows.Scan(&s.FileID, &s.Name, &s.State, &s.Warning, &s.Version); err != nil {
				rows.Close()
				return nil, err
			}
			all = append(all, s)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		visible := []status{}
		for _, s := range all {
			if k.authorizeEntity(ctx, k.db, e, "file", s.FileID) == nil {
				visible = append(visible, s)
			}
		}
		return map[string]any{"files": visible}, nil
	})
}
func extractDocumentJob(ctx context.Context, k *Kernel, j contracts.JobsJob) error {
	p, ok := j.Payload.Value.(contracts.JobsJobPayloadVariant2)
	if !ok {
		return Fail("INVALID_REQUEST", "Expected file extraction payload")
	}
	if p.ExtractorVersion != documentExtractorVersion {
		return Fail("UNSUPPORTED_SCHEMA", "Unknown document extractor version")
	}
	e, err := k.JobExecution(ctx, string(j.JobId))
	if err != nil {
		return err
	}
	defer k.RevokeGrant(string(e.ScopeGrantId))
	f, meta, err := k.ReadFile(ctx, e, string(p.FileId))
	if err != nil {
		return err
	}
	defer f.Close()
	_ = k.ProgressJob(ctx, string(j.JobId), string(*j.LeaseToken), contracts.JobsProgress{Sequence: 0, Completed: 0, Unit: "items"})
	var previous string
	if k.db.QueryRowContext(ctx, "SELECT state FROM document_extractions WHERE file_id=? AND file_revision=? AND extractor_version=?", p.FileId, meta.Revision, p.ExtractorVersion).Scan(&previous) == nil && (previous == "ready" || previous == "partial") {
		return nil
	}
	result, extractErr := ExtractDocument(ctx, f, meta.OriginalName, meta.MediaType)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	state, warning := "ready", result.Warning
	if warning != "" {
		state = "partial"
	}
	if extractErr != nil {
		state = "failed"
		warning = extractErr.Error()
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		if _, err := k.checkLease(ctx, tx, string(j.JobId), string(*j.LeaseToken), false); err != nil {
			return err
		}
		if err := k.authorizeEntity(ctx, tx, e, "file", string(p.FileId)); err != nil {
			return err
		}
		var rev string
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM files WHERE id=?", p.FileId).Scan(&rev); err != nil {
			return err
		}
		if rev != string(meta.Revision) {
			return Fail("CONFLICT", "File changed during extraction")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM document_sections WHERE file_id=?", p.FileId); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO document_extractions VALUES(?,?,?,?,?,?)", p.FileId, meta.Revision, p.ExtractorVersion, state, warning, now()); err != nil {
			return err
		}
		ordinal := 0
		for _, section := range result.Sections {
			for _, text := range structuralText(section.Text, 2400) {
				if _, err := tx.ExecContext(ctx, "INSERT INTO document_sections VALUES(?,?,?,?,?)", p.FileId, ordinal, section.Page, section.Heading, text); err != nil {
					return err
				}
				ordinal++
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO search_dirty SELECT page_id FROM attachments WHERE file_id=?", p.FileId)
		return err
	})
	if err != nil {
		return err
	}
	if extractErr != nil {
		return extractErr
	}
	if err = k.FlushSearch(ctx); err != nil {
		return err
	}
	total := int64(1)
	return k.ProgressJob(ctx, string(j.JobId), string(*j.LeaseToken), contracts.JobsProgress{Sequence: 1, Completed: 1, Total: &total, Unit: "items"})
}

// ExtractDocument only decodes a deliberately small allowlist, never executes
// macros/links or asks a model to reconstruct unavailable source information.
func ExtractDocument(ctx context.Context, f *os.File, name, media string) (DocumentExtraction, error) {
	out := DocumentExtraction{Sections: []DocumentSection{}}
	st, err := f.Stat()
	if err != nil {
		return out, err
	}
	if st.Size() > maxDocumentBytes {
		return out, Fail("RESOURCE_EXHAUSTED", "Document exceeds 128 MiB extraction limit")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".pdf" || media == "application/pdf" {
		return extractPDF(ctx, f)
	}
	if ext == ".docx" {
		return extractDOCX(ctx, f, st.Size())
	}
	if ext != ".txt" && ext != ".md" && ext != ".markdown" && ext != ".csv" && ext != ".html" && ext != ".htm" && !strings.HasPrefix(media, "text/") {
		return out, Fail("UNSUPPORTED_SCHEMA", "This document format is retained but not supported for extraction. Use PDF, DOCX, text, Markdown, CSV, or HTML.")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxExtractedBytes+1))
	if err != nil {
		return out, err
	}
	if len(raw) > maxExtractedBytes {
		return out, Fail("RESOURCE_EXHAUSTED", "Text document exceeds 16 MiB extraction limit")
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return out, Fail("UNSUPPORTED_SCHEMA", "Text must be UTF-8 without binary content; original file is retained")
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	switch ext {
	case ".html", ".htm":
		z := html.NewTokenizer(bytes.NewReader(raw))
		var b strings.Builder
		skip := 0
		for {
			kind := z.Next()
			if kind == html.ErrorToken {
				if z.Err() != io.EOF {
					return out, z.Err()
				}
				break
			}
			tok := z.Token()
			if kind == html.StartTagToken && (tok.Data == "script" || tok.Data == "style") {
				skip++
			}
			if kind == html.EndTagToken && (tok.Data == "script" || tok.Data == "style") {
				if skip > 0 {
					skip--
				}
				continue
			}
			if skip > 0 {
				continue
			}
			if kind == html.TextToken {
				b.WriteString(tok.Data)
			}
			if kind == html.EndTagToken && (tok.Data == "p" || tok.Data == "div" || tok.Data == "li" || strings.HasPrefix(tok.Data, "h")) {
				b.WriteByte('\n')
			}
		}
		out.Sections = append(out.Sections, DocumentSection{Text: b.String()})
		out.Warning = "HTML text extracted; styling, scripts, and external resources are not indexed."
	case ".csv":
		reader := csv.NewReader(bytes.NewReader(raw))
		reader.FieldsPerRecord = -1
		rows := 0
		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return out, Fail("INVALID_REQUEST", "CSV cannot be parsed: "+err.Error())
			}
			rows++
			out.Sections = append(out.Sections, DocumentSection{Heading: fmt.Sprintf("Row %d", rows), Text: strings.Join(record, " | ")})
		}
	default:
		out.Sections = append(out.Sections, DocumentSection{Text: strings.TrimPrefix(string(raw), "\ufeff")})
	}
	return out, nil
}
func extractDOCX(ctx context.Context, f *os.File, size int64) (DocumentExtraction, error) {
	out := DocumentExtraction{Sections: []DocumentSection{}, Warning: "DOCX paragraphs and tables extracted; drawings, embedded objects, comments, and tracked changes may need the original document."}
	archive, err := zip.NewReader(f, size)
	if err != nil {
		return out, Fail("INVALID_REQUEST", "Invalid DOCX archive")
	}
	var document *zip.File
	for _, entry := range archive.File {
		if entry.Name == "word/document.xml" {
			if document != nil {
				return out, Fail("INVALID_REQUEST", "DOCX has duplicate document parts")
			}
			document = entry
		}
	}
	if document == nil {
		return out, Fail("INVALID_REQUEST", "DOCX document part is missing")
	}
	if document.UncompressedSize64 > maxExtractedBytes {
		return out, Fail("RESOURCE_EXHAUSTED", "DOCX document exceeds extraction limit")
	}
	reader, err := document.Open()
	if err != nil {
		return out, err
	}
	defer reader.Close()
	decoder := xml.NewDecoder(io.LimitReader(reader, maxExtractedBytes+1))
	var b strings.Builder
	inText := false
	for {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, Fail("INVALID_REQUEST", "DOCX XML cannot be parsed")
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inText = true
			}
			if t.Name.Local == "tab" {
				b.WriteByte('\t')
			}
			if t.Name.Local == "br" {
				b.WriteByte('\n')
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
			}
			if t.Name.Local == "p" || t.Name.Local == "tr" {
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
		if b.Len() > maxExtractedBytes {
			return out, Fail("RESOURCE_EXHAUSTED", "DOCX text exceeds extraction limit")
		}
	}
	out.Sections = append(out.Sections, DocumentSection{Text: b.String()})
	return out, nil
}

type limitedDocumentOutput struct {
	bytes.Buffer
	limit int
}

func (w *limitedDocumentOutput) Write(p []byte) (int, error) {
	if w.Len()+len(p) > w.limit {
		return 0, Fail("RESOURCE_EXHAUSTED", "Extracted document exceeds output limit")
	}
	return w.Buffer.Write(p)
}
func extractPDF(ctx context.Context, f *os.File) (out DocumentExtraction, resultErr error) {
	out = DocumentExtraction{Sections: []DocumentSection{}}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	portable, err := native.FindDocumentHelper(ctx)
	if err != nil {
		return out, Fail("INTEGRITY_ERROR", "Document helper validation failed: "+err.Error())
	}
	if portable != nil {
		job, err := portable.NewJob(ctx, "extract")
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, job.Close()) }()
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return out, err
		}
		if err = job.AddFile(ctx, "input.pdf", f, maxDocumentBytes, ""); err != nil {
			return out, err
		}
		stdout := &limitedDocumentOutput{limit: maxExtractedBytes}
		if err = job.Run(ctx, stdout); err != nil {
			return out, err
		}
		return parsePDFText(stdout.Bytes())
	}

	executable, _ := os.Executable()
	candidates := []string{os.Getenv("TROVE_DOCUMENT_EXTRACTOR")}
	if bundle := os.Getenv("TROVE_NATIVE_BUNDLE"); bundle != "" {
		candidates = append(candidates, filepath.Join(bundle, "document-extractor"))
	}
	candidates = append(candidates, filepath.Join(filepath.Dir(executable), "../Resources/native/document-extractor"), filepath.Join(filepath.Dir(executable), "native/document-extractor"))
	if cwd, err := os.Getwd(); err == nil {
		for i := 0; i < 6; i++ {
			candidates = append(candidates, filepath.Join(cwd, "build/native/document-extractor"))
			next := filepath.Dir(cwd)
			if next == cwd {
				break
			}
			cwd = next
		}
	}
	tool := ""
	args := []string{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if st, err := os.Lstat(candidate); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
			manifest, err := os.ReadFile(candidate + ".json")
			if err != nil {
				return out, Fail("INTEGRITY_ERROR", "PDF extractor manifest is missing")
			}
			var meta struct {
				SchemaVersion int    `json:"schemaVersion"`
				Name          string `json:"name"`
				SHA256        string `json:"sha256"`
			}
			if json.Unmarshal(manifest, &meta) != nil || meta.SchemaVersion != 1 || meta.Name != "document-extractor" {
				return out, Fail("INTEGRITY_ERROR", "PDF extractor manifest is invalid")
			}
			binary, err := openAsset(candidate)
			if err != nil {
				return out, err
			}
			digest := sha256.New()
			_, err = io.Copy(digest, binary)
			binary.Close()
			if err != nil {
				return out, err
			}
			if hex.EncodeToString(digest.Sum(nil)) != meta.SHA256 {
				return out, Fail("INTEGRITY_ERROR", "PDF extractor hash mismatch")
			}
			tool = candidate
			break
		}
	}
	if tool == "" {
		return out, Fail("WORKER_FAILED", "PDF extractor is unavailable. Install the complete Trove application and retry; the original PDF is retained.")
	}
	command := exec.CommandContext(ctx, tool, args...)
	command.Stdin = f
	stdout := &limitedDocumentOutput{limit: maxExtractedBytes}
	stderr := &limitedDocumentOutput{limit: 16 << 10}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, Fail("WORKER_FAILED", "PDF extraction failed; encrypted, damaged, or unsupported PDF. Original retained. "+strings.TrimSpace(stderr.String()))
	}
	return parsePDFText(stdout.Bytes())
}
func parsePDFText(raw []byte) (DocumentExtraction, error) {
	out := DocumentExtraction{Sections: []DocumentSection{}}
	if len(raw) > maxExtractedBytes {
		return out, Fail("RESOURCE_EXHAUSTED", "PDF text exceeds output limit")
	}
	if !utf8.Valid(raw) {
		return out, Fail("WORKER_FAILED", "PDF extractor returned invalid UTF-8")
	}
	count := 0
	pages := strings.Split(string(raw), "\f")
	if len(pages) > 10000 {
		return out, Fail("RESOURCE_EXHAUSTED", "PDF exceeds 10000 pages")
	}
	for page, text := range pages {
		if strings.TrimSpace(text) != "" {
			out.Sections = append(out.Sections, DocumentSection{Page: page + 1, Text: text})
			count++
		}
	}
	if count == 0 {
		return out, Fail("UNSUPPORTED_SCHEMA", "PDF contains no extractable text. Scanned pages require OCR; the original PDF is retained.")
	}
	out.Warning = "PDF text layer extracted. Scanned/image-only regions, reading order, and figures require the original PDF; OCR is not applied."
	return out, nil
}
