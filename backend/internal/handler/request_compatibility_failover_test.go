//go:build unit

package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestRequestCompatibilityFailoverConsumers(t *testing.T) {
	failure := &service.UpstreamFailoverError{StatusCode: 400, ClientStatusCode: 400, ClientMessage: "cursor: unsupported variant", Reason: service.RequestCompatibilityRejectedReason, NextAccountAction: service.NextAccountRetry}
	fs := NewFailoverState(2, false)
	scheduler := &mockTempUnscheduler{}
	require.Equal(t, FailoverContinue, fs.HandleFailoverError(context.Background(), scheduler, 17, service.PlatformCursor, 2, failure))
	require.Contains(t, fs.FailedAccountIDs, int64(17))
	require.Empty(t, scheduler.calls)
	for name, write := range map[string]func(*gin.Context, *service.UpstreamFailoverError, bool){
		"messages": func(c *gin.Context, e *service.UpstreamFailoverError, stream bool) {
			(&GatewayHandler{}).handleFailoverExhausted(c, e, service.PlatformCursor, stream)
		},
		"openai_messages":  (&OpenAIGatewayHandler{}).handleAnthropicFailoverExhausted,
		"openai_responses": (&OpenAIGatewayHandler{}).handleFailoverExhausted,
		"chat":             (&GatewayHandler{}).handleCCFailoverExhausted,
		"responses":        (&GatewayHandler{}).handleResponsesFailoverExhausted,
	} {
		t.Run(name, func(t *testing.T) {
			for _, stream := range []bool{false, true} {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				write(c, failure, stream)
				require.Contains(t, recorder.Body.String(), failure.ClientMessage)
				if name == "responses" && stream {
					require.Contains(t, recorder.Body.String(), `"code":"invalid_request"`)
				} else {
					require.Contains(t, recorder.Body.String(), "invalid_request_error")
				}
				if !stream {
					require.Equal(t, 400, recorder.Code)
				}
			}
		})
	}
}
