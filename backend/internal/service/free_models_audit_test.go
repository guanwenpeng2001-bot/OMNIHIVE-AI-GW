//go:build unit

package service

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/antigravity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestW05OpenCodePaidPrivateCredential(t *testing.T) {
	for _, mode := range []string{AccountModeZen, AccountModeGo} {
		for _, key := range []string{"", "public", "private-key"} {
			a := freeAccount(PlatformOpenCodeGo, mode)
			a.Credentials["api_key"] = key
			err := validateExpandedPlatformAccount(a)
			if key == "private-key" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			old := freeAccount(PlatformOpenCodeGo, AccountModeZenFree)
			old.Credentials["api_key"] = "public"
			in := &BulkUpdateAccountsInput{Credentials: map[string]any{"account_mode": mode}}
			require.Error(t, normalizeFreeModelBulkCredentials([]*Account{old}, in))
			in.Credentials["api_key"] = "replacement-key"
			require.NoError(t, normalizeFreeModelBulkCredentials([]*Account{old}, in))
		}
	}
}

func TestW05ConnectionPartialAndKiloPaidEvidence(t *testing.T) {
	for _, page := range []string{
		`{"data":[{"id":"one"}],"has_more":true,"last_id":"one"}`,
		`{"data":[{"id":"one"}],"nextPageToken":"two"}`,
		`{"data":[{"id":"one"}],"next_cursor":"two"}`,
		`{"data":[{"id":"one"}],"links":{"next":"?cursor=two"}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, page) }))
		a := freeAccount(PlatformOpenAI, "")
		a.Credentials["api_key"] = "private-key"
		req := httptest.NewRequest("GET", server.URL+"/models", nil)
		req.RequestURI = ""
		req.Header.Set("Authorization", "Bearer private-key")
		result := verifyConnectionRequest(req, server.Client(), a)
		require.Equal(t, "failed", result.Discovery)
		require.Empty(t, result.Models)
		require.Contains(t, result.Detail, "incomplete")
		server.Close()
	}
	var rows []string
	for i := 0; i < 1025; i++ {
		rows = append(rows, fmt.Sprintf(`{"id":"m-%d"}`, i))
	}
	require.True(t, ConnectionCatalogIncomplete([]byte(`{}`), len(rows)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"data":[{"id":"paid/model"}]}`) }))
	defer server.Close()
	a := freeAccount(PlatformKilo, KiloAccountModeAPIKey)
	a.Credentials["api_key"] = "invalid-private-key"
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("Authorization", "Bearer invalid-private-key")
	result := verifyConnectionRequest(req, server.Client(), a)
	require.Equal(t, "passed", result.Discovery)
	require.Equal(t, "unknown", result.Authorization)
}

func TestW05AuthorizationControlDiagnostics(t *testing.T) {
	primary := ConnectionProbe{Network: "passed", Discovery: "passed", Authorization: "unknown", Models: []string{"one"}}
	for _, status := range []int{200, 401, 403, 429, 302, 503} {
		got := ApplyConnectionAuthorizationControl(primary, &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("secret"))}, nil)
		require.Equal(t, "passed", got.Network)
		require.Equal(t, "passed", got.Discovery)
		if status == 401 || status == 403 {
			require.Equal(t, "passed", got.Authorization)
		}
		if status == 302 || status == 503 || status == 429 {
			require.NotEmpty(t, got.AuthorizationReason)
			require.NotEmpty(t, got.Detail)
		}
		require.NotContains(t, got.Detail, "secret")
	}
	for _, err := range []error{x509.UnknownAuthorityError{}, context.DeadlineExceeded, errors.New("https://secret.invalid/token")} {
		got := ApplyConnectionAuthorizationControl(primary, nil, err)
		require.Equal(t, "passed", got.Network)
		require.Equal(t, "PROBE_FAILED", got.AuthorizationReason)
		require.NotContains(t, got.Detail, "secret.invalid")
	}
}

type w05CursorDriver struct {
	cursorDriverStub
	err   error
	token string
}

