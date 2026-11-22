package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shopspring/decimal"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/paymentauditlog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/paymentorder"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/paymentproviderinstance"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/payment/provider"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/servertiming"
)

// --- Refund Flow ---

var createPaymentProviderFromInstance = provider.CreateProvider

// getOrderProviderInstance looks up the provider instance that processed this order.
// For legacy orders without provider_instance_id, it resolves only when the
// historical instance is uniquely identifiable from the stored order fields.
func (s *PaymentService) getOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return s.resolveUniqueLegacyOrderProviderInstance(ctx, o)
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, nil
	}
	return s.entClient.PaymentProviderInstance.Get(ctx, instID)
}

// getRefundOrderProviderInstance resolves the provider instance for refund paths.
// Refunds must be pinned to an explicit historical binding, so legacy
// "best-effort" provider guessing is intentionally not allowed here.
func (s *PaymentService) getRefundOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return nil, nil
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("order %d refund provider instance id is invalid: %s", o.ID, instIDStr)
	}
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, instID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, fmt.Errorf("order %d refund provider instance %s is missing", o.ID, instIDStr)
		}
		return nil, err
	}
	return inst, nil
}

func (s *PaymentService) resolveUniqueLegacyOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	paymentType := payment.GetBasePaymentType(strings.TrimSpace(o.PaymentType))
	providerKey := strings.TrimSpace(psStringValue(o.ProviderKey))
	if providerKey != "" {
		instances, err := s.entClient.PaymentProviderInstance.Query().
			Where(paymentproviderinstance.ProviderKeyEQ(providerKey)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
		if len(matched) == 1 {
			return matched[0], nil
		}
		return nil, nil
	}

	if paymentType == "" {
		return nil, nil
	}

	instances, err := s.entClient.PaymentProviderInstance.Query().
		All(ctx)
	if err != nil {
		return nil, err
	}

	matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
	if len(matched) == 1 {
		return matched[0], nil
	}
	return nil, nil
}

func psFilterLegacyOrderProviderInstances(orderPaymentType string, instances []*dbent.PaymentProviderInstance) []*dbent.PaymentProviderInstance {
	if len(instances) == 0 {
		return nil
	}
	if strings.TrimSpace(orderPaymentType) == "" {
		return instances
	}
	var matched []*dbent.PaymentProviderInstance
	for _, inst := range instances {
		if psLegacyOrderMatchesInstance(orderPaymentType, inst) {
			matched = append(matched, inst)
		}
	}
	return matched
}

func psLegacyOrderMatchesInstance(orderPaymentType string, inst *dbent.PaymentProviderInstance) bool {
	if inst == nil {
		return false
	}

	baseType := payment.GetBasePaymentType(strings.TrimSpace(orderPaymentType))
	instanceProviderKey := strings.TrimSpace(inst.ProviderKey)
	if baseType == "" {
		return false
	}

	if baseType == payment.TypeStripe {
		return instanceProviderKey == payment.TypeStripe
	}
	if instanceProviderKey == payment.TypeStripe {
		return false
	}
	if instanceProviderKey == baseType {
		return true
	}
	return payment.InstanceSupportsType(inst.SupportedTypes, baseType)
}

