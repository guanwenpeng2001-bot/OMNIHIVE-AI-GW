package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment"
	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

// Stripe constants.
const (
	stripeEventPaymentSuccess = "payment_intent.succeeded"
	stripeEventPaymentFailed  = "payment_intent.payment_failed"
)

// Stripe implements the payment.CancelableProvider interface for Stripe payments.
type Stripe struct {
	instanceID string
	config     map[string]string

	mu          sync.Mutex
	initialized bool
	sc          *stripe.Client
}

// NewStripe creates a new Stripe provider instance.
func NewStripe(instanceID string, config map[string]string) (*Stripe, error) {
	if config["secretKey"] == "" {
		return nil, fmt.Errorf("stripe config missing required key: secretKey")
	}
	cfg := cloneStringMap(config)
	currency, err := payment.NormalizePaymentCurrency(cfg["currency"])
	if err != nil {
		return nil, fmt.Errorf("stripe config currency: %w", err)
	}
	cfg["currency"] = currency
	return &Stripe{
		instanceID: instanceID,
		config:     cfg,
	}, nil
}

func (s *Stripe) ensureInit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		s.sc = stripe.NewClient(s.config["secretKey"])
		s.initialized = true
	}
}

// GetPublishableKey returns the publishable key for frontend use.
func (s *Stripe) GetPublishableKey() string {
	return s.config["publishableKey"]
}

func (s *Stripe) Name() string        { return "Stripe" }
func (s *Stripe) ProviderKey() string { return payment.TypeStripe }
func (s *Stripe) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeStripe}
}

func (s *Stripe) MerchantIdentityMetadata() map[string]string {
	if s == nil {
		return nil
	}
	return map[string]string{"currency": s.currency()}
}

func (s *Stripe) currency() string {
	if s == nil {
		return payment.DefaultPaymentCurrency
	}
	currency, err := payment.NormalizePaymentCurrency(s.config["currency"])
	if err != nil {
		return payment.DefaultPaymentCurrency
	}
	return currency
}

// stripePaymentMethodTypes maps our PaymentType to Stripe payment_method_types.
var stripePaymentMethodTypes = map[string][]string{
	payment.TypeCard:   {"card"},
	payment.TypeAlipay: {"alipay"},
	payment.TypeWxpay:  {"wechat_pay"},
	payment.TypeLink:   {"link"},
}

// CreatePayment creates a Stripe PaymentIntent.
func (s *Stripe) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	s.ensureInit()

	currency := s.currency()
	amountInMinorUnit, err := payment.AmountToMinorUnit(req.Amount, currency)
	if err != nil {
		return nil, fmt.Errorf("stripe create payment: %w", err)
	}

	// Collect all Stripe payment_method_types from the instance's configured sub-methods
	methods := resolveStripeMethodTypes(req.InstanceSubMethods)

	pmTypes := make([]*string, len(methods))
	for i, m := range methods {
		pmTypes[i] = stripe.String(m)
	}

	params := &stripe.PaymentIntentCreateParams{
		Amount:             stripe.Int64(amountInMinorUnit),
		Currency:           stripe.String(strings.ToLower(currency)),
		PaymentMethodTypes: pmTypes,
		Description:        stripe.String(req.Subject),
		Metadata:           map[string]string{"orderId": req.OrderID},
	}

	// WeChat Pay requires payment_method_options with client type
	if hasStripeMethod(methods, "wechat_pay") {
		params.PaymentMethodOptions = &stripe.PaymentIntentCreatePaymentMethodOptionsParams{
			WeChatPay: &stripe.PaymentIntentCreatePaymentMethodOptionsWeChatPayParams{
				Client: stripe.String("web"),
			},
		}
	}

	params.SetIdempotencyKey(fmt.Sprintf("pi-%s", req.OrderID))
	params.Context = ctx

	pi, err := s.sc.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe create payment: %w", err)
	}

	return &payment.CreatePaymentResponse{
		TradeNo:      pi.ID,
		ClientSecret: pi.ClientSecret,
		Currency:     currency,
	}, nil
}

// QueryOrder retrieves a PaymentIntent by ID.
func (s *Stripe) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	s.ensureInit()

	pi, err := s.sc.V1PaymentIntents.Retrieve(ctx, tradeNo, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe query order: %w", err)
	}

	status := payment.ProviderStatusPending
	switch pi.Status {
	case stripe.PaymentIntentStatusSucceeded:
		status = payment.ProviderStatusPaid
	case stripe.PaymentIntentStatusCanceled:
		status = payment.ProviderStatusFailed
	}

	currency := stripeIntentCurrency(pi.Currency, s.currency())
	amount := pi.Amount
	if status == payment.ProviderStatusPaid {
		if pi.Currency == "" || currency != s.currency() {
			return nil, fmt.Errorf("stripe payment currency mismatch")
		}
		amount = pi.AmountReceived
	}
	return &payment.QueryOrderResponse{
		TradeNo:    pi.ID,
		InstanceID: s.instanceID, Currency: currency, Minor: amount,
		Status: status,
		Amount: payment.LegacyCallbackAmount(amount, currency),
		Metadata: map[string]string{
			"currency": currency,
		},
	}, nil
}