func (d *w05CursorDriver) AvailableModels(_ context.Context, c cursor.Credentials) ([]cursor.AvailableModel, error) {
	d.token = c.AccessToken
	return []cursor.AvailableModel{{ID: "native-live"}}, d.err
}
func TestW05CursorOAuthProbeUsesProposedToken(t *testing.T) {
	gateway, a, exchanges := cursorTestGateway()
	a.Type = AccountTypeOAuth
	a.Credentials = map[string]any{"access_token": "proposed-token"}
	driver := &w05CursorDriver{}
	gateway.native = driver
	s := &AccountTestService{cfg: &config.Config{}, cursorGatewayService: gateway}
	got := s.ProbeConnection(t.Context(), a, "")
	require.Equal(t, "passed", got.Authorization)
	require.Equal(t, []string{"native-live"}, got.Models)
	require.Equal(t, "proposed-token", driver.token)
	require.Zero(t, exchanges.Load())
	driver.err = &cursor.HTTPError{Status: 401}
	require.Equal(t, "failed", s.ProbeConnection(t.Context(), a, "").Authorization)
	driver.err = context.DeadlineExceeded
	require.Equal(t, "PROBE_FAILED", s.ProbeConnection(t.Context(), a, "").NetworkReason)
}

func TestW05AntigravityOAuthProbeUsesNativeRoute(t *testing.T) {
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1internal:fetchAvailableModels", r.URL.Path)
		require.Equal(t, "Bearer proposed-token", r.Header.Get("Authorization"))
		w.WriteHeader(status)
		io.WriteString(w, `{"models":{"native-live":{"displayName":"Live"}}}`)
	}))
	defer server.Close()
	previous := antigravity.BaseURLs
	antigravity.BaseURLs = []string{server.URL}
	defer func() { antigravity.BaseURLs = previous }()
	cfg := &config.Config{}
	cfg.Security.URLAllowlist = config.URLAllowlistConfig{Enabled: true, AllowInsecureHTTP: true, AllowPrivateHosts: true, UpstreamHosts: []string{"127.0.0.1"}}
	s := &AccountTestService{cfg: cfg}
	a := &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "proposed-token"}}
	got := s.ProbeConnection(t.Context(), a, "")
	require.Equal(t, "passed", got.Authorization)
	require.Equal(t, []string{"native-live"}, got.Models)
	status = 401
	require.Equal(t, "AUTH_REJECTED", s.ProbeConnection(t.Context(), a, "").AuthorizationReason)
	status = 403
	require.Equal(t, "AUTH_REJECTED", s.ProbeConnection(t.Context(), a, "").AuthorizationReason)
	status = 302
	require.Equal(t, "PROBE_FAILED", s.ProbeConnection(t.Context(), a, "").NetworkReason)
}

func TestW05OpenCodePaidSendGuard(t *testing.T) {
	for _, mode := range []string{AccountModeZen, AccountModeGo} {
		for _, key := range []string{"", "public", " public "} {
			for _, tls := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%q/tls=%t", mode, key, tls), func(t *testing.T) {
					a := freeAccount(PlatformOpenCodeGo, mode)
					a.Credentials["api_key"] = key
					base := &egressRecordingUpstream{}
					transport := accountHTTPUpstream(base, a)
					req := httptest.NewRequest(http.MethodPost, "https://upstream.invalid/chat/completions", strings.NewReader(`{"model":"paid-model"}`))
					var resp *http.Response
					var err error
					if tls {
						resp, err = transport.DoWithTLS(req, "", a.ID, 1, nil)
					} else {
						resp, err = transport.Do(req, "", a.ID, 1)
					}
					require.True(t, infraerrors.IsConflict(err))
					require.Nil(t, resp)
					require.Empty(t, base.urls, "invalid paid credentials must never reach the upstream")
					require.Equal(t, key, a.GetCredential("api_key"))
				})
			}
		}
		a := freeAccount(PlatformOpenCodeGo, mode)
		a.Credentials["api_key"] = "private-key"
		base := &egressRecordingUpstream{}
		require.Same(t, base, accountHTTPUpstream(base, a), "valid paid accounts retain their existing transport")
	}
}