func (s *PaymentService) RequestRefund(ctx context.Context, oid, uid int64, reason string) error {
	o, err := s.validateRefundRequest(ctx, oid, uid)
	if err != nil {
		return err
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u.Balance < o.Amount {
		return infraerrors.BadRequest("BALANCE_NOT_ENOUGH", "refund amount exceeds balance")
	}
	nr := strings.TrimSpace(reason)
	now := time.Now()
	by := fmt.Sprintf("%d", uid)
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(oid), paymentorder.UserIDEQ(uid), paymentorder.StatusEQ(OrderStatusCompleted), paymentorder.OrderTypeEQ(payment.OrderTypeBalance)).SetStatus(OrderStatusRefundRequested).SetRefundRequestedAt(now).SetRefundRequestReason(nr).SetRefundRequestedBy(by).SetRefundAmount(o.Amount).Save(ctx)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if c == 0 {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	s.writeAuditLog(ctx, oid, "REFUND_REQUESTED", fmt.Sprintf("user:%d", uid), map[string]any{"amount": o.Amount, "reason": nr})
	return nil
}

func (s *PaymentService) validateRefundRequest(ctx context.Context, oid, uid int64) (*dbent.PaymentOrder, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != uid {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission")
	}
	if o.OrderType != payment.OrderTypeBalance {
		return nil, infraerrors.BadRequest("INVALID_ORDER_TYPE", "only balance orders can request refund")
	}
	if o.Status != OrderStatusCompleted {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only completed orders can request refund")
	}
	// Check provider instance allows user refund
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil || inst == nil {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.AllowUserRefund {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "user refund is not enabled for this provider")
	}
	return o, nil
}

func (s *PaymentService) PrepareRefund(ctx context.Context, oid int64, amt float64, reason string, force, deduct bool) (*RefundPlan, *RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	ok := []string{OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed}
	if !psSliceContains(ok, o.Status) {
		return nil, nil, infraerrors.BadRequest("INVALID_STATUS", "order status does not allow refund")
	}
	// Check provider instance allows admin refund
	inst, instErr := s.getRefundOrderProviderInstance(ctx, o)
	if instErr != nil {
		slog.Warn("refund: provider instance lookup failed", "orderID", oid, "error", instErr)
		return nil, nil, infraerrors.InternalServer("PROVIDER_LOOKUP_FAILED", "failed to look up payment provider for this order")
	}
	if inst == nil {
		// Legacy order without provider_instance_id — block refund
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.RefundEnabled {
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not enabled for this provider")
	}
	if math.IsNaN(amt) || math.IsInf(amt, 0) {
		return nil, nil, infraerrors.BadRequest("INVALID_AMOUNT", "invalid refund amount")
	}
	if amt <= 0 {
		amt = o.Amount
	}
	orderCurrency := PaymentOrderCurrency(o)
	if amt > o.Amount {
		return nil, nil, infraerrors.BadRequest("REFUND_AMOUNT_EXCEEDED", "refund amount exceeds recharge")
	}
	sourceAmount, gatewayAmount, err := refundEconomicAmounts(o, amt, orderCurrency)
	if err != nil {
		return nil, nil, err
	}
	rr := strings.TrimSpace(reason)
	if rr == "" && o.RefundRequestReason != nil {
		rr = *o.RefundRequestReason
	}
	if rr == "" {
		rr = fmt.Sprintf("refund order:%d", o.ID)
	}
	p := &RefundPlan{OrderID: oid, Order: o, RefundAmount: sourceAmount, GatewayAmount: gatewayAmount, Reason: rr, Force: force, DeductBalance: deduct, DeductionType: payment.DeductionTypeNone}
	if deduct {
		if er := s.prepDeduct(ctx, o, p, force); er != nil {
			return nil, er, nil
		}
	}
	return p, nil, nil
}

// Round to the provider's indivisible units first. The entitlement amount
// must represent the fraction of the captured transaction actually refunded.
func refundEconomicAmounts(o *dbent.PaymentOrder, request float64, currency string) (float64, float64, error) {
	original := decimal.NewFromFloat(o.Amount)
	paid := decimal.NewFromFloat(o.PayAmount)
	if !original.IsPositive() || !paid.IsPositive() {
		return 0, 0, infraerrors.BadRequest("INVALID_AMOUNT", "invalid original payment amount")
	}
	amount := paid.Mul(decimal.NewFromFloat(request)).DivRound(original, int32(payment.CurrencyMaxFractionDigits(currency)))
	if !amount.IsPositive() || amount.GreaterThan(paid) {
		return 0, 0, infraerrors.BadRequest("INVALID_AMOUNT", "refund must cover at least one provider minor unit within the captured amount")
	}
	return original.Mul(amount).DivRound(paid, 8).InexactFloat64(), amount.InexactFloat64(), nil
}

func (s *PaymentService) prepDeduct(ctx context.Context, o *dbent.PaymentOrder, p *RefundPlan, force bool) *RefundResult {
	if o.OrderType == payment.OrderTypeSubscription {
		p.DeductionType = payment.DeductionTypeSubscription
		if o.SubscriptionGroupID != nil && o.SubscriptionDays != nil {
			if o.PayAmount > 0 {
				days, _ := decimal.NewFromInt(int64(*o.SubscriptionDays)).Mul(decimal.NewFromFloat(p.GatewayAmount)).
					QuoRem(decimal.NewFromFloat(o.PayAmount), 0)
				p.SubDaysToDeduct = int(days.IntPart())
			}
			if p.SubDaysToDeduct == 0 {
				return nil
			}
			sub, err := s.subscriptionSvc.GetActiveSubscription(ctx, o.UserID, *o.SubscriptionGroupID)
			if err == nil && sub != nil {
				p.SubscriptionID = sub.ID
			} else if !force {
				return &RefundResult{Success: false, Warning: "cannot find active subscription for deduction, use force", RequireForce: true}
			} else {
				p.SubDaysToDeduct = 0
			}
		}
		return nil
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		if !force {
			return &RefundResult{Success: false, Warning: "cannot fetch user balance, use force", RequireForce: true}
		}
		return nil
	}
	p.DeductionType = payment.DeductionTypeBalance
	if u.Balance < p.RefundAmount && !force {
		return &RefundResult{Success: false, Warning: "user balance is insufficient for deduction, use force", RequireForce: true}
	}
	p.BalanceToDeduct = math.Max(0, math.Min(p.RefundAmount, u.Balance))
	return nil
}

type availableBalanceDeductor interface {
	DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error)
}

func (s *PaymentService) deductAvailableBalance(ctx context.Context, userID int64, amount float64) (float64, error) {
	repo, ok := s.userRepo.(availableBalanceDeductor)
	if !ok {
		return 0, errors.New("user repository does not support available balance deduction")
	}
	return repo.DeductAvailableBalance(ctx, userID, amount)
}

func refundRequestID(p *RefundPlan) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", p.OrderID, p.Order.OutTradeNo,
		PaymentOrderCurrency(p.Order), formatGatewayRefundAmount(p.GatewayAmount, p.Order))))
	return fmt.Sprintf("oh-%x", sum[:20])
}

