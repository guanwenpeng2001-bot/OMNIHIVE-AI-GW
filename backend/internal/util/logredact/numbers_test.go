package logredact

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactJSONPreservesNumbersAndSecrets(t *testing.T) {
	input := `{"id":9223372036854775807,"amount":0.123456789012345678901,"nested":[9007199254740993,{"token":"private-value"}],"api_key":"private-value"}`
	for name, result := range map[string]string{
		"json": RedactJSON([]byte(input)),
		"text": RedactText(input),
	} {
		t.Run(name, func(t *testing.T) {
			require.NotContains(t, result, "private-value")
			require.Contains(t, result, `"id":9223372036854775807`)
			require.Contains(t, result, `"amount":0.123456789012345678901`)
			require.Contains(t, result, `9007199254740993`)
			require.True(t, json.Valid([]byte(result)))
		})
	}
	for _, input := range []string{`{"id":1}{"id":2}`, `{"id":1} null`, `{"id":1} bad`} {
		require.Equal(t, "<non-json payload redacted>", RedactJSON([]byte(input)))
	}
}