func w05KiloNativeAccount(model string) (*Account, *AccountModelCatalogSnapshot) {
	a := freeAccount(PlatformKilo, KiloAccountModeFree)
	yes := true
	a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Source: "live", Models: map[string]UpstreamModelMetadata{model: {ID: model, Free: &yes}}})
	catalog := &AccountModelCatalogSnapshot{Complete: true, SourceKind: "live", DiscoveryScope: "account", DiscoveredModels: []string{model}, EligibleModels: []AccountCatalogModel{{ID: model, Endpoints: []string{"chat_completions"}}}}
	a.Extra[UpstreamModelCatalogExtraKey] = catalog
	return a, catalog
}

func TestW05KiloFreeRequiresCurrentNativeEvidence(t *testing.T) {
	const model = "vendor/new-native-model"
	for _, tc := range []struct {
		name   string
		mutate func(*Account, *AccountModelCatalogSnapshot)
	}{
		{"no complete catalog", func(a *Account, c *AccountModelCatalogSnapshot) { delete(a.Extra, UpstreamModelCatalogExtraKey) }},
		{"partial catalog", func(a *Account, c *AccountModelCatalogSnapshot) { c.Complete = false }},
		{"stale identity", func(a *Account, c *AccountModelCatalogSnapshot) {
			a.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
		}},
		{"configured source", func(a *Account, c *AccountModelCatalogSnapshot) { c.SourceKind = ModelEvidenceConfiguredSelection }},
		{"legacy source", func(a *Account, c *AccountModelCatalogSnapshot) { c.SourceKind = modelEvidenceLegacyMappingUpgrade }},
		{"missing source", func(a *Account, c *AccountModelCatalogSnapshot) { c.SourceKind = "" }},
		{"not discovered", func(a *Account, c *AccountModelCatalogSnapshot) { c.DiscoveredModels = []string{} }},
		{"missing metadata", func(a *Account, c *AccountModelCatalogSnapshot) { delete(a.Extra, UpstreamModelMetadataExtraKey) }},
		{"missing free fact", func(a *Account, c *AccountModelCatalogSnapshot) {
			a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{model: {ID: model}}})
		}},
		{"explicit paid fact", func(a *Account, c *AccountModelCatalogSnapshot) {
			no := false
			a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{model: {ID: model, Free: &no}}})
		}},
		{"credentials invalid", func(a *Account, c *AccountModelCatalogSnapshot) {
			a.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "credentials_invalid"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, c := w05KiloNativeAccount(model)
			tc.mutate(a, c)
			require.False(t, kiloFreeModelAllowed(a, model))
			require.False(t, zenFreeModelAllowed(a, model))
			_, _, err := prepareKiloChat(a, []byte(`{"model":"`+model+`"}`))
			require.ErrorIs(t, err, ErrAccountModelUnsupported)
		})
	}
	a, _ := w05KiloNativeAccount(model)
	require.True(t, kiloFreeModelAllowed(a, model))
	require.True(t, zenFreeModelAllowed(a, model))
	_, _, err := prepareKiloChat(a, []byte(`{"model":"`+model+`"}`))
	require.NoError(t, err)
	for _, price := range []string{`"0.1"`, `"1e-400"`, `"invalid"`, `{`} {
		t.Run("nonzero or invalid price "+price, func(t *testing.T) {
			a, _ := w05KiloNativeAccount(model)
			yes := true
			a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{model: {ID: model, Free: &yes, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(price)}}}})
			require.False(t, kiloFreeModelAllowed(a, model))
		})
	}
}

func TestW05KiloBootstrapCannotBeExpandedBySuffixOrMetadata(t *testing.T) {
	for _, model := range []string{"vendor/unknown:free", "vendor/unknown/free", "vendor/unknown"} {
		a := freeAccount(PlatformKilo, KiloAccountModeFree)
		require.False(t, kiloFreeModelAllowed(a, model))
		yes := true
		a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{model: {ID: model, Free: &yes}}})
		require.False(t, kiloFreeModelAllowed(a, model), "detached metadata must not grant membership")
	}
	for _, model := range kiloFreeModelIDs {
		require.True(t, kiloFreeModelAllowed(freeAccount(PlatformKilo, KiloAccountModeFree), model))
	}
	// Current discovery and identity revocation take priority even for a bootstrap ID.
	a, c := w05KiloNativeAccount("vendor/other")
	require.False(t, kiloFreeModelAllowed(a, kiloFreeModelIDs[0]))
	c.DiscoveredModels = append(c.DiscoveredModels, kiloFreeModelIDs[0])
	a.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 2}
	require.False(t, kiloFreeModelAllowed(a, kiloFreeModelIDs[0]))
	paid := freeAccount(PlatformKilo, KiloAccountModeAPIKey)
	require.True(t, kiloFreeModelAllowed(paid, "vendor/paid-model"))
}

