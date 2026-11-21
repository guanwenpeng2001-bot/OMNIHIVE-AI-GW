package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMcpAdminWriteRequiresVerifiedStepUp(t *testing.T) {
	h := NewMCPGrantHandler()
	h.stepUpVerifier = mcpStepUpVerifierFunc(func(context.Context, int64, string, string) error {
		return errors.New("step-up required")
	})
	body := `{"decision":"approve","client_id":"client","name":"Client","redirect_uri":"https://example.test/callback","scope":"admin","permission":"write","totp_code":"123456"}`
	require.Equal(t, 403, mcpRequest(h, 1, "admin", "POST", "/authorize", body).Code)
}

func TestMcpGrantsSharePersistentStateAcrossInstances(t *testing.T) {
	store := newMemoryMCPGrantStore()
	first := NewMCPGrantHandler()
	second := NewMCPGrantHandler()
	first.grantStore = store
	second.grantStore = store

	created := mcpData[MCPGrantDTO](t, mcpRequest(first, 1, "user", "POST", "/grants", `{"name":"Client","redirect_uri":"https://example.test/callback","scope":"user","permission":"write"}`))
	require.Equal(t, 200, mcpRequest(second, 1, "user", "DELETE", "/grants/"+created.ID, "").Code)
	_, ok := first.storeGrant(context.Background(), 1, created.ID)
	require.False(t, ok)
}

type mcpStepUpVerifierFunc func(context.Context, int64, string, string) error

func (fn mcpStepUpVerifierFunc) VerifyStepUp(ctx context.Context, userID int64, sessionKey, code string) (time.Duration, error) {
	return 0, fn(ctx, userID, sessionKey, code)
}
