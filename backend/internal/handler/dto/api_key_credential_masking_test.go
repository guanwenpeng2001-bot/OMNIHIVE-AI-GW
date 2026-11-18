package dto

import (
	"encoding/json"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// A key DTO carries a mask built from the stored display suffix, never the secret.
func TestAPIKeyFromService_MasksTheCredentialWithItsDisplaySuffix(t *testing.T) {
	out := APIKeyFromService(&service.APIKey{
		ID:            1,
		UserID:        2,
		Key:           "sk-plaintext-secret-value",
		DisplaySuffix: "alue",
		Name:          "masked",
		Status:        service.StatusAPIKeyActive,
	})

	require.NotNil(t, out)
	require.Equal(t, "****alue", out.Key)
	require.NotContains(t, out.Key, "sk-plaintext")
}

// Without a suffix the mask must stay a mask: no secret, no empty field.
func TestAPIKeyFromService_WithoutDisplaySuffixStillMasks(t *testing.T) {
	out := APIKeyFromService(&service.APIKey{
		ID:     1,
		UserID: 2,
		Key:    "sk-plaintext-secret-value",
		Name:   "unmasked",
		Status: service.StatusAPIKeyActive,
	})

	require.NotNil(t, out)
	require.Equal(t, MaskAPIKeyCredential(""), out.Key)
	require.Equal(t, "****", out.Key)
}

// The serialized DTO is what leaves the process, so the secret must not survive it.
func TestAPIKeyFromService_SerializedPayloadNeverCarriesTheSecret(t *testing.T) {
	secret := "sk-plaintext-secret-value"
	out := APIKeyFromService(&service.APIKey{
		ID:            1,
		UserID:        2,
		Key:           secret,
		DisplaySuffix: service.APIKeyCredentialDisplaySuffix(secret),
		Name:          "serialized",
		Status:        service.StatusAPIKeyActive,
	})

	payload, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(payload), secret)
	require.Contains(t, string(payload), "****"+service.APIKeyCredentialDisplaySuffix(secret))
}

func TestAPIKeyFromService_MasksTheCredentialForNestedUserKeys(t *testing.T) {
	secret := "sk-nested-plaintext-secret"
	user := UserFromServiceAdmin(&service.User{
		ID:       2,
		Email:    "nested@example.invalid",
		Username: "nested",
		Role:     "user",
		APIKeys: []service.APIKey{{
			ID:            3,
			UserID:        2,
			Key:           secret,
			DisplaySuffix: service.APIKeyCredentialDisplaySuffix(secret),
			Name:          "nested-key",
			Status:        service.StatusAPIKeyActive,
		}},
	})

	require.Len(t, user.APIKeys, 1)
	require.NotContains(t, user.APIKeys[0].Key, secret)
	require.Equal(t, "****"+service.APIKeyCredentialDisplaySuffix(secret), user.APIKeys[0].Key)
}
