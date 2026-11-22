package connections

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCompatibleProbeDiagnostics(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://secret.example")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("hidden-upstream-body"))
			}))
			defer upstream.Close()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, upstream.URL, nil)
			require.NoError(t, err)
			got, retry := probeCompatibleModels(upstream.Client(), req, "bearer", "secret-key")
			require.False(t, retry)
			if status == 302 {
				require.Equal(t, "failed", got.Network)
				require.Equal(t, "PROBE_FAILED", got.NetworkReason)
			} else {
				require.Equal(t, "passed", got.Network)
			}
			switch status {
			case 401, 403:
				require.Equal(t, "failed", got.Authorization)
				require.Equal(t, "AUTH_REJECTED", got.AuthorizationReason)
			case 429:
				require.Equal(t, "unknown", got.Authorization)
				require.Equal(t, "UPSTREAM_RATE_LIMITED", got.AuthorizationReason)
				require.Equal(t, "UPSTREAM_RATE_LIMITED", got.DiscoveryReason)
			}
			require.NotEqual(t, "unsupported", got.Discovery)
			require.NotEmpty(t, got.Detail)
			require.NotContains(t, got.Detail, "secret")
			require.NotContains(t, got.Detail, "hidden-upstream-body")
		})
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist = config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"allowed.example"}}
	native := service.NewAccountTestService(nil, nil, nil, nil, nil, nil, cfg, nil)
	store := New(nil, nil, nil, native, cfg)
	a := &service.Account{Platform: "a6api", Type: service.AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://blocked.example", "api_key": "secret-key"}}
	got := store.probe.ProbeConnection(t.Context(), a, "")
	require.Equal(t, "URL_NOT_ALLOWED", got.NetworkReason)
	require.Contains(t, got.Detail, "SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS")
}

func TestCheckDetailJSONOptional(t *testing.T) {
	reason := "URL_NOT_ALLOWED"
	body, err := json.Marshal(Check{Name: "network", State: "failed", Reason: &reason, Detail: "Safe operator diagnostic."})
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"network","state":"failed","reason":"URL_NOT_ALLOWED","detail":"Safe operator diagnostic."}`, string(body))
	body, err = json.Marshal(Check{Name: "network", State: "passed"})
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"network","state":"passed","reason":null}`, string(body))
}

func TestVerificationCheckDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		probe  service.ConnectionProbe
		reason string
	}{
		{"network", service.FailedConnectionProbe("URL_NOT_ALLOWED"), "URL_NOT_ALLOWED"},
		{"network", service.FailedConnectionProbe("PROBE_FAILED"), "PROBE_FAILED"},
		{"authorization", service.ConnectionProbeHTTPStatus(401), "AUTH_REJECTED"},
		{"authorization", service.ConnectionProbeHTTPStatus(429), "UPSTREAM_RATE_LIMITED"},
		{"discover_models", service.ConnectionProbeHTTPStatus(429), "UPSTREAM_RATE_LIMITED"},
		{"network", service.ConnectionProbe{Network: "unknown"}, "EVIDENCE_UNAVAILABLE"},
		{"authorization", service.ConnectionProbe{Authorization: "failed"}, "PROBE_FAILED"},
		{"discover_models", service.ConnectionProbe{Discovery: "unsupported"}, "CHECK_UNSUPPORTED"},
	} {
		check := connectionProbeCheck(tc.name, tc.probe)
		require.NotNil(t, check.Reason)
		require.Equal(t, tc.reason, *check.Reason)
		require.Equal(t, tc.probe.Detail, check.Detail)
	}
	check := connectionProbeCheck("network", service.ConnectionProbeHTTPStatus(401))
	require.Equal(t, "passed", check.State)
	require.Nil(t, check.Reason)
	require.Empty(t, check.Detail)
}

func TestConnectionsOpenAPIContractAlignment(t *testing.T) {
	specBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi", "openapi.yaml"))
	require.NoError(t, err)
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `yaml:"properties"`
				OneOf      []struct {
					Properties map[string]any `yaml:"properties"`
				} `yaml:"oneOf"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(specBytes, &spec))

	assertKeysIn := func(schemaName string, value any) {
		t.Helper()
		payload, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		var rendered map[string]any
		require.NoError(t, json.Unmarshal(payload, &rendered))
		props := spec.Components.Schemas[schemaName].Properties
		for key := range rendered {
			require.Contains(t, props, key, "%s missing runtime key %q", schemaName, key)
		}
	}

	reason := "URL_NOT_ALLOWED"
	assertKeysIn("ConnectionCheck", Check{Name: "network", State: "failed", Reason: &reason, Detail: "Safe diagnostic."})

	mode := "free"
	assertKeysIn("ConnectionCreate", Create{Provider: "kilo", SchemaVersion: "1", AuthType: "api_key", AccountMode: mode, Name: "kilo-free"})
	assertKeysIn("ConnectionPatch", Patch{AccountMode: &mode})
	assertKeysIn("ConnectionResource", Connection{
		ID: 1, Workspace: 1,
		Create: Create{Provider: "kilo", SchemaVersion: "1", AuthType: "api_key", AccountMode: mode, Name: "kilo-free"},
		State:  "active", Version: 1, Models: []string{}, Pools: []string{}, Allowed: []string{}, Redactions: []string{},
	})

	credPayload, err := json.Marshal(credentials.Change{
		Operation: "set", Type: "oauth", Secret: "tok", RefreshToken: "ref",
		Metadata: map[string]any{"site": "global", "uid": "u1"},
	})
	require.NoError(t, err)
	var credMap map[string]any
	require.NoError(t, json.Unmarshal(credPayload, &credMap))
	require.Len(t, spec.Components.Schemas["ConnectionCredentialInput"].OneOf, 2)
	setProps := spec.Components.Schemas["ConnectionCredentialInput"].OneOf[1].Properties
	for key := range credMap {
		require.Contains(t, setProps, key, "ConnectionCredentialInput[set] missing runtime key %q", key)
	}
}
