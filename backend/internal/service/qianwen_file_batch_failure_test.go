package service

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestQianwenResourceFailureDiagnostics(tester *testing.T) {
	const fallback = "DashScope rejected resource request (HTTP 400)"
	for _, testCase := range []struct {
		name string
		body string
		want string
	}{
		{name: "account qualification", body: `{"error":{"message":"The user information is not completed."}}`, want: fallback + ": The user information is not completed."},
		{name: "empty body", want: fallback},
		{name: "HTML", body: "<html>provider error</html>", want: fallback},
		{name: "invalid JSON", body: `{"error":{"message":"partial"}`, want: fallback},
		{name: "missing message", body: `{"error":{"code":"access_denied"}}`, want: fallback},
		{name: "non-string message", body: `{"error":{"message":{"password":"secret"}}}`, want: fallback},
		{name: "blank message", body: `{"error":{"message":"  "}}`, want: fallback},
	} {
		tester.Run(testCase.name, func(tester *testing.T) {
			update := resourceFailure(400, []byte(testCase.body))
			require.Equal(tester, "failed", update.Status)
			require.Equal(tester, "upstream_rejected", update.ErrorCode)
			require.Equal(tester, testCase.want, update.ErrorMessage)
		})
	}
	for _, reason := range []string{
		"invalid sk-secret-value", "invalid Bearer short-secret", "invalid bearer\tshort-secret",
		strings.Repeat("abcdef0123456789", 4), strings.Repeat("Ab9+/", 10), strings.Repeat("Ab9_-", 10),
		`api_key="short secret"`, `{"authorization":"short secret"}`, `password: short secret`, `token=short-secret`,
		strings.Repeat("ordinary reason ", 80) + "sk-hidden-after-limit",
	} {
		body, err := json.Marshal(map[string]any{"error": map[string]string{"message": reason}})
		require.NoError(tester, err)
		update := resourceFailure(400, body)
		require.Equal(tester, fallback+": [redacted]", update.ErrorMessage)
	}
	for _, reason := range []string{strings.Repeat("reason ", 100), strings.Repeat("原因", 400)} {
		body, err := json.Marshal(map[string]any{"error": map[string]string{"message": reason}})
		require.NoError(tester, err)
		message := resourceFailure(400, body).ErrorMessage
		require.Equal(tester, 512, utf8.RuneCountInString(message))
		require.True(tester, utf8.ValidString(message))
		require.True(tester, strings.HasSuffix(message, "…"))
	}
}

func TestQianwenBatchTerminalFailureDiagnostics(tester *testing.T) {
	for _, body := range []string{
		`{"id":"batch-id","status":"failed","error":{"message":"File is invalid."}}`,
		`{"id":"batch-id","status":"failed","errors":{"data":[{"message":"File is invalid."}]}}`,
	} {
		update, err := parseQianwenBatch([]byte(body))
		require.NoError(tester, err)
		require.Equal(tester, "failed", update.Status)
		require.Equal(tester, "upstream_failed", update.ErrorCode)
		require.Equal(tester, "DashScope batch failed or was cancelled: File is invalid.", update.ErrorMessage)
	}
}
