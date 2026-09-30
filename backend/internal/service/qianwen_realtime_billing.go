package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
)

const QianwenRealtimePriceVersion = "dashscope-tts-characters-usd-2026-09-14"

var ErrQianwenRealtimeCapacity = errors.New("realtime session concurrency limit reached")
var ErrQianwenRealtimeConflict = errors.New("conflicting realtime receipt")

type QianwenRealtimeSnapshot struct {
	TokenPricing *ModelPricingDescriptor `json:"token_pricing,omitempty"`
	PriceQuote   *ModelUnitPrice         `json:"price_quote,omitempty"`
	SessionID    string                  `json:"session_id"`
	Region       string                  `json:"region"`
	PriceVersion string                  `json:"price_version"`
}

// The lease lasts longer than the absolute 120s relay + 12s handshake + 5s
// drain. Admission is serialized in PostgreSQL and works across replicas.
type QianwenRealtimeSessionRepository interface {
	CreateRealtimeSession(context.Context, *UpstreamJob) (*UpstreamJob, error)
}

// Realtime receipts must be committed with all replay inputs in one insert.
type QianwenRealtimeReceiptRepository interface {
	InsertRealtimeReceipt(context.Context, *UpstreamJob) (*UpstreamJob, error)
}

// EqualQianwenRealtimeReceipt compares immutable evidence, excluding settlement state.
func EqualQianwenRealtimeReceipt(stored, receipt *UpstreamJob) bool {
	// These fields are settlement outputs, added after the first receipt commits.
	// Compare all original tariff inputs, but do not confuse a settled replay with
	// conflicting evidence or mutate the persisted pricing explanation.
	storedBilling, receiptBilling := stored.Billing, receipt.Billing
	storedBilling.PricingState, receiptBilling.PricingState = "", ""
	storedBilling.PricingSnapshot, receiptBilling.PricingSnapshot = "", ""
	oldUsage, oldErr := CanonicalUpstreamJobBody(stored.UsageJSON)
	newUsage, newErr := CanonicalUpstreamJobBody(receipt.UsageJSON)
	return oldErr == nil && newErr == nil && bytes.Equal(oldUsage, newUsage) &&
		stored.SubmissionKey == receipt.SubmissionKey && stored.RequestFingerprint == receipt.RequestFingerprint &&
		stored.Platform == receipt.Platform && stored.Family == receipt.Family &&
		stored.UserID == receipt.UserID && stored.APIKeyID == receipt.APIKeyID && stored.AccountID == receipt.AccountID &&
		reflect.DeepEqual(stored.GroupID, receipt.GroupID) && reflect.DeepEqual(storedBilling, receiptBilling) &&
		stored.UpstreamTaskID == receipt.UpstreamTaskID && stored.ResultRef == receipt.ResultRef
}

func IsQianwenRealtimeModel(model string) bool {
	switch model {
	case "qwen3-tts-flash-realtime", "qwen3-tts-flash-realtime-2025-11-27", "qwen3-tts-flash-realtime-2025-09-18":
		return true
	}
	return false
}

func QianwenRealtimeBillingSnapshot(ctx context.Context, group *Group, account *Account, key *APIKey, model string, subscription *UserSubscription) (UpstreamJobBilling, error) {
	b := UpstreamJobBilling{RequestedModel: model}
	if group == nil || account == nil || key == nil || account.Platform != PlatformQianwen {
		return b, ErrUpstreamJobTariff
	}
	b.Model = account.GetMappedModel(model)
	_, region, err := qianwenRealtimeEndpoint(account, b.Model)
	if err != nil {
		return b, err
	}
	b.UnitPrice = 0.143353 / 10000
	if region == "singapore" {
		b.UnitPrice = 0.13 / 10000
	}
	pricingAt := time.Now()
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil && !plan.Snapshot().PricingAt.IsZero() {
		pricingAt = plan.Snapshot().PricingAt
	}
	b.RateMultiplier = group.RateMultiplier * group.PeakMultiplierAt(pricingAt)
	b.AccountRateMultiplier = account.BillingRateMultiplier()
	b.KeyQuota, b.KeyRateLimit, b.AccountQuota = key.Quota > 0, key.HasRateLimits(), account.HasAnyQuotaLimit()
	if subscription != nil {
		b.SubscriptionID = &subscription.ID
	}
	b.Realtime = &QianwenRealtimeSnapshot{Region: region, PriceVersion: QianwenRealtimePriceVersion}
	descriptor, found, err := NewModelPricingResolver(nil, nil).configuredPriceDescriptor(ctx, PricingInput{Model: model, Group: group})
	if err != nil {
		return b, err
	}
	if found {
		quote, quoteErr := descriptor.UnitPrice("character", "output", "")
		if quoteErr != nil {
			return b, quoteErr
		}
		if quote.Cost == nil {
			return b, fmt.Errorf("%w: realtime character cost missing", ErrModelPricingUnavailable)
		}
		b.UnitPrice = *quote.Sales
		b.Realtime.PriceQuote = &quote
		b.Realtime.PriceVersion = "operator-character-quote"
	}

	if !finitePositive(b.RateMultiplier) || !finitePositive(b.AccountRateMultiplier) {
		return b, ErrUpstreamJobTariff
	}
	return b, nil
}

