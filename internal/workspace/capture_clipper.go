package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/clipper"
)

func (k *Kernel) ClipperDestinations(ctx context.Context, e contracts.ServiceExecutionContext, allowed []string) ([]clipper.Destination, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	if len(allowed) == 0 || len(allowed) > 50 {
		return nil, Fail("INVALID_REQUEST", "Approve between 1 and 50 clip destinations")
	}
	out := []clipper.Destination{}
	for _, id := range allowed {
		if id == "inbox" {
			out = append(out, clipper.Destination{ID: id, Title: "Inbox (unfiled clips)", Revision: "0"})
			continue
		}
		if err := k.authorizePage(ctx, k.db, e, id); err != nil {
			return nil, err
		}
		var title, rev string
		if err := k.db.QueryRowContext(ctx, "SELECT title,revision FROM pages WHERE id=? AND deleted_at IS NULL", id).Scan(&title, &rev); err != nil {
			return nil, Fail("NOT_FOUND", "Approved clip destination is unavailable")
		}
		out = append(out, clipper.Destination{ID: id, Title: title, Revision: rev})
	}
	return out, nil
}

// A paired browser can create only a durable owner-review plan. It cannot
// commit a plan, choose an arbitrary command or request a remote resource fetch.
func (k *Kernel) PreviewClipperCapture(ctx context.Context, e contracts.ServiceExecutionContext, extension string, request clipper.Capture) (ImportPlan, error) {
	var empty ImportPlan
	if err := requireOwnerScope(k, e); err != nil {
		return empty, err
	}
	raw := marshal(request)
	if len(raw) > clipper.MaximumRequestBytes || request.Version != 1 || len(request.RequestID) < 16 || len(request.RequestID) > 100 || len(request.Title) > 512 || len(request.HTML) > 512<<10 || len(request.Text) > 512<<10 {
		return empty, Fail("INVALID_REQUEST", "Capture exceeds its bounded format")
	}
	if request.Mode != "article" && request.Mode != "selection" && request.Mode != "url" {
		return empty, Fail("INVALID_REQUEST", "Unknown capture mode")
	}
	source, err := url.Parse(request.URL)
	if err != nil || source.User != nil || (source.Scheme != "http" && source.Scheme != "https") || source.Hostname() == "" || len(request.URL) > 8192 {
		return empty, Fail("INVALID_REQUEST", "Capture source must be an HTTP(S) page URL")
	}
	if _, err = time.Parse(time.RFC3339Nano, request.CapturedAt); err != nil {
		return empty, Fail("INVALID_REQUEST", "Capture timestamp is missing")
	}
	destinations, err := k.ClipperDestinations(ctx, e, []string{request.DestinationID})
	if err != nil {
		return empty, err
	}
	if destinations[0].Revision != request.DestinationRevision {
		return empty, Fail("CONFLICT", "Capture destination changed; refresh destinations and review again")
	}
	namespace := "browser-clip:" + importDigest([]byte(extension+"\n"+request.RequestID))
	digest := importDigest(raw)
	var saved string
	err = k.db.QueryRowContext(ctx, "SELECT plan_json FROM import_runs WHERE json_extract(plan_json,'$.sourceNamespace')=? ORDER BY rowid DESC LIMIT 1", namespace).Scan(&saved)
	if err == nil {
		var previous ImportPlan
		if json.Unmarshal([]byte(saved), &previous) == nil && previous.CaptureDigest == digest {
			return previous, nil
		}
		return empty, Fail("CONFLICT", "Capture identity already belongs to different content")
	}
	if err != sql.ErrNoRows {
		return empty, err
	}
	if len(request.Images) > 8 || (!request.ResourceConsent && len(request.Images) > 0) {
		return empty, Fail("SCOPE_DENIED", "Image acquisition requires explicit consent and at most eight images")
	}
	type asset struct {
		path, url, media string
		bytes            []byte
	}
	assets := []asset{}
	replacements := map[string]string{}
	total := 0
	for i, resource := range request.Images {
		parsed, er := url.Parse(resource.URL)
		if er != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || len(resource.URL) > 8192 {
			return empty, Fail("INVALID_REQUEST", "Invalid acquired image source")
		}
		if replacements[resource.URL] != "" {
			return empty, Fail("INVALID_REQUEST", "Duplicate image source")
		}
		data, er := base64.StdEncoding.DecodeString(resource.Base64)
		if er != nil || len(data) > 2<<20 {
			return empty, Fail("RESOURCE_EXHAUSTED", "Acquired image exceeds 2 MiB")
		}
		total += len(data)
		if total > 2<<20 {
			return empty, Fail("RESOURCE_EXHAUSTED", "Acquired images exceed 2 MiB total")
		}
		detected := http.DetectContentType(data)
		suffix := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif"}[detected]
		if suffix == "" || detected != resource.MediaType {
			return empty, Fail("INVALID_REQUEST", "Only verified PNG, JPEG and GIF image bytes are accepted")
		}
		config, _, er := image.DecodeConfig(bytes.NewReader(data))
		if er != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 20_000_000 {
			return empty, Fail("RESOURCE_EXHAUSTED", "Image dimensions are invalid or exceed 20 megapixels")
		}
		name := fmt.Sprintf("assets/image-%d%s", i+1, suffix)
		assets = append(assets, asset{name, resource.URL, detected, data})
		replacements[resource.URL] = name
	}
	title := strings.TrimSpace(request.Title)
	if title == "" {
		title = "Web clip"
	}
	content := request.HTML
	if content == "" {
		content = "<p>" + html.EscapeString(request.Text) + "</p>"
	}
	if request.Mode == "url" {
		content = "<p>Saved link: " + html.EscapeString(request.URL) + "</p>"
	}
	tree, err := xhtml.Parse(strings.NewReader(content))
	if err != nil {
		return empty, err
	}
	var rewrite func(*xhtml.Node)
	rewrite = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "img" {
			for i, a := range n.Attr {
				if a.Key == "src" && replacements[a.Val] != "" {
					n.Attr[i].Val = replacements[a.Val]
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			rewrite(child)
		}
	}
	rewrite(tree)
	var sanitized bytes.Buffer
	if err = xhtml.Render(&sanitized, tree); err != nil {
		return empty, err
	}
	body := "<!doctype html><html><head><meta charset=\"utf-8\"><title>" + html.EscapeString(title) + "</title></head><body><h1>" + html.EscapeString(title) + "</h1><p><a href=\"" + html.EscapeString(request.URL) + "\">Original source</a></p>" + sanitized.String() + "</body></html>"
	staged, err := os.CreateTemp(filepath.Join(k.path, ".staging"), "browser-clip-*.zip")
	if err != nil {
		return empty, err
	}
	defer os.Remove(staged.Name())
	zw := zip.NewWriter(staged)
	w, err := zw.Create("clip.html")
	if err == nil {
		_, err = w.Write([]byte(body))
	}
	for _, file := range assets {
		if err != nil {
			break
		}
		w, err = zw.Create(file.path)
		if err == nil {
			_, err = w.Write(file.bytes)
		}
	}
	closeErr := zw.Close()
	if err == nil {
		err = closeErr
	}
	closeErr = staged.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return empty, err
	}
	handle, err := k.RegisterSelection(staged.Name())
	if err != nil {
		return empty, err
	}
	preview, err := previewPortableImport(ctx, k, e, map[string]any{"sourceHandle": handle, "sourceNamespace": namespace, "sourceSystem": "web_clip"})
	if err != nil {
		return empty, err
	}
	plan := preview.(ImportPlan)
	plan.SourceName = title
	plan.CaptureDigest = digest
	plan.CaptureExtension = extension
	plan.CaptureDestination = request.DestinationID
	plan.CaptureDestinationTitle = destinations[0].Title
	if request.DestinationID != "inbox" {
		plan.RootParentID = request.DestinationID
		plan.ExpectedParentRevision = request.DestinationRevision
	}
	acquired := []any{}
	for _, file := range assets {
		acquired = append(acquired, map[string]any{"sourceURL": file.url, "mediaType": file.media, "sha256": importDigest(file.bytes), "sizeBytes": len(file.bytes), "acquisition": "explicit_browser_fetch_without_credentials"})
	}
	for i := range plan.Items {
		plan.Items[i].Provenance = map[string]any{"channel": "browser-extension", "extensionId": extension, "sourceURL": request.URL, "capturedAt": now(), "browserReportedCapturedAt": request.CapturedAt, "mode": request.Mode, "offline": true, "resourceConsent": request.ResourceConsent, "acquiredImages": acquired}
		if plan.Items[i].Kind == "html" {
			plan.Items[i].Title = title
		}
	}
	plan.SourceCompleteness = "partial"
	plan.SourceOmissions = append(plan.SourceOmissions, "Web clips retain the explicitly selected rendered content and acquired PNG/JPEG/GIF bytes; scripts, interactive controls, linked pages and unfetched resources are not captured.")
	plan.UnknownCompleteness = []string{"A browser clip cannot establish completeness of content hidden behind login, pagination or dynamic controls."}
	plan.Hash = importPlanHash(plan)
	err = k.write(ctx, func(tx *sql.Tx) error { return saveImportPlan(ctx, tx, plan) })
	return plan, err
}
func (k *Kernel) ClipperReviewPlans(ctx context.Context, e contracts.ServiceExecutionContext) ([]ImportPlan, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	rows, err := k.db.QueryContext(ctx, "SELECT plan_json FROM import_runs WHERE json_extract(plan_json,'$.captureExtension') IS NOT NULL ORDER BY rowid DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImportPlan{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p ImportPlan
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Refreshing a destination is a distinct native owner review action; it never
// commits the capture and cannot change its approved destination identity.
func (k *Kernel) RefreshClipperDestination(ctx context.Context, e contracts.ServiceExecutionContext, id, hash string) (ImportPlan, error) {
	var result ImportPlan
	if err := requireOwnerScope(k, e); err != nil {
		return result, err
	}
	err := k.write(ctx, func(tx *sql.Tx) error {
		plan, err := loadImportPlan(ctx, tx, id)
		if err != nil {
			return err
		}
		if plan.CaptureExtension == "" || plan.Hash != hash || plan.Initialized {
			return Fail("CONFLICT", "Capture review has changed or already started")
		}
		if plan.RootParentID != "" {
			if err = k.authorizePage(ctx, tx, e, plan.RootParentID); err != nil {
				return err
			}
			if err = tx.QueryRowContext(ctx, "SELECT title,revision FROM pages WHERE id=? AND deleted_at IS NULL", plan.RootParentID).Scan(&plan.CaptureDestinationTitle, &plan.ExpectedParentRevision); err != nil {
				return Fail("NOT_FOUND", "Capture destination is unavailable")
			}
		}
		plan.Hash = importPlanHash(plan)
		if err = saveImportPlan(ctx, tx, plan); err != nil {
			return err
		}
		result = plan
		return nil
	})
	return result, err
}
