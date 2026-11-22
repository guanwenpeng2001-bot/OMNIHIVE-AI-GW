package playground

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPriceConsentVersionIsStrictAndPartOfIdempotencyFingerprint(t *testing.T) {
	base := `{"api_key_id":"7","model_id":"model","protocol":"openai_chat","action":"text.chat","input":{},"funding_acknowledged":true}`
	_, original, err := Parse([]byte(base), "same-key", "")
	require.NoError(t, err)
	quoted := strings.Replace(base, `"input":{}`, `"expected_price_version":"sale_1","input":{}`, 1)
	parsed, fingerprint, err := Parse([]byte(quoted), "same-key", "")
	require.NoError(t, err)
	require.Equal(t, "sale_1", parsed.ExpectedPriceVersion)
	require.NotEqual(t, original, fingerprint)
	_, _, err = Parse([]byte(strings.Replace(quoted, "sale_1", " sale_1", 1)), "same-key", "")
	require.Error(t, err)
	_, _, err = Parse([]byte(strings.Replace(quoted, "sale_1", strings.Repeat("x", 201), 1)), "same-key", "")
	require.Error(t, err)
}
