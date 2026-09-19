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
func TestGatewayDiagnosticProjectionReuse(t *testing.T) {
	for _, code := range []string{"model_not_found", "rate_limit_exceeded", ""} {
		t.Run(code, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			setOpsSelectedAccount(requestContext, 919, "openai")
			(&GatewayHandler{}).errorResponseWithCode(requestContext, http.StatusBadRequest, "invalid_request_error", code, "Request rejected")
			parsed := parseOpsErrorResponse(recorder.Body.Bytes())
			require.Equal(t, "invalid_request_error", parsed.ErrorType)
			require.Equal(t, code, parsed.Code)
			require.Equal(t, "Request rejected", parsed.Message)
			require.NotContains(t, recorder.Body.String(), "919")
			var response map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Len(t, response, 2)
			require.Equal(t, "error", response["type"])
			stream := "event: error\ndata: " + recorder.Body.String() + "\n\n"
			streamParsed := parseOpsErrorResponse([]byte(stream))
			require.Equal(t, parsed.ErrorType, streamParsed.ErrorType)
			require.Equal(t, parsed.Code, streamParsed.Code)
			require.Equal(t, parsed.Message, streamParsed.Message)
		})
	}
	for _, code := range []any{" spaced_code ", 429, nil} {
		payload, err := json.Marshal(gin.H{"error": gin.H{"type": "rate_limit_error", "message": "Retry later", "code": code}})
		require.NoError(t, err)
		ordinary := parseOpsErrorResponse(payload)
		stream := parseOpsErrorResponse([]byte("event: error\ndata: " + string(payload) + "\n\n"))
		require.Equal(t, ordinary.Code, stream.Code)
	}
}
