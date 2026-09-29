package repository

import (
	"context"
	"encoding/json"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Upgrade only old seeds, while holding the same transaction and row lock as
// credential renewal. The caller publishes its normal durable outbox event.
func upgradeLegacyCatalogBeforeCredentials(ctx context.Context, client *dbent.Client, id int64) error {
	rows, err := client.QueryContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL
	AND extra->'upstream_model_catalog'->>'source_kind'='legacy-mapping-upgrade'
	AND NOT (extra->'upstream_model_catalog' ? 'legacy_mapping_fingerprint') FOR UPDATE`, id)
	if err != nil {
		return err
	}
	found := rows.Next()
	err = rows.Err()
	_ = rows.Close()
	if err != nil || !found {
		return err
	}
	current, err := client.Account.Get(ctx, id)
	if err != nil {
		return err
	}
	account := accountEntityToService(current)
	if !service.UpgradeLegacyModelCatalogEvidence(account) {
		return nil
	}
	encoded, err := json.Marshal(account.Extra[service.UpstreamModelCatalogExtraKey])
	if err != nil {
		return err
	}
	_, err = client.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{upstream_model_catalog}',$2::jsonb) WHERE id=$1`, id, string(encoded))
	return err
}

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
