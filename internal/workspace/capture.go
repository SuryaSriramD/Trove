package workspace

import (
	"archive/zip"
	"context"
	"database/sql"
	"html"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"trove.local/phase0/contracts"
)

func init() { RegisterQuery("capture.preview", previewWebCapture) }

// Preview uses the same durable importer, owner permissions, conflict review,
// original-byte retention and committed-event origin as any file import.
func previewWebCapture(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(str(r["title"]))
	if title == "" {
		title = "Web clip"
	}
	if len(title) > 512 {
		return nil, Fail("INVALID_REQUEST", "Capture title exceeds 512 characters")
	}
	sourceURL := str(r["url"])
	if sourceURL != "" && (!safeImportURL(sourceURL) || (!strings.HasPrefix(sourceURL, "https://") && !strings.HasPrefix(sourceURL, "http://"))) {
		return nil, Fail("INVALID_REQUEST", "Capture source must be an HTTP or HTTPS URL")
	}
	content := str(r["html"])
	if content == "" {
		content = "<p>" + html.EscapeString(str(r["text"])) + "</p>"
	}
	if len(content) > 4<<20 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Clip exceeds 4 MiB")
	}
	body := "<!doctype html><html><head><title>" + html.EscapeString(title) + "</title></head><body><h1>" + html.EscapeString(title) + "</h1>"
	if sourceURL != "" {
		body += "<p><a href=\"" + html.EscapeString(sourceURL) + "\">Original source</a></p>"
	}
	body += content + "</body></html>"
	temp, err := os.CreateTemp(filepath.Join(k.path, ".staging"), "clip-*.html")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temp.Name())
	_, err = temp.WriteString(body)
	ce := temp.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return nil, err
	}
	handle, err := k.RegisterSelection(temp.Name())
	if err != nil {
		return nil, err
	}
	namespace := "web-capture:" + sourceURL
	if sourceURL == "" {
		namespace = "web-capture:" + NewID()
	}
	if len(namespace) > 512 {
		namespace = "web-capture:" + importDigest([]byte(sourceURL))
	}
	result, err := previewPortableImport(ctx, k, e, map[string]any{"sourceHandle": handle, "sourceNamespace": namespace, "sourceSystem": "web_clip"})
	if err != nil {
		return nil, err
	}
	plan := result.(ImportPlan)
	plan.SourceName = title
	for i := range plan.Items {
		plan.Items[i].Title = title
		plan.Items[i].Provenance = map[string]any{"channel": "share-paste", "sourceURL": sourceURL, "capturedAt": now(), "offline": true}
	}
	plan.Hash = importPlanHash(plan)
	err = k.write(ctx, func(tx *sql.Tx) error { return saveImportPlan(ctx, tx, plan) })
	return plan, err
}

// RegisterImportFolder explicitly copies a user-selected directory into a
// bounded staged archive; no folder path is accepted from renderer dispatch.
func (k *Kernel) RegisterImportFolder(ctx context.Context, e contracts.ServiceExecutionContext, selected string) (string, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return "", err
	}
	st, err := os.Lstat(selected)
	if err != nil {
		return "", err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", Fail("INVALID_REQUEST", "Select a real folder")
	}
	root, err := os.OpenRoot(selected)
	if err != nil {
		return "", err
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(st, actual) {
		return "", Fail("CONFLICT", "Selected folder changed")
	}
	out, err := os.CreateTemp(filepath.Join(k.path, ".staging"), "folder-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	zw := zip.NewWriter(out)
	count := 0
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if strings.Count(name, "/") > 64 {
			return Fail("RESOURCE_EXHAUSTED", "Folder nesting exceeds 64 levels")
		}
		count++
		if count > maxImportEntries {
			return Fail("RESOURCE_EXHAUSTED", "Folder exceeds 10,000 entries")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > maxImportEntryBytes {
			return Fail("RESOURCE_EXHAUSTED", "Folder member exceeds 64 MiB")
		}
		total += info.Size()
		if total > maxImportTotalBytes {
			return Fail("RESOURCE_EXHAUSTED", "Folder exceeds 1 GiB")
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		after, err := f.Stat()
		if err != nil || !os.SameFile(info, after) {
			return Fail("CONFLICT", "Folder member changed while copying")
		}
		w, err := zw.Create(path.Clean(name))
		if err != nil {
			return err
		}
		_, err = copyBounded(ctx, w, io.LimitReader(f, maxImportEntryBytes+1), maxImportEntryBytes)
		return err
	})
	if err != nil {
		return "", err
	}
	if err = zw.Close(); err != nil {
		return "", err
	}
	if err = out.Close(); err != nil {
		return "", err
	}
	return k.RegisterSelection(out.Name())
}
