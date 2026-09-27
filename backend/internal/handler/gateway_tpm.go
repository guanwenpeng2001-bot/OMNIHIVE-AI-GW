package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/gatewayerror"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// writeGatewayTPMError exposes scope and reset only, never an account identity,
// another user's activity, or the shared group's remaining capacity.
func writeGatewayTPMError(c *gin.Context, err error) bool {
	var exceeded *service.TPMLimitExceeded
	if errors.As(err, &exceeded) {
		seconds := service.RetryAfterSecondsUntil(exceeded.ResetAt, time.Now())
		if seconds > 0 {
			c.Header("Retry-After", strconv.Itoa(seconds))
		}
		gatewayerror.Write(c, http.StatusTooManyRequests, exceeded.Error(), "token budget for the admission minute is exhausted")
		return true
	}
	if errors.Is(err, service.ErrTPMEstimateUnavailable) {
		gatewayerror.Write(c, http.StatusBadRequest, "TPM_ESTIMATE_UNAVAILABLE", "a verified input and output token bound is required while a token limit is enabled")
		return true
	}
	if errors.Is(err, service.ErrTPMStateUnavailable) {
		gatewayerror.Write(c, http.StatusServiceUnavailable, "TPM_STATE_UNAVAILABLE", "token admission state is unavailable")
		return true
	}
	if errors.Is(err, service.ErrTPMReservationConflict) {
		gatewayerror.Write(c, http.StatusConflict, "TPM_RESERVATION_CONFLICT", "token reservation does not match this upstream attempt")
		return true
	}
	return false
}
