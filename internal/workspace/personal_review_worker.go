package workspace

import (
	"context"
	"database/sql"
	"trove.local/phase0/contracts"
)

// CheckPersonalReviewGeneration lets an owning background worker stop expensive
// native work when its retained occurrence is cancelled or its evidence changes.
func (k *Kernel) CheckPersonalReviewGeneration(ctx context.Context, e contracts.ServiceExecutionContext, id string) error {
	if err := requireOwnerScope(k, e); err != nil {
		return err
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := readPersonalReviewRun(ctx, tx, id)
	if err != nil {
		return err
	}
	if r.Status != "queued" && r.Status != "generating" {
		return Fail("CANCELLED", "Review is no longer queued or generating")
	}
	return k.validateReviewGenerationSnapshot(ctx, tx, e, r)
}
