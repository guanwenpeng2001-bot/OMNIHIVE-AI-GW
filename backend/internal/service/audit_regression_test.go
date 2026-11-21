package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestV2AuditRetainsTraceIdentityAndRedactedChanges(t *testing.T) {
	actor := int64(7)
	entry := &AuditLog{
		Path:             "/api/v2/workspaces/:workspace_id/connections/:id",
		Action:           "connections.update",
		Method:           "PUT",
		ActorUserID:      &actor,
		ActorEmail:       "operator@example.test",
		AuthMethod:       "jwt",
		RequestID:        "request-7",
		ClientIP:         "192.0.2.10",
		RequestBody:      `{"name":"renamed","api_key":"***"}`,
		CredentialMasked: "****",
		Extra: map[string]any{
			"params":         map[string]string{"workspace_id": "3", "id": "9"},
			"submitted_diff": []ConfigurationFieldDiff{{Field: "name", Before: "old", After: "renamed"}},
		},
	}
	service := NewAuditLogService(nil, nil)
	service.Record(entry)
	for _, got := range []*AuditLog{<-service.queue, auditManagementView(entry)} {
		require.Equal(t, entry.Path, got.Path)
		require.Equal(t, entry.Action, got.Action)
		require.Equal(t, "request-7", got.RequestID)
		require.Equal(t, "operator@example.test", got.ActorEmail)
		require.Equal(t, "jwt", got.AuthMethod)
		require.Equal(t, "192.0.2.10", got.ClientIP)
		require.Equal(t, entry.Extra, got.Extra)
		require.Equal(t, entry.RequestBody, got.RequestBody)
		require.Empty(t, got.CredentialMasked)
	}
}

func TestSubmittedConfigurationDiffKeepsOnlySafeValues(t *testing.T) {
	diff := SubmittedConfigurationDiff([]byte(`{"name":"renamed","enabled":true,"password":"secret-canary","credentials":{"api_key":"key-canary"}}`))
	encoded, err := json.Marshal(diff)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"before":null`)
	require.Contains(t, string(encoded), `"after":"renamed"`)
	require.Contains(t, string(encoded), `"after":true`)
	require.Contains(t, string(encoded), `"after":"changed"`)
	require.NotContains(t, string(encoded), "secret-canary")
	require.NotContains(t, string(encoded), "key-canary")
}
