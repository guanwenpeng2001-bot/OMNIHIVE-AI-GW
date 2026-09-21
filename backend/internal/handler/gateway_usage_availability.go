package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// usageAvailabilityFields renders the credential-availability part of the
// /v1/usage payload. Every mode includes it so a client can tell whether the
// key can actually be called (isValid) and why not (unavailable_reason), while
// expired and quota-exhausted keys keep being able to read their own usage.
//
// expires_at is always present (null when the key never expires) so callers do
// not have to special-case modes.
func usageAvailabilityFields(apiKey *service.APIKey) gin.H {
	availability := apiKey.Callability(timezone.Now())
	fields := gin.H{
		"isValid":           availability.Callable,
		"status":            apiKey.Status,
		"expires_at":        apiKey.ExpiresAt,
		"days_until_expiry": apiKey.GetDaysUntilExpiry(),
	}
	if !availability.Callable {
		fields["unavailable_reason"] = string(availability.Reason)
	}
	return fields
}
