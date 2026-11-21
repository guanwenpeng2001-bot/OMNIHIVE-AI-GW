package service

import (
	"context"
	"slices"
)

// ExtensionInventoryItem is the operator-facing package identity. The digest is
// absent for pre-archive installations until an operator reimports the original.
type ExtensionInventoryItem struct {
	ID                string             `json:"id"`
	SDKVersion        int                `json:"sdk_version"`
	PublisherID       string             `json:"publisher_id,omitempty"`
	AuthorizedActions []string           `json:"authorized_actions"`
	Capabilities      []PluginCapability `json:"capabilities"`
	State             string             `json:"state"`
	Digest            string             `json:"digest,omitempty"`
}

func (m *PluginManager) ExtensionInventory(ctx context.Context) ([]ExtensionInventoryItem, error) {
	items, err := m.repo.ExtensionInventory(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		item := &items[i]
		item.AuthorizedActions = slices.DeleteFunc(item.AuthorizedActions, func(cap string) bool {
			for _, decl := range item.Capabilities {
				if decl.ID != cap {
					continue
				}
				r := m.actionRuntime(cap, decl.Platform, decl.AccountType)
				if r != nil && r.extension.manifest.ID == item.ID && r.extension.archiveDigest == item.Digest && r.extension.trusted() == nil {
					return false
				}
			}
			return true
		})
	}
	return items, nil
}
