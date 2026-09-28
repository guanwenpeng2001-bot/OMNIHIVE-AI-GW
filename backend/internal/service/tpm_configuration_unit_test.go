//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTPMConfig_UserCannotClearAdminLimit(t *testing.T) {
	owner, repository := newBudgetServiceStub(t, APIKeyBudgetPolicy{}, 0)
	repository.apiKey.TPMLimit = 55
	var request UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"renamed","tpm_limit":0}`), &request))
	key, err := owner.Update(context.Background(), 7, 3, request)
	require.NoError(t, err)
	require.Equal(t, int64(55), key.TPMLimit)
}
