package repository

import (
	"context"
	"strconv"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
)

// requireLegacyAccountConfiguration runs only after the corresponding account
// rows are locked. Connection adoption takes a conflicting account lock, so a
// successful check remains valid until the caller commits its configuration.
func requireLegacyAccountConfiguration(ctx context.Context, database sqlExecutor, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	query := "SELECT account_id FROM provider_connections WHERE account_id=$1 AND NOT legacy_dirty"
	var argument any = ids[0]
	if len(ids) > 1 {
		query = "SELECT account_id FROM provider_connections WHERE account_id=ANY($1) AND NOT legacy_dirty"
		argument = pq.Array(ids)
	}
	query += workspaceClause(ctx, "workspace_id") + " ORDER BY account_id LIMIT 1"
	rows, err := database.QueryContext(ctx, query, argument)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		return service.ErrAccountConnectionManaged.WithMetadata(map[string]string{
			"account_id": strconv.FormatInt(id, 10), "management_surface": "connections",
		})
	}
	return rows.Err()
}

// Lock targets in a stable order before checking ownership. The scope filter
// comes before the metadata-bearing check, so a foreign account is never named
// in a managed-connection response. The caller must supply its write transaction.
func lockLegacyAccountConfigurations(ctx context.Context, database sqlExecutor, ids []int64) ([]int64, error) {
	rows, err := database.QueryContext(ctx, `/* account_configuration_owner_lock */
SELECT id FROM accounts WHERE id=ANY($1) AND deleted_at IS NULL`+workspaceClause(ctx, "workspace_id")+`
ORDER BY id FOR NO KEY UPDATE`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	locked := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		locked = append(locked, id)
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return locked, requireLegacyAccountConfiguration(ctx, database, locked)
}

// The 429 observer uses the bulk writer solely to merge the observed plan type.
// This is not a connection editing command; a mixed patch is always management.
func accountBulkPlanObservation(ctx context.Context, ids []int64, updates service.AccountBulkUpdate) bool {
	_, observed := updates.Credentials["plan_type"]
	return len(ids) == 1 && service.IsAccountPlanObservation(ctx, ids[0]) &&
		observed && len(updates.Credentials) == 1 && len(updates.Extra) == 0 &&
		updates.TPMLimit == nil && updates.Name == nil && updates.ProxyID == nil && updates.ProxyGroupID == nil &&
		updates.Concurrency == nil && updates.Priority == nil && updates.RateMultiplier == nil && updates.LoadFactor == nil &&
		updates.Status == nil && updates.Schedulable == nil && updates.ProbeEnabled == nil && !updates.EnsureCodexFingerprintSeed
}
