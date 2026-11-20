package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/gatewayerror"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func writeGatewayTPMWSError(ctx context.Context, conn *coderws.Conn, err error) {
	code := "TPM_STATE_UNAVAILABLE"
	var exceeded *service.TPMLimitExceeded
	if errors.As(err, &exceeded) {
		code = exceeded.Error()
	} else if errors.Is(err, service.ErrTPMEstimateUnavailable) {
		code = "TPM_ESTIMATE_UNAVAILABLE"
	} else if errors.Is(err, service.ErrTPMReservationConflict) {
		code = "TPM_RESERVATION_CONFLICT"
	}
	payload, _ := json.Marshal(gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "code": code, "message": "token admission rejected for this turn"}})
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = conn.Write(writeCtx, coderws.MessageText, payload)
}

func writeGatewayTPMRequestError(c *gin.Context, err error) bool {
	var exceeded *service.TPMLimitExceeded
	if errors.As(err, &exceeded) && exceeded.Scope == service.TPMScopeAccount {
		return false
	}
	return writeGatewayTPMError(c, err)
}

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
