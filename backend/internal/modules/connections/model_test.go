package connections

import (
	"encoding/json"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchemasAndRedactedProjection(t *testing.T) {
	for _, p := range domain.PlatformDescriptors() {
		s := Schemas(p.ID, "")
		require.Len(t, s, 1)
		require.NotEmpty(t, s[0].AuthTypes)
	}
	require.Empty(t, Schemas("unknown", "api_key"))
	c := Connection{ID: 742, Create: Create{Name: "internal-route", Endpoint: "https://internal.example"}, Credential: &credentials.Metadata{State: "secret-meta"}}
	for _, actions := range [][]string{{}, {"credentials.read_metadata"}, {"connections.read"}} {
		b, e := json.Marshal(c.Project(identity.Access{Actions: actions}))
		require.NoError(t, e)
		require.NotContains(t, string(b), "secret-meta")
		if len(actions) == 0 || actions[0] != "connections.read" {
			require.NotContains(t, string(b), "internal-route")
		}
	}
	require.False(t, ValidEndpoint("https://user@upstream.test"))
	require.False(t, ValidEndpoint("https://upstream.test?key=bad"))
	require.False(t, ValidEndpoint("http://upstream.test#fragment"))
	require.False(t, ValidEndpoint("http://upstream.test/v1%2Fmodels"))
	require.False(t, ValidEndpoint("ftp://upstream.test"))
	require.Contains(t, Patch{Pools: []string{}}.Snapshot(), "pool_ids")
	require.NotContains(t, Patch{}.Snapshot(), "pool_ids")
}

func TestConnectionHTTPEndpoint(t *testing.T) {
	const local = "http://127.0.0.1:3425"
	require.True(t, ValidEndpoint(local))
	require.True(t, ValidEndpoint(local+"/v1"))
	require.True(t, ValidEndpoint("http://host.docker.internal:3425/v1"))

	endpoint := local + "/v1///"
	c := Connection{Create: Create{Endpoint: local + "/v1"}}
	require.False(t, endpointChanged(c.Endpoint, &endpoint))
	require.True(t, endpointChanged(local, &endpoint))
	applyPatch(&c, Patch{Endpoint: &endpoint})
	require.Equal(t, local+"/v1", c.Endpoint)
}

func TestConnectionHeaderOverrideValidationAndProjection(t *testing.T) {
	account := &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}
	var patch Patch
	require.NoError(t, json.Unmarshal([]byte(`{"header_overrides":{"User-Agent":" Gateway/1 ","X-Upstream-Route":"fixture"}}`), &patch))
	headers, err := normalizeConnectionHeaders(account, patch.HeaderOverrides)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"user-agent": "Gateway/1", "x-upstream-route": "fixture"}, headers)

	connection := Connection{Create: Create{HeaderOverrides: headers}}
	reader := connection.Project(identity.Access{Actions: []string{"connections.read"}}).(Connection)
	require.Empty(t, reader.HeaderOverrides)
	require.Contains(t, reader.Redactions, "header_overrides")
	editor := connection.Project(identity.Access{Actions: []string{"connections.read", "connections.edit"}}).(Connection)
	require.Equal(t, headers, editor.HeaderOverrides)

	for _, invalid := range []map[string]string{
		{"Authorization": "other credential"},
		{"Api-Key": "other credential"},
		{"X-Route": "first\r\nX-Injected: second"},
		{"User-Agent": "first", "user-agent": "second"},
	} {
		_, err = normalizeConnectionHeaders(account, invalid)
		require.Error(t, err)
	}
	account.Type = service.AccountTypeOAuth
	_, err = normalizeConnectionHeaders(account, headers)
	require.Error(t, err)
	require.NoError(t, json.Unmarshal([]byte(`{"header_overrides":{}}`), &patch))
	require.Equal(t, map[string]any{"header_overrides": map[string]string{}}, patch.Snapshot())
	require.NotContains(t, Patch{}.Snapshot(), "header_overrides")
}

func TestCreateReportsSchemaFields(t *testing.T) {
	for _, tc := range []struct {
		provider, auth, version, field string
	}{
		{"unregistered", "api_key", SchemaVersion, "/provider"},
		{"", "api_key", SchemaVersion, "/provider"},
		{"a6api", "", SchemaVersion, "/auth_type"},
		{"a6api", "oauth", SchemaVersion, "/auth_type"},
		{"a6api", "api_key", "0", "/schema_version"},
	} {
		err := (Create{Provider: tc.provider, AuthType: tc.auth, SchemaVersion: tc.version}).validateSchema()
		var f *fault.Fault
		require.ErrorAs(t, err, &f)
		require.Equal(t, tc.field, f.Field)
	}
	require.NoError(t, (Create{Provider: "a6api", AuthType: "api_key", SchemaVersion: SchemaVersion}).validateSchema())
}
