package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func querySearch(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, q string, extra map[string]any) map[string]any {
	t.Helper()
	r := recoveryRequest(k, map[string]any{"query": q, "limit": 50, "mode": "keyword"})
	for key, v := range extra {
		r[key] = v
	}
	return testOK(t, testDispatch(t, k, e, "search.query", r))
}
func TestSearchFTSChangesDeletesScopesAndRebuild(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Constellation research")
	hidden := createPage(t, k, e, "Constellation secret")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Andromeda telescope café"}}}}}}, "page", page, "block", block))
	out := querySearch(t, k, e, "cafe telescope", nil)
	hits := out["hits"].([]any)
	if len(hits) != 1 {
		t.Fatal(out)
	}
	loc := hits[0].(map[string]any)["location"].(map[string]any)
	if loc["blockId"] != block || loc["pageId"] != page {
		t.Fatal(loc)
	}
	citation := testOK(t, testDispatch(t, k, e, "resolveCitation", recoveryRequest(k, map[string]any{"location": loc})))
	if citation["state"] != "current" || citation["excerpt"] != "Andromeda telescope café" {
		t.Fatal(citation)
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page)}}})
	out = querySearch(t, k, scoped, "Constellation", map[string]any{"scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}})
	for _, raw := range out["hits"].([]any) {
		if raw.(map[string]any)["location"].(map[string]any)["pageId"] == hidden {
			t.Fatal("query scope widened caller grant")
		}
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Jupiter observation"}}}}}, "page", page, "block", block))
	if len(querySearch(t, k, e, "Andromeda", nil)["hits"].([]any)) != 0 {
		t.Fatal("stale text remained indexed")
	}
	if len(querySearch(t, k, e, "Jupiter", nil)["hits"].([]any)) != 1 {
		t.Fatal("new text missing")
	}
	citation = testOK(t, testDispatch(t, k, e, "resolveCitation", recoveryRequest(k, map[string]any{"location": loc})))
	if citation["state"] != "stale" {
		t.Fatal(citation)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": true}}, "page", page))
	if len(querySearch(t, k, e, "Jupiter", nil)["hits"].([]any)) != 0 {
		t.Fatal("deleted text leaked")
	}
	if err := k.RebuildSearch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(querySearch(t, k, e, "secret", nil)["hits"].([]any)) != 1 {
		t.Fatal("rebuild missing canonical source")
	}
	testCode(t, testDispatch(t, k, scoped, "search.rebuild", recoveryRequest(k, nil)), "SCOPE_DENIED")
	// MATCH operators are literal input, never an executable FTS query.
	querySearch(t, k, e, `" OR * NEAR()`, nil)
	request := recoveryRequest(k, map[string]any{"query": "secret", "mode": "keyword", "limit": 5, "scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}})
	raw := k.Dispatch(context.Background(), e, "search", marshal(request))
	if err := contracts.Validate("service", "SearchResponse", raw); err != nil {
		t.Fatal(err, string(raw))
	}
}
func TestSearchTranscriptNavigationAndNoModelFallback(t *testing.T) {
	k, e := testKernel(t)
	page, meeting, file := meetingFixture(t, k, e)
	enqueueTestJob(t, k, e, "TRANSCRIBE_AUDIO", map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()})
	j, err := k.ClaimJob(context.Background(), []string{"TRANSCRIBE_AUDIO"})
	if err != nil {
		t.Fatal(err)
	}
	jobScope, _ := k.JobExecution(context.Background(), string(j.JobId))
	r := jobTranscriptRequest(t, k, jobScope, j, meeting)
	response := k.CommitJobBatch(context.Background(), jobScope, string(j.JobId), string(*j.LeaseToken), "transcript-v1", r)
	if _, ok := response.Value.(contracts.ServiceBatchResponseVariant1); !ok {
		t.Fatal(response)
	}
	out := querySearch(t, k, e, "Durable", map[string]any{"kinds": []any{"transcript"}, "mode": "hybrid"})
	hits := out["hits"].([]any)
	if len(hits) != 1 || out["partial"] != true || out["mode"] != "keyword" {
		t.Fatal(out)
	}
	loc := hits[0].(map[string]any)["location"].(map[string]any)
	if loc["pageId"] != page || loc["transcript"].(map[string]any)["meetingId"] != meeting {
		t.Fatal(loc)
	}
}

func TestSearchLimitPreservesScopeAndPageFiltering(t *testing.T) {
	k, e := testKernel(t)
	for i := 0; i < 40; i++ {
		createPage(t, k, e, "Needle")
	}
	page := createPage(t, k, e, "Needle within a much longer lower ranked authorized page title")
	scope := contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page)}}}
	scoped := k.Grant(e.Actor, "user", scope)
	defer k.RevokeGrant(string(scoped.ScopeGrantId))
	projectPage := createPage(t, k, e, "Search authorization project")
	project := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "container.create", "containerId": project, "owningPageId": projectPage, "label": "Search project", "containerKind": "project", "parentProjectId": nil}}, "container", project, "page", projectPage))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "project.setScope", "projectId": project, "memberPageIds": []any{page}, "pinnedPageIds": []any{}, "excludedPageIds": []any{}, "inheritAncestorPreferences": false, "inheritWorkspacePreferences": false}}, "container", project))
	projectScope := contracts.DomainScope{Value: contracts.DomainScopeVariant2{Kind: "project", WorkspaceId: contracts.DomainId(k.ID()), ProjectId: contracts.DomainId(project)}}
	projectScoped := k.Grant(e.Actor, "user", projectScope)
	defer k.RevokeGrant(string(projectScoped.ScopeGrantId))
	for _, tc := range []struct {
		name  string
		actor contracts.ServiceExecutionContext
		extra map[string]any
	}{
		{"granted pages", scoped, map[string]any{"limit": 1}},
		{"requested pages", e, map[string]any{"limit": 1, "scope": scope}},
		{"granted project", projectScoped, map[string]any{"limit": 1}},
		{"requested project", e, map[string]any{"limit": 1, "scope": projectScope}},
		{"page filter", e, map[string]any{"limit": 1, "pageId": page}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := querySearch(t, k, tc.actor, "Needle", tc.extra)
			hits := result["hits"].([]any)
			if len(hits) != 1 || hits[0].(map[string]any)["location"].(map[string]any)["pageId"] != page {
				t.Fatal("limit applied before visibility excluded the authorized result", result)
			}
		})
	}
	result := querySearch(t, k, e, "Needle", map[string]any{"limit": 1, "kinds": []any{"file"}})
	if len(result["hits"].([]any)) != 0 {
		t.Fatal("source-kind filter returned a page result", result)
	}
}
func TestSearchExtractionJobPersistsAndIndexesFile(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Research assets")
	file, block := attachTestFile(t, k, e, page, stageTestFile(t, k, e, "Unique otter document\n\nSecond structural section"), true)
	enqueueTestJob(t, k, e, "EXTRACT_DOCUMENT", map[string]any{"kind": "file", "fileId": file, "extractorVersion": documentExtractorVersion})
	j, err := k.ClaimJob(context.Background(), []string{"EXTRACT_DOCUMENT"})
	if err != nil {
		t.Fatal(err)
	}
	if err = extractDocumentJob(context.Background(), k, j); err != nil {
		t.Fatal(err)
	}
	out := querySearch(t, k, e, "otter", map[string]any{"kinds": []any{"file"}})
	hits := out["hits"].([]any)
	if len(hits) != 1 {
		t.Fatal(out)
	}
	h := hits[0].(map[string]any)
	if h["fileId"] != file || h["location"].(map[string]any)["blockId"] != block {
		t.Fatal(h)
	}
	if err = extractDocumentJob(context.Background(), k, j); err != nil {
		t.Fatal("resume", err)
	}
	var n int
	k.db.QueryRow("SELECT count(*) FROM document_sections WHERE file_id=?", file).Scan(&n)
	if n != 2 {
		t.Fatal("structural sections duplicated or lost", n)
	}
	if err = k.RebuildSearch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(querySearch(t, k, e, "otter", nil)["hits"].([]any)) != 1 {
		t.Fatal("extraction missing after rebuild")
	}
}
func TestSearchSemanticRevisionBoundAndHybrid(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Canine care")
	other := createPage(t, k, e, "Astronomy")
	model := NewID()
	k.SetEmbeddingProvider(func(_ context.Context, _ string, texts []string, _ []contracts.DomainLocation) ([][]float32, error) {
		v := [][]float32{}
		for _, text := range texts {
			if strings.Contains(text, "Astronomy") {
				v = append(v, []float32{0, 1})
			} else {
				v = append(v, []float32{1, 0})
			}
		}
		return v, nil
	})
	defer k.SetEmbeddingProvider(nil)
	enqueueTestJob(t, k, e, "GENERATE_EMBEDDINGS", map[string]any{"kind": "embedding", "locations": []any{map[string]any{"pageId": page, "sourceRevision": "1"}, map[string]any{"pageId": other, "sourceRevision": "1"}}, "modelPackId": model, "dimension": 2})
	j, err := k.ClaimJob(context.Background(), []string{"GENERATE_EMBEDDINGS"})
	if err != nil {
		t.Fatal(err)
	}
	if err = registeredJobs["GENERATE_EMBEDDINGS"](context.Background(), k, j); err != nil {
		t.Fatal(err)
	}
	out := querySearch(t, k, e, "dog", map[string]any{"mode": "semantic", "modelPackId": model})
	hits := out["hits"].([]any)
	if len(hits) < 1 || hits[0].(map[string]any)["location"].(map[string]any)["pageId"] != page {
		t.Fatal(out)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Edited source"}}, "page", page))
	out = querySearch(t, k, e, "dog", map[string]any{"mode": "semantic", "modelPackId": model})
	for _, h := range out["hits"].([]any) {
		if h.(map[string]any)["location"].(map[string]any)["pageId"] == page {
			t.Fatal("stale vector survived edit")
		}
	}
}
func TestDocumentAllowlistAndReadableExtraction(t *testing.T) {
	for _, fixture := range []struct{ name, body, want string }{{"a.md", "# Heading\n\nUseful notes", "Useful"}, {"a.csv", "Name,Count\n\"A, B\",2\n", "A, B"}, {"a.html", "<p>Readable</p><script>secretScript</script>", "Readable"}} {
		t.Run(fixture.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "doc-")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			f.WriteString(fixture.body)
			f.Seek(0, 0)
			result, err := ExtractDocument(context.Background(), f, fixture.name, "text/plain")
			if err != nil {
				t.Fatal(err)
			}
			raw := string(marshal(result))
			if !strings.Contains(raw, fixture.want) || strings.Contains(raw, "secretScript") {
				t.Fatal(raw)
			}
		})
	}
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	w, _ := z.Create("word/document.xml")
	w.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Durable DOCX</w:t></w:r></w:p></w:body></w:document>`))
	z.Close()
	f, _ := os.CreateTemp(t.TempDir(), "docx-")
	defer f.Close()
	f.Write(archive.Bytes())
	f.Seek(0, 0)
	result, err := ExtractDocument(context.Background(), f, "sample.docx", "application/zip")
	if err != nil || !strings.Contains(string(marshal(result)), "Durable DOCX") {
		t.Fatal(result, err)
	}
	f.Seek(0, 0)
	if _, err = ExtractDocument(context.Background(), f, "unsupported.exe", "application/octet-stream"); err == nil {
		t.Fatal("unsupported binary accepted")
	}
}
func TestDocumentPDFTextLayerAndIntegrity(t *testing.T) {
	// A portable PDF fixture with a real text layer and a correct xref table.
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	stream := "BT /F1 18 Tf 50 750 Td (PDF searchable nebula) Tj ET"
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	var pdf strings.Builder
	pdf.WriteString("%PDF-1.4\n")
	offsets := []int{}
	for i, object := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	f, _ := os.CreateTemp(t.TempDir(), "pdf-")
	defer f.Close()
	f.WriteString(pdf.String())
	f.Seek(0, 0)
	result, err := ExtractDocument(context.Background(), f, "fixture.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sections) != 1 || result.Sections[0].Page != 1 || !strings.Contains(result.Sections[0].Text, "nebula") {
		t.Fatal(result)
	}
	// A supplied helper must have an intact manifest; never run unchecked bytes.
	helper := filepath.Join(t.TempDir(), "document-extractor")
	os.WriteFile(helper, []byte("#!/bin/sh\necho unsafe"), 0700)
	os.WriteFile(helper+".json", []byte(`{"schemaVersion":1,"name":"document-extractor","sha256":"bad"}`), 0600)
	t.Setenv("TROVE_DOCUMENT_EXTRACTOR", helper)
	f.Seek(0, 0)
	if _, err = ExtractDocument(context.Background(), f, "fixture.pdf", "application/pdf"); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatal(err)
	}
}

func TestSearchDropsSourceEditedDuringSemanticInference(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "before inference")
	model := NewID()
	k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
		return [][]float32{{1, 0}}, nil
	})
	defer k.SetEmbeddingProvider(nil)
	enqueueTestJob(t, k, e, "GENERATE_EMBEDDINGS", map[string]any{"kind": "embedding", "locations": []any{map[string]any{"pageId": page, "sourceRevision": "1"}}, "modelPackId": model, "dimension": 2})
	job, err := k.ClaimJob(context.Background(), []string{"GENERATE_EMBEDDINGS"})
	if err != nil {
		t.Fatal(err)
	}
	if err = registeredJobs["GENERATE_EMBEDDINGS"](context.Background(), k, job); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
		close(started)
		<-release
		return [][]float32{{1, 0}}, nil
	})
	type response struct {
		result any
		err    error
	}
	done := make(chan response, 1)
	go func() {
		r, e2 := searchQuery(context.Background(), k, e, map[string]any{"query": "before", "mode": "semantic", "modelPackId": model})
		done <- response{r, e2}
	}()
	<-started
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "after inference"}}, "page", page))
	close(release)
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if len(result.result.(SearchResults).Hits) != 0 {
		t.Fatal("stale source survived concurrent inference", result.result)
	}
}
