package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"

	"trove.local/phase0/contracts"
)

func init() {
	RegisterQuery("search.semanticSettings", semanticSettings)
	RegisterQuery("search.configureSemantic", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		k.semanticMaintenanceMu.Lock()
		defer k.semanticMaintenanceMu.Unlock()
		if err := requireOwnerScope(k, e); err != nil {
			return nil, err
		}
		enabled, ok := r["enabled"].(bool)
		if !ok {
			return nil, Fail("INVALID_REQUEST", "Choose whether semantic maintenance is enabled")
		}
		model, dim := str(r["modelPackId"]), number(r["dimension"])
		if enabled {
			if err := contracts.Validate("domain", "Id", marshal(model)); err != nil || dim < 1 || dim > 8192 {
				return nil, Fail("INVALID_REQUEST", "Choose a verified embedding model and dimension")
			}
		} else {
			model = ""
			dim = 0
		}
		if err := k.write(ctx, func(tx *sql.Tx) error {
			for key, value := range map[string]string{"semantic_model": model, "semantic_dimension": strconv.FormatInt(dim, 10), "semantic_epoch": NewID()} {
				if _, err := tx.ExecContext(ctx, "INSERT INTO search_meta VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value); err != nil {
					return err
				}
			}
			_, err := tx.ExecContext(ctx, "DELETE FROM semantic_auto_attempts")
			return err
		}); err != nil {
			return nil, err
		}
		return semanticSettings(ctx, k, e, r)
	})
}
func semanticSettings(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, _ map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	var model, dimension string
	if err := k.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT value FROM search_meta WHERE key='semantic_model'),''),COALESCE((SELECT value FROM search_meta WHERE key='semantic_dimension'),'0')`).Scan(&model, &dimension); err != nil {
		return nil, err
	}
	dim, _ := strconv.Atoi(dimension)
	return map[string]any{"enabled": model != "", "modelPackId": model, "dimension": dim}, nil
}

// QueueSemanticUpdates runs only after the user enables an installed model.
// Each source fingerprint is attempted once automatically; failures remain
// visible for explicit retry instead of creating a loop of failing native jobs.
func (k *Kernel) QueueSemanticUpdates(ctx context.Context, e contracts.ServiceExecutionContext) error {
	// Serialize upkeep with configuration so one job cannot combine model A
	// with model B's dimension, or enqueue after a completed disable request.
	k.semanticMaintenanceMu.Lock()
	defer k.semanticMaintenanceMu.Unlock()
	if err := requireOwnerScope(k, e); err != nil {
		return err
	}
	var model, dimension, generation, epoch string
	if err := k.db.QueryRowContext(ctx, "SELECT value FROM search_meta WHERE key='semantic_model'").Scan(&model); err == sql.ErrNoRows || model == "" {
		return nil
	} else if err != nil {
		return err
	}
	if err := k.db.QueryRowContext(ctx, "SELECT value FROM search_meta WHERE key='semantic_dimension'").Scan(&dimension); err != nil {
		return err
	}
	dim, err := strconv.Atoi(dimension)
	if err != nil {
		return err
	}
	identity, err := k.embeddingIdentity(ctx, model)
	if err != nil {
		return err
	}
	if err = k.write(ctx, func(tx *sql.Tx) error {
		_, err := ensureVectorGeneration(ctx, tx, model, identity, dim)
		return err
	}); err != nil {
		return err
	}
	if err = k.db.QueryRowContext(ctx, "SELECT value FROM search_meta WHERE key='generation'").Scan(&generation); err != nil {
		return err
	}
	if err = k.db.QueryRowContext(ctx, "SELECT value FROM search_meta WHERE key='semantic_epoch'").Scan(&epoch); err != nil {
		return err
	}
	var active int
	if err = k.db.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE kind='GENERATE_EMBEDDINGS' AND state IN('queued','running','retry_wait','cancel_requested')").Scan(&active); err != nil || active > 0 {
		return err
	}
	if err = k.FlushSearch(ctx); err != nil {
		return err
	}
	rows, err := k.db.QueryContext(ctx, `SELECT c.page_id,c.source_revision,c.id,c.content_hash FROM search_chunks c JOIN pages p ON p.id=c.page_id AND p.revision=c.source_revision AND p.deleted_at IS NULL WHERE NOT EXISTS(SELECT 1 FROM search_embeddings v WHERE v.chunk_id=c.id AND v.content_hash=c.content_hash AND v.model_pack_id=?) AND NOT EXISTS(SELECT 1 FROM semantic_auto_attempts a WHERE a.page_id=c.page_id AND a.revision=c.source_revision AND a.model_id=?) ORDER BY c.page_id,c.id`, model, model)
	if err != nil {
		return err
	}
	locations := []contracts.DomainLocation{}
	fingerprints := []string{}
	seen := map[string]bool{}
	for rows.Next() {
		var loc contracts.DomainLocation
		var chunk, hash string
		if err = rows.Scan(&loc.PageId, &loc.SourceRevision, &chunk, &hash); err != nil {
			rows.Close()
			return err
		}
		page := string(loc.PageId)
		if !seen[page] {
			if len(locations) == 32 {
				break
			}
			locations = append(locations, loc)
			seen[page] = true
		}
		fingerprints = append(fingerprints, chunk+":"+hash)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(locations) == 0 {
		return err
	}
	sort.Strings(fingerprints)
	key := fmt.Sprintf("semantic-auto-%s-%s", model, searchDigest(string(marshal([]any{generation, epoch, locations, fingerprints})))[:32])
	_, err = k.enqueueJob(ctx, e, map[string]any{"kind": "GENERATE_EMBEDDINGS", "idempotencyKey": key, "payload": map[string]any{"kind": "embedding", "locations": locations, "modelPackId": model, "dimension": dim}})
	if err != nil {
		return err
	}
	return k.write(ctx, func(tx *sql.Tx) error {
		for _, loc := range locations {
			if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO semantic_auto_attempts VALUES(?,?,?)", loc.PageId, loc.SourceRevision, model); err != nil {
				return err
			}
		}
		return nil
	})
}
