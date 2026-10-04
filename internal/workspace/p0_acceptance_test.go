package workspace

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This child deliberately keeps both an audio descriptor and a SQL transaction
// open. The parent kills the process, so Close cannot turn the crash test into a
// graceful-restart test.
func p0CrashWriterProcess(t *testing.T) {
	root := os.Getenv("TROVE_P0_CRASH_WORKSPACE")
	k, err := Create(root)
	if err != nil {
		t.Fatal(err)
	}
	e := k.OwnerContext()
	plan := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{
		"Notebook 0123456789abcdef0123456789abcdef.md": "# CommittedCrashEvidence\nSource body survives interruption.",
		"Tasks.csv": "Name,Count\nQualify release,42\n",
	}))
	page := createPage(t, k, e, "Committed user edit")
	f := newDatabase(t, k, e)
	record, recordPage := newRecord(t, k, e, f, "Committed database edit")
	testOK(t, setValue(t, k, e, f, record, recordPage, f.status, "status", f.done))
	source := createPage(t, k, e, "Memory evidence")
	forgotten, forgottenPage := memoryFixture(t, k, e, source)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.forget", "memoryId": forgotten}}, "memory", forgotten, "page", forgottenPage))
	stale, _ := memoryFixture(t, k, e, source)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": source, "title": "Changed source evidence"}}, "page", source))
	draft, audio, err := k.BeginRecording(context.Background(), e, "SIGKILL recording")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = audio.Write(make([]byte, 64000)); err != nil {
			t.Fatal(err)
		}
		if err = audio.Sync(); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err = k.CheckpointRecording(context.Background(), e, draft.ID, false, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	tx, err := k.writeDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("UPDATE pages SET title='Uncommitted update must disappear' WHERE id=?", page); err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(marshal(map[string]string{"page": page, "record": record, "property": f.status, "value": f.done, "forgotten": forgotten, "stale": stale, "recording": draft.ID, "import": plan.ID})))
	// Stdin remains open until the parent terminates us.
	_, _ = io.Copy(io.Discard, os.Stdin)
	t.Fatal("crash fixture was not killed")
}

func TestP0SIGKILLPreservesCommittedJourneyAndRestores(t *testing.T) {
	if os.Getenv("TROVE_P0_CRASH_WORKSPACE") != "" {
		p0CrashWriterProcess(t)
		return
	}
	root := filepath.Join(t.TempDir(), "workspace")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestP0SIGKILLPreservesCommittedJourneyAndRestores$")
	cmd.Env = append(os.Environ(), "TROVE_P0_CRASH_WORKSPACE="+root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
		} else {
			ready <- ""
		}
	}()
	var ids map[string]string
	select {
	case line := <-ready:
		if err = json.Unmarshal([]byte(line), &ids); err != nil {
			t.Fatalf("crash helper failed: %q (%v)", line, err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("crash helper did not reach durable checkpoint")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err == nil {
		t.Fatal("fixture exited normally instead of being killed")
	}
	k, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	verify := func(k *Kernel) {
		t.Helper()
		e := k.OwnerContext()
		if title := readPage(t, k, e, ids["page"])["page"].(map[string]any)["title"]; title != "Committed user edit" {
			t.Fatalf("commit lost or partial transaction survived: %v", title)
		}
		var value string
		if err = k.db.QueryRow("SELECT json_extract(value_json,'$') FROM property_values WHERE record_id=? AND property_id=?", ids["record"], ids["property"]).Scan(&value); err != nil || value != ids["value"] {
			t.Fatal("committed database value lost", value, err)
		}
		if len(querySearch(t, k, e, "CommittedCrashEvidence", nil)["hits"].([]any)) != 1 {
			t.Fatal("imported content missing from keyword search")
		}
		plan, err := loadImportPlan(context.Background(), k.db, ids["import"])
		if err != nil || plan.State != "complete" {
			t.Fatal("import receipt lost", plan.State, err)
		}
		for key, want := range map[string]string{"forgotten": "forgotten", "stale": "stale"} {
			var state string
			if err = k.db.QueryRow("SELECT status FROM memory_entries WHERE id=?", ids[key]).Scan(&state); err != nil || state != want {
				t.Fatal("memory state changed", key, state, err)
			}
		}
		memories, err := readMemories(context.Background(), k, k.db, e, false)
		if err != nil || len(memories) != 0 {
			t.Fatal("forgotten/stale memory returned after recovery", memories, err)
		}
		drafts, err := k.ListRecordings(context.Background(), e)
		if err != nil || len(drafts) != 1 || drafts[0].ID != ids["recording"] || drafts[0].State != "interrupted" {
			t.Fatal("recording not recoverable after process death", drafts, err)
		}
	}
	verify(k)
	backup := filepath.Join(t.TempDir(), "backup")
	manifest, err := k.CreateBackup(context.Background(), k.OwnerContext(), backup)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackupReviewed(context.Background(), backup, destination, manifest); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	verify(restored)
	draft, err := restored.FinalizeRecording(context.Background(), restored.OwnerContext(), ids["recording"])
	if err != nil || draft.DurationMS != 2000 || len(draft.Segments) != 2 {
		t.Fatal("durable audio tail lost across crash/backup/restore", draft, err)
	}
}

func TestP0HostileZIPInventoryIsAccountedAndRetained(t *testing.T) {
	k, e := testKernel(t)
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	for _, name := range []string{"../escape.md", "/absolute.md", "C:/drive.md", "back\\slash.md", "nul\x00.md", "safe.md", "SAFE.md", "bad-crc.md", "non-utf8.md"} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		body := []byte("retained original")
		if name == "non-utf8.md" {
			body = []byte{0xff, 0xfe, 0x80}
		}
		if _, err = w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []struct {
		name string
		mode os.FileMode
	}{{"link.md", os.ModeSymlink | 0777}, {"pipe.md", os.ModeNamedPipe | 0600}} {
		header := &zip.FileHeader{Name: entry.name}
		header.SetMode(entry.mode)
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("../outside"))
	}
	// Raw headers exercise rejection before allocating attacker-declared sizes.
	for _, header := range []zip.FileHeader{{Name: "oversize.md", Method: zip.Store, UncompressedSize64: uint64(maxImportEntryBytes + 1)}, {Name: "ratio.md", Method: zip.Deflate, CompressedSize64: 1, UncompressedSize64: 1000000}} {
		if _, err := zw.CreateRaw(&header); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "bad-crc.md" {
			offset, err := f.DataOffset()
			if err != nil {
				t.Fatal(err)
			}
			data[offset] ^= 0xff
		}
	}
	name := filepath.Join(t.TempDir(), "hostile.zip")
	if err = os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := k.RegisterSelection(name)
	if err != nil {
		t.Fatal(err)
	}
	result, err := previewPortableImport(context.Background(), k, e, map[string]any{"sourceHandle": handle, "sourceNamespace": "hostile"})
	if err != nil {
		t.Fatal(err)
	}
	p := result.(ImportPlan)
	if len(p.Items) != len(zr.File) {
		t.Fatalf("inventory dropped entries: got %d want %d", len(p.Items), len(zr.File))
	}
	for _, item := range p.Items {
		switch item.Locator {
		case "safe.md":
			if item.Kind != "markdown" {
				t.Fatal("safe member rejected", item)
			}
		case "non-utf8.md":
			if item.Outcome != "unsupported_but_retained" {
				t.Fatal("unknown encoding silently transformed", item)
			}
		default:
			if item.Outcome != "failed" || item.RawFileID != p.SourceFileID || item.PageID != "" {
				t.Fatal("hostile member not rejected and accounted", item)
			}
		}
	}
	p = completeImportFixture(t, k, e, p)
	report := portableImportReport(p)
	if report["state"] != "partial" || report["accountedCount"] != report["discoveredCount"] {
		t.Fatal("hostile import reported false success", report)
	}
	original, err := k.readImportBytes(context.Background(), p.SourceFileID)
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("exact hostile source evidence not retained", err)
	}
	if len(querySearch(t, k, e, "retained original", nil)["hits"].([]any)) == 0 {
		t.Fatal("model-free search lost the safe imported member")
	}
}

