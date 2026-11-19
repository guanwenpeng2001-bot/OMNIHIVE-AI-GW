package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	v2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2"
	"github.com/stretchr/testify/require"
)

func extensionKeys(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return key
}

const testExtensionID = "com.example.openai-transport"

func writePolicy(t *testing.T, dir string, keys map[string]ed25519.PrivateKey, grants ...string) {
	t.Helper()
	p := ExtensionTrust{Publishers: map[string]ExtensionPublisher{}}
	for id, key := range keys {
		root := ExtensionPublisher{PublicKey: base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)), Grants: map[string][]string{testExtensionID: grants}}
		root.Predecessors = map[string]struct {
			PluginID string `json:"plugin_id"`
			KeyID    string `json:"key_id"`
		}{testExtensionID: {testExtensionID, "legacy"}}
		p.Publishers[id] = root
	}
	b, e := json.Marshal(p)
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "trust-v2.json"), b, 0600))
}
func extensionFixture(module []byte) (PluginManifest, map[string][]byte) {
	files := map[string][]byte{"runtime.wasm": module, "ui/index.html": []byte("ui")}
	m := testPluginManifest(files)
	m.SchemaVersion = 2
	m.ProductID = v2.ProductID
	m.PublisherID = "new"
	m.Requires = PluginRequirements{OmniHive: ">=0.1.0", PluginProtocol: 2, TransportAPI: 1, UIBridge: 1, TestedOmniHiveVersions: []string{"0.1.179"}}
	m.Runtimes = map[string]PluginRuntime{v2.RuntimeKey: {Path: "runtime.wasm"}}
	m.Capabilities = []PluginCapability{{ID: v2.Provider, Platform: PlatformOpenAI, AccountType: AccountTypeOAuth}, {ID: v2.UI}}
	return m, files
}
func exactSignedArchive(t *testing.T, raw []byte, payload map[string][]byte, keyID string, key ed25519.PrivateKey, signed []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	writeZipEntry(t, z, pluginManifestFilename, raw)
	if key != nil {
		sig, e := json.Marshal(PluginSignature{Algorithm: "ed25519", KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, signed))})
		require.NoError(t, e)
		writeZipEntry(t, z, pluginSignatureFilename, sig)
	}
	for p, b := range payload {
		writeZipEntry(t, z, p, b)
	}
	require.NoError(t, z.Close())
	return buf.Bytes()
}
func TestExtensionPackageTrust(t *testing.T) {
	key, next := extensionKeys(t), extensionKeys(t)
	cfg := testPluginConfig(t.TempDir(), true)
	i := NewPluginPackageInstaller(cfg, PluginHostInfo{Version: "0.1.179"})
	m, files := extensionFixture([]byte("module"))
	install := func(m PluginManifest, k ed25519.PrivateKey) (*PluginInstallation, error) {
		raw, e := json.Marshal(m)
		require.NoError(t, e)
		return i.Install(t.Context(), bytes.NewReader(exactSignedArchive(t, raw, files, m.PublisherID, k, raw)), nil)
	}
	_, e := install(m, key)
	require.ErrorContains(t, e, "trust-v2.json")
	writePolicy(t, i.rootDir, map[string]ed25519.PrivateKey{"new": key}, v2.Provider, v2.UI)
	for _, tc := range []struct {
		name   string
		change func(*PluginManifest)
	}{
		{"wrong product", func(m *PluginManifest) { m.ProductID = "foreign" }},
		{"schema", func(m *PluginManifest) { m.SchemaVersion = 3 }},
		{"protocol", func(m *PluginManifest) { m.Requires.PluginProtocol++ }},
		{"capability", func(m *PluginManifest) { m.Capabilities = append(m.Capabilities, PluginCapability{ID: "wallet"}) }},
		{"provider scope", func(m *PluginManifest) { m.Capabilities = []PluginCapability{{ID: v2.Provider, Platform: "unknown"}} }},
		{"ungranted", func(m *PluginManifest) { m.Capabilities = append(m.Capabilities, PluginCapability{ID: v2.Task}) }},
	} {
		t.Run(tc.name, func(t *testing.T) { bad := m; tc.change(&bad); _, e := install(bad, key); require.Error(t, e) })
	}
	_, e = install(m, next)
	require.ErrorContains(t, e, "signature")
	_, e = install(m, nil)
	require.ErrorContains(t, e, "signature.json required")
	p, e := install(m, key)
	require.NoError(t, e)
	require.Equal(t, PluginStateDisabled, p.State)
	require.Equal(t, v2.Extension, filepath.Ext(p.ArtifactPath))
	writePolicy(t, i.rootDir, map[string]ed25519.PrivateKey{"new": key, "next": next}, v2.Provider, v2.UI)
	m.PublisherID = "next"
	_, e = install(m, next)
	require.NoError(t, e)
	writePolicy(t, i.rootDir, map[string]ed25519.PrivateKey{"next": next}, v2.Provider, v2.UI)
	m.PublisherID = "new"
	_, e = install(m, key)
	require.ErrorContains(t, e, "publisher")
}
func TestExtensionRebuildUsesVerifiedOriginalBytes(t *testing.T) {
	prior, next := extensionKeys(t), extensionKeys(t)
	cfg := testPluginConfig(t.TempDir(), false)
	i := NewPluginPackageInstaller(cfg, PluginHostInfo{Version: "0.1.179"})
	writePolicy(t, i.rootDir, map[string]ed25519.PrivateKey{"next": next}, v2.Provider, v2.UI)
	files := map[string][]byte{"bin/plugin": []byte("binary"), "ui/index.html": []byte("ui")}
	m := testPluginManifest(files)
	raw, e := json.MarshalIndent(m, "", "    ")
	require.NoError(t, e)
	raw = append(raw, '\n')
	canonical, e := json.Marshal(m)
	require.NoError(t, e)
	archive := exactSignedArchive(t, raw, files, "legacy", prior, raw)
	var result bytes.Buffer
	rebuild := func(source []byte) error {
		result.Reset()
		return i.RebuildV1(t.Context(), bytes.NewReader(source), bytes.NewReader([]byte{0, 'a', 's', 'm', 1, 0, 0, 0}), &result, testExtensionID, "next", next)
	}
	require.ErrorContains(t, rebuild(archive), "不受信任")
	require.Empty(t, result.Bytes())
	cfg.Plugins.TrustedPublishers["legacy"] = base64.StdEncoding.EncodeToString(prior.Public().(ed25519.PublicKey))
	require.ErrorContains(t, rebuild(exactSignedArchive(t, raw, files, "legacy", prior, canonical)), "签名校验")
	require.Empty(t, result.Bytes())
	require.NoError(t, rebuild(archive))
	p, e := i.Install(t.Context(), bytes.NewReader(result.Bytes()), nil)
	require.NoError(t, e)
	require.Equal(t, &v2.Origin{PluginID: m.ID, KeyID: "legacy", ArchiveSHA256: pluginSHA(archive), ManifestSHA256: pluginSHA(raw)}, p.Manifest.Origin)
	delete(cfg.Plugins.TrustedPublishers, "legacy")
	_, err := i.Install(t.Context(), bytes.NewReader(result.Bytes()), nil)
	require.ErrorContains(t, err, "original v1 signature")
	cfg.Plugins.TrustedPublishers["legacy"] = base64.StdEncoding.EncodeToString(prior.Public().(ed25519.PublicKey))
	require.Equal(t, PluginStateDisabled, p.State)
	require.Equal(t, m.Requires.Sub2API, p.Manifest.Requires.OmniHive)
	require.Equal(t, "next", p.Manifest.PublisherID)
	files["ui/index.html"] = []byte("wrong")
	require.ErrorContains(t, rebuild(exactSignedArchive(t, raw, files, "legacy", prior, raw)), "哈希")
	require.Empty(t, result.Bytes())
}

