package byok

import (
	"github.com/golang-jwt/jwt/v5"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTenantClassBoundaries(t *testing.T) {
	for _, pair := range [][2]string{{"openai", "oauth"}, {"anthropic", "setup-token"}, {"gemini", "service_account"}, {"bedrock", "bedrock"}, {"cursor", "oauth"}, {"antigravity", "oauth"}, {"stepfun_api", "api_key"}} {
		require.True(t, Supported(pair[0], pair[1]), "%v", pair)
	}
	for _, pair := range [][2]string{{"openai", "service_account"}, {"cursor", "api_key"}, {"antigravity", "api_key"}, {"bedrock", "api_key"}, {"unknown", "oauth"}, {"aigocode", "api_key"}, {"", "api_key"}, {"openai", ""}} {
		require.False(t, Supported(pair[0], pair[1]), "%v", pair)
	}
}

func TestStructuredTenantCredentials(t *testing.T) {
	_, err := Payload("gemini", "service_account", `{"type":"authorized_user"}`)
	require.Error(t, err)
	_, err = Payload("bedrock", "bedrock", `{"aws_access_key_id":"a","aws_secret_access_key":"b","aws_region":"us-east-1","endpoint":"elsewhere"}`)
	require.Error(t, err)
	fields, err := Payload("bedrock", "bedrock", `{"aws_access_key_id":"a","aws_secret_access_key":"b","aws_region":"us-east-1"}`)
	require.NoError(t, err)
	require.NotContains(t, fields, "api_key")
	fields, err = Payload("anthropic", "service_account", `{"type":"service_account","project_id":"p","private_key":"k","client_email":"m"}`)
	require.NoError(t, err)
	require.Equal(t, "global", fields["location"])
}

func TestBearerExpiryOnlyNarrowsOAuthVerification(t *testing.T) {
	expiry := time.Now().UTC().Add(time.Minute).Truncate(time.Second)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(expiry)}).SignedString([]byte("fixture"))
	require.NoError(t, err)
	fields := map[string]any{"access_token": token}
	require.True(t, expiry.Equal(*BearerExpiry("oauth", fields)))
	require.Nil(t, BearerExpiry("api_key", fields))
	require.Nil(t, BearerExpiry("oauth", map[string]any{"access_token": "opaque-token"}))
}
