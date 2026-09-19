package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Exercise the production public writers and the parser consumed by ops logging.
// Administrative attribution stays in context; it is not another public code.
func TestGatewayDiagnosticProjectionReuse(test *testing.T) {
	for _, code := range []string{"model_not_found", "rate_limit_exceeded", ""} {
		test.Run(code, func(test *testing.T) {
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			setOpsSelectedAccount(requestContext, 919, "openai")
			(&GatewayHandler{}).errorResponseWithCode(requestContext, http.StatusBadRequest, "invalid_request_error", code, "Request rejected")
			parsed := parseOpsErrorResponse(recorder.Body.Bytes())
			require.Equal(test, "invalid_request_error", parsed.ErrorType)
			require.Equal(test, code, parsed.Code)
			require.Equal(test, "Request rejected", parsed.Message)
			require.NotContains(test, recorder.Body.String(), "919")
			var response map[string]any
			require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Len(test, response, 2)
			require.Equal(test, "error", response["type"])
			stream := "event: error\ndata: " + recorder.Body.String() + "\n\n"
			streamParsed := parseOpsErrorResponse([]byte(stream))
			require.Equal(test, parsed.ErrorType, streamParsed.ErrorType)
			require.Equal(test, parsed.Code, streamParsed.Code)
			require.Equal(test, parsed.Message, streamParsed.Message)
		})
	}
	for _, code := range []any{" spaced_code ", 429, nil} {
		payload, err := json.Marshal(gin.H{"error": gin.H{"type": "rate_limit_error", "message": "Retry later", "code": code}})
		require.NoError(test, err)
		ordinary := parseOpsErrorResponse(payload)
		stream := parseOpsErrorResponse([]byte("event: error\ndata: " + string(payload) + "\n\n"))
		require.Equal(test, ordinary.Code, stream.Code)
	}
}