func TestP0ImportRenameReconcilesIdentityAndRejectsStaleReview(t *testing.T) {
	k, e := testKernel(t)
	suffix := " 0123456789abcdef0123456789abcdef.md"
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Before" + suffix: "original body"}))
	page := first.Items[0].PageID
	next := importFixture(t, k, e, map[string]string{"After" + suffix: "incoming body"})
	if next.Items[0].PageID != page || next.Items[0].Conflict == "" {
		t.Fatal("Notion identity changed on rename", next.Items[0])
	}
	reviewed, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": next.ID, "planHash": next.Hash, "choices": []any{map[string]any{"sourceLocator": "After" + suffix, "choice": "replace"}}})
	if err != nil {
		t.Fatal(err)
	}
	next = reviewed.(ImportPlan)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Edited since review"}}, "page", page))
	if _, err = commitPortableImport(context.Background(), k, e, map[string]any{"planId": next.ID, "planHash": next.Hash}); err == nil {
		t.Fatal("stale import review overwrote current content")
	}
	if body := string(marshal(readPage(t, k, e, page))); !strings.Contains(body, "Edited since review") || !strings.Contains(body, "original body") || strings.Contains(body, "incoming body") {
		t.Fatal("failed reconciliation partially changed content", body)
	}
	var count int
	if err = k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", next.RootID).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected reconciliation left a partial import root", count, err)
	}
}

func TestP0ImportHostileHierarchyRollsBackAndMalformedCSVRetainsEvidence(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{
		"A.md": "A body", "B.md": "B body",
		"trove-manifest.json": `{"pages":[{"path":"A.md","parentPath":"B.md"},{"path":"B.md","parentPath":"A.md"}]}`,
	})
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatal("hostile manifest hierarchy not rejected", err)
	}
	var pages int
	if err := k.db.QueryRow("SELECT count(*) FROM pages").Scan(&pages); err != nil || pages != 0 {
		t.Fatal("cyclic import partially committed canonical pages", pages, err)
	}
	if source, err := k.readImportBytes(context.Background(), p.SourceFileID); err != nil || len(source) == 0 {
		t.Fatal("rejected hierarchy lost its original container", err)
	}
	malformed := "Name,Count\nValid row,17\nBroken,\"unterminated quote\n"
	p = completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Malformed.csv": malformed}))
	var csv, row *ImportItem
	for i := range p.Items {
		if p.Items[i].Kind == "csv" {
			csv = &p.Items[i]
		}
		if p.Items[i].Kind == "row" {
			row = &p.Items[i]
		}
	}
	if csv == nil || csv.Outcome != "partial" || row == nil || row.Title != "Valid row" {
		t.Fatal("malformed CSV silently discarded parsed or unreadable content", p)
	}
	if raw, err := k.readImportBytes(context.Background(), csv.RawFileID); err != nil || string(raw) != malformed {
		t.Fatal("malformed CSV original lost", err)
	}
	if body := string(marshal(readPage(t, k, e, csv.PageID))); !strings.Contains(body, "unterminated quote") {
		t.Fatal("malformed row has no readable recovery fallback", body)
	}
}
