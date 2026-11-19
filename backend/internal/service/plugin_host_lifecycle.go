package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	v2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2"
)

func isProviderPlugin(p *PluginInstallation) bool {
	if p.Manifest.SchemaVersion != 2 {
		return true
	}
	for _, c := range p.Manifest.Capabilities {
		if c.ID == v2.Provider {
			return true
		}
	}
	return false
}

// m.mu protects both registries. Only provider plugins participate in OAuth routing.
func (m *PluginManager) runtimeLocked(id int64) *pluginRuntime {
	if r := m.runtimes[id]; r != nil {
		return r
	}
	return m.auxiliary[id]
}

func (m *PluginManager) reconcileAuxiliary(ctx context.Context, inventory []*PluginInstallation) {
	desired := map[int64]*PluginInstallation{}
	for _, p := range inventory {
		if p.State == PluginStateEnabled && !isProviderPlugin(p) {
			desired[p.ID] = p
		}
	}
	m.mu.Lock()
	var stale []*pluginRuntime
	for id, r := range m.auxiliary {
		p := desired[id]
		if p != nil && samePluginPackage(r.installation, p) && r.installation.ConfigEncrypted == p.ConfigEncrypted {
			delete(desired, id)
			continue
		}
		r.draining.Store(true)
		stale = append(stale, r)
		delete(m.auxiliary, id)
	}
	m.mu.Unlock()
	for _, r := range stale {
		r.drain(10 * time.Second)
	}
	for id, p := range desired {
		r, err := m.prepareRuntime(ctx, p, true)
		if err != nil {
			slog.Warn("plugin_reconcile_failed", "plugin_id", id, "error", err)
			continue
		}
		m.mu.Lock()
		m.auxiliary[id] = r
		m.mu.Unlock()
	}
}

// AuthorizeExtension gates a host-selected operation. Credentials, destinations,
// quantities and charging never come from a plugin response.
func (m *PluginManager) AuthorizeExtension(ctx context.Context, id int64, cap string) error {
	m.mu.Lock()
	r := m.runtimeLocked(id)
	m.mu.Unlock()
	if r == nil || r.extension == nil {
		return extensionDenied("unavailable", cap, "v2 runtime is not enabled")
	}
	return m.authorizeRuntime(ctx, r, cap)
}

func (m *PluginManager) authorizeRuntime(ctx context.Context, r *pluginRuntime, cap string) error {
	p, err := m.repo.GetByID(ctx, r.installation.ID)
	if err != nil {
		return extensionDenied(r.installation.PluginKey, cap, "could not read authoritative grant")
	}
	if p.State != PluginStateEnabled || !samePluginPackage(r.installation, p) {
		return extensionDenied(p.PluginKey, cap, "extension disabled or package replaced")
	}
	if err = extensionGrant(p, cap); err != nil {
		return err
	}
	return r.authorize(ctx, cap)
}

func (m *PluginManager) checkUIGrant(ctx context.Context, p *PluginInstallation) error {
	if p.Manifest.SchemaVersion != 2 {
		return nil
	}
	if p.State != PluginStateEnabled {
		return extensionDenied(p.PluginKey, v2.UI, "extension disabled")
	}
	if err := extensionGrant(p, v2.UI); err != nil {
		return err
	}
	m.mu.Lock()
	r := m.runtimeLocked(p.ID)
	m.mu.Unlock()
	if r == nil || r.extension == nil || r.draining.Load() || !samePluginPackage(r.installation, p) {
		return errors.New("verified UI runtime unavailable")
	}
	return r.extension.trusted()
}
