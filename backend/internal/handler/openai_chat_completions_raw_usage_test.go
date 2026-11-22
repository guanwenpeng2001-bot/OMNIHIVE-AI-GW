//go:build unit

package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRawChatUnknownUsageErrorRetainsReviewReceipt(t *testing.T) {
	for _, platform := range []string{service.PlatformMistral, service.PlatformOpenRouter} {
		t.Run(platform, func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("X-Request-Id", "strict-partial-receipt")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"delivered\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7}}\n\ndata: [DONE]\n\n")
			}))
			defer upstream.Close()
			account := compatibleReviewAccount(upstream.URL + "/v1")
			account.Platform = platform
			handler, group, receipts, usage := reviewHandlerForAccount(t, account, compatibleReviewConfig())
			client, rec := requestPlanHTTPContext("/v1/chat/completions", `{"model":"fixture-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`, group)
			handler.ChatCompletions(client)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "delivered")
			require.Contains(t, rec.Body.String(), "upstream_stream_read_error")
			require.NotContains(t, rec.Body.String(), "[DONE]")
			require.EqualValues(t, 1, calls.Load(), "a delivered stream must not be replayed")
			require.Len(t, receipts.receipts, 1)
			for _, receipt := range receipts.receipts {
				require.Equal(t, "strict-partial-receipt", receipt.Key.RequestID)
				require.Equal(t, 7, receipt.Usage.InputTokens)
				require.Equal(t, "interrupted", receipt.CompletionState)
				require.Equal(t, "unresolved", receipt.ResolutionState)
				require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
			}
			require.Zero(t, receipts.settles)
			require.Zero(t, receipts.debit)
			require.Empty(t, usage.logs)
		})
	}
}
