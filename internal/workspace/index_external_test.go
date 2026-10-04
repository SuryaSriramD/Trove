package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovedRepositoryIndexExcludesSecretsAndTracksMoves(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	folder := t.TempDir()
	os.Mkdir(filepath.Join(folder, "nested"), 0700)
	files := map[string]string{"main.go": "package main\nfunc First() { }\nfunc Second() { }", ".env": "SECRET=never", "private.pem": "key", ".gitignore": "ignored.txt\nnested/generated/\n", "ignored.txt": "ignored secret", "nested/.gitignore": "*.log\n", "nested/debug.log": "ignored log", "binary.dat": "\x00binary"}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	os.WriteFile(outside, []byte("OUTSIDE_SCOPE"), 0600)
	os.Symlink(outside, filepath.Join(folder, "linked.go"))
	if _, err := scanExternalRoot(ctx, k, e, map[string]any{"rootId": "unapproved"}); err == nil {
		t.Fatal("unapproved scan accepted")
	}
	root, err := k.ApproveIndexRoot(ctx, e, folder, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	out, err := listExternalSources(ctx, k, e, map[string]any{"rootId": root.ID})
	if err != nil {
		t.Fatal(err)
	}
	sources := out.([]ExternalSource)
	var original ExternalSource
	for _, source := range sources {
		if source.Path == "main.go" {
			original = source
		}
		if strings.Contains(source.Path, "ignored") || strings.HasSuffix(source.Path, ".log") || strings.HasSuffix(source.Path, ".pem") || source.Path == ".env" || source.Path == "binary.dat" || source.Path == "linked.go" {
			t.Fatalf("unsafe file indexed: %+v", source)
		}
	}
	if original.ID == "" {
		t.Fatal("source missing")
	}
	hits, err := searchExternalSources(ctx, k, e, map[string]any{"query": "Second"})
	if err != nil || len(hits.([]any)) != 1 {
		t.Fatal("language chunk search failed", hits, err)
	}
	p := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Reference.md": "External reference"}))
	if _, err = referenceExternalSource(ctx, k, e, map[string]any{"sourceId": original.ID, "pageId": p.RootID}); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(filepath.Join(folder, "main.go"), filepath.Join(folder, "renamed.go")); err != nil {
		t.Fatal(err)
	}
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	var name, state string
	if err = k.db.QueryRow("SELECT path,state FROM external_sources WHERE id=?", original.ID).Scan(&name, &state); err != nil || name != "renamed.go" || state != "present" {
		t.Fatal("move lost identity", name, state, err)
	}
	os.Remove(filepath.Join(folder, "renamed.go"))
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	var refs int
	k.db.QueryRow("SELECT state FROM external_sources WHERE id=?", original.ID).Scan(&state)
	k.db.QueryRow("SELECT count(*) FROM external_page_references WHERE source_id=?", original.ID).Scan(&refs)
	if state != "missing" || refs != 1 {
		t.Fatal("deletion lost reference", state, refs)
	}
	if _, err = revokeExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err == nil {
		t.Fatal("revoked scan accepted")
	}
}
func TestIndexRootIdentityChangeAndConfinement(t *testing.T) {
	k, e := testKernel(t)
	folder := filepath.Join(t.TempDir(), "approved")
	os.Mkdir(folder, 0700)
	root, err := k.ApproveIndexRoot(context.Background(), e, folder, false)
	if err != nil {
		t.Fatal(err)
	}
	os.Rename(folder, folder+"-old")
	os.Mkdir(folder, 0700)
	os.WriteFile(filepath.Join(folder, "secret.txt"), []byte("NEW ROOT"), 0600)
	if _, err = scanExternalRoot(context.Background(), k, e, map[string]any{"rootId": root.ID}); err == nil {
		t.Fatal("retargeted directory indexed")
	}
	if _, err = k.ApproveIndexRoot(context.Background(), e, k.Path(), true); err == nil {
		t.Fatal("workspace storage indexed")
	}
}
func TestExternalIgnoreRulesAndBoundedLanguageChunks(t *testing.T) {
	rules := []externalIgnore{{".", "**/generated/*.go", false}, {".", "*.log", false}, {".", "keep.log", true}}
	if !externalIgnored("a/generated/test.go", false, rules) || !externalIgnored("a/test.log", false, rules) || externalIgnored("keep.log", false, rules) {
		t.Fatal("ignore semantics wrong")
	}
	lang, chunks := languageChunks("example.py", "def one():\n    return 1\n\ndef two():\n    return 2")
	if lang != "python" || len(chunks) != 2 || chunks[0].Symbol != "one" || chunks[1].StartLine != 4 {
		t.Fatal(lang, chunks)
	}
}