func TestW05KiloConfiguredCatalogUsesSendPolicy(t *testing.T) {
	const allowed, denied = "vendor/live", "vendor/paid:free"
	a, c := w05KiloNativeAccount(allowed)
	no := false
	metadata := a.GetUpstreamModelMetadataSnapshot().Models
	metadata[denied] = UpstreamModelMetadata{ID: denied, Free: &no}
	a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: metadata})
	c.DiscoveredModels = append(c.DiscoveredModels, denied)
	c.EligibleModels = append(c.EligibleModels, AccountCatalogModel{ID: denied, Endpoints: []string{"chat_completions"}})
	a.Credentials["model_mapping"] = map[string]any{"public-live": allowed, "public-paid": denied, "public-unknown": "vendor/unknown:free"}
	a.Credentials["compact_model_mapping"] = map[string]any{"public-paid": denied}
	projected := a.EffectiveUpstreamModelCatalog()
	require.NotNil(t, projected)
	require.Len(t, projected.EligibleModels, 1)
	require.Equal(t, allowed, projected.EligibleModels[0].ID)
	require.False(t, ResolveAccountModelBinding(t.Context(), a, "public-paid", AccountModelBindingOptions{Endpoint: "chat_completions"}).Supported)
	require.False(t, ResolveAccountModelBinding(t.Context(), a, "public-unknown", AccountModelBindingOptions{Endpoint: "chat_completions"}).Supported)
	fresh := freeAccount(PlatformKilo, KiloAccountModeFree)
	fresh.Credentials["model_mapping"] = map[string]any{"public-paid": denied}
	require.Nil(t, fresh.EffectiveUpstreamModelCatalog())
	require.Nil(t, appendConfiguredModels(fresh, nil, map[string]string{"public-paid": denied}, false))
}

func TestW05KiloMonitorOverrideCannotBypassFreeAdmission(t *testing.T) {
	const bootstrap, unknown = "kilo-auto/free", "vendor/paid:free"
	require.NoError(t, validateMonitorProbeKey(PlatformKilo, "", bootstrap, nil, MonitorBodyOverrideModeOff, nil))
	require.ErrorIs(t, validateMonitorProbeKey(PlatformKilo, "", bootstrap, []string{unknown}, MonitorBodyOverrideModeOff, nil), ErrChannelMonitorMissingAPIKey)
	body := map[string]any{"model": unknown, "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	require.ErrorIs(t, validateMonitorProbeKey(PlatformKilo, "", bootstrap, nil, MonitorBodyOverrideModeReplace, body), ErrChannelMonitorMissingAPIKey)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid free monitor request reached the network")
	}))
	defer server.Close()
	_, _, _, err := callProvider(t.Context(), PlatformKilo, server.URL, "", bootstrap, "hello", &CheckOptions{BodyOverrideMode: MonitorBodyOverrideModeReplace, BodyOverride: body})
	require.ErrorIs(t, err, ErrAccountModelUnsupported)
	// Merge protects the model field itself; prove that it cannot replace the
	// approved bootstrap target with the unverified suffix model.
	adapter, apiMode, ok := providerAdapterFor(PlatformKilo, "")
	require.True(t, ok)
	merged, err := buildRequestBody(adapter, PlatformKilo, apiMode, bootstrap, "hello", &CheckOptions{BodyOverrideMode: MonitorBodyOverrideModeMerge, BodyOverride: body})
	require.NoError(t, err)
	var decoded struct {
		Model string `json:"model"`
	}
	require.NoError(t, json.Unmarshal(merged, &decoded))
	require.Equal(t, bootstrap, decoded.Model)
}
