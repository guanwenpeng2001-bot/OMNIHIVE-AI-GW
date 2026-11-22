//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestKiloNestedSecretRedaction(t *testing.T) {
	account := &Account{Credentials: map[string]any{"api_key": "account-secret", "access_token": "account-access"}}
	for _, message := range []string{
		`nested {"api_key":"json-secret","items":[{"access_token":"access-secret","refresh_token":"refresh-secret"}]}`,
		`escaped {\"token\":\"escaped-secret\",\"api_key\":\"escaped-key\"}`,
		`{"nested":{"message":"Bearer bearer-secret Cookie: session=cookie-secret; other=private"}}`,
		`unlabelled account-secret and account-access`,
		`Authorization: Bearer bearer-secret`,
	} {
		t.Run(message, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"error": map[string]any{"message": message}})
			require.NoError(t, err)
			body, err := json.Marshal(map[string]any{"error": map[string]any{"message": "provider refused", "metadata": map[string]any{"raw": string(raw)}}})
			require.NoError(t, err)
			for _, model := range []string{"kilo-auto/free", "other/free"} {
				out := normalizeKiloPayload(body, model, account)
				require.True(t, json.Valid(out))
				require.False(t, gjson.GetBytes(out, "error.metadata.raw").Exists())
				for _, secret := range []string{"json-secret", "access-secret", "refresh-secret", "escaped-secret", "escaped-key", "bearer-secret", "cookie-secret", "account-secret", "account-access", "other=private"} {
					require.NotContains(t, string(out), secret)
				}
				require.LessOrEqual(t, len([]rune(gjson.GetBytes(out, "error.message").String())), 200)
			}
		})
	}
	twice, err := json.Marshal(`{"message":"diagnostic {\"api_key\":\"double-secret\"}"}`)
	require.NoError(t, err)
	require.NotContains(t, kiloRedactText(string(twice), nil), "double-secret")
	for _, raw := range []string{`{"error":{"metadata":{"raw":{"token":"hidden"}}}}`, `{"metadata":{"raw":"opaque"}}`, `{"error":{"message":"Bearer hidden"}}`} {
		out := normalizeKiloPayload([]byte(raw), "")
		require.NotContains(t, string(out), "hidden")
		require.NotContains(t, string(out), `"raw"`)
	}
	require.Len(t, []rune(kiloErrorDetail([]byte(`{"error":{"message":"`+strings.Repeat("界", 300)+`"}}`))), 200)
}

func TestKiloCorrectedCacheBucketsSharePromptLimit(t *testing.T) {
	for _, tc := range []struct{ read, write, wantRead, wantWrite int }{
		{300000, 1, 256000, 0}, {0, 500000, 0, 256000},
		{100000, 200000, 100000, 156000}, {5, 9, 5, 9}, {-2, 3, 0, 3},
	} {
		body := fmt.Appendf(nil, `{"usage":{"prompt_tokens":300000,"prompt_tokens_details":{"cached_tokens":%d,"cache_write_tokens":%d}}}`, tc.read, tc.write)
		usage := gjson.GetBytes(normalizeKiloPayload(body, "kilo-auto/free"), "usage")
		require.Equal(t, tc.wantRead, openAICacheReadTokensFromUsage(usage))
		require.Equal(t, tc.wantWrite, openAICacheCreationTokensFromUsage(usage))
	}
	body := []byte(`{"usage":{"prompt_tokens":300000,"cache_read_input_tokens":200000,"prompt_tokens_details":{"cache_creation_input_tokens":90000}}}`)
	usage := gjson.GetBytes(normalizeKiloPayload(body, "kilo-auto/free"), "usage")
	require.Equal(t, 56000, openAICacheCreationTokensFromUsage(usage))
}
