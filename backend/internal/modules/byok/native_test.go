package byok

import (
	"context"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/stretchr/testify/require"
)

func TestNativeFundingGrant(t *testing.T) {
	a := scope.Authorized{WorkspaceID: 7}
	d := Dispatch{CredentialID: 3, AccountID: 3, Source: "byok"}
	c := Credential{ID: 3, Workspace: 7, State: "verified"}
	require.NoError(t, checkFundingGrant(a, d, c))
	c.Workspace++
	require.Error(t, checkFundingGrant(a, d, c))
	c.Workspace--
	c.State = "revoked"
	require.Error(t, checkFundingGrant(a, d, c))
	c.State = "verified"
	d.Source, d.AccountID, d.Consent = "wallet", 4, true
	require.Error(t, checkFundingGrant(a, d, c))
	c.Fallback = true
	require.NoError(t, checkFundingGrant(a, d, c))
	d.Consent = false
	require.Error(t, checkFundingGrant(a, d, c))
}

func TestFundedNativeDispatchRejectsWrongWorkspaceAndDisabledFallback(t *testing.T) {
	a := scope.Authorized{WorkspaceID: 7}
	ctx := scope.With(context.Background(), a)
	d := Dispatch{RequestID: "r", Ordinal: 1, AccountID: 24, CredentialID: 24, Source: "byok"}
	c := Credential{ID: 24, Workspace: 7, State: "verified"}
	f := Funding{Estimate: Estimate{Source: "byok"}, Attempt: journal.Attempt{ID: "permit", RequestID: "r", Number: 1, AccountID: 24}}
	var sends int
	transport := func(_ context.Context, _ Funding, secret map[string]any) error {
		require.Equal(t, "tenant-only", secret["api_key"])
		sends++
		return nil
	}
	secret := map[string]any{"api_key": "tenant-only"}
	c.Workspace++
	require.Error(t, sendFundedAttempt(ctx, d, c, f, secret, transport))
	require.Zero(t, sends)
	c.Workspace--
	require.NoError(t, sendFundedAttempt(ctx, d, c, f, secret, transport))
	require.Equal(t, 1, sends)
	d.Source, d.AccountID, d.Consent = "wallet", 40, true
	f.Source, f.Attempt.AccountID = "wallet", 40
	require.Error(t, sendFundedAttempt(ctx, d, c, f, nil, transport))
	require.Equal(t, 1, sends)
}
