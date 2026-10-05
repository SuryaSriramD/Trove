package workspace

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func supportArchive(t *testing.T, path string) map[string]string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	out := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(raw)
	}
	return out
}
func assertSupportClean(t *testing.T, dir string) {
	t.Helper()
	items, err := filepath.Glob(filepath.Join(dir, ".trove-support-*"))
	if err != nil || len(items) != 0 {
		t.Fatalf("private staging remains: %v %v", items, err)
	}
}
func TestSupportDefaultExactAllowlistAndReviewBytes(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "secret-title-account@example.test")
	id := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": id, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("secret-page-token-sk-123")}}}}, "page", page, "block", id))
	_, err := k.db.Exec(`INSERT INTO jobs(job_id,workspace_id,kind,payload_version,payload_json,state,revision,idempotency_key,attempt,max_attempts,error_json,created_at,updated_at) VALUES(?,?,?,1,?,'failed',1,?,1,1,?,?,?)`, NewID(), k.ID(), "secret-job-label", `{"token":"secret-job-token","path":"/secret/filename"}`, NewID(), `{"code":"secret-error-code","message":"secret-error-message","account":"secret-account"}`, now(), now())
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{}, SupportRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "support.zip")
	if err = k.ExportSupportBundle(context.Background(), e, b, path); err != nil {
		t.Fatal(err)
	}
	files := supportArchive(t, path)
	if len(files) != 2 {
		t.Fatal(files)
	}
	for _, f := range b.Preview().Files {
		if files[f.Name] != f.Content {
			t.Fatal("saved bytes differ from consent preview")
		}
	}
	joined := files["metadata.json"] + files["manifest.json"]
	for _, secret := range []string{"secret-", page, k.ID(), string(e.ScopeGrantId), id, k.Path(), "/secret/filename"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("default leaked %q", secret)
		}
	}
	var metadata map[string]any
	if err = json.Unmarshal([]byte(files["metadata.json"]), &metadata); err != nil {
		t.Fatal(err)
	}
	allowed := []string{"formatVersion", "application", "appVersion", "sourceRevision", "sourceModified", "os", "architecture", "capturedAt", "schemaUserVersion", "counts", "jobs", "errorCodes", "capabilities"}
	if len(metadata) != len(allowed) {
		t.Fatal(metadata)
	}
	for _, key := range allowed {
		if _, ok := metadata[key]; !ok {
			t.Fatal("missing allowlisted field", key)
		}
	}
	if metadata["errorCodes"].(map[string]any)["OTHER"] != float64(1) {
		t.Fatal(metadata)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm()&0077 != 0 {
		t.Fatal("ambient-readable archive")
	}
	assertSupportClean(t, dir)
	before, _ := os.ReadFile(path)
	if err = k.ExportSupportBundle(context.Background(), e, b, path); err == nil {
		t.Fatal("overwrote existing destination")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("changed destination")
	}
	assertSupportClean(t, dir)
}
func TestSupportOptionalContentSeparateConsentAndStaleness(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "consented title")
	createPage(t, k, e, "unselected secret title")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "imp", "marks": []any{"bold"}}, map[string]any{"text": "ortant consented secret", "link": "https://secret-token.example.test"}}}}}}, "page", page, "block", block))
	for _, c := range []struct {
		name        string
		title, text bool
	}{{"title", true, false}, {"text", false, true}, {"both", true, true}} {
		t.Run(c.name, func(t *testing.T) {
			b, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{[]string{page}, c.title, c.text}, SupportRuntime{})
			if err != nil {
				t.Fatal(err)
			}
			raw := ""
			for _, f := range b.Preview().Files {
				if f.Name == "selected-content.json" {
					raw = f.Content
				}
			}
			if strings.Contains(raw, "consented title") != c.title || strings.Contains(raw, "important consented secret") != c.text {
				t.Fatal("incorrect consent inclusion", raw)
			}
			for _, secret := range []string{"unselected secret", "secret-token.example.test", page, block} {
				if strings.Contains(raw, secret) {
					t.Fatal("unselected fields leaked", raw)
				}
			}
		})
	}
	b, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{[]string{page}, true, true}, SupportRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "changed after review"}}, "page", page))
	dir := t.TempDir()
	if err = k.ExportSupportBundle(context.Background(), e, b, filepath.Join(dir, "stale.zip")); err == nil {
		t.Fatal("stale consent saved")
	}
	assertSupportClean(t, dir)
}
func TestSupportBoundsRevocationExpiryCancellationAndHostileDestination(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "small")
	for _, s := range []SupportSelection{{PageIDs: []string{page}}, {Titles: true}, {PageIDs: []string{page, page}, Titles: true}, {PageIDs: []string{"bad"}, Titles: true}, {PageIDs: make([]string, 6), Text: true}} {
		if _, err := k.PrepareSupportBundle(context.Background(), e, s, SupportRuntime{}); err == nil {
			t.Fatal("invalid selection accepted", s)
		}
	}
	_, err := k.db.Exec("UPDATE pages SET title=? WHERE id=?", strings.Repeat("x", supportContentLimit+1), page)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.PrepareSupportBundle(context.Background(), e, SupportSelection{[]string{page}, true, false}, SupportRuntime{}); err == nil {
		t.Fatal("oversized title accepted")
	}
	b, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{}, SupportRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = k.ExportSupportBundle(ctx, e, b, filepath.Join(dir, "cancel.zip")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertSupportClean(t, dir)
	if err = k.ExportSupportBundle(context.Background(), e, b, "relative.zip"); err == nil {
		t.Fatal("relative destination accepted")
	}
	target := filepath.Join(dir, "target")
	if err = os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err = os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err = k.ExportSupportBundle(context.Background(), e, b, link); err == nil {
		t.Fatal("symlink overwritten")
	}
	raw, _ := os.ReadFile(target)
	if string(raw) != "keep" {
		t.Fatal("target modified")
	}
	assertSupportClean(t, dir)
	b.expires = time.Now().Add(-time.Second)
	if err = k.ExportSupportBundle(context.Background(), e, b, filepath.Join(dir, "expired.zip")); err == nil {
		t.Fatal("expired accepted")
	}
	b.expires = time.Now().Add(time.Minute)
	k.RevokeGrant(string(e.ScopeGrantId))
	if err = k.ExportSupportBundle(context.Background(), e, b, filepath.Join(dir, "revoked.zip")); err == nil {
		t.Fatal("revoked accepted")
	}
	assertSupportClean(t, dir)
}
func TestSupportArchiveWriterCancellationAndByteLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var sink strings.Builder
	w := supportWriter{ctx: ctx, w: &sink}
	if _, err := w.Write([]byte("first")); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := w.Write([]byte("must not persist")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if sink.String() != "first" {
		t.Fatal("write after cancel")
	}
	w.ctx = context.Background()
	w.n = supportBundleLimit
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("overflow accepted")
	}
}
func TestSupportBlockProjectionNeverExpandsMetadata(t *testing.T) {
	s, ok := supportBlockText("code", map[string]any{"source": "// exact\r\ntext", "filename": "secret-name", "language": "secret"})
	if !ok || s != "// exact\r\ntext" {
		t.Fatal(s, ok)
	}
	if _, ok := supportBlockText("todo", map[string]any{"taskRecordId": NewID()}); ok {
		t.Fatal("canonical todo expanded linked task content")
	}
	if s, ok := supportBlockText("toggle_heading", map[string]any{"richText": richImport("Toggle title"), "collapsed": true, "level": 2}); !ok || s != "Toggle title" {
		t.Fatal("toggle heading not projected", s, ok)
	}
	for _, kind := range []string{"synced_block", "file", "bookmark", "unsupported_external", "table"} {
		if _, ok := supportBlockText(kind, map[string]any{"text": "secret", "raw": "secret"}); ok {
			t.Fatal(kind)
		}
	}
}

