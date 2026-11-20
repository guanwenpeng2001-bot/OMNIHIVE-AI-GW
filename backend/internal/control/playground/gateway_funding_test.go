package playground

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/routing/trace"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	pg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/playground"
	"github.com/stretchr/testify/require"
)

func TestPlaygroundSharedAdapterRequiresAuthorizedFunding(t *testing.T) {
	var sends int
	g := Gateway{Routes: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sends++ })}
	for name, value := range map[string]any{"allow_shared_fallback": false, "byok_credential_id": "other-workspace-credential", "funding_source": "byok"} {
		_, err := g.prepare(context.Background(), identity.Access{}, pg.Submit{Input: map[string]any{name: value}}, "", false)
		require.ErrorIs(t, err, fault.Forbidden)
	}
	p := prepared{native: pg.Native{Path: "/v1/messages"}, decision: trace.DecisionTrace{Selected: &trace.Selection{FundingSource: "byok"}}}
	require.ErrorIs(t, g.execute(context.Background(), p, httptest.NewRecorder()), fault.Forbidden)
	require.Zero(t, sends)
	p.decision.Selected.FundingSource = "wallet"
	require.NoError(t, g.execute(context.Background(), p, httptest.NewRecorder()))
	require.Equal(t, 1, sends)
}
