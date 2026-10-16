package workspace

import (
	"context"

	"trove.local/phase0/contracts"
)

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
	raw, err := searchQuery(ctx, k, e, request)
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
		raw, err = searchQuery(ctx, k, e, request)
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