func TestSupportBuildIdentityAcceptsOnlyFixedBuildFields(t *testing.T) {
	k, e := testKernel(t)
	for _, env := range []SupportRuntime{{AppVersion: "(devel)", SourceRevision: "secret-account@host"}, {AppVersion: "secret/version/path", SourceRevision: strings.Repeat("x", 40)}} {
		b, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{}, env)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		json.Unmarshal([]byte(b.Preview().Files[0].Content), &m)
		if m["appVersion"] != "unknown" || m["sourceRevision"] != "unknown" || m["sourceModified"] != nil {
			t.Fatal(m)
		}
	}
	modified := true
	b, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{}, SupportRuntime{AppVersion: "v1.2.3", SourceRevision: strings.Repeat("a", 40), SourceModified: &modified})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal([]byte(b.Preview().Files[0].Content), &m)
	if m["appVersion"] != "v1.2.3" || m["sourceRevision"] != strings.Repeat("a", 40) || m["sourceModified"] != true {
		t.Fatal(m)
	}
}

func TestSupportPreviewFenceRejectsMidPreparationRevocationAndDeletion(t *testing.T) {
	for _, change := range []string{"revoke", "delete", "close"} {
		t.Run(change, func(t *testing.T) {
			k, e := testKernel(t)
			page := createPage(t, k, e, "private content")
			_, err := k.prepareSupportBundle(context.Background(), e, SupportSelection{[]string{page}, true, false}, SupportRuntime{}, func() {
				switch change {
				case "revoke":
					k.RevokeGrant(string(e.ScopeGrantId))
				case "close":
					k.Close()
				case "delete":
					testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, "page", page))
				}
			})
			if err == nil {
				t.Fatal("stale/private preview published after", change)
			}
		})
	}
}
func TestSupportWaitingExportRechecksExpiry(t *testing.T) {
	k, e := testKernel(t)
	b, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{}, SupportRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	b.expires = time.Now().Add(150 * time.Millisecond)
	dir := t.TempDir()
	path := filepath.Join(dir, "expired.zip")
	k.writer <- struct{}{}
	done := make(chan error, 1)
	go func() { done <- k.ExportSupportBundle(context.Background(), e, b, path) }()
	deadline := time.Now().Add(time.Second)
	for {
		files, _ := filepath.Glob(filepath.Join(dir, ".trove-support-*", "bundle.zip"))
		if len(files) > 0 {
			break
		}
		if time.Now().After(deadline) {
			<-k.writer
			t.Fatal("staging not reached")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(time.Until(b.expires.Add(10 * time.Millisecond)))
	<-k.writer
	if err = <-done; err == nil {
		t.Fatal("expired queued bundle published")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired bundle exists", err)
	}
	assertSupportClean(t, dir)
}

func TestSupportTextOmitsActualTranscriptParagraphs(t *testing.T) {
	k, e := testKernel(t)
	page, meeting, _ := meetingFixture(t, k, e)
	transcript, ordinary := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "meeting.appendSegments", "meetingId": meeting, "segments": []any{map[string]any{"segmentId": NewID(), "blockId": transcript, "startMs": 0, "endMs": 500, "text": "private transcript must stay excluded"}}}, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": ordinary, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("ordinary explicitly selected note")}}}}, "page", page, "meeting", meeting, "block", transcript, "block", ordinary))
	b, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{[]string{page}, false, true}, SupportRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	raw := ""
	for _, f := range b.Preview().Files {
		if f.Name == "selected-content.json" {
			raw = f.Content
		}
	}
	if strings.Contains(raw, "private transcript") || !strings.Contains(raw, "ordinary explicitly selected note") || !strings.Contains(raw, `"omittedBlocks": 1`) {
		t.Fatal("transcript provenance exclusion failed", raw)
	}
}

func TestSupportDamagedErrorJSONMapsToOtherWithoutReadingMessages(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	conn, err := k.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO jobs(job_id,workspace_id,kind,payload_version,payload_json,state,revision,idempotency_key,attempt,max_attempts,error_json,created_at,updated_at) VALUES(?,?,'INDEX_CONTENT',1,'{}','failed',1,?,1,1,?,?,?)`, NewID(), k.ID(), NewID(), `{"code":"broken secret`, now(), now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	b, err := k.PrepareSupportBundle(ctx, e, SupportSelection{}, SupportRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	raw := b.Preview().Files[0].Content
	if strings.Contains(raw, "broken secret") || !strings.Contains(raw, `"OTHER": 1`) {
		t.Fatal(raw)
	}
}
