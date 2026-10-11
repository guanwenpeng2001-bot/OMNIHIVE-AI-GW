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
	"strings"
	"time"

	"github.com/google/uuid"
)

const QianwenRealtimePriceVersion = "dashscope-tts-characters-usd-2026-09-14"

var ErrQianwenRealtimeCapacity = errors.New("realtime session concurrency limit reached")
var ErrQianwenRealtimeConflict = errors.New("conflicting realtime receipt")

type QianwenRealtimeSnapshot struct {
	UpstreamResponseModel string                  `json:"upstream_response_model,omitempty"`
	TokenPricing          *ModelPricingDescriptor `json:"token_pricing,omitempty"`
	PriceQuote            *ModelUnitPrice         `json:"price_quote,omitempty"`
	SessionID             string                  `json:"session_id"`
	Region                string                  `json:"region"`
	PriceVersion          string                  `json:"price_version"`
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

// QianwenRealtimeFamily classifies the implemented classic DashScope families.
// Selected future snapshots remain eligible; workspace-hosted Omni needs separate configuration.
func QianwenRealtimeFamily(model string) string {
	for _, generation := range []string{"qwen3-", "qwen3.8-"} {
		for _, family := range []string{"tts", "omni", "asr"} {
			prefix := generation + family + "-flash-realtime"
			if model == prefix || strings.HasPrefix(model, prefix+"-") {
				return family
			}
		}
	}
	return ""
}
func IsQianwenRealtimeModel(model string) bool { return QianwenRealtimeFamily(model) != "" }

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
	family := QianwenRealtimeFamily(b.Model)
	if family == "omni" {
		b.UnitPrice = 0
		b.Realtime.PriceVersion = "operator-modality-token"
		if found {
			// Persisted descriptors contain only public price facts, never resolver state.
			encoded, encodeErr := json.Marshal(descriptor)
			if encodeErr != nil {
				return b, encodeErr
			}
			var frozen ModelPricingDescriptor
			if decodeErr := json.Unmarshal(encoded, &frozen); decodeErr != nil {
				return b, decodeErr
			}
			b.Realtime.TokenPricing = &frozen
		}
	} else if family == "asr" {
		b.UnitPrice = 0
		b.Realtime.PriceVersion = "operator-stt-seconds"
		if group.AudioSTTPricePerHour != nil && validBillingNumber(*group.AudioSTTPricePerHour) {
			price := *group.AudioSTTPricePerHour / 3600
			b.UnitPrice = price
			b.Realtime.PriceQuote = &ModelUnitPrice{Unit: "second", Dimension: "input", Currency: "USD", Sales: clonePriceNumber(&price), Cost: clonePriceNumber(&price)}
		}
	} else if found {
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

// Qwen3 TTS bills the provider's characters, even when its response.done also
// carries the documented optional token counters. Those counters are validated
// as evidence, never converted to characters or added to the character charge.
// https://www.alibabacloud.com/help/en/model-studio/qwen-tts-realtime-server-events
func QianwenRealtimeCharacters(raw []byte) (int64, error) {
	var usage map[string]json.RawMessage
	if json.Unmarshal(raw, &usage) != nil || usage == nil {
		return 0, ErrUpstreamJobUnsupportedUsage
	}
	count := func(value json.RawMessage, limit int64) (int64, bool) {
		var quantity int64
		valid := !bytes.Equal(bytes.TrimSpace(value), []byte("null")) && json.Unmarshal(value, &quantity) == nil && quantity >= 0 && quantity <= limit
		return quantity, valid
	}
	characters, valid := count(usage["characters"], 2000)
	if !valid {
		return 0, ErrUpstreamJobUnsupportedUsage
	}
	tokens := map[string]int64{}
	for field, value := range usage {
		switch field {
		case "characters", "input_tokens_details", "output_tokens_details":
		case "input_tokens", "output_tokens", "total_tokens":
			quantity, valid := count(value, 10000000)
			if !valid {
				return 0, ErrUpstreamJobUnsupportedUsage
			}
			tokens[field] = quantity
		default:
			return 0, ErrUpstreamJobUnsupportedUsage
		}
	}
	for _, direction := range []string{"input", "output"} {
		value, present := usage[direction+"_tokens_details"]
		if !present {
			continue
		}
		var details map[string]json.RawMessage
		if json.Unmarshal(value, &details) != nil || details == nil {
			return 0, ErrUpstreamJobUnsupportedUsage
		}
		sum := int64(0)
		for modality, value := range details {
			if modality != "text_tokens" && (direction != "output" || modality != "audio_tokens") {
				return 0, ErrUpstreamJobUnsupportedUsage
			}
			quantity, valid := count(value, 10000000)
			if !valid {
				return 0, ErrUpstreamJobUnsupportedUsage
			}
			sum += quantity
		}
		if quantity, present := tokens[direction+"_tokens"]; present && quantity != sum {
			return 0, ErrUpstreamJobUnsupportedUsage
		}
	}
	input, hasInput := tokens["input_tokens"]
	output, hasOutput := tokens["output_tokens"]
	if total, present := tokens["total_tokens"]; present && hasInput && hasOutput && total != input+output {
		return 0, ErrUpstreamJobUnsupportedUsage
	}
	return characters, nil
}

func QianwenRealtimeCharge(j *UpstreamJob) (usageLog *UsageLog, command *UsageBillingCommand, chargeErr error) {
	defer func() { recordJobPricingFailure(j, chargeErr) }()
	if QianwenRealtimeFamily(j.Billing.Model) != "tts" {
		return qianwenRealtimeAudioCharge(j)
	}
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
	switch QianwenRealtimeFamily(j.Billing.Model) {
	case "asr":
		validation.UsageJSON = json.RawMessage(`{"seconds":1,"meter":"relay_pcm_bytes"}`)
	case "omni":
		validation.UsageJSON = json.RawMessage(`{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"text_tokens":0,"audio_tokens":0},"output_tokens_details":{"text_tokens":0,"audio_tokens":0}}`)
	}
	if _, _, err := QianwenRealtimeCharge(&validation); err != nil && (QianwenRealtimeFamily(j.Billing.Model) == "tts" || !errors.Is(err, ErrModelPricingUnavailable)) {
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
	if _, usageErr := qianwenRealtimeQuantities(session.Billing.Model, canonical); usageErr != nil {
		s.finish(j, "failed", "unsupported_usage", "Unexplained usage retained; not charged")
	}
	if QianwenRealtimeFamily(session.Billing.Model) == "asr" && status != "completed" {
		s.finish(j, "failed", "unsupported_usage", "Incomplete transcription retained; not charged")
	}
	switch status {
	case "completed", "cancelled", "failed", "incomplete":
	default:
		s.finish(j, "failed", "unsupported_usage", "Unexplained response status retained; not charged")
	}
	if !j.Terminal() && QianwenRealtimeFamily(j.Billing.Model) != "tts" {
		if _, _, priceErr := QianwenRealtimeCharge(j); errors.Is(priceErr, ErrModelPricingUnavailable) {
			s.finish(j, "failed", "settlement_needs_review", "Realtime price unavailable; receipt retained for review")
		}
	}
	if j.ErrorCode == "unsupported_usage" {
		recordJobPricingFailure(j, ErrUpstreamJobUnsupportedUsage)
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

// qianwenRealtimeQuantities validates each family's evidence without substituting units.
func qianwenRealtimeQuantities(model string, raw []byte) (map[string]float64, error) {
	if QianwenRealtimeFamily(model) == "tts" {
		n, err := QianwenRealtimeCharacters(raw)
		return map[string]float64{"characters": float64(n)}, err
	}
	var usage map[string]json.RawMessage
	if json.Unmarshal(raw, &usage) != nil || usage == nil {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	quantities := map[string]float64{}
	if QianwenRealtimeFamily(model) == "asr" {
		var seconds float64
		var meter string
		if len(usage) != 2 || json.Unmarshal(usage["seconds"], &seconds) != nil || bytes.Equal(usage["seconds"], []byte("null")) || json.Unmarshal(usage["meter"], &meter) != nil || meter != "relay_pcm_bytes" || !validBillingNumber(seconds) || seconds <= 0 || seconds > 600 {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
		quantities["seconds"] = seconds
		return quantities, nil
	}
	if QianwenRealtimeFamily(model) != "omni" {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	for key := range usage {
		switch key {
		case "input_tokens", "output_tokens", "input_tokens_details", "output_tokens_details", "total_tokens", "plugins":
		default:
			return nil, ErrUpstreamJobUnsupportedUsage
		}
	}
	total := int64(0)
	for _, direction := range []string{"input", "output"} {
		var count int64
		var details map[string]json.RawMessage
		if json.Unmarshal(usage[direction+"_tokens"], &count) != nil || bytes.Equal(usage[direction+"_tokens"], []byte("null")) || count < 0 || count > 10000000 || json.Unmarshal(usage[direction+"_tokens_details"], &details) != nil || len(details) != 2 {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
		sum := int64(0)
		for _, modality := range []string{"text", "audio"} {
			var tokens int64
			if json.Unmarshal(details[modality+"_tokens"], &tokens) != nil || bytes.Equal(details[modality+"_tokens"], []byte("null")) || tokens < 0 || tokens > 10000000 {
				return nil, ErrUpstreamJobUnsupportedUsage
			}
			quantities[direction+"_"+modality] = float64(tokens)
			sum += tokens
		}
		if sum != count {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
		total += count
	}
	if value, ok := usage["total_tokens"]; ok {
		var count int64
		if json.Unmarshal(value, &count) != nil || count != total {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
	}
	return quantities, nil
}

func qianwenRealtimeAudioCharge(j *UpstreamJob) (*UsageLog, *UsageBillingCommand, error) {
	b := j.Billing
	snapshot := b.Realtime
	if j.Platform != PlatformQianwen || snapshot == nil || snapshot.SessionID == "" || !finitePositive(b.RateMultiplier) || !finitePositive(b.AccountRateMultiplier) {
		return nil, nil, ErrUpstreamJobTariff
	}
	quantities, err := qianwenRealtimeQuantities(b.Model, j.UsageJSON)
	if err != nil {
		return nil, nil, err
	}
	total, upstreamTotal := 0.0, 0.0
	for _, dimension := range []string{"seconds", "input_text", "input_audio", "output_text", "output_audio"} {
		quantity, present := quantities[dimension]
		if !present {
			continue
		}
		var price ModelUnitPrice
		if QianwenRealtimeFamily(b.Model) == "asr" {
			if snapshot.PriceVersion != "operator-stt-seconds" {
				return nil, nil, ErrUpstreamJobTariff
			}
			if snapshot.PriceQuote == nil {
				return nil, nil, ErrModelPricingUnavailable
			}
			price = *snapshot.PriceQuote
			if price.Unit != "second" || price.Dimension != "input" {
				return nil, nil, ErrUpstreamJobTariff
			}
		} else {
			if snapshot.PriceVersion != "operator-modality-token" {
				return nil, nil, ErrUpstreamJobTariff
			}
			if snapshot.TokenPricing == nil {
				return nil, nil, ErrModelPricingUnavailable
			}
			price, err = snapshot.TokenPricing.UnitPrice("token", dimension, "")
			if err != nil {
				return nil, nil, err
			}
		}
		if price.Sales == nil || price.Cost == nil {
			return nil, nil, ErrModelPricingUnavailable
		}
		if price.Currency != "USD" || !validBillingNumber(*price.Sales) || !validBillingNumber(*price.Cost) {
			return nil, nil, ErrUpstreamJobTariff
		}
		total += quantity * *price.Sales
		upstreamTotal += quantity * *price.Cost
	}
	total = QuantizeUsageBillingAmount(total)
	upstreamTotal = QuantizeUsageBillingAmount(upstreamTotal)
	actual := QuantizeUsageBillingAmount(total * b.RateMultiplier)
	accountCost := QuantizeUsageBillingAmount(upstreamTotal * b.AccountRateMultiplier)
	if ValidateUsageBillingAmount(actual) != nil || ValidateUsageBillingAmount(accountCost) != nil {
		return nil, nil, ErrUpstreamJobTariff
	}
	mode, source, version := "realtime", "qianwen_realtime_group", snapshot.PriceVersion
	tier := "qwen3_omni_modality_tokens"
	if QianwenRealtimeFamily(b.Model) == "asr" {
		tier = fmt.Sprintf("qwen3_asr_seconds:%g", quantities["seconds"])
	}
	log := &UsageLog{UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, RequestID: j.PublicRequestID, Model: b.Model, RequestedModel: b.RequestedModel, UpstreamModel: &b.Model, GroupID: j.GroupID, SubscriptionID: b.SubscriptionID, TotalCost: total, ActualCost: actual, RateMultiplier: b.RateMultiplier, AccountRateMultiplier: &b.AccountRateMultiplier, AccountStatsCost: &upstreamTotal, BillingMode: &mode, BillingTier: &tier, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC(), InputTokens: int(quantities["input_text"] + quantities["input_audio"]), OutputTokens: int(quantities["output_text"] + quantities["output_audio"])}
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
	// The descriptor and exact quantities remain frozen in Billing.Realtime and UsageJSON.
	// A one-turn aggregate preserves the existing pricing snapshot recalculation contract.
	if err := freezeJobPricing(j, log, PricingSnapshot{UnitPrice: &total, Units: 1, SizeTier: tier}); err != nil {
		return nil, nil, err
	}
	return log, cmd, nil
}
