package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVolcengineCredentialsRedactMergeAndClear(t *testing.T) {
	stored := map[string]any{"api_key": "inference-secret", "volcengine_access_key_id": "management-access", "volcengine_secret_access_key": "management-secret", "volcengine_plan_edition": "team", "volcengine_seat_id": "seat"}
	visible, status := RedactCredentials(stored)
	raw, err := json.Marshal(visible)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "management-access")
	require.NotContains(t, string(raw), "management-secret")
	require.NotContains(t, string(raw), "inference-secret")
	require.True(t, status["has_volcengine_access_key_id"])
	require.True(t, status["has_volcengine_secret_access_key"])
	require.Equal(t, "team", visible["volcengine_plan_edition"])
	require.Equal(t, "seat", visible["volcengine_seat_id"])
	merged := service.MergePreservingSensitiveCreds(stored, visible)
	require.Equal(t, stored, merged)
	visible["volcengine_access_key_id"], visible["volcengine_secret_access_key"] = "", " "
	merged = service.MergePreservingSensitiveCreds(stored, visible)
	require.Equal(t, stored, merged)
	visible["volcengine_access_key_id"], visible["volcengine_secret_access_key"] = nil, nil
	merged = service.MergePreservingSensitiveCreds(stored, visible)
	_, status = RedactCredentials(merged)
	require.False(t, status["has_volcengine_access_key_id"])
	require.False(t, status["has_volcengine_secret_access_key"])
	require.True(t, status["has_api_key"])
}

func TestByteplusCredentialsRedactMergeAndClear(test *testing.T) {
	stored := map[string]any{"api_key": "inference-secret", "byteplus_access_key_id": "management-access", "byteplus_secret_access_key": "management-secret", "billing_plan": "team", "byteplus_seat_id": "seat"}
	visible, status := RedactCredentials(stored)
	raw, err := json.Marshal(visible)
	require.NoError(test, err)
	require.NotContains(test, string(raw), "management-access")
	require.NotContains(test, string(raw), "management-secret")
	require.NotContains(test, string(raw), "inference-secret")
	require.True(test, status["has_byteplus_access_key_id"])
	require.True(test, status["has_byteplus_secret_access_key"])
	require.Equal(test, "team", visible["billing_plan"])
	require.Equal(test, "seat", visible["byteplus_seat_id"])
	merged := service.MergePreservingSensitiveCreds(stored, visible)
	require.Equal(test, stored, merged)
	visible["byteplus_access_key_id"], visible["byteplus_secret_access_key"] = "", " "
	merged = service.MergePreservingSensitiveCreds(stored, visible)
	require.Equal(test, stored, merged)
	visible["byteplus_access_key_id"], visible["byteplus_secret_access_key"] = nil, nil
	merged = service.MergePreservingSensitiveCreds(stored, visible)
	_, status = RedactCredentials(merged)
	require.False(test, status["has_byteplus_access_key_id"])
	require.False(test, status["has_byteplus_secret_access_key"])
	require.True(test, status["has_api_key"])
}
