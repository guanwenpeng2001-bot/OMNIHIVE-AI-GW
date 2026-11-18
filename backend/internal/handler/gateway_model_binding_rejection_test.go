//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestModelBindingRejectionConsumers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	consumers := []struct {
		name  string
		path  string
		write func(*gin.Context, *service.UpstreamFailoverError, bool)
	}{
		{"anthropic", "/v1/messages", func(c *gin.Context, err *service.UpstreamFailoverError, stream bool) {
			(&GatewayHandler{}).handleFailoverExhausted(c, err, service.PlatformAnthropic, stream)
		}},
		{"openai", "/v1/responses", func(c *gin.Context, err *service.UpstreamFailoverError, stream bool) {
			(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, err, stream)
		}},
		{"openai messages", "/v1/messages", func(c *gin.Context, err *service.UpstreamFailoverError, stream bool) {
			(&OpenAIGatewayHandler{}).handleAnthropicFailoverExhausted(c, err, stream)
		}},
		{"chat completions", "/v1/chat/completions", func(c *gin.Context, err *service.UpstreamFailoverError, stream bool) {
			(&GatewayHandler{}).handleCCFailoverExhausted(c, err, stream)
		}},
		{"responses", "/v1/responses", func(c *gin.Context, err *service.UpstreamFailoverError, stream bool) {
			(&GatewayHandler{}).handleResponsesFailoverExhausted(c, err, stream)
		}},
		{"gemini", "/v1beta/models/fixture:generateContent", func(c *gin.Context, err *service.UpstreamFailoverError, _ bool) {
			(&GatewayHandler{}).handleGeminiFailoverExhausted(c, err)
		}},
	}
	for _, consumer := range consumers {
		t.Run(consumer.name, func(t *testing.T) {
			for _, stream := range []bool{false, true} {
				if stream && consumer.name == "gemini" {
					continue
				}
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				requestContext.Request = httptest.NewRequest(http.MethodPost, consumer.path, nil)
				requestContext.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{Kind: "model_binding_rejected", Message: service.ModelBindingRejectedClientMessage}})
				failure := &service.UpstreamFailoverError{
					StatusCode:       http.StatusUnprocessableEntity,
					Reason:           service.ModelBindingRejectedReason,
					ClientStatusCode: http.StatusUnprocessableEntity,
					ClientMessage:    service.ModelBindingRejectedClientMessage,
				}
				consumer.write(requestContext, failure, stream)
				body := recorder.Body.String()
				require.Contains(t, body, service.ModelBindingRejectedClientMessage)
				if !stream {
					require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
					if consumer.name == "gemini" {
						require.Equal(t, int64(422), gjson.Get(body, "error.code").Int())
					} else {
						require.Contains(t, body, "model_binding_rejected")
					}
				} else if consumer.path == "/v1/responses" {
					require.Contains(t, body, "event: response.failed")
				} else {
					require.Contains(t, body, "data:")
				}
				require.Zero(t, requestContext.GetInt(service.OpsUpstreamStatusCodeKey), "consumer must not stamp a synthetic upstream status")
				entry := &service.OpsInsertErrorLogInput{}
				applyOpsUpstreamFieldsFromContext(requestContext, entry)
				require.Nil(t, entry.UpstreamStatusCode)
			}
			// A real upstream 422 or 502 must not take the local branch.
			for _, status := range []int{http.StatusUnprocessableEntity, http.StatusBadGateway} {
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				requestContext.Request = httptest.NewRequest(http.MethodPost, consumer.path, nil)
				consumer.write(requestContext, &service.UpstreamFailoverError{StatusCode: status, ResponseBody: []byte(`{"error":{"message":"provider rejection"}}`)}, false)
				require.NotContains(t, recorder.Body.String(), "model_binding_rejected")
				require.NotContains(t, recorder.Body.String(), service.ModelBindingRejectedClientMessage)
				if status == http.StatusBadGateway {
					require.Equal(t, http.StatusBadGateway, recorder.Code)
				}
			}
		})
	}
}