func (s *PaymentService) ExecuteRefund(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	p.requestID = refundRequestID(p)
	if err := s.claimRefund(ctx, p); err != nil {
		return nil, err
	}
	resp, err := s.gwRefund(ctx, p)
	// The committed intent is sufficient to reconcile after a lost response.
	// Keep it claimed while the outcome is uncertain.
	if err != nil {
		slog.Warn("refund outcome requires confirmation", "orderID", p.OrderID, "error", err)
		return &RefundResult{Success: false, Warning: "gateway refund is pending confirmation"}, nil
	}
	return s.finishRefund(ctx, p, resp)
}

func (s *PaymentService) claimRefund(ctx context.Context, p *RefundPlan) error {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	claimedAt := time.Now().UTC().Truncate(time.Microsecond)
	claim := tx.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID),
		paymentorder.StatusIn(OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed),
		paymentorder.StatusEQ(p.Order.Status))
	s.matchRefundUpdateVersion(claim, p.Order.UpdatedAt)
	claimed, err := claim.SetStatus(OrderStatusRefunding).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).
		SetForceRefund(p.Force).ClearRefundAt().ClearFailedAt().ClearFailedReason().SetUpdatedAt(claimedAt).Save(ctx)
	if err != nil {
		return err
	}
	if claimed == 0 {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	detail := refundPendingAuditDetail{
		RequestID: p.requestID, DeductBalance: &p.DeductBalance, DeductionRollbackOK: true,
		GatewayAmount: p.GatewayAmount, DeductionType: p.DeductionType, BalanceToDeduct: p.BalanceToDeduct,
		SubDaysToDeduct: p.SubDaysToDeduct, SubscriptionID: p.SubscriptionID,
	}
	if err := writeRefundAudit(ctx, tx.Client(), p.OrderID, "REFUND_INTENT", detail); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	p.claimedAt = claimedAt
	return nil
}