// Only the documented Qwen3 character schema is accepted. No elapsed time,
// guessed PCM duration, text length, token fallback or unknown fields are billed.
func QianwenRealtimeCharacters(raw []byte) (int64, error) {
	var usage map[string]json.RawMessage
	if json.Unmarshal(raw, &usage) != nil || len(usage) != 1 {
		return 0, ErrUpstreamJobUnsupportedUsage
	}
	var n int64
	value, ok := usage["characters"]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &n) != nil || n < 0 || n > 2000 {
		return 0, ErrUpstreamJobUnsupportedUsage
	}
	return n, nil
}

func QianwenRealtimeCharge(j *UpstreamJob) (usageLog *UsageLog, command *UsageBillingCommand, chargeErr error) {
	defer func() { recordJobPricingFailure(j, chargeErr) }()
	b := j.Billing
	r := b.Realtime
	if j.Platform != PlatformQianwen || r == nil || r.SessionID == "" || (r.PriceVersion != QianwenRealtimePriceVersion && r.PriceVersion != "operator-character-quote") || !IsQianwenRealtimeModel(b.Model) || !finitePositive(b.RateMultiplier) || !finitePositive(b.AccountRateMultiplier) {
		return nil, nil, ErrUpstreamJobTariff
	}
	if r.PriceQuote != nil && r.PriceVersion != "operator-character-quote" {
		return nil, nil, ErrUpstreamJobTariff
	}
	expected := 0.143353 / 10000
	if r.Region == "singapore" {
		expected = 0.13 / 10000
	} else if r.Region != "beijing" {
		return nil, nil, ErrUpstreamJobTariff
	}
	if r.PriceVersion == "operator-character-quote" {
		quote := r.PriceQuote
		if quote == nil || quote.Sales == nil || quote.Cost == nil || quote.Currency != "USD" || quote.Unit != "character" || quote.Dimension != "output" || !validBillingNumber(*quote.Sales) || !validBillingNumber(*quote.Cost) || b.UnitPrice != *quote.Sales {
			return nil, nil, ErrUpstreamJobTariff
		}
		expected = *quote.Sales
	}
	if b.UnitPrice != expected {
		return nil, nil, ErrUpstreamJobTariff
	}
	n, err := QianwenRealtimeCharacters(j.UsageJSON)
	if err != nil {
		return nil, nil, err
	}
	total := QuantizeUsageBillingAmount(float64(n) * b.UnitPrice)
	actual := QuantizeUsageBillingAmount(total * b.RateMultiplier)
	upstreamTotal := total
	if r.PriceQuote != nil {
		upstreamTotal = QuantizeUsageBillingAmount(float64(n) * *r.PriceQuote.Cost)
	}
	accountCost := QuantizeUsageBillingAmount(upstreamTotal * b.AccountRateMultiplier)
	if !validBillingNumber(actual) || !validBillingNumber(accountCost) {
		return nil, nil, ErrUpstreamJobTariff
	}
	mode, source, version := "realtime", "dashscope-official-usd-"+r.Region, r.PriceVersion
	if r.PriceQuote != nil {
		source = "qianwen_realtime_group"
	}
	tier := fmt.Sprintf("qwen3_tts_characters:%d", n)
	log := &UsageLog{UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, RequestID: j.PublicRequestID, Model: b.Model, RequestedModel: b.RequestedModel, UpstreamModel: &b.Model, GroupID: j.GroupID, SubscriptionID: b.SubscriptionID, TotalCost: total, ActualCost: actual, RateMultiplier: b.RateMultiplier, AccountRateMultiplier: &b.AccountRateMultiplier, AccountStatsCost: &upstreamTotal, BillingMode: &mode, BillingTier: &tier, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
	cmd := &UsageBillingCommand{RequestID: j.PublicRequestID, RequestFingerprint: j.RequestFingerprint, UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, AccountType: AccountTypeAPIKey, Model: b.Model, MediaType: "realtime", BalanceCost: actual}
	if b.SubscriptionID != nil {
		log.BillingType = BillingTypeSubscription
		cmd.BillingType = BillingTypeSubscription
		cmd.SubscriptionID = b.SubscriptionID
		cmd.SubscriptionCost = actual
		cmd.BalanceCost = 0
	}
	if b.KeyQuota {
		cmd.APIKeyQuotaCost = actual
	}
	if b.KeyRateLimit {
		cmd.APIKeyRateLimitCost = actual
	}
	if b.AccountQuota {
		cmd.AccountQuotaCost = accountCost
	}
	if err := freezeJobPricing(j, log, PricingSnapshot{UnitPrice: &b.UnitPrice, Units: float64(n), SizeTier: "characters"}); err != nil {
		return nil, nil, err
	}
	return log, cmd, nil
}

func (s *UpstreamJobService) StartQianwenRealtime(ctx context.Context, j *UpstreamJob) (*UpstreamJob, error) {
	release, err := s.beginWork()
	if err != nil {
		return nil, err
	}
	defer release()
	repo, ok := s.repo.(QianwenRealtimeSessionRepository)
	if !ok || j == nil || j.Billing.Realtime == nil || j.UserID <= 0 || j.APIKeyID <= 0 || j.AccountID <= 0 || j.Platform != PlatformQianwen {
		return nil, errors.New("realtime session store unavailable")
	}
	j.PublicRequestID = uuid.NewString()
	j.Family = "realtime_session"
	j.Status = "submitted"
	j.Billing.Realtime.SessionID = j.PublicRequestID
	validation := *j
	validation.UsageJSON = json.RawMessage(`{"characters":1}`)
	if _, _, err := QianwenRealtimeCharge(&validation); err != nil {
		return nil, err
	}
	j.RequestFingerprint = j.PublicRequestID
	j.SubmissionKey = j.PublicRequestID
	j.SubmittedAt = s.now().UTC()
	j.ExpiresAt = j.SubmittedAt.Add(150 * time.Second)
	stored, err := repo.CreateRealtimeSession(ctx, j)
	if err == nil {
		logJobEvent(ctx, stored, "admitted")
	}
	return stored, err
}

func (s *UpstreamJobService) FinishQianwenRealtime(ctx context.Context, id, code string) error {
	_, err := s.locked(ctx, id, func(j *UpstreamJob, _ UpstreamJobTx) error {
		if !j.Terminal() {
			s.finish(j, "failed", code, "Realtime session closed; inspect child turn receipts")
		}
		return nil
	})
	return err
}

// Persist the trusted receipt in its own transaction BEFORE attempting money
// writes. A crash/failure between these steps leaves a replayable pending job.
// Identity is server session UUID + provider response ID, never client event_id.
func (s *UpstreamJobService) RecordQianwenRealtimeTurn(ctx context.Context, session *UpstreamJob, responseID string, raw []byte, status string) (*UpstreamJob, error) {
	if responseID == "" || len(responseID) > 256 || session == nil || session.Billing.Realtime == nil {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	canonical, err := CanonicalUpstreamJobBody(raw)
	if err != nil {
		canonical = []byte("null")
	}
	sum := sha256.Sum256([]byte(session.PublicRequestID + "\x00" + responseID))
	identity := hex.EncodeToString(sum[:])
	repo, ok := s.repo.(QianwenRealtimeReceiptRepository)
	if !ok {
		return nil, errors.New("realtime receipt store unavailable")
	}
	now := s.now().UTC()
	j := &UpstreamJob{
		PublicRequestID: uuid.NewString(), Family: "realtime_turn", Status: "running",
		Platform: session.Platform, UserID: session.UserID, APIKeyID: session.APIKeyID,
		AccountID: session.AccountID, GroupID: session.GroupID, Billing: session.Billing,
		RequestFingerprint: identity, SubmissionKey: identity,
		SubmittedAt: now, NextPollAt: now, ExpiresAt: now.Add(24 * time.Hour),
		UpstreamTaskID: responseID, UsageJSON: append(json.RawMessage(nil), canonical...), ResultRef: status,
	}
	if _, usageErr := QianwenRealtimeCharacters(canonical); usageErr != nil {
		s.finish(j, "failed", "unsupported_usage", "Unexplained usage retained; not charged")
	}
	switch status {
	case "completed", "cancelled", "failed", "incomplete":
	default:
		s.finish(j, "failed", "unsupported_usage", "Unexplained response status retained; not charged")
	}
	logJobEvent(ctx, j, "receipt_received")
	attemptedReceipt := j
	j, err = repo.InsertRealtimeReceipt(ctx, j)
	if err != nil {
		logJobEvent(ctx, attemptedReceipt, billingFailureCategory(err))
		return j, err
	}
	if j.Terminal() || !s.Ready() {
		return j, nil
	}
	return s.Poll(ctx, j.PublicRequestID)
}
