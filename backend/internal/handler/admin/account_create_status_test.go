package admin

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCreateAccountRequestForwardsStatus(t *testing.T) {
	admin := newStubAdminService()
	handler := &AccountHandler{adminService: admin}
	_, err := handler.createAccountFromRequest(context.Background(), nil, CreateAccountRequest{Name: "inactive", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: "inactive"}, false, false)
	require.NoError(t, err)
	require.Len(t, admin.createdAccounts, 1)
	require.Equal(t, "inactive", admin.createdAccounts[0].Status)
}