func (s *PaymentService) markRefundPendingWithoutDeduction(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	pendingAt := time.Now().UTC().Truncate(time.Microsecond)
	claim := tx.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(OrderStatusRefunding))
	s.matchRefundUpdateVersion(claim, p.claimedAt)
	claimed, err := claim.SetStatus(OrderStatusRefundPending).SetUpdatedAt(pendingAt).Save(ctx)
	if err != nil {
		return nil, err
	}
	if claimed == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}
	detail := refundPendingAuditDetail{RequestID: p.requestID, RefundID: refundResponseID(resp),
		DeductBalance: &p.DeductBalance, DeductionRollbackOK: true, GatewayAmount: p.GatewayAmount,
		DeductionType: p.DeductionType, BalanceToDeduct: p.BalanceToDeduct,
		SubDaysToDeduct: p.SubDaysToDeduct, SubscriptionID: p.SubscriptionID}
	if err := writeRefundAudit(ctx, tx.Client(), p.OrderID, "REFUND_PENDING", detail); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	p.claimedAt = pendingAt
	return &RefundResult{Success: false, Warning: "gateway refund is pending confirmation"}, nil
}

func writeRefundAudit(ctx context.Context, client *dbent.Client, orderID int64, action string, data any) error {
	detail, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = client.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(orderID, 10)).
		SetAction(action).SetDetail(string(detail)).SetOperator("admin").Save(ctx)
	return err
}

