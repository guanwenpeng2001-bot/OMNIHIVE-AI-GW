package connections

import (
	"encoding/json"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
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
	require.Contains(t, Patch{Pools: []string{}}.Snapshot(), "pool_ids")
	require.NotContains(t, Patch{}.Snapshot(), "pool_ids")
}
