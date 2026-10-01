package repository

import (
	"context"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// Lock the final logical pair before a bulk route change. Membership/group
// deletion uses the same transaction and group reference lock as single writes.
func validateBulkAccountProxyGroups(ctx context.Context, exec sqlExecutor, client *dbent.Client, ids []int64, patch service.AccountBulkUpdate) error {
	rows, err := exec.QueryContext(ctx, "SELECT workspace_id, proxy_id, proxy_group_id FROM accounts WHERE id = ANY($1) AND deleted_at IS NULL AND "+workspacePredicate(ctx, "workspace_id")+" FOR UPDATE", pq.Array(ids))
	if err != nil {
		return err
	}
	type binding struct {
		workspace   int64
		node, group *int64
	}
	var bindings []binding
	for rows.Next() {
		var b binding
		if err := rows.Scan(&b.workspace, &b.node, &b.group); err != nil {
			_ = rows.Close()
			return err
		}
		bindings = append(bindings, b)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, b := range bindings {
		if patch.ProxyID != nil {
			b.node = patch.ProxyID
		}
		if patch.ProxyGroupID != nil {
			b.group = patch.ProxyGroupID
		}
		if (b.node != nil && *b.node < 0) || (b.group != nil && *b.group < 0) || (b.node != nil && *b.node > 0 && b.group != nil && *b.group > 0) {
			return infraerrors.BadRequest("PROXY_BINDING_CONFLICT", "select either an IP node or an IP group")
		}
		if b.group != nil && *b.group > 0 {
			if err := validateAccountProxyGroup(ctx, client, b.group, b.workspace); err != nil {
				return err
			}
		}
	}
	return nil
}
