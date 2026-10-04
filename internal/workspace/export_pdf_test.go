package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRichPDFExportRequiresOwnerAndBundledAssets(t *testing.T) {
	k, e := testKernel(t)
	bad := e
	bad.Origin = "ai"
	destination := filepath.Join(t.TempDir(), "denied.pdf")
	if _, err := k.ExportPDF(context.Background(), bad, destination); err == nil {
		t.Fatal("authority bypass")
	}
	if _, err := k.ExportPDF(context.Background(), e, destination); err == nil {
		t.Fatal("missing print asset bypass")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("failed export left output")
	}
	if runtime.GOOS == "darwin" {
		bundle := t.TempDir()
		t.Setenv("TROVE_NATIVE_BUNDLE", bundle)
		os.WriteFile(filepath.Join(bundle, "pdf-renderer"), []byte("untrusted"), 0700)
		if _, err := k.ExportPDF(context.Background(), e, destination, fstest.MapFS{"print.html": {Data: []byte("safe")}}); err == nil || !strings.Contains(err.Error(), "manifest") {
			t.Fatal("unverified helper accepted", err)
		}
	}
}
func TestRichPDFExportBundledRenderer(t *testing.T) {
	if os.Getenv("TROVE_PDF_SMOKE") != "1" {
		t.Skip("opt-in native WebKit visual qualification")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("macOS renderer")
	}
	k, e := testKernel(t)
	ctx := context.Background()
	var imageBytes bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 640, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 640; x++ {
			canvas.Set(x, y, color.RGBA{uint8(30 + x/4), uint8(80 + y/2), 180, 255})
		}
	}
	png.Encode(&imageBytes, canvas)
	plan := importFixture(t, k, e, map[string]string{"Rich.md": "# Résumé café 東京\n![Managed image](image.png)", "image.png": imageBytes.String()})
	var image ImportBlock
	var document *ImportItem
	for i := range plan.Items {
		if plan.Items[i].Kind == "markdown" {
			document = &plan.Items[i]
			for _, b := range document.Blocks {
				if b.Type == "image" {
					image = b
				}
			}
		}
	}
	if document == nil || image.ID == "" {
		t.Fatal("image import missing")
	}
	rich := func(s string) []any { return []any{map[string]any{"text": s}} }
	table := map[string]any{"rowCount": 38, "columnCount": 3, "headerRows": 1, "headerColumns": 0, "columnWidths": []any{150, 200, 250}}
	cells := []any{}
	for row := 0; row < 38; row++ {
		for column := 0; column < 3; column++ {
			cells = append(cells, map[string]any{"id": NewID(), "row": row, "column": column, "rowSpan": 1, "colSpan": 1, "richText": rich("Retained table row café 東京"), "preMergeCells": []any{}})
		}
	}
	table["cells"] = cells
	columns, first, second := NewID(), NewID(), NewID()
	document.Blocks = []ImportBlock{{NewID(), "paragraph", map[string]any{"richText": rich("Unicode: café 東京 Ελληνικά हिन्दी 中文 🌱")}}, {NewID(), "mermaid", map[string]any{"source": "flowchart LR\n A[Capture] --> B[Organize] --> C[Find]", "renderMode": "inert_svg"}}, {NewID(), "equation", map[string]any{"latex": `E=mc^2 \quad \int_0^1 x^2 dx = \frac{1}{3}`, "display": true}}, image, {columns, "columns", map[string]any{"columns": []any{map[string]any{"id": NewID(), "widthRatio": 1, "childBlockIds": []any{first}}, map[string]any{"id": NewID(), "widthRatio": 1, "childBlockIds": []any{second}}}}}, {first, "paragraph", map[string]any{"richText": []any{map[string]any{"text": "Left rich column", "marks": []any{"bold"}, "color": "purple", "highlight": "yellow"}}}}, {second, "paragraph", map[string]any{"richText": rich("Right column preserves readable layout.")}}, {NewID(), "synced_block", map[string]any{"contentSourceId": first}}, {NewID(), "table", table}, {NewID(), "code", map[string]any{"source": strings.Repeat("// Retained long source line 🌱\n", 80), "language": "go"}}, {NewID(), "embed", map[string]any{"url": "https://example.test/no-request", "provider": "example.test", "sandboxPolicy": "no_scripts_no_network", "offlineText": "Offline retained embed"}}, {NewID(), "unsupported_external", map[string]any{"sourceType": "fixture", "sourceVersion": 1, "readableText": "Retained unknown evidence", "reason": "unsupported_behavior", "raw": map[string]any{"retained": true}}}}
	document.BlockParents = map[string]string{first: columns, second: columns}
	plan.Hash = importPlanHash(plan)
	if err := k.write(ctx, func(tx *sql.Tx) error { return saveImportPlan(ctx, tx, plan) }); err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, k, e, plan)
	destination := filepath.Join(t.TempDir(), "rich.pdf")
	result, err := k.ExportPDF(ctx, e, destination, os.DirFS("../../frontend/dist"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(destination)
	if err != nil || !strings.HasPrefix(string(raw), "%PDF-") || len(raw) < 10000 {
		t.Fatal("invalid rich PDF", err)
	}
	if !strings.Contains(strings.Join(result.SourceOmissions, " "), "offline text") {
		t.Fatal("missing truthful attachment fallback report", result)
	}
	if output := os.Getenv("TROVE_PDF_EVIDENCE"); output != "" {
		if err = os.WriteFile(output, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(destination)
	if _, err = k.ExportPDF(ctx, e, destination, os.DirFS("../../frontend/dist")); err == nil {
		t.Fatal("existing destination overwritten")
	}
	after, _ := os.ReadFile(destination)
	if string(before) != string(after) {
		t.Fatal("existing export changed")
	}
}
