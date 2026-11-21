package service

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestSecurityBackportBufferedAnthropicInvalidIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, index := range []int{-1, -2147483648, 2147483647} {
		badDelta := fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":\"INVALID\"}}\n\n", index)
		body := strings.Replace(miniAnthropicSSEStream(), "event: content_block_delta", badDelta+"event: content_block_delta", 1)
		for _, route := range []string{"chat", "responses", "native-chat", "native-responses"} {
			t.Run(fmt.Sprintf("%s/%d", route, index), func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
				svc := newNativeAnthropicHangTestService(5)
				var err error
				require.NotPanics(t, func() {
					switch route {
					case "chat":
						_, err = (&GatewayService{}).handleCCBufferedFromAnthropic(resp, c, "glm-4.7", "glm-4.7", nil, time.Now())
					case "responses":
						_, err = (&GatewayService{}).handleResponsesBufferedStreamingResponse(resp, c, "glm-4.7", "glm-4.7", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
					case "native-chat":
						_, err = svc.handleCCBufferedFromNativeAnthropic(resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now())
					case "native-responses":
						_, err = svc.handleResponsesBufferedFromNativeAnthropic(resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
					}
				})
				if index < 0 && (route == "chat" || route == "responses") {
					require.ErrorContains(t, err, "invalid Anthropic content index")
					return
				}
				require.NoError(t, err)
				require.Contains(t, rec.Body.String(), "Hello")
				require.NotContains(t, rec.Body.String(), "INVALID")
			})
		}
	}
}

func TestSecurityBackportModelLimitSnapshotIsolation(t *testing.T) {
	now := time.Now().UTC()
	oldLimit := map[string]any{"reason": "original"}
	shared := map[string]any{modelRateLimitsKey: map[string]any{"existing": oldLimit}, "preserved": "value"}
	account := &Account{Extra: shared}
	setAccountModelRateLimitSnapshot(account, "new", now.Add(time.Minute), "cooldown", now)
	require.NotContains(t, shared[modelRateLimitsKey], "new")
	require.Equal(t, "original", oldLimit["reason"])
	require.Equal(t, "value", account.Extra["preserved"])
	require.Contains(t, account.Extra[modelRateLimitsKey], "existing")
	require.Contains(t, account.Extra[modelRateLimitsKey], "new")

	// Independent account objects may share the immutable cache snapshot.
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			copy := &Account{Extra: shared}
			setAccountModelRateLimitSnapshot(copy, fmt.Sprint(i), now.Add(time.Minute), "", now)
		})
	}
	wg.Wait()
	require.Len(t, shared[modelRateLimitsKey], 1)
}
