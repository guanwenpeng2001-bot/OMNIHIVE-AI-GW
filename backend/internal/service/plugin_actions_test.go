package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	grants "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/extensions/actions"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2/actions"
	"github.com/stretchr/testify/require"
)

type actionTestRepo struct {
	extensionRepo
	target          grants.Target
	grant           grants.Grant
	reads           int
	revokeAfterRead bool
	inventory       ExtensionInventoryItem
}

func (r *actionTestRepo) ExtensionInventory(context.Context) ([]ExtensionInventoryItem, error) {
	item := r.inventory
	item.AuthorizedActions = slices.Clone(item.AuthorizedActions)
	return []ExtensionInventoryItem{item}, nil
}

func (r *actionTestRepo) LookupExtensionAction(_ context.Context, target grants.Target) (grants.Grant, error) {
	r.reads++
	if target != r.target || (r.revokeAfterRead && r.reads > 1) {
		return grants.Grant{}, sql.ErrNoRows
	}
	return r.grant, nil
}

type actionHTTPFixture struct {
	HTTPUpstream
	send func(*http.Request, string, int64, int) (*http.Response, error)
}

func (u actionHTTPFixture) Do(r *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	return u.send(r, proxy, id, concurrency)
}

// Use the real signer, WASI verifier, activation and host transport policy.
func TestExtensionScopedActions(t *testing.T) {
	modulePath := filepath.Join(t.TempDir(), "actions.wasm")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-ldflags", "-X main.scopes=actions", "-o", modulePath, "../../pkg/pluginapi/v2/testdata/runtime")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	data, err := cmd.CombinedOutput()
	require.NoError(t, err, string(data))
	data, err = os.ReadFile(modulePath)
	require.NoError(t, err)
	dir := t.TempDir()
	cfg := testPluginConfig(dir, false)
	cfg.Plugins.StartTimeoutSeconds = 15
	repo := &actionTestRepo{}
	manager := NewPluginManager(repo, pluginTokenEncryptor{}, cfg, PluginHostInfo{Version: "0.1.179"})
	t.Cleanup(manager.Stop)
	key := extensionKeys(t)
	publishers := map[string]ed25519.PrivateKey{"new": key}
	writePolicy(t, dir, publishers, actions.ProviderResponses, actions.TaskImageGenerate)
	m, files := extensionFixture(data)
	m.UI = PluginUIManifest{}
	delete(m.Files, "ui/index.html")
	delete(files, "ui/index.html")
	m.Capabilities = nil
	for _, label := range []string{actions.ProviderResponses, actions.TaskImageGenerate} {
		m.Capabilities = append(m.Capabilities, PluginCapability{ID: label, Platform: PlatformOpenAI, AccountType: AccountTypeOAuth})
	}
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	p, err := manager.installer.Install(t.Context(), bytes.NewReader(exactSignedArchive(t, raw, files, "new", key, raw)), nil)
	require.NoError(t, err)
	p.ID = 98
	for i, c := range m.Capabilities {
		p.Bindings = append(p.Bindings, PluginBinding{ID: int64(i + 1), Capability: c.ID, Platform: c.Platform, AccountType: c.AccountType})
	}
	repo.p = p
	_, err = manager.Enable(t.Context(), p.ID, false, 100)
	require.NoError(t, err)
	rt := manager.auxiliary[p.ID]
	acc := &Account{ID: 12, WorkspaceID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{CredentialSecretReferenceKey: "fixture-ref", CredentialSecretVersionKey: int64(4)}}
	require.False(t, manager.ShouldRouteOpenAIOAuth(acc))
	require.Error(t, manager.AuthorizeExtension(t.Context(), p.ID, actions.ProviderResponses))
	frozen := scope.Authorized{WorkspaceID: 8, ProjectID: 21, ActorID: 13, KeyID: 39, KeyOwnerID: 13, FundingUserID: 13, Version: 2}
	meter := &tpmBoundaryRepository{}
	plan := boundaryTPMPlan(meter, "r1")
	plan.spec.WorkspaceID, plan.spec.APIKeyID = frozen.WorkspaceID, frozen.KeyID
	plan.spec.ExecutionKind, plan.spec.Protocol, plan.spec.Model = "request", "responses", "private-model"
	plan.execution = journal.AdmittedExecution{RequestID: "r1", Scope: frozen}
	plan.budgetAdmitted = true
	ctx := WithGatewayRequestPlan(scope.With(t.Context(), frozen), plan)
	accessErr := error(nil)
	ctx = journal.WithReauthorization(ctx, func(context.Context, scope.Authorized, string) error { return accessErr })
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.example.invalid/v1/responses", strings.NewReader("private-content"))
	require.NoError(t, err)
	req.Header.Set("Authorization", "private-auth")
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	repo.grant = grants.Grant{ID: 1, BindingID: 1, PluginID: p.ID, Version: p.Version, ArchiveDigest: rt.extension.archiveDigest, Rollout: 100}
	repo.target = grants.Target{Workspace: 8, Project: 21, Account: 12, CredentialVersion: 4, CredentialReference: "fixture-ref", Model: "private-model", Capability: actions.ProviderResponses}
	check := func(want actions.Transfer, selected bool) {
		t.Helper()
		transfer, ok := manager.actionTransfer(req, acc)
		require.Equal(t, want, transfer)
		require.Equal(t, selected, ok)
	}
	check(actions.Transfer_IDENTITY, true)
	for _, field := range []string{`"actor_id":13`, `"workspace_id":8`, `"project_id":21`, `"action":"provider.responses.v1"`, `"outcome":"applied"`} {
		require.Contains(t, log.String(), field)
	}
	accessErr = errors.New("private-authorization-message")
	check(0, false)
	accessErr = nil
	repo.target.Project++
	check(0, false)
	repo.target.Project--
	repo.target.Account++
	check(0, false)
	repo.target.Account--
	repo.target.CredentialVersion++
	check(0, false)
	repo.target.CredentialVersion--
	plan.budgetAdmitted = false
	check(0, false)
	plan.budgetAdmitted = true
	repo.grant.Version = "0.0.9"
	check(0, false)
	repo.grant.Version = p.Version
	repo.grant.ArchiveDigest = strings.Repeat("b", 64)
	check(0, false)
	repo.grant.ArchiveDigest = rt.extension.archiveDigest
	repo.grant.BindingID++
	check(actions.Transfer_HOST_DEFAULT, true)
	repo.grant.BindingID--
	repo.reads = 0
	repo.revokeAfterRead = true
	check(actions.Transfer_HOST_DEFAULT, true)
	repo.revokeAfterRead = false
	repo.inventory = ExtensionInventoryItem{ID: m.ID, Capabilities: m.Capabilities, Digest: repo.grant.ArchiveDigest, AuthorizedActions: []string{actions.ProviderResponses}}
	getLabels := func() []string {
		items, err := manager.ExtensionInventory(ctx)
		require.NoError(t, err)
		return items[0].AuthorizedActions
	}
	require.Equal(t, []string{actions.ProviderResponses}, getLabels())
	writePolicy(t, dir, publishers, actions.ProviderResponses)
	require.Empty(t, getLabels())
	check(actions.Transfer_HOST_DEFAULT, true)
	writePolicy(t, dir, publishers, actions.ProviderResponses, actions.TaskImageGenerate)
	for _, mode := range []string{"trap", "timeout"} {
		config := []byte(`{"mode":"` + mode + `"}`)
		rt.extension.config.Store(&config)
		calls := 0
		response, err := manager.forwardAction(req, acc, func(out *http.Request) (*http.Response, error) {
			calls++
			require.Empty(t, out.Header.Get("Accept-Encoding"))
			return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
		})
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, 1, calls)
		require.NoError(t, ctx.Err())
	}
	rt.extension.config.Store(nil)
	plan.spec.ExecutionKind, plan.spec.Protocol = "job", "images"
	req.URL.Path = "/v1/images/generations"
	check(0, false) // A provider approval cannot approve the image task.
	repo.target.Capability = actions.TaskImageGenerate
	repo.grant.BindingID = 2
	check(actions.Transfer_IDENTITY, true)
	bounded, stop := WithAttemptBudget(ctx, 1, time.Minute)
	defer stop()
	request, err := http.NewRequestWithContext(bounded, http.MethodPost, "https://api.example.invalid/v1/responses", strings.NewReader(`{"model":"private-model","input":"private-content"}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "private-auth")
	acc.Extra = catalogEvidenceAccount(acc.ID, acc.Platform, "private-model").Extra
	acc.Status, acc.Schedulable = StatusActive, true
	sends := 0
	host := &OpenAIGatewayService{cfg: cfg, pluginManager: manager, httpUpstream: actionHTTPFixture{send: func(r *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
		sends++
		require.Equal(t, "https://host-proxy.invalid", proxy)
		require.Equal(t, acc.ID, id)
		require.Equal(t, acc.Concurrency, concurrency)
		require.Equal(t, "identity", r.Header.Get("Accept-Encoding"))
		require.True(t, HTTPUpstreamRedirectsDisabled(r.Context()))
		require.Equal(t, "private-auth", r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("metered host response"))}, nil
	}}}
	resp, err := host.doOpenAIUpstream(request, "https://host-proxy.invalid", acc)
	require.NoError(t, err)
	require.Len(t, meter.commands, 1)
	require.Equal(t, acc.ID, meter.commands[0].AccountID)
	require.Equal(t, "r1", meter.commands[0].ExecutionID)
	require.Equal(t, 1, sends)
	require.Empty(t, request.Header.Get("Accept-Encoding"))
	require.True(t, plan.Record())
	require.False(t, plan.Record())
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "metered host response", string(body))
	require.NoError(t, resp.Body.Close())
	for _, secret := range []string{"private-model", "private-content", "private-auth", "fixture-ref", "private-authorization-message"} {
		require.NotContains(t, log.String(), secret)
	}
}