type extensionRepo struct {
	PluginRepository
	mu sync.Mutex
	p  *PluginInstallation
}

func (r *extensionRepo) GetArtifact(context.Context, int64) ([]byte, error) {
	return r.p.ArtifactData, nil
}
func (r *extensionRepo) GetByID(context.Context, int64) (*PluginInstallation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := *r.p
	p.Bindings = slices.Clone(p.Bindings)
	return &p, nil
}
func (r *extensionRepo) BeginEnable(context.Context, int64, string, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.p.State = PluginStateStarting
	return nil
}
func (r *extensionRepo) UpdateState(_ context.Context, _ int64, state, reason string, _ *time.Time, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.p.State = state
	r.p.LastError = reason
	return nil
}
func (r *extensionRepo) UpdateBindingsAndState(_ context.Context, _ int64, b []PluginBinding, state, _ string, _ *time.Time, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.p.State = state
	r.p.Bindings = slices.Clone(b)
	return nil
}

func TestExtensionWASILifecycle(t *testing.T) {
	modulePath := filepath.Join(t.TempDir(), "fixture.wasm")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", modulePath, "../../pkg/pluginapi/v2/testdata/runtime")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	log, e := cmd.CombinedOutput()
	require.NoError(t, e, string(log))
	module, e := os.ReadFile(modulePath)
	require.NoError(t, e)
	cfg := testPluginConfig(t.TempDir(), false)
	cfg.Plugins.StartTimeoutSeconds = 15
	repo := &extensionRepo{}
	manager := NewPluginManager(repo, pluginTokenEncryptor{}, cfg, PluginHostInfo{Version: "0.1.179"})
	key := extensionKeys(t)
	writePolicy(t, manager.installer.rootDir, map[string]ed25519.PrivateKey{"new": key}, v2.Provider, v2.UI)
	m, files := extensionFixture(module)
	raw, e := json.Marshal(m)
	require.NoError(t, e)
	p, e := manager.installer.Install(t.Context(), bytes.NewReader(exactSignedArchive(t, raw, files, "new", key, raw)), nil)
	require.NoError(t, e)
	p.ID = 17
	for _, c := range m.Capabilities {
		p.Bindings = append(p.Bindings, PluginBinding{Capability: c.ID, Platform: c.Platform, AccountType: c.AccountType})
	}
	repo.p = p
	_, _, e = manager.CreateUIAssetToken(t.Context(), p.ID, time.Minute)
	require.ErrorContains(t, e, "disabled")
	_, e = manager.Enable(t.Context(), p.ID, false, 100)
	require.NoError(t, e)
	t.Cleanup(manager.Stop)
	_, _, e = manager.CreateUIAssetToken(t.Context(), p.ID, time.Minute)
	require.NoError(t, e)
	b, _, e := manager.ReadUIAsset(t.Context(), p.ID, "index.html")
	require.NoError(t, e)
	require.Equal(t, []byte("ui"), b)
	req, e := http.NewRequestWithContext(t.Context(), "GET", "https://example.invalid", nil)
	require.NoError(t, e)
	_, handled, e := manager.RoundTripOpenAIOAuth(t.Context(), req, "", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.NoError(t, e)
	require.False(t, handled)
	require.ErrorContains(t, manager.AuthorizeExtension(t.Context(), p.ID, v2.Task), "binding required")
	r := manager.runtimes[p.ID]
	cfgMode := func(mode string) { data := []byte(`{"mode":"` + mode + `"}`); r.extension.config.Store(&data) }
	cfgMode("trap")
	require.ErrorContains(t, manager.AuthorizeExtension(t.Context(), p.ID, v2.Provider), "trapped")
	cfgMode("memory")
	require.Error(t, manager.AuthorizeExtension(t.Context(), p.ID, v2.Provider))
	cfgMode("output")
	require.ErrorContains(t, manager.AuthorizeExtension(t.Context(), p.ID, v2.Provider), "output")
	cfgMode("timeout")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	require.ErrorIs(t, manager.AuthorizeExtension(ctx, p.ID, v2.Provider), context.DeadlineExceeded)
	cancel()
	for j := 0; j < cap(extensionSlots); j++ {
		extensionSlots <- struct{}{}
	}
	err := manager.AuthorizeExtension(t.Context(), p.ID, v2.Provider)
	for j := 0; j < cap(extensionSlots); j++ {
		<-extensionSlots
	}
	require.ErrorContains(t, err, "concurrent invocation limit")
	writePolicy(t, manager.installer.rootDir, map[string]ed25519.PrivateKey{}, v2.Provider, v2.UI)
	require.ErrorContains(t, manager.AuthorizeExtension(t.Context(), p.ID, v2.Provider), "publisher")
	writePolicy(t, manager.installer.rootDir, map[string]ed25519.PrivateKey{"new": key}, v2.Provider, v2.UI)
	done := make(chan error, 1)
	go func() { done <- manager.AuthorizeExtension(t.Context(), p.ID, v2.Provider) }()
	require.Eventually(t, func() bool { return r.inFlight.Load() == 1 }, time.Second, time.Millisecond)
	disabled := make(chan error, 1)
	go func() { _, e := manager.Disable(t.Context(), p.ID); disabled <- e }()
	require.Eventually(t, func() bool { return r.draining.Load() }, time.Second, time.Millisecond)
	require.Error(t, manager.AuthorizeExtension(t.Context(), p.ID, v2.Provider))
	select {
	case err := <-done:
		require.True(t, errors.Is(err, context.DeadlineExceeded) || r.exited())
	case <-time.After(7 * time.Second):
		t.Fatal("disable did not bound active guest")
	}
	require.NoError(t, <-disabled)
	require.True(t, r.exited())
}

func TestExtensionTaskOnlyHasNoProviderOrUIGrant(t *testing.T) {
	modulePath := filepath.Join(t.TempDir(), "task.wasm")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-ldflags", "-X main.scopes=task", "-o", modulePath, "../../pkg/pluginapi/v2/testdata/runtime")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	log, err := cmd.CombinedOutput()
	require.NoError(t, err, string(log))
	module, err := os.ReadFile(modulePath)
	require.NoError(t, err)
	cfg := testPluginConfig(t.TempDir(), false)
	cfg.Plugins.StartTimeoutSeconds = 15
	repo := &extensionRepo{}
	manager := NewPluginManager(repo, pluginTokenEncryptor{}, cfg, PluginHostInfo{Version: "0.1.179"})
	t.Cleanup(manager.Stop)
	key := extensionKeys(t)
	writePolicy(t, manager.installer.rootDir, map[string]ed25519.PrivateKey{"new": key}, v2.Task)
	m, files := extensionFixture(module)
	m.Capabilities = []PluginCapability{{ID: v2.Task}}
	m.UI = PluginUIManifest{}
	delete(m.Files, "ui/index.html")
	delete(files, "ui/index.html")
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	p, err := manager.installer.Install(t.Context(), bytes.NewReader(exactSignedArchive(t, raw, files, "new", key, raw)), nil)
	require.NoError(t, err)
	p.ID = 9
	p.Bindings = []PluginBinding{{Capability: v2.Task}}
	repo.p = p
	_, err = manager.Enable(t.Context(), p.ID, false, 100)
	require.NoError(t, err)
	require.False(t, manager.ShouldRouteOpenAIOAuth(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.NoError(t, manager.AuthorizeExtension(t.Context(), p.ID, v2.Task))
	require.Error(t, manager.AuthorizeExtension(t.Context(), p.ID, v2.Provider))
	_, _, err = manager.CreateUIAssetToken(t.Context(), p.ID, time.Minute)
	require.ErrorContains(t, err, "binding required")
	_, err = manager.Disable(t.Context(), p.ID)
	require.NoError(t, err)
	require.Error(t, manager.AuthorizeExtension(t.Context(), p.ID, v2.Task))
}
