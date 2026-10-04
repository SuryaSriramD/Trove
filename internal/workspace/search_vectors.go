package workspace

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"sync"

	"trove.local/phase0/contracts"
)

// Identity includes model bytes, runtime and embedding preprocessing. A model
// ID alone cannot establish compatibility after a model pack replacement.
type EmbeddingIdentityProvider func(context.Context, string) (string, error)

var embeddingIdentities sync.Map

func (k *Kernel) SetEmbeddingIdentityProvider(provider EmbeddingIdentityProvider) {
	if provider == nil {
		embeddingIdentities.Delete(k)
	} else {
		embeddingIdentities.Store(k, provider)
	}
}
func (k *Kernel) embeddingIdentity(ctx context.Context, model string) (string, error) {
	if p, ok := embeddingIdentities.Load(k); ok {
		return p.(EmbeddingIdentityProvider)(ctx, model)
	}
	// Injected providers in service tests have no native model manifest.
	return "injected-provider:" + model, nil
}

var vectorTableName = regexp.MustCompile(`^search_vec_[0-9a-f]{64}$`)

func floatVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, n := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(n))
	}
	return b
}

func ensureVectorGeneration(ctx context.Context, tx *sql.Tx, model, identity string, dimension int) (string, error) {
	if dimension < 1 || dimension > 8192 || contracts.Validate("domain", "Id", marshal(model)) != nil || identity == "" {
		return "", Fail("INVALID_REQUEST", "Invalid embedding generation")
	}
	table := "search_vec_" + searchDigest(string(marshal([]any{model, identity, dimension, "cosine-f32-v1"})))
	var old string
	err := tx.QueryRowContext(ctx, "SELECT table_name FROM search_vector_models WHERE model_pack_id=?", model).Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if old == table {
		return table, nil
	}
	if old != "" {
		if !vectorTableName.MatchString(old) {
			return "", Fail("INTEGRITY_ERROR", "Invalid vector generation table")
		}
		if _, err = tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+old+"_delete; DROP TABLE IF EXISTS "+old); err != nil {
			return "", err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM search_embeddings WHERE model_pack_id=?", model); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM semantic_auto_attempts WHERE model_id=?", model); err != nil {
		return "", err
	}
	// Identifiers derive solely from a SHA-256; dimension and UUID were validated.
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("CREATE VIRTUAL TABLE %s USING vec0(embedding float[%d] distance_metric=cosine)", table, dimension)); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER %s_delete AFTER DELETE ON search_embeddings WHEN old.model_pack_id='%s' BEGIN DELETE FROM %s WHERE rowid=old.rowid; END", table, model, table)); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO search_vector_models VALUES(?,?,?,?) ON CONFLICT(model_pack_id) DO UPDATE SET dimension=excluded.dimension,identity=excluded.identity,table_name=excluded.table_name", model, dimension, identity, table)
	return table, err
}

func storeSearchVector(ctx context.Context, tx *sql.Tx, table string, hit SearchHit, model string, v []float32) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM search_embeddings WHERE chunk_id=? AND model_pack_id=?", hit.ChunkID, model); err != nil {
		return err
	}
	// Keep the legacy metadata shape for existing queries. Vectors themselves
	// now live only as compact native float32 values in the disposable vec0 table.
	r, err := tx.ExecContext(ctx, "INSERT INTO search_embeddings VALUES(?,?,?,?,?)", hit.ChunkID, hit.ContentHash, model, len(v), "")
	if err != nil {
		return err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+table+"(rowid,embedding) VALUES(?,?)", id, floatVector(v))
	return err
}

// The predicates match authorizePage, and are intersected for caller/requested
// scopes before KNN ranking, so inaccessible neighbours cannot crowd out hits.
func vectorScopeSQL(scope contracts.DomainScope) (string, []any) {
	switch s := scope.Value.(type) {
	case contracts.DomainScopeVariant1:
		return "1", nil
	case contracts.DomainScopeVariant3:
		return "c.page_id IN (SELECT value FROM json_each(?))", []any{string(marshal(s.PageIds))}
	case contracts.DomainScopeVariant2:
		return "c.page_id IN (SELECT owning_page_id FROM containers WHERE id=? AND kind='project' UNION SELECT page_id FROM project_pages WHERE project_id=? AND relationship IN('member','pin')) AND c.page_id NOT IN (SELECT page_id FROM project_pages WHERE project_id=? AND relationship='exclude')", []any{s.ProjectId, s.ProjectId, s.ProjectId}
	default:
		return "0", nil
	}
}
