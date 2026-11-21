package handler

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentWebhookVerificationFailureDoesNotLogRawBody(t *testing.T) {
	source, err := os.ReadFile("payment_webhook_handler.go")
	require.NoError(t, err)
	require.NotContains(t, string(source), `slog.Debug("[Payment Webhook] verify failed body"`)
}