func (s *PaymentService) gwRefund(ctx context.Context, p *RefundPlan) (*payment.RefundResponse, error) {
	if p.Order.PaymentTradeNo == "" {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_NO_TRADE_NO", "admin", map[string]any{"detail": "skipped"})
		return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
	}

	// Use the exact provider instance that created this order, not a random one
	// from the registry. Each instance has its own merchant credentials.
	prov, err := s.getRefundProvider(ctx, p.Order)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	if err := validateProviderSnapshotMetadata(p.Order, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_PROVIDER_METADATA_MISMATCH", "admin", map[string]any{
			"detail": err.Error(),
		})
		return nil, err
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.Refund(ctx, payment.RefundRequest{
		RequestID: p.requestID,
		TradeNo:   p.Order.PaymentTradeNo,
		OrderID:   p.Order.OutTradeNo,
		Amount:    formatGatewayRefundAmount(p.GatewayAmount, p.Order),
		Reason:    p.Reason,
	})
	finishProviderCall()
	if err != nil {
		if resp != nil && strings.TrimSpace(resp.Status) == payment.ProviderStatusPending {
			return resp, nil
		}
		return nil, err
	}
	if err := validateRefundProviderResponse(resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func formatGatewayRefundAmount(amount float64, order *dbent.PaymentOrder) string {
	return payment.FormatAmountForCurrency(amount, PaymentOrderCurrency(order))
}

func validateRefundProviderResponse(resp *payment.RefundResponse) error {
	if resp == nil {
		return fmt.Errorf("payment refund response missing")
	}
	status := strings.TrimSpace(resp.Status)
	switch status {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded, payment.ProviderStatusPending:
		return nil
	case payment.ProviderStatusFailed:
		return fmt.Errorf("payment refund failed: status %s", status)
	default:
		return fmt.Errorf("payment refund returned unknown status: %s", status)
	}
}

func (s *PaymentService) finishRefund(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	if err := validateRefundProviderResponse(resp); err != nil {
		return nil, err
	}
	result, err := s.markRefundPendingWithoutDeduction(ctx, p, resp)
	if err != nil || resp.Status == payment.ProviderStatusPending {
		return result, err
	}
	p.verifiedRefund = resp
	return s.finalizeRefundSuccess(ctx, p, OrderStatusRefundPending)
}

func (s *PaymentService) QueryAndFinalizeRefund(ctx context.Context, oid int64) (*RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status != OrderStatusRefundPending && o.Status != OrderStatusRefunding {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only in-progress refunds can be finalized")
	}
	pendingDetail, err := s.latestRefundPendingDetail(ctx, o)
	if err != nil {
		return nil, err
	}

	prov, err := s.getRefundProvider(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	if err := validateProviderSnapshotMetadata(o, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
		return nil, err
	}
	queryProvider, ok := prov.(payment.RefundQueryProvider)
	if !ok {
		return nil, infraerrors.BadRequest("REFUND_QUERY_UNSUPPORTED", "this payment provider does not support refund status query; please verify manually")
	}

	plan := s.refundFinalizePlan(o)
	if pendingDetail.RequestID != "" {
		plan.requestID = pendingDetail.RequestID
		plan.GatewayAmount = pendingDetail.GatewayAmount
	}
	queryAmount := plan.GatewayAmount
	refundID := pendingDetail.RefundID
	if refundID == "" && prov.ProviderKey() == payment.TypeAlipay {
		refundID = plan.requestID
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := queryProvider.QueryRefund(ctx, payment.RefundQueryRequest{
		TradeNo:  o.PaymentTradeNo,
		OrderID:  o.OutTradeNo,
		RefundID: refundID,
		Amount:   formatGatewayRefundAmount(queryAmount, o),
	})
	finishProviderCall()
	if err != nil {
		return nil, fmt.Errorf("query refund: %w", err)
	}
	if err := validateRefundProviderResponse(resp); err != nil {
		if resp != nil && resp.Status == payment.ProviderStatusFailed {
			return s.finalizeRefundFailed(ctx, o, err)
		}
		return nil, err
	}

	if pendingDetail.DeductBalance != nil && !*pendingDetail.DeductBalance {
		plan.DeductBalance = false
		plan.DeductionType = payment.DeductionTypeNone
		plan.BalanceToDeduct = 0
	} else if pendingDetail.RequestID != "" {
		plan.DeductionType = pendingDetail.DeductionType
		plan.BalanceToDeduct = pendingDetail.BalanceToDeduct
		plan.SubDaysToDeduct = pendingDetail.SubDaysToDeduct
		plan.SubscriptionID = pendingDetail.SubscriptionID
	} else if !pendingDetail.DeductionRollbackOK {
		plan.BalanceToDeduct = 0
		plan.SubDaysToDeduct = 0
	} else if o.OrderType == payment.OrderTypeSubscription {
		if early := s.prepDeduct(ctx, o, plan, true); early != nil {
			return early, nil
		}
	}
	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		plan.verifiedRefund = resp
		return s.finalizeRefundSuccess(ctx, plan, o.Status)
	case payment.ProviderStatusPending:
		s.writeAuditLog(ctx, oid, "REFUND_QUERY_PENDING", "admin", map[string]any{"refundID": resp.RefundID})
		return &RefundResult{Success: false, Warning: "gateway refund is still pending confirmation"}, nil
	default:
		return s.finalizeRefundFailed(ctx, o, fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status)))
	}
}