// VerifyNotification verifies a Stripe webhook event.
func (s *Stripe) VerifyNotification(_ context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	s.ensureInit()

	webhookSecret := s.config["webhookSecret"]
	if webhookSecret == "" {
		return nil, fmt.Errorf("stripe webhookSecret not configured")
	}

	sig := headers["stripe-signature"]
	if sig == "" {
		return nil, fmt.Errorf("stripe notification missing stripe-signature header")
	}

	event, err := webhook.ConstructEvent([]byte(rawBody), sig, webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("stripe verify notification: %w", err)
	}

	switch event.Type {
	case stripeEventPaymentSuccess:
		return s.parseIntent(&event, payment.ProviderStatusSuccess, rawBody)
	case stripeEventPaymentFailed:
		return s.parseIntent(&event, payment.ProviderStatusFailed, rawBody)
	case "refund.created", "refund.updated", "charge.dispute.closed":
		return s.parseReversal(&event, rawBody)
	}

	return nil, nil
}

func (s *Stripe) parseIntent(event *stripe.Event, status string, rawBody string) (*payment.PaymentNotification, error) {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
		return nil, fmt.Errorf("stripe parse payment_intent: %w", err)
	}
	currency, err := payment.NormalizePaymentCurrency(string(pi.Currency))
	if err != nil || pi.Currency == "" || currency != s.currency() {
		return nil, fmt.Errorf("stripe notification currency mismatch")
	}
	minor := pi.Amount
	if status == payment.ProviderStatusSuccess {
		minor = pi.AmountReceived
	}
	if minor <= 0 || event.ID == "" || pi.ID == "" || pi.Metadata["orderId"] == "" {
		return nil, fmt.Errorf("incomplete stripe payment notification")
	}
	return &payment.PaymentNotification{
		EventID: event.ID, EffectID: pi.ID, InstanceID: s.instanceID, Currency: currency, Minor: minor,
		TradeNo: pi.ID,
		OrderID: pi.Metadata["orderId"],
		Amount:  payment.LegacyCallbackAmount(minor, currency),
		Status:  status,
		RawData: rawBody,
		Metadata: map[string]string{
			"currency": currency,
		},
	}, nil
}

// Refund creates a Stripe refund.
func (s *Stripe) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	s.ensureInit()

	amountInMinorUnit, err := payment.AmountToMinorUnit(req.Amount, s.currency())
	if err != nil {
		return nil, fmt.Errorf("stripe refund: %w", err)
	}

	params := &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(req.TradeNo),
		Amount:        stripe.Int64(amountInMinorUnit),
		Reason:        stripe.String(string(stripe.RefundReasonRequestedByCustomer)),
	}
	params.AddMetadata("omnihive_refund_order", req.OrderID)
	params.SetIdempotencyKey(fmt.Sprintf("re-%s-%d", req.OrderID, amountInMinorUnit))
	params.Context = ctx

	r, err := s.sc.V1Refunds.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe refund: %w", err)
	}

	refundStatus := payment.ProviderStatusPending
	if r.Status == stripe.RefundStatusSucceeded {
		refundStatus = payment.ProviderStatusSuccess
	}

	proof, err := s.refundResponse(r, req.TradeNo, amountInMinorUnit, refundStatus)
	if err != nil {
		// Stripe accepted the request. Preserve its ID for reconciliation
		// without treating incomplete response evidence as a final failure.
		return &payment.RefundResponse{RefundID: r.ID, Status: payment.ProviderStatusPending}, err
	}
	return proof, nil
}

// QueryRefund retrieves the exact refund accepted by the original admin request.
func (s *Stripe) QueryRefund(ctx context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	s.ensureInit()
	id := strings.TrimSpace(req.RefundID)
	if id == "" {
		return nil, fmt.Errorf("stripe refund ID is missing; reconcile against the provider statement")
	}
	minor, err := payment.AmountToMinorUnit(req.Amount, s.currency())
	if err != nil {
		return nil, err
	}
	r, err := s.sc.V1Refunds.Retrieve(ctx, id, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe query refund: %w", err)
	}
	if r.ID != id {
		return nil, fmt.Errorf("stripe query refund ID mismatch")
	}
	return s.refundResponse(r, req.TradeNo, minor, stripeRefundProviderStatus(r.Status))
}

