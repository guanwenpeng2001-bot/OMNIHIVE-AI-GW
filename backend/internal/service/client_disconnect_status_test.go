//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 两条传输层失败处理的共同契约：请求 context 已取消（客户端断开）时不记 Ops 上游错误事件；
// 请求 context 仍存活时即便错误是 context.Canceled 也照常记录，不归为客户端断开。
func TestTransportErrorHandlers_ClientDisconnectRecordsNoOpsEvent(t *testing.T) {
	account := &Account{ID: 9, Name: "acc", Platform: PlatformAnthropic}
	clientErr := &url.Error{Op: "Post", URL: "https://upstream.example/v1/messages", Err: context.Canceled}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	handlers := []struct {
		name   string
		handle func(ctx context.Context, ginContext *gin.Context) error
	}{
		{"anthropic", func(ctx context.Context, ginContext *gin.Context) error {
			gateway := &GatewayService{accountRepo: &transportTempUnschedRepoStub{}}
			return gateway.handleUpstreamTransportError(ctx, ginContext, account, clientErr, OpsUpstreamErrorEvent{})
		}},
		{"openai", func(ctx context.Context, ginContext *gin.Context) error {
			gateway := &OpenAIGatewayService{accountRepo: &openaiTransportAccountRepoStub{}}
			return gateway.handleOpenAIUpstreamTransportError(ctx, ginContext, account, clientErr, false)
		}},
	}

	for _, handlerCase := range handlers {
		t.Run(handlerCase.name+"/client disconnected", func(t *testing.T) {
			ginContext := newTransportErrorTestGin(t)

			err := handlerCase.handle(canceledCtx, ginContext)

			require.ErrorIs(t, err, context.Canceled)
			_, recorded := ginContext.Get(OpsUpstreamErrorsKey)
			require.False(t, recorded, "客户端断开不是上游故障")
			_, messageSet := ginContext.Get(OpsUpstreamErrorMessageKey)
			require.False(t, messageSet)
		})
		t.Run(handlerCase.name+"/request context alive", func(t *testing.T) {
			ginContext := newTransportErrorTestGin(t)

			err := handlerCase.handle(context.Background(), ginContext)

			require.ErrorIs(t, err, context.Canceled)
			raw, recorded := ginContext.Get(OpsUpstreamErrorsKey)
			require.True(t, recorded)
			require.Len(t, raw.([]*OpsUpstreamErrorEvent), 1)
		})
	}
}

func TestAntigravityCompatTransportError_ClientDisconnectWrites499(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)

	err := (&AntigravityGatewayService{}).handleAntigravityCompatTransportError(ginContext, context.Canceled)

	require.Error(t, err)
	require.Equal(t, antigravityStatusClientClosed, rec.Code)
	require.Equal(t, "client_disconnected", gjson.Get(rec.Body.String(), "error.type").String())
}

func TestAntigravityWriteGoogleError_ClientClosedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(rec)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", nil)

	_ = (&AntigravityGatewayService{}).writeGoogleError(ginContext, antigravityStatusClientClosed, "Client disconnected before upstream response")

	require.Equal(t, antigravityStatusClientClosed, rec.Code)
	require.Equal(t, "CANCELLED", gjson.Get(rec.Body.String(), "error.status").String())
}