func (s *PaymentService) finalizeRefundSuccess(ctx context.Context, p *RefundPlan, expectedStatus string) (_ *RefundResult, err error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund finalization: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	txCtx := dbent.NewTxContext(ctx, tx)

	claim := tx.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(expectedStatus))
	version := p.claimedAt
	if version.IsZero() {
		version = p.Order.UpdatedAt
	}
	s.matchRefundUpdateVersion(claim, version)
	claimed, err := claim.
		SetStatus(OrderStatusRefunding).
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("claim pending refund: %w", err)
	}
	if claimed == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}

	if proof := p.verifiedRefund; proof != nil && proof.Minor > 0 {
		currency := PaymentOrderCurrency(p.Order)
		expected, err := payment.AmountToMinorUnit(formatGatewayRefundAmount(p.GatewayAmount, p.Order), currency)
		if err != nil || proof.Currency != currency || proof.Minor != expected || proof.RefundID == "" {
			return nil, infraerrors.Conflict("REFUND_EVIDENCE_MISMATCH", "provider refund amount or currency differs from the frozen intent")
		}
	}
	if err := s.applyRefundFinalDeduction(txCtx, p); err != nil {
		return nil, err
	}
	result, err := s.markRefundOkTx(txCtx, tx.Client(), p)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund finalization: %w", err)
	}
	return result, nil
}

func (s *PaymentService) refundFinalizePlan(o *dbent.PaymentOrder) *RefundPlan {
	refundAmount := o.RefundAmount
	reason := strings.TrimSpace(psStringValue(o.RefundReason))
	if reason == "" {
		reason = fmt.Sprintf("refund order:%d", o.ID)
	}
	return &RefundPlan{
		OrderID:       o.ID,
		Order:         o,
		RefundAmount:  refundAmount,
		GatewayAmount: calculateGatewayRefundAmount(o.Amount, o.PayAmount, refundAmount, PaymentOrderCurrency(o)),
		Reason:        reason,
		Force:         o.ForceRefund,
		DeductBalance: true,
		DeductionType: payment.DeductionTypeBalance,
		BalanceToDeduct: func() float64 {
			if o.OrderType == payment.OrderTypeBalance {
				return refundAmount
			}
			return 0
		}(),
	}
}

func (s *PaymentService) applyRefundFinalDeduction(ctx context.Context, p *RefundPlan) error {
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		deducted, err := s.deductAvailableBalance(ctx, p.Order.UserID, p.BalanceToDeduct)
		if err != nil {
			return fmt.Errorf("deduction: %w", err)
		}
		if p.requestID != "" && !p.Force && deducted < p.BalanceToDeduct {
			return infraerrors.Conflict("BALANCE_NOT_ENOUGH", "available balance changed before refund confirmation")
		}
		p.BalanceToDeduct = deducted
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.SubDaysToDeduct > 0 && p.SubscriptionID > 0 {
		if _, err := s.subscriptionSvc.ExtendSubscription(ctx, p.SubscriptionID, -p.SubDaysToDeduct); err != nil {
			if errors.Is(err, ErrAdjustWouldExpire) {
				if revokeErr := s.subscriptionSvc.RevokeSubscription(ctx, p.SubscriptionID); revokeErr != nil {
					return fmt.Errorf("revoke subscription: %w", revokeErr)
				}
			} else {
				return fmt.Errorf("deduct subscription days: %w", err)
			}
		}
	}
	return nil
}

func (s *PaymentService) matchRefundUpdateVersion(update *dbent.PaymentOrderUpdate, updatedAt time.Time) {
	if paymentAuditDialect(s.entClient) == dialect.Postgres {
		update.Where(paymentorder.UpdatedAtEQ(updatedAt))
	}
}

func (s *PaymentService) finalizeRefundFailed(ctx context.Context, o *dbent.PaymentOrder, gErr error) (*RefundResult, error) {
	now := time.Now()
	claim := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(o.ID), paymentorder.StatusEQ(o.Status))
	s.matchRefundUpdateVersion(claim, o.UpdatedAt)
	claimed, err := claim.SetStatus(OrderStatusRefundFailed).SetFailedAt(now).SetFailedReason(psErrMsg(gErr)).Save(ctx)
	if err != nil {
		return nil, err
	}
	if claimed == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}
	s.writeAuditLog(ctx, o.ID, "REFUND_FAILED", "admin", map[string]any{"detail": psErrMsg(gErr)})
	return &RefundResult{Success: false, Warning: "gateway refund failed: " + psErrMsg(gErr)}, nil
}

