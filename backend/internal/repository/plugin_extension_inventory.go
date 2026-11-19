package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	grants "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/extensions/actions"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func (r *pluginRepository) ExtensionInventory(ctx context.Context) ([]service.ExtensionInventoryItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT i.plugin_key, i.sdk_version, COALESCE(i.publisher_id,''), i.capabilities, i.state, COALESCE(i.digest,''),
		g.account_id,g.action,g.rollout_percent FROM omnihive_extension_inventory i
		LEFT JOIN (SELECT DISTINCT plugin_id,archive_sha256,account_id,action,rollout_percent FROM omnihive_verified_extension_actions) g
		ON g.plugin_id=i.id AND g.archive_sha256=i.digest ORDER BY i.plugin_key, g.action`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []service.ExtensionInventoryItem{}
	for rows.Next() {
		var item service.ExtensionInventoryItem
		var raw []byte
		var account, rollout sql.NullInt64
		var action sql.NullString
		if err := rows.Scan(&item.ID, &item.SDKVersion, &item.PublisherID, &raw, &item.State, &item.Digest, &account, &action, &rollout); err != nil {
			return nil, err
		}
		if len(items) == 0 || items[len(items)-1].ID != item.ID {
			if err := json.Unmarshal(raw, &item.Capabilities); err != nil {
				return nil, err
			}
			item.AuthorizedActions = []string{}
			items = append(items, item)
		}
		selected := &items[len(items)-1]
		if action.Valid && grants.InRollout(account.Int64, int(rollout.Int64)) && !slices.Contains(selected.AuthorizedActions, action.String) {
			selected.AuthorizedActions = append(selected.AuthorizedActions, action.String)
		}
	}
	return items, rows.Err()
}
