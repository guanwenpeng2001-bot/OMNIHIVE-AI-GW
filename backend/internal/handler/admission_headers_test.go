//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newAdmissionHeaderContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return c, recorder
}

func TestWriteAdmissionRejectionHeaders_RPMRejectionExposesOnlyUserScopeFacts(t *testing.T) {
	c, recorder := newAdmissionHeaderContext()

	err := service.ErrGroupRPMExceeded.WithMetadata(map[string]string{
		service.AdmissionWindowResetsAtMetadataKey: time.Now().Add(12 * time.Second).UTC().Format(time.RFC3339),
		service.AdmissionRateLimitLimitKey:         "60",
		service.AdmissionRateLimitUsedKey:          "60",
	})
	writeAdmissionRejectionHeaders(c, err)

	require.Equal(t, "12", recorder.Header().Get("Retry-After"))
	require.Equal(t, "12", recorder.Header().Get("X-RateLimit-Reset-Requests"))
	require.Equal(t, "60", recorder.Header().Get("X-RateLimit-Limit-Requests"))
	require.Equal(t, "0", recorder.Header().Get("X-RateLimit-Remaining-Requests"))
}

func TestWriteAdmissionRejectionHeaders_NeverInventsOrLeaks(t *testing.T) {
	t.Run("no window evidence means no retry hint", func(t *testing.T) {
		c, recorder := newAdmissionHeaderContext()
		writeAdmissionRejectionHeaders(c, service.ErrInsufficientBalance)
		require.Empty(t, recorder.Header().Get("Retry-After"),
			"balance exhaustion has no time window, so no Retry-After may be fabricated")
		require.Empty(t, recorder.Header().Get("X-RateLimit-Reset-Requests"))
	})

	t.Run("upstream account quota facts never reach the client", func(t *testing.T) {
		c, recorder := newAdmissionHeaderContext()
		upstreamOnly := service.ErrGroupRPMExceeded.WithMetadata(map[string]string{
			"upstream_account_remaining": "0",
			"upstream_account_reset_at":  time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
		writeAdmissionRejectionHeaders(c, upstreamOnly)
		for _, name := range []string{
			"Retry-After",
			"X-RateLimit-Reset-Requests",
			"X-RateLimit-Limit-Requests",
			"X-RateLimit-Remaining-Requests",
			"upstream_account_remaining",
			"upstream_account_reset_at",
		} {
			require.Empty(t, recorder.Header().Get(name), "header %s must not be emitted", name)
		}
	})
}

func TestWriteAdmissionRejectionHeaders_OverloadUsesBoundedBackoff(t *testing.T) {
	c, recorder := newAdmissionHeaderContext()
	writeAdmissionRejectionHeaders(c, service.ErrGatewayOverloaded)
	require.Equal(t, "1", recorder.Header().Get("Retry-After"),
		"overload sheds load with the documented minimum retry hint")
}

func TestBillingErrorDetails_GatewayOverloadMapsTo503(t *testing.T) {
	for _, err := range []error{service.ErrGatewayOverloaded, service.ErrGatewayAdmissionTimeout} {
		status, code, message, retryAfter := billingErrorDetails(err)
		require.Equal(t, http.StatusServiceUnavailable, status, "status for %v", err)
		require.NotEmpty(t, code)
		require.NotEmpty(t, message)
		require.Zero(t, retryAfter, "the overload hint is written as a header, not as this value")
	}
}

func TestBillingErrorDetails_APIKeyWindowsUseRealResetTimes(t *testing.T) {
	resetAt := time.Now().Add(90 * time.Second).UTC().Format(time.RFC3339)
	for name, err := range map[string]error{
		"5h": service.ErrAPIKeyRateLimit5hExceeded.WithMetadata(map[string]string{
			service.AdmissionWindowResetsAtMetadataKey: resetAt,
		}),
		"1d": service.ErrAPIKeyRateLimit1dExceeded.WithMetadata(map[string]string{
			service.AdmissionWindowResetsAtMetadataKey: resetAt,
		}),
		"7d": service.ErrAPIKeyRateLimit7dExceeded.WithMetadata(map[string]string{
			service.AdmissionWindowResetsAtMetadataKey: resetAt,
		}),
	} {
		status, code, _, retryAfter := billingErrorDetails(err)
		require.Equal(t, http.StatusTooManyRequests, status, name)
		require.Equal(t, "rate_limit_exceeded", code, name)
		require.Equal(t, 90, retryAfter, name)
	}
}
