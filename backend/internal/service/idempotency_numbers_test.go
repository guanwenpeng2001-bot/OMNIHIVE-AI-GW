package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdempotencyStoredResponsePreservesJSONNumbers(t *testing.T) {
	coordinator := NewIdempotencyCoordinator(nil, DefaultIdempotencyConfig())
	stored := `{"subscription_id":9223372036854775807,"negative":-9223372036854775808,"unsigned":18446744073709551615,"unsafe_integer":9007199254740993,"decimal":0.12345678901234567890123456789,"exponent":1e+100,"nested":[{"id":9007199254740995},true,null,"9223372036854775807"]}`
	decoded, err := coordinator.decodeStoredResponse(&stored)
	require.NoError(t, err)
	reencoded, err := json.Marshal(decoded)
	require.NoError(t, err)
	require.Equal(t, stored, string(reencodedOrderedNumbers(t, reencoded)))
}

func TestIdempotencyStoredResponseRoundTripLargeInteger(t *testing.T) {
	coordinator := NewIdempotencyCoordinator(nil, DefaultIdempotencyConfig())
	stored, err := coordinator.marshalStoredResponse(map[string]any{
		"subscription_id": int64(9223372036854775807),
		"access_token":    "synthetic-secret",
	})
	require.NoError(t, err)
	require.NotContains(t, stored, "synthetic-secret")
	require.Contains(t, stored, `"subscription_id":9223372036854775807`)
	decoded, err := coordinator.decodeStoredResponse(&stored)
	require.NoError(t, err)
	reencoded, err := json.Marshal(decoded)
	require.NoError(t, err)
	require.Contains(t, string(reencoded), `"subscription_id":9223372036854775807`)
}

func reencodedOrderedNumbers(t *testing.T, data []byte) []byte {
	t.Helper()
	var fields struct {
		SubscriptionID json.Number     `json:"subscription_id"`
		Negative       json.Number     `json:"negative"`
		Unsigned       json.Number     `json:"unsigned"`
		UnsafeInteger  json.Number     `json:"unsafe_integer"`
		Decimal        json.Number     `json:"decimal"`
		Exponent       json.Number     `json:"exponent"`
		Nested         json.RawMessage `json:"nested"`
	}
	require.NoError(t, json.Unmarshal(data, &fields))
	result, err := json.Marshal(fields)
	require.NoError(t, err)
	return result
}

func TestIdempotencyStoredResponseRejectsTrailingJSON(t *testing.T) {
	coordinator := NewIdempotencyCoordinator(nil, DefaultIdempotencyConfig())
	for _, stored := range []string{`{"id":1}{"id":2}`, `{"id":1} null`, `{"id":1} garbage`, `{"id":`, `[1,]`} {
		t.Run(stored, func(t *testing.T) {
			_, err := coordinator.decodeStoredResponse(&stored)
			require.Error(t, err)
		})
	}
	for _, stored := range []string{`null`, `[]`, `true`, `12345678901234567890`, "\"text\"", " {\"id\":1} \n\t"} {
		t.Run(stored, func(t *testing.T) {
			_, err := coordinator.decodeStoredResponse(&stored)
			require.NoError(t, err)
		})
	}
}
