package workspace

import (
	"context"
	"fmt"
	"testing"

	"trove.local/phase0/contracts"
)

func TestSemanticMaintenanceTracksEditsExtractionAndRebuild(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	model := NewID()
	page := createPage(t, k, e, "Automatic vectors")
	file, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, "Late document content"), true)
	queue := func() {
		t.Helper()
		if err := k.QueueSemanticUpdates(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		t.Helper()
		var n int
		if err := k.db.QueryRow("SELECT count(*) FROM jobs WHERE kind='GENERATE_EMBEDDINGS'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	queue()
	if count() != 0 {
		t.Fatal("semantic generation enabled without consent")
	}
	configure := func(enabled bool) {
		testOK(t, testDispatch(t, k, e, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": enabled, "modelPackId": model, "dimension": 2})))
	}
	configure(true)
	k.SetEmbeddingProvider(func(_ context.Context, _ string, texts []string, _ []contracts.DomainLocation) ([][]float32, error) {
		r := [][]float32{}
		for range texts {
			r = append(r, []float32{1, 0})
		}
		return r, nil
	})
	defer k.SetEmbeddingProvider(nil)
	complete := func(fail bool) {
		t.Helper()
		j, err := k.ClaimJob(ctx, []string{"GENERATE_EMBEDDINGS"})
		if err != nil {
			t.Fatal(err)
		}
		if fail {
			err = Fail("WORKER_FAILED", "fixture needs user retry")
		} else {
			err = registeredJobs["GENERATE_EMBEDDINGS"](ctx, k, j)
		}
		if err != nil && !fail {
			t.Fatal(err)
		}
		if err = k.FinishJob(ctx, string(j.JobId), string(*j.LeaseToken), err, false); err != nil {
			t.Fatal(err)
		}
	}
	queue()
	if count() != 1 {
		t.Fatal("first index not queued")
	}
	queue()
	if count() != 1 {
		t.Fatal("duplicate while active")
	}
	complete(false)
	queue()
	if count() != 1 {
		t.Fatal("current vectors needlessly queued")
	}
	// Extraction adds chunks without changing the page revision.
	enqueueTestJob(t, k, e, "EXTRACT_DOCUMENT", map[string]any{"kind": "file", "fileId": file, "extractorVersion": documentExtractorVersion})
	j, err := k.ClaimJob(ctx, []string{"EXTRACT_DOCUMENT"})
	if err != nil {
		t.Fatal(err)
	}
	if err = extractDocumentJob(ctx, k, j); err != nil {
		t.Fatal(err)
	}
	queue()
	if count() != 2 {
		t.Fatal("extracted chunks not queued", count())
	}
	complete(false)
	if err = k.RebuildSearch(ctx); err != nil {
		t.Fatal(err)
	}
	queue()
	if count() != 3 {
		t.Fatal("rebuild replayed old successful job", count())
	}
	complete(false)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Updated automatic source"}}, "page", page))
	queue()
	if count() != 4 {
		t.Fatal("edit not queued")
	}
	complete(true)
	queue()
	if count() != 4 {
		t.Fatal("failed job loop")
	}
	configure(false)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Paused maintenance"}}, "page", page))
	queue()
	if count() != 4 {
		t.Fatal("disable ignored")
	}
	configure(true)
	queue()
	if count() != 5 {
		t.Fatal("explicit reenable failed")
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page)}}})
	testCode(t, testDispatch(t, k, scoped, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": false})), "SCOPE_DENIED")
}

func TestSemanticSettingsKeepModelDimensionAtomic(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, b := NewID(), NewID()
	configure := func(model string, dimension int) {
		t.Helper()
		testOK(t, testDispatch(t, k, e, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": true, "modelPackId": model, "dimension": dimension})))
	}
	configure(a, 2)
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 150; i++ {
			raw, err := semanticSettings(ctx, k, e, nil)
			if err != nil {
				done <- err
				return
			}
			settings := raw.(map[string]any)
			model, dim := settings["modelPackId"], settings["dimension"]
			if (model == a && dim != 2) || (model == b && dim != 3) {
				done <- fmt.Errorf("mixed semantic settings: %#v", settings)
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 30; i++ {
		configure(b, 3)
		configure(a, 2)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": false})))
	createPage(t, k, e, "Disabled automatic source")
	if err := k.QueueSemanticUpdates(ctx, e); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := k.db.QueryRow("SELECT count(*) FROM jobs WHERE kind='GENERATE_EMBEDDINGS'").Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal(jobs, err)
	}
}

func TestClosingWorkspaceReleasesEmbeddingProvider(t *testing.T) {
	k, _ := testKernel(t)
	provider := func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
		return nil, nil
	}
	k.SetEmbeddingProvider(provider)
	if _, ok := embeddingProviders.Load(k); !ok {
		t.Fatal("provider was not installed")
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := embeddingProviders.Load(k); ok {
		t.Fatal("closed workspace retained its provider and supervisor")
	}
	k.SetEmbeddingProvider(provider)
	if _, ok := embeddingProviders.Load(k); ok {
		t.Fatal("provider attached after workspace closed")
	}
}