type refundPendingAuditDetail struct {
	RequestID           string  `json:"requestID"`
	RefundID            string  `json:"refundID"`
	DeductBalance       *bool   `json:"deductBalance"`
	DeductionRollbackOK bool    `json:"deductionRollbackOK"`
	GatewayAmount       float64 `json:"gatewayAmount"`
	DeductionType       string  `json:"deductionType"`
	BalanceToDeduct     float64 `json:"balanceToDeduct"`
	SubDaysToDeduct     int     `json:"subDaysToDeduct"`
	SubscriptionID      int64   `json:"subscriptionID"`
}

func (s *PaymentService) latestRefundPendingDetail(ctx context.Context, order *dbent.PaymentOrder) (refundPendingAuditDetail, error) {
	action := "REFUND_PENDING"
	if order.Status == OrderStatusRefunding {
		action = "REFUND_INTENT"
	}
	logEntry, err := s.entClient.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ(action)).
		Order(paymentauditlog.ByID(sql.OrderDesc())).First(ctx)
	detail := refundPendingAuditDetail{DeductionRollbackOK: true}
	if dbent.IsNotFound(err) && action == "REFUND_PENDING" {
		if paymentAuditDialect(s.entClient) != dialect.Postgres {
			return detail, nil // compatibility for legacy test fixtures
		}
		// The original preference is unknown for historical rows without evidence.
		preserve := false
		detail.DeductBalance = &preserve
		return detail, nil
	}
	if err != nil {
		return detail, err
	}
	if err := json.Unmarshal([]byte(logEntry.Detail), &detail); err != nil {
		return detail, err
	}
	if detail.DeductBalance == nil && paymentAuditDialect(s.entClient) == dialect.Postgres {
		preserve := false
		detail.DeductBalance = &preserve
	}
	return detail, nil
}

// getRefundProvider creates a provider using the order's original instance config.
// Delegates to getOrderProvider which handles instance lookup and fallback.
func (s *PaymentService) getRefundProvider(ctx context.Context, o *dbent.PaymentOrder) (payment.Provider, error) {
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("refund provider instance is unavailable for order %d", o.ID)
	}
	return s.createProviderFromInstance(ctx, inst)
}

func (s *PaymentService) markRefundOkTx(ctx context.Context, client *dbent.Client, p *RefundPlan) (*RefundResult, error) {
	fs := OrderStatusRefunded
	if p.RefundAmount < p.Order.Amount {
		fs = OrderStatusPartiallyRefunded
	}
	now := time.Now()
	_, err := client.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(fs).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetRefundAt(now).SetForceRefund(p.Force).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark refund: %w", err)
	}
	fields := map[string]any{"refundAmount": p.RefundAmount, "reason": p.Reason, "balanceDeducted": p.BalanceToDeduct, "subDaysDeducted": p.SubDaysToDeduct, "force": p.Force, "deductBalance": p.DeductBalance, "requestID": p.requestID}
	if proof := p.verifiedRefund; proof != nil && proof.RefundID != "" && proof.Minor > 0 {
		fields["refundID"], fields["providerMinor"], fields["providerCurrency"] = proof.RefundID, proof.Minor, proof.Currency
	}
	if err := writeRefundAudit(ctx, client, p.OrderID, "REFUND_SUCCESS", fields); err != nil {
		return nil, fmt.Errorf("write refund audit: %w", err)
	}
	return &RefundResult{Success: true, BalanceDeducted: p.BalanceToDeduct, SubDaysDeducted: p.SubDaysToDeduct}, nil
}

func refundResponseID(resp *payment.RefundResponse) string {
	if resp == nil {
		return ""
	}
	return strings.TrimSpace(resp.RefundID)
}
