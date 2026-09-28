package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayTPM_AccountExhaustionUsesBoundedReselection(t *testing.T) {
	state := NewFailoverState(2, false)
	denial := &service.UpstreamFailoverError{StatusCode: 429, TPM: &service.TPMLimitExceeded{Scope: service.TPMScopeAccount}}
	for accountID := int64(1); accountID <= 3; accountID++ {
		action := state.HandleFailoverError(context.Background(), nil, accountID, service.PlatformOpenAI, 0, denial)
		if accountID < 3 {
			require.Equal(t, FailoverContinue, action)
		} else {
			require.Equal(t, FailoverExhausted, action)
		}
	}
	require.Equal(t, 2, state.SwitchCount)
}

func TestGatewayTPM_GroupExhaustionCannotFailoverAway(t *testing.T) {
	response := httptest.NewRecorder()
	request, _ := gin.CreateTestContext(response)
	request.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	require.True(t, writeGatewayTPMRequestError(request, &service.TPMLimitExceeded{Scope: service.TPMScopeGroup, ResetAt: time.Now().Add(time.Minute)}))
	require.Equal(t, 429, response.Code)
}

func TestGatewayTPM_StorageFailureIs503(t *testing.T) {
	response := httptest.NewRecorder()
	request, _ := gin.CreateTestContext(response)
	request.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.True(t, writeGatewayTPMError(request, errors.Join(service.ErrTPMStateUnavailable, errors.New("private database detail"))))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), "TPM_STATE_UNAVAILABLE")
	require.NotContains(t, response.Body.String(), "private database detail")
	require.Empty(t, response.Header().Get("Retry-After"))
}

func TestGatewayTPM_LimitErrorsPreserveRPMHeaders(t *testing.T) {
	for _, scope := range []service.TPMScope{service.TPMScopeAPIKey, service.TPMScopeGroup, service.TPMScopeAccount} {
		t.Run(string(scope), func(t *testing.T) {
			response := httptest.NewRecorder()
			request, _ := gin.CreateTestContext(response)
			request.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			request.Header("X-RateLimit-Limit-Requests", "7")
			err := &service.TPMLimitExceeded{Scope: scope, ResetAt: time.Now().Add(30 * time.Second)}
			require.True(t, writeGatewayTPMError(request, err))
			require.Equal(t, http.StatusTooManyRequests, response.Code)
			require.Contains(t, response.Body.String(), err.Error())
			require.NotEmpty(t, response.Header().Get("Retry-After"))
			require.Equal(t, "7", response.Header().Get("X-RateLimit-Limit-Requests"))
		})
	}
}

func TestGatewayTPM_UnknownEstimateIsValidationError(t *testing.T) {
	response := httptest.NewRecorder()
	request, _ := gin.CreateTestContext(response)
	request.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	require.True(t, writeGatewayTPMError(request, service.ErrTPMEstimateUnavailable))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "TPM_ESTIMATE_UNAVAILABLE")
}
