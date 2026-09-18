package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Hold a transaction-scoped advisory lock through discovery and commit. It is
// released even if the process exits; no lease can expire during an upstream call.
func (r *accountRepository) TryLockAccountModelCatalog(ctx context.Context, id int64) (func(), error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('account_model_catalog:' || $1::text, 0))", id)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	var acquired bool
	if rows.Next() {
		err = rows.Scan(&acquired)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil || !acquired {
		_ = tx.Rollback()
		if err != nil {
			return nil, err
		}
		return nil, service.ErrAccountModelCatalogConflict
	}
	return func() { _ = tx.Rollback() }, nil
}
