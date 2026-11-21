package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTruncateForLogRedactsSensitiveDiagnostics(t *testing.T) {
	body := []byte(`prompt failed for person@example.com; api_key=secret-key Authorization: Bearer private-token sk-abcdef123456`)
	got := truncateForLog(body, 2048)
	for _, secret := range []string{"person@example.com", "secret-key", "private-token", "sk-abcdef123456"} {
		require.NotContains(t, got, secret)
	}
}
