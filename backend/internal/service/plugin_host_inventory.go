package service

import "context"

// ExtensionInventoryItem is the operator-facing package identity. The digest is
// absent for pre-archive installations until an operator reimports the original.
type ExtensionInventoryItem struct {
	ID           string             `json:"id"`
	SDKVersion   int                `json:"sdk_version"`
	PublisherID  string             `json:"publisher_id,omitempty"`
	Capabilities []PluginCapability `json:"capabilities"`
	State        string             `json:"state"`
	Digest       string             `json:"digest,omitempty"`
}

func (m *PluginManager) ExtensionInventory(ctx context.Context) ([]ExtensionInventoryItem, error) {
	return m.repo.ExtensionInventory(ctx)
}