func (s *Stripe) refundResponse(r *stripe.Refund, intent string, minor int64, status string) (*payment.RefundResponse, error) {
	currency, err := payment.NormalizePaymentCurrency(string(r.Currency))
	if err != nil || r.Currency == "" || currency != s.currency() ||
		r.ID == "" || r.PaymentIntent == nil || r.PaymentIntent.ID != intent || r.Amount != minor {
		return nil, fmt.Errorf("stripe refund evidence mismatch")
	}
	return &payment.RefundResponse{RefundID: r.ID, Status: status, Minor: minor, Currency: currency}, nil
}

func stripeRefundProviderStatus(status stripe.RefundStatus) string {
	switch status {
	case stripe.RefundStatusSucceeded:
		return payment.ProviderStatusSuccess
	case stripe.RefundStatusFailed, stripe.RefundStatusCanceled:
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusPending
	}
}

func stripeIntentCurrency(raw stripe.Currency, fallback string) string {
	currency, err := payment.NormalizePaymentCurrency(string(raw))
	if err != nil || currency == payment.DefaultPaymentCurrency && strings.TrimSpace(string(raw)) == "" {
		normalizedFallback, fallbackErr := payment.NormalizePaymentCurrency(fallback)
		if fallbackErr == nil {
			return normalizedFallback
		}
		return payment.DefaultPaymentCurrency
	}
	return currency
}

// resolveStripeMethodTypes converts instance supported_types (comma-separated)
// into Stripe API payment_method_types. Falls back to ["card"] if empty.
func resolveStripeMethodTypes(instanceSubMethods string) []string {
	if instanceSubMethods == "" {
		return []string{"card"}
	}
	var methods []string
	for _, t := range strings.Split(instanceSubMethods, ",") {
		t = strings.TrimSpace(t)
		if mapped, ok := stripePaymentMethodTypes[t]; ok {
			methods = append(methods, mapped...)
		}
	}
	if len(methods) == 0 {
		return []string{"card"}
	}
	return methods
}

// hasStripeMethod checks if the given Stripe method list contains the target method.
func hasStripeMethod(methods []string, target string) bool {
	for _, m := range methods {
		if m == target {
			return true
		}
	}
	return false
}

// CancelPayment cancels a pending PaymentIntent.
func (s *Stripe) CancelPayment(ctx context.Context, tradeNo string) error {
	s.ensureInit()

	_, err := s.sc.V1PaymentIntents.Cancel(ctx, tradeNo, nil)
	if err != nil {
		return fmt.Errorf("stripe cancel payment: %w", err)
	}
	return nil
}

// Ensure interface compliance.
var (
	_ payment.Provider                 = (*Stripe)(nil)
	_ payment.CancelableProvider       = (*Stripe)(nil)
	_ payment.MerchantIdentityProvider = (*Stripe)(nil)
)

// Only terminal succeeded refunds and lost disputes change local funding.
func (s *Stripe) parseReversal(event *stripe.Event, raw string) (*payment.PaymentNotification, error) {
	var obj struct {
		ID       string
		Status   string
		Amount   int64
		Currency string
		Intent   *stripe.PaymentIntent `json:"payment_intent"`
		Metadata map[string]string     `json:"metadata"`
	}
	if err := json.Unmarshal(event.Data.Raw, &obj); err != nil {
		return nil, err
	}
	kind := "refund"
	if event.Type == "charge.dispute.closed" {
		if obj.Status != "lost" {
			return nil, nil
		}
		kind = "chargeback"
	} else if obj.Status != "succeeded" {
		return nil, nil
	}
	cur, err := payment.NormalizePaymentCurrency(obj.Currency)
	if err != nil || obj.Currency == "" || cur != s.currency() || obj.Amount <= 0 || obj.Intent == nil || obj.Intent.ID == "" || obj.ID == "" || event.ID == "" {
		return nil, fmt.Errorf("incomplete stripe reversal evidence")
	}
	orderID := ""
	if kind == "refund" && obj.Metadata["omnihive_refund_order"] != "" {
		kind = "refund_confirmation"
		orderID = obj.Metadata["omnihive_refund_order"]
	}
	return &payment.PaymentNotification{EventID: event.ID, EffectID: obj.ID, InstanceID: s.instanceID, OrderID: orderID,
		TradeNo: obj.Intent.ID, Status: kind, Minor: obj.Amount, Currency: cur, RawData: raw}, nil
}
