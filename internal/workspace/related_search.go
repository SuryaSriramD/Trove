package workspace

import (
	"context"

	"trove.local/phase0/contracts"
)

// Related-content ranking v1 uses a page's best substantive chunk. Its title is
// eligible only when the current indexed page has no nonempty content chunks.
// Apply this before candidate limits so generic titles cannot displace body
// evidence. Ordinary page-name search continues to use every chunk.
const relatedContentChunkSQL = `(c.id<>(c.page_id||':title') OR NOT EXISTS(SELECT 1 FROM search_chunks substantive WHERE substantive.page_id=c.page_id AND substantive.source_revision=c.source_revision AND substantive.id<>(substantive.page_id||':title') AND trim(substantive.body)<>''))`

// Related surfaces honor the configured model, including opt-out, even when
// an older vector generation is retained. A changed consent epoch invalidates
// an in-flight semantic interpretation before it can reach either surface.
func configuredRelatedSearch(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, query, page string, limit int) (SearchResults, error) {
	var model, epoch string
	if err := k.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT value FROM search_meta WHERE key='semantic_model'),''),COALESCE((SELECT value FROM search_meta WHERE key='semantic_epoch'),'')`).Scan(&model, &epoch); err != nil {
		return SearchResults{}, err
	}
	request := map[string]any{"query": query, "mode": "keyword", "limit": int64(limit), "excludePageId": page}
	if model != "" {
		request["mode"], request["modelPackId"] = "semantic", model
	}
	raw, err := searchQueryWithRelatedContent(ctx, k, e, request, true)
	if err != nil {
		return SearchResults{}, err
	}
	search := raw.(SearchResults)
	if model == "" {
		search.Warning = "Semantic related content is disabled. Enable a local embedding model in Search index settings. Showing keyword matches."
	}
	var currentModel, currentEpoch string
	if err := k.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT value FROM search_meta WHERE key='semantic_model'),''),COALESCE((SELECT value FROM search_meta WHERE key='semantic_epoch'),'')`).Scan(&currentModel, &currentEpoch); err != nil {
		return SearchResults{}, err
	}
	if currentModel != model || currentEpoch != epoch {
		// Consent or model selection changed while the worker was running.
		// Drop that interpretation; a fresh keyword query requires no model.
		request["mode"] = "keyword"
		delete(request, "modelPackId")
		raw, err = searchQueryWithRelatedContent(ctx, k, e, request, true)
		if err != nil {
			return SearchResults{}, err
		}
		search = raw.(SearchResults)
		search.Warning = "Semantic settings changed during retrieval. Showing fresh keyword matches; find again to use the current setting."
	}
	return search, nil
}

func relatedPageCurrent(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, id, revision string) (bool, error) {
	if err := k.authorizePage(ctx, k.db, e, id); err != nil {
		return false, nil
	}
	var live int
	err := k.db.QueryRowContext(ctx, `SELECT count(*) FROM pages p WHERE p.id=? AND p.revision=? AND `+relatedEligiblePageSQL, id, revision).Scan(&live)
	return live == 1, err
}
