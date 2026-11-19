package repository

import (
	"context"
	"encoding/json"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func (r *pluginRepository) ExtensionInventory(ctx context.Context) ([]service.ExtensionInventoryItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT plugin_key, sdk_version, COALESCE(publisher_id,''), capabilities, state, COALESCE(digest,'') FROM omnihive_extension_inventory ORDER BY plugin_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []service.ExtensionInventoryItem{}
	for rows.Next() {
		var item service.ExtensionInventoryItem
		var raw []byte
		if err := rows.Scan(&item.ID, &item.SDKVersion, &item.PublisherID, &raw, &item.State, &item.Digest); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Capabilities); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
