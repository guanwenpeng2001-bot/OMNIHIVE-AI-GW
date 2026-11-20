//go:build integration

package repository

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	v2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2"
	"github.com/stretchr/testify/require"
)

func TestExtensionInventoryAndProviderSingleton(t *testing.T) {
	ctx := t.Context()
	r := &pluginRepository{db: integrationDB}
	first := fmt.Sprintf("example.extension-%d", time.Now().UnixNano())
	install := func(key string, sdk int) *service.PluginInstallation {
		p := &service.PluginInstallation{PluginKey: key, Name: key, Version: "1.0.0", Manifest: service.PluginManifest{ID: key, SchemaVersion: sdk, PublisherID: "test"}, BinarySHA256: strings.Repeat("e", 64), ArtifactData: []byte(key), State: service.PluginStateDisabled, SignatureStatus: service.PluginSignatureTrusted}
		if sdk == 2 {
			p.Manifest.Origin = &v2.Origin{PluginID: first, KeyID: "v1", ManifestSHA256: strings.Repeat("a", 64), ArchiveSHA256: strings.Repeat("b", 64)}
			p.Manifest.Capabilities = []service.PluginCapability{{ID: v2.UI}}
		}
		x, e := r.Install(ctx, p, nil)
		require.NoError(t, e)
		t.Cleanup(func() { _, _ = integrationDB.Exec("DELETE FROM sub2api_plugin_installations WHERE id=$1", x.ID) })
		return x
	}
	a, b := install(first, 1), install(first+"-v2", 2)
	items, e := r.ExtensionInventory(ctx)
	require.NoError(t, e)
	found := false
	for _, item := range items {
		if item.ID == b.PluginKey {
			found = true
			require.Equal(t, 2, item.SDKVersion)
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(b.PluginKey))), item.Digest)
			require.Equal(t, "test", item.PublisherID)
		}
	}
	require.True(t, found)
	bind := func(id int64, c string) error {
		_, e := integrationDB.ExecContext(ctx, `INSERT INTO sub2api_plugin_bindings(plugin_id,capability,platform,account_type,enabled) VALUES($1,$2,'openai','oauth',true)`, id, c)
		return e
	}
	require.NoError(t, bind(a.ID, service.PluginCapabilityOpenAIOAuthOutbound))
	require.Error(t, bind(b.ID, "provider"))
	require.NoError(t, bind(a.ID, "ui"))
	require.NoError(t, bind(b.ID, "ui"))
	_, e = integrationDB.ExecContext(ctx, "DELETE FROM sub2api_plugin_installations WHERE id=$1", b.ID)
	require.NoError(t, e)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(b.PluginKey)))
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM omnihive_extension_migrations WHERE target_digest=$1", digest)
	})
	var previous string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT source_key_id FROM omnihive_extension_migrations WHERE target_digest=$1", digest).Scan(&previous))
	require.Equal(t, "v1", previous)
}
