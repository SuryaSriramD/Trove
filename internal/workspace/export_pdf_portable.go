package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

func (k *Kernel) exportPortablePDF(ctx context.Context, e contracts.ServiceExecutionContext, destination string, helper *native.DocumentHelper) (result PortableExportResult, err error) {
	result.Path = destination
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	job, err := helper.NewJob(ctx, "render")
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, job.Close()) }()
	snapshot, files, imageIDs, pageCount, err := k.pdfSnapshot(ctx)
	if err != nil {
		return result, err
	}
	result.Pages = pageCount
	result.Assets = len(files)
	result.SourceOmissions = []string{"PDF is a printable rendition of active pages; use portable export for exact schemas, database definitions and original attachment bytes."}
	if err = job.AddFile(ctx, "snapshot.json", bytes.NewReader(snapshot), 32<<20, ""); err != nil {
		return result, err
	}
	var total int64
	for _, asset := range files {
		id := string(asset.Id)
		if !imageIDs[id] {
			continue
		}
		if len(id) != 36 || strings.Trim(id, "0123456789abcdef-") != "" {
			return result, Fail("INTEGRITY_ERROR", "Invalid image identity")
		}
		size, er := strconv.ParseInt(asset.SizeBytes, 10, 64)
		if er != nil || size < 0 {
			return result, Fail("INTEGRITY_ERROR", "Invalid image size")
		}
		if size > 64<<20 {
			continue
		}
		total += size
		if total > 256<<20 {
			return result, Fail("RESOURCE_EXHAUSTED", "PDF images exceed256MiB")
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
		if _, er = input.Seek(0, io.SeekStart); er == nil {
			er = job.AddFile(ctx, "managed/"+id, input, size, string(asset.ContentHash))
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
	if err = job.Run(ctx, &boundedPDFWriter{w: out}); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = requireOwnerScope(k, e); err != nil {
		return result, err
	}
	if err = out.Sync(); err != nil {
		return result, err
	}
	if err = out.Close(); err != nil {
		return result, err
	}
	file, err := os.Open(destination)
	if err != nil {
		return result, err
	}
	head := make([]byte, 5)
	_, err = io.ReadFull(file, head)
	file.Close()
	if err != nil || string(head) != "%PDF-" {
		return result, Fail("INTEGRITY_ERROR", "Document helper did not produce a PDF")
	}
	report, err := openAsset(filepath.Join(job.Root(), "result.json"))
	if err != nil {
		return result, err
	}
	raw, err := io.ReadAll(io.LimitReader(report, (64<<10)+1))
	report.Close()
	if err != nil {
		return result, err
	}
	var status struct {
		Ready          bool     `json:"ready"`
		Warnings       []string `json:"warnings"`
		PhysicalPages  int      `json:"physicalPages"`
		RuntimeVersion string   `json:"runtimeVersion"`
	}
	if len(raw) > 64<<10 || json.Unmarshal(raw, &status) != nil || !status.Ready || status.PhysicalPages < 1 || status.PhysicalPages > 2000 || len(status.Warnings) > 1000 || status.RuntimeVersion == "" {
		return result, Fail("INTEGRITY_ERROR", "Document print completion report is invalid")
	}
	result.SourceOmissions = append(result.SourceOmissions, status.Warnings...)
	complete = true
	return result, nil
}
