package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

type boundedPDFWriter struct {
	w io.Writer
	n int64
}

func (w *boundedPDFWriter) Write(b []byte) (int, error) {
	if w.n+int64(len(b)) > 256<<20 {
		return 0, Fail("RESOURCE_EXHAUSTED", "PDF exceeds 256 MiB")
	}
	n, err := w.w.Write(b)
	w.n += int64(n)
	return n, err
}

// Only a trusted native caller supplies embedded build assets and a save-dialog
// destination. The isolated helper has no Wails bridge or workspace path.
func (k *Kernel) ExportPDF(ctx context.Context, e contracts.ServiceExecutionContext, destination string, printAssets ...fs.FS) (result PortableExportResult, err error) {
	result.Path = destination
	if err = requireOwnerScope(k, e); err != nil {
		return result, err
	}
	portable, findErr := native.FindDocumentHelper(ctx)
	if findErr != nil {
		return result, Fail("INTEGRITY_ERROR", "Document helper validation failed: "+findErr.Error())
	}
	if portable != nil {
		return k.exportPortablePDF(ctx, e, destination, portable)
	}
	if runtime.GOOS != "darwin" {
		return result, Fail("UNSUPPORTED_SCHEMA", "The compatible local PDF helper is unavailable in this bundle. Install the complete platform package or export HTML.")
	}
	if len(printAssets) != 1 || printAssets[0] == nil {
		return result, Fail("WORKER_FAILED", "The bundled PDF print assets are unavailable. Reinstall the complete application or export HTML.")
	}
	helper, err := findPDFRenderer()
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stage, err := os.MkdirTemp(filepath.Join(k.path, ".staging"), "rich-pdf-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	result.SourceOmissions = []string{"PDF is a printable rendition of active pages; use portable export for exact schemas, database definitions and original attachment bytes."}
	snapshot, files, imageIDs, pageCount, err := k.pdfSnapshot(ctx)
	if err != nil {
		return result, err
	}
	result.Pages = pageCount
	result.Assets = len(files)
	document := filepath.Join(stage, "document")
	if err = os.Mkdir(document, 0700); err != nil {
		return result, err
	}
	var total int64
	err = fs.WalkDir(printAssets[0], ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			if name != "." && name != "assets" {
				return fs.SkipDir
			}
			return nil
		}
		if name != "print.html" && !strings.HasPrefix(name, "assets/") {
			return nil
		}
		if strings.HasPrefix(filepath.Base(name), ".") {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return Fail("INTEGRITY_ERROR", "PDF assets must be regular files")
		}
		f, er := printAssets[0].Open(name)
		if er != nil {
			return er
		}
		defer f.Close()
		target := filepath.Join(document, filepath.FromSlash(name))
		if er = os.MkdirAll(filepath.Dir(target), 0700); er != nil {
			return er
		}
		out, er := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if er != nil {
			return er
		}
		n, er := copyBounded(ctx, out, io.LimitReader(f, (64<<20)+1), 64<<20)
		ce := out.Close()
		total += n
		if er == nil {
			er = ce
		}
		if total > 128<<20 {
			return Fail("RESOURCE_EXHAUSTED", "PDF renderer assets exceed 128 MiB")
		}
		return er
	})
	if err != nil {
		return result, err
	}
	if _, err = os.Stat(filepath.Join(document, "print.html")); err != nil {
		return result, Fail("WORKER_FAILED", "Bundled print entry is missing")
	}
	if err = os.WriteFile(filepath.Join(document, "snapshot.json"), snapshot, 0600); err != nil {
		return result, err
	}
	if err = os.Mkdir(filepath.Join(document, "managed"), 0700); err != nil {
		return result, err
	}
	// Copy immutable, verified image bytes only. Unrelated workspace attachments
	// are never staged merely to print page content.
	var imageBytes int64
	for _, asset := range files {
		id := string(asset.Id)
		if !imageIDs[id] {
			continue
		}
		if len(id) != 36 || strings.Trim(id, "0123456789abcdef-") != "" {
			return result, Fail("INTEGRITY_ERROR", "Invalid image identity")
		}
		size, er := strconv.ParseInt(asset.SizeBytes, 10, 64)
		if er != nil {
			return result, er
		}
		if size > 64<<20 {
			continue
		}
		imageBytes += size
		if imageBytes > 256<<20 {
			return result, Fail("RESOURCE_EXHAUSTED", "PDF images exceed 256 MiB")
		}
		full := filepath.Join(k.path, asset.StorageKey)
		if er = verifyAsset(full, string(asset.ContentHash), size); er != nil {
			return result, er
		}
		input, er := openAsset(full)
		if er != nil {
			return result, er
		}
		config, _, decodeErr := image.DecodeConfig(input)
		if decodeErr == nil && (config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 64_000_000) {
			input.Close()
			continue
		}
		if _, er = input.Seek(0, io.SeekStart); er != nil {
			input.Close()
			return result, er
		}
		target, er := os.OpenFile(filepath.Join(document, "managed", id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if er == nil {
			_, er = copyBounded(ctx, target, input, 64<<20)
			ce := target.Close()
			if er == nil {
				er = ce
			}
		}
		input.Close()
		if er != nil {
			return result, er
		}
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	complete := false
	defer func() {
		out.Close()
		if !complete {
			os.Remove(destination)
		}
	}()
	cmd := exec.CommandContext(ctx, helper, document)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin", "LANG=en_US.UTF-8", "HOME=" + stage, "TMPDIR=" + stage}
	cmd.Stdout = &boundedPDFWriter{w: out}
	stderr := &limitedDocumentOutput{limit: 16 << 10}
	cmd.Stderr = stderr
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("Rich PDF conversion failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err = out.Sync(); err != nil {
		return result, err
	}
	if err = out.Close(); err != nil {
		return result, err
	}
	f, err := os.Open(destination)
	if err != nil {
		return result, err
	}
	head := make([]byte, 5)
	_, err = io.ReadFull(f, head)
	f.Close()
	if err != nil || string(head) != "%PDF-" {
		return result, Fail("INTEGRITY_ERROR", "Native renderer did not produce a PDF")
	}
	report, err := os.ReadFile(filepath.Join(document, "result.json"))
	if err != nil {
		return result, err
	}
	var status struct {
		Ready         bool     `json:"ready"`
		Warnings      []string `json:"warnings"`
		PhysicalPages int      `json:"physicalPages"`
	}
	if len(report) > 64<<10 || json.Unmarshal(report, &status) != nil || !status.Ready || status.PhysicalPages < 1 || status.PhysicalPages > 2000 {
		return result, Fail("INTEGRITY_ERROR", "Native print completion report is invalid")
	}
	result.SourceOmissions = append(result.SourceOmissions, status.Warnings...)
	complete = true
	return result, nil
}
func findPDFRenderer() (string, error) {
	executable, _ := os.Executable()
	candidates := []string{}
	if bundle := os.Getenv("TROVE_NATIVE_BUNDLE"); bundle != "" {
		candidates = append(candidates, filepath.Join(bundle, "pdf-renderer"))
	}
	candidates = append(candidates, filepath.Join(filepath.Dir(executable), "../Resources/native/pdf-renderer"), filepath.Join(filepath.Dir(executable), "native/pdf-renderer"))
	if cwd, err := os.Getwd(); err == nil {
		for range 6 {
			candidates = append(candidates, filepath.Join(cwd, "build/native/pdf-renderer"))
			next := filepath.Dir(cwd)
			if next == cwd {
				break
			}
			cwd = next
		}
	}
	for _, candidate := range candidates {
		st, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
			return "", Fail("INTEGRITY_ERROR", "Invalid PDF renderer executable")
		}
		manifest, err := os.ReadFile(candidate + ".json")
		if err != nil {
			return "", Fail("INTEGRITY_ERROR", "PDF renderer manifest is missing")
		}
		var meta struct {
			SchemaVersion int    `json:"schemaVersion"`
			Name          string `json:"name"`
			SHA256        string `json:"sha256"`
		}
		if len(manifest) > 64<<10 || json.Unmarshal(manifest, &meta) != nil || meta.SchemaVersion != 1 || meta.Name != "pdf-renderer" {
			return "", Fail("INTEGRITY_ERROR", "PDF renderer manifest is invalid")
		}
		binary, err := openAsset(candidate)
		if err != nil {
			return "", err
		}
		digest := sha256.New()
		_, err = io.Copy(digest, binary)
		binary.Close()
		if err != nil {
			return "", err
		}
		if hex.EncodeToString(digest.Sum(nil)) != meta.SHA256 {
			return "", Fail("INTEGRITY_ERROR", "PDF renderer hash mismatch")
		}
		return candidate, nil
	}
	return "", Fail("WORKER_FAILED", "The bundled macOS PDF renderer is unavailable. Reinstall the complete application or export HTML.")
}

// One read transaction supplies a coherent canonical snapshot. Only referenced
// file metadata leaves SQLite; only printed images are subsequently read.
func (k *Kernel) pdfSnapshot(ctx context.Context) ([]byte, []contracts.DomainFile, map[string]bool, int, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, nil, 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM pages WHERE deleted_at IS NULL ORDER BY title,id")
	if err != nil {
		return nil, nil, nil, 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, nil, 0, err
		}
		ids = append(ids, id)
		if len(ids) > 2000 {
			rows.Close()
			return nil, nil, nil, 0, Fail("RESOURCE_EXHAUSTED", "PDF export exceeds 2,000 source pages")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, nil, 0, err
	}
	pages := []json.RawMessage{}
	fileIDs, images := map[string]bool{}, map[string]bool{}
	var sourceBytes int
	for _, id := range ids {
		page, er := readPageTx(ctx, tx, id)
		if er != nil {
			return nil, nil, nil, 0, er
		}
		raw := marshal(map[string]any{"id": id, "canonicalPage": page.Page, "canonicalBlocks": page.Blocks})
		sourceBytes += len(raw)
		if sourceBytes > 32<<20 {
			return nil, nil, nil, 0, Fail("RESOURCE_EXHAUSTED", "PDF source exceeds 32 MiB")
		}
		pages = append(pages, raw)
		for _, block := range page.Blocks {
			var data map[string]any
			if er = json.Unmarshal(block.Data, &data); er != nil {
				return nil, nil, nil, 0, er
			}
			collectFileIDs(data, fileIDs)
			if block.Type == "image" {
				images[str(data["fileId"])] = true
			}
		}
	}
	files := []contracts.DomainFile{}
	attachments := []any{}
	for id := range fileIDs {
		file, er := readFileMetadata(ctx, tx, id)
		if er != nil {
			return nil, nil, nil, 0, er
		}
		files = append(files, file)
		attachments = append(attachments, map[string]any{"id": id, "path": file.OriginalName, "mediaType": file.MediaType})
	}
	snapshot := marshal(map[string]any{"pages": pages, "attachments": attachments})
	if len(snapshot) > 32<<20 {
		return nil, nil, nil, 0, Fail("RESOURCE_EXHAUSTED", "PDF source exceeds 32 MiB")
	}
	return snapshot, files, images, len(ids), nil
}
