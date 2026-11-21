package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	payments "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/payments"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// PaymentWebhookHandler handles payment provider webhook callbacks.
type PaymentWebhookHandler struct {
	paymentService *service.PaymentService
	evidence       *payments.Store
}

// maxWebhookBodySize is the maximum allowed webhook request body size (1 MB).
const maxWebhookBodySize = 1 << 20

// NewPaymentWebhookHandler creates a new PaymentWebhookHandler.
func NewPaymentWebhookHandler(paymentService *service.PaymentService, evidence *payments.Store) *PaymentWebhookHandler {
	return &PaymentWebhookHandler{paymentService: paymentService, evidence: evidence}
}

// EasyPayNotify handles EasyPay payment notifications.
// POST /api/v1/payment/webhook/easypay
func (h *PaymentWebhookHandler) EasyPayNotify(c *gin.Context) {
	h.handleNotify(c, payment.TypeEasyPay)
}

// AlipayNotify handles Alipay payment notifications.
// POST /api/v1/payment/webhook/alipay
func (h *PaymentWebhookHandler) AlipayNotify(c *gin.Context) {
	h.handleNotify(c, payment.TypeAlipay)
}

// WxpayNotify handles WeChat Pay payment notifications.
// POST /api/v1/payment/webhook/wxpay
func (h *PaymentWebhookHandler) WxpayNotify(c *gin.Context) {
	h.handleNotify(c, payment.TypeWxpay)
}

// StripeWebhook handles Stripe webhook events.
// POST /api/v1/payment/webhook/stripe
func (h *PaymentWebhookHandler) StripeWebhook(c *gin.Context) {
	h.handleNotify(c, payment.TypeStripe)
}

// AirwallexWebhook 处理空中云汇 Webhook 事件。
// POST /api/v1/payment/webhook/airwallex
func (h *PaymentWebhookHandler) AirwallexWebhook(c *gin.Context) {
	h.handleNotify(c, payment.TypeAirwallex)
}

// handleNotify is the shared logic for all provider webhook handlers.
func (h *PaymentWebhookHandler) handleNotify(c *gin.Context, providerKey string) {
	var rawBody string
	if c.Request.Method == http.MethodGet {
		// GET callbacks (e.g. EasyPay) pass params as URL query string
		rawBody = c.Request.URL.RawQuery
	} else {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize))
		if err != nil {
			slog.Error("[Payment Webhook] failed to read body", "provider", providerKey, "error", err)
			c.String(http.StatusBadRequest, "failed to read body")
			return
		}
		rawBody = string(body)
	}

	// Extract out_trade_no to look up the order's specific provider instance.
	// This is needed when multiple instances of the same provider exist (e.g. multiple EasyPay accounts).
	outTradeNo := extractOutTradeNo(rawBody, providerKey)

	ctx := c.Request.Context()
	var providers []payment.Provider
	var err error
	var intent string
	if providerKey == payment.TypeStripe && outTradeNo == "" {
		intent = stripeReversalIntent(rawBody)
	}
	if intent != "" {
		providers, err = h.paymentService.GetStripeReversalWebhookProviders(ctx, intent)
	} else {
		providers, err = h.paymentService.GetWebhookProviders(ctx, providerKey, outTradeNo)
	}
	if err != nil || len(providers) == 0 {
		slog.Warn("[Payment Webhook] provider not found", "provider", providerKey, "outTradeNo", outTradeNo, "error", err)
		c.String(http.StatusServiceUnavailable, "payment provider unavailable")
		return
	}

	headers := make(map[string]string)
	for k := range c.Request.Header {
		headers[strings.ToLower(k)] = c.GetHeader(k)
	}

	resolvedProviderKey, notification, err := verifyNotificationWithProviders(ctx, providers, rawBody, headers)
	if err == nil && resolvedProviderKey != providerKey {
		err = errors.New("verified payment provider does not match the callback route")
	}
	if err != nil {
		slog.Error("[Payment Webhook] verify failed", "provider", providerKey, "error", err, "method", c.Request.Method, "bodyLen", len(rawBody))
		c.String(http.StatusBadRequest, "verify failed")
		return
	}

	// nil notification means irrelevant event (e.g. Stripe non-payment event); return success.
	if notification == nil {
		writeSuccessResponse(c, resolvedProviderKey)
		return
	}

	if err := h.dispatchVerified(ctx, resolvedProviderKey, notification); err != nil {
		// Unknown order: ack with 2xx so the provider stops retrying. This
		// guards against foreign environments whose webhook endpoints are
		// (mis)configured to point at us — without a 2xx, the provider will
		// retry for days and spam our error logs. We still emit a WARN so the
		// event is discoverable in logs.
		if errors.Is(err, service.ErrOrderNotFound) {
			slog.Warn("[Payment Webhook] unknown order, acking to stop retries",
				"provider", resolvedProviderKey,
				"outTradeNo", notification.OrderID,
				"tradeNo", notification.TradeNo,
			)
			writeSuccessResponse(c, resolvedProviderKey)
			return
		}
		slog.Error("[Payment Webhook] handle notification failed", "provider", resolvedProviderKey, "error", err)
		c.String(http.StatusInternalServerError, "handle failed")
		return
	}

	writeSuccessResponse(c, resolvedProviderKey)
}

