package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"trove.local/phase0/contracts"
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

// PDF is a readable text rendition using the OS print converter. Exact rich
// structures, database definitions and attachments travel in the HTML/portable
// export; this limitation is returned explicitly rather than claiming fidelity.
func (k *Kernel) ExportPDF(ctx context.Context, e contracts.ServiceExecutionContext, destination string) (PortableExportResult, error) {
	result := PortableExportResult{Path: destination, SourceOmissions: []string{"PDF is a readable text rendition; use HTML or portable export for canonical structures, database definitions and attachment bytes."}}
	if err := requireOwnerScope(k, e); err != nil {
		return result, err
	}
	if runtime.GOOS != "darwin" {
		return result, Fail("UNSUPPORTED_SCHEMA", "PDF export requires the macOS print converter; use HTML export and Print to PDF on this platform")
	}
	if _, err := os.Stat("/usr/sbin/cupsfilter"); err != nil {
		return result, Fail("UNSUPPORTED_SCHEMA", "macOS print converter is unavailable; export HTML and use Print to PDF")
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM pages WHERE deleted_at IS NULL ORDER BY title,id")
	if err != nil {
		return result, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	var body strings.Builder
	body.WriteString("Trove readable export\n\n" + result.SourceOmissions[0] + "\n\n")
	for _, id := range ids {
		page, er := readPageTx(ctx, tx, id)
		if er != nil {
			return result, er
		}
		body.WriteString(page.Page.Title + "\n\n")
		for _, block := range page.Blocks {
			body.WriteString(exportBlockMarkdown(block, "", nil, nil) + "\n\n")
		}
		body.WriteString("\f")
		result.Pages++
		if body.Len() > 32<<20 {
			return result, Fail("RESOURCE_EXHAUSTED", "Readable PDF source exceeds 32 MiB")
		}
	}
	input, err := os.CreateTemp(filepath.Join(k.path, ".staging"), "pdf-export-*.txt")
	if err != nil {
		return result, err
	}
	defer os.Remove(input.Name())
	_, err = input.WriteString(body.String())
	ce := input.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return result, err
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
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, "/usr/sbin/cupsfilter", "-i", "text/plain", "-m", "application/pdf", input.Name())
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin", "LANG=en_US.UTF-8"}
	cmd.Stdout = &boundedPDFWriter{w: out}
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		return result, fmt.Errorf("PDF conversion failed: %w", err)
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
		return result, Fail("INTEGRITY_ERROR", "OS converter did not produce a PDF")
	}
	complete = true
	return result, nil
}
