package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestManagementQueryCredentialsRedactPreserveAndClear(t *testing.T) {
	stored := map[string]any{"api_key": "inference", "tencent_query_region": "ap-shanghai", "tencent_query_team_id": "team", "usage_query_user_id": "user"}
	fields := []string{"tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token"}
	for _, field := range fields {
		stored[field] = "query-secret"
	}
	visible, status := RedactCredentials(stored)
	for _, field := range fields {
		require.NotContains(t, visible, field)
		require.True(t, status["has_"+field])
	}
	encoded, err := json.Marshal(visible)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "query-secret")
	for _, field := range []string{"tencent_query_region", "tencent_query_team_id", "usage_query_user_id"} {
		require.Equal(t, stored[field], visible[field])
	}
	merged := service.MergePreservingSensitiveCreds(stored, visible)
	for _, field := range fields {
		require.Equal(t, "query-secret", merged[field])
		visible[field] = " "
	}
	merged = service.MergePreservingSensitiveCreds(stored, visible)
	for _, field := range fields {
		require.Equal(t, "query-secret", merged[field])
		visible[field] = nil
	}
	merged = service.MergePreservingSensitiveCreds(stored, visible)
	_, status = RedactCredentials(merged)
	for _, field := range fields {
		require.Nil(t, merged[field])
		require.False(t, status["has_"+field])
	}
	require.Equal(t, "inference", merged["api_key"])
}