func (h *PaymentWebhookHandler) dispatchVerified(ctx context.Context, provider string, n *payment.PaymentNotification) error {
	if h.evidence != nil {
		exact, err := h.evidence.ExactWebhook(ctx, provider, n)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrOrderNotFound
		}
		if err != nil {
			return err
		}
		if exact {
			return h.evidence.HandleVerified(ctx, provider, n)
		}
	}
	return h.paymentService.HandlePaymentNotification(ctx, n, provider)
}

// extractOutTradeNo parses the webhook body to find the out_trade_no.
// This allows looking up the correct provider instance before verification.
func extractOutTradeNo(rawBody, providerKey string) string {
	switch providerKey {
	case payment.TypeEasyPay, payment.TypeAlipay:
		values, err := url.ParseQuery(rawBody)
		if err == nil {
			return values.Get("out_trade_no")
		}
	case payment.TypeAirwallex:
		var payload struct {
			Data struct {
				Object struct {
					MerchantOrderID string `json:"merchant_order_id"`
				} `json:"object"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(rawBody), &payload); err == nil {
			return strings.TrimSpace(payload.Data.Object.MerchantOrderID)
		}
	case payment.TypeStripe:
		event := stripeWebhookHint(rawBody)
		switch event.Type {
		case "payment_intent.succeeded", "payment_intent.payment_failed":
			return strings.TrimSpace(event.Data.Object.Metadata["orderId"])
		case "refund.created", "refund.updated":
			return strings.TrimSpace(event.Data.Object.Metadata["omnihive_refund_order"])
		}
	}
	// For other providers (Stripe, Alipay direct, WxPay direct), the registry
	// typically has only one instance, so no instance lookup is needed.
	return ""
}

type stripeWebhookLookup struct {
	Type string
	Data struct {
		Object struct {
			Metadata map[string]string
			Intent   json.RawMessage `json:"payment_intent"`
		}
	}
}

func stripeWebhookHint(rawBody string) stripeWebhookLookup {
	var event stripeWebhookLookup
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return stripeWebhookLookup{}
	}
	return event
}

func stripeReversalIntent(rawBody string) string {
	event := stripeWebhookHint(rawBody)
	switch event.Type {
	case "refund.created", "refund.updated", "charge.dispute.closed":
		var id string
		if err := json.Unmarshal(event.Data.Object.Intent, &id); err == nil {
			return strings.TrimSpace(id)
		}
		var intent struct{ ID string }
		if err := json.Unmarshal(event.Data.Object.Intent, &intent); err == nil {
			return strings.TrimSpace(intent.ID)
		}
	}
	return ""
}

func verifyNotificationWithProviders(ctx context.Context, providers []payment.Provider, rawBody string, headers map[string]string) (string, *payment.PaymentNotification, error) {
	var lastErr error
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		notification, err := provider.VerifyNotification(ctx, rawBody, headers)
		if err != nil {
			lastErr = err
			continue
		}
		return provider.ProviderKey(), notification, nil
	}
	if lastErr != nil {
		return "", nil, lastErr
	}
	return "", nil, fmt.Errorf("no webhook provider could verify notification")
}

// wxpaySuccessResponse is the JSON response expected by WeChat Pay webhook.
type wxpaySuccessResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WeChat Pay webhook success response constants.
const (
	wxpaySuccessCode    = "SUCCESS"
	wxpaySuccessMessage = "成功"
)

// writeSuccessResponse 返回各支付服务商要求的成功响应。
// 微信支付需要 JSON {"code":"SUCCESS","message":"成功"}；
// Stripe 和空中云汇接受空 200，其它服务商接受纯文本 "success"。
func writeSuccessResponse(c *gin.Context, providerKey string) {
	switch providerKey {
	case payment.TypeWxpay:
		c.JSON(http.StatusOK, wxpaySuccessResponse{Code: wxpaySuccessCode, Message: wxpaySuccessMessage})
	case payment.TypeStripe, payment.TypeAirwallex:
		c.String(http.StatusOK, "")
	default:
		c.String(http.StatusOK, "success")
	}
}
