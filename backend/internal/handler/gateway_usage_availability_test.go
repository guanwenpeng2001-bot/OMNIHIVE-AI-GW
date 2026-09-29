package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// callUsageRoute drives the real /v1/usage handler with the API key context the
// gateway middleware would have set.
func callUsageRoute(t *testing.T, apiKey *service.APIKey) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID})

	(&GatewayHandler{}).Usage(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

func quotaLimitedKey(status string, quota, quotaUsed float64, expiresAt *time.Time) *service.APIKey {
	return &service.APIKey{
		ID:        11,
		UserID:    7,
		Status:    status,
		Quota:     quota,
		QuotaUsed: quotaUsed,
		ExpiresAt: expiresAt,
		User:      &service.User{ID: 7, Status: service.StatusActive, Balance: 10},
	}
}

func TestUsageQuotaLimitedReportsRestrictedKeyAsUnavailableButQueryable(t *testing.T) {
	expiresAt := time.Now().Add(-2 * time.Hour)
	body := callUsageRoute(t, quotaLimitedKey(service.StatusAPIKeyExpired, 5, 5, &expiresAt))

	require.Equal(t, "quota_limited", body["mode"])
	require.Equal(t, false, body["isValid"])
	require.Equal(t, service.StatusAPIKeyExpired, body["status"])
	require.Equal(t, string(service.APIKeyUnavailableReasonExpired), body["unavailable_reason"])
	require.NotNil(t, body["expires_at"])
}

func TestUsageQuotaLimitedReportsExhaustedQuotaAsUnavailable(t *testing.T) {
	body := callUsageRoute(t, quotaLimitedKey(service.StatusAPIKeyQuotaExhausted, 5, 5, nil))

	require.Equal(t, false, body["isValid"])
	require.Equal(t, string(service.APIKeyUnavailableReasonQuotaExhausted), body["unavailable_reason"])
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, body["status"])
	// expires_at stays in the payload even for a key that never expires.
	require.Contains(t, body, "expires_at")
	require.Nil(t, body["expires_at"])
}

func TestUsageQuotaLimitedReportsCallableKeyAfterRestrictionIsCleared(t *testing.T) {
	// Same key as the exhausted case, after the owner (or an admin) raised the
	// quota: the restriction is gone and isValid flips back to true.
	key := quotaLimitedKey(service.StatusAPIKeyActive, 20, 5, nil)
	key.User.Balance = 0 // wallet/subscription limits are reported separately, not as key validity

	body := callUsageRoute(t, key)

	require.Equal(t, true, body["isValid"])
	require.Equal(t, service.StatusAPIKeyActive, body["status"])
	require.NotContains(t, body, "unavailable_reason")
	require.Contains(t, body, "expires_at")
}

func TestUsageQuotaLimitedReportsManuallyDisabledKeyAsUnavailable(t *testing.T) {
	body := callUsageRoute(t, quotaLimitedKey(service.StatusAPIKeyDisabled, 5, 0, nil))

	require.Equal(t, false, body["isValid"])
	require.Equal(t, string(service.APIKeyUnavailableReasonDisabled), body["unavailable_reason"])
}

func TestUsageUnrestrictedReportsExpiredKeyAsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)

	expiresAt := time.Now().Add(-time.Hour)
	apiKey := &service.APIKey{
		ID:        21,
		UserID:    7,
		Status:    service.StatusAPIKeyExpired,
		ExpiresAt: &expiresAt,
		Group:     &service.Group{ID: 9, Name: "Weekly plan", SubscriptionType: service.SubscriptionTypeSubscription},
		User:      &service.User{ID: 7, Status: service.StatusActive, Balance: 10},
	}

	(&GatewayHandler{}).usageUnrestricted(c, c.Request.Context(), apiKey, middleware.AuthSubject{UserID: 7}, nil, nil, nil)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))

	require.Equal(t, "unrestricted", body["mode"])
	require.Equal(t, false, body["isValid"])
	require.Equal(t, string(service.APIKeyUnavailableReasonExpired), body["unavailable_reason"])
	require.NotNil(t, body["expires_at"])
	require.Equal(t, service.StatusAPIKeyExpired, body["status"])
}

func TestUsageUnrestrictedReportsCallableKeyWithoutExpiration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)

	apiKey := &service.APIKey{
		ID:     22,
		UserID: 7,
		Status: service.StatusAPIKeyActive,
		Group:  &service.Group{ID: 9, Name: "Weekly plan", SubscriptionType: service.SubscriptionTypeSubscription},
		User:   &service.User{ID: 7, Status: service.StatusActive, Balance: 10},
	}

	(&GatewayHandler{}).usageUnrestricted(c, c.Request.Context(), apiKey, middleware.AuthSubject{UserID: 7}, nil, nil, nil)

	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))

	require.Equal(t, true, body["isValid"])
	require.NotContains(t, body, "unavailable_reason")
	require.Contains(t, body, "expires_at")
	require.Nil(t, body["expires_at"])
	require.Equal(t, float64(-1), body["days_until_expiry"])
}
