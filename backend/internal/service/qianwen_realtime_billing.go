package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const QianwenRealtimePriceVersion = "dashscope-tts-characters-usd-2026-09-14"

var ErrQianwenRealtimeCapacity = errors.New("realtime session concurrency limit reached")
var ErrQianwenRealtimeConflict = errors.New("conflicting realtime receipt")

type QianwenRealtimeSnapshot struct {
	SessionID    string `json:"session_id"`
	Region       string `json:"region"`
	PriceVersion string `json:"price_version"`
}

// The lease lasts longer than the absolute 120s relay + 12s handshake + 5s
// drain. Admission is serialized in PostgreSQL and works across replicas.
type QianwenRealtimeSessionRepository interface {
	CreateRealtimeSession(context.Context, *UpstreamJob) (*UpstreamJob, error)
}

func IsQianwenRealtimeModel(model string) bool {
	switch model {
	case "qwen3-tts-flash-realtime", "qwen3-tts-flash-realtime-2025-11-27", "qwen3-tts-flash-realtime-2025-09-18":
		return true
	}
	return false
}

func QianwenRealtimeBillingSnapshot(group *Group, account *Account, key *APIKey, model string, subscription *UserSubscription) (UpstreamJobBilling, error) {
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
	b.RateMultiplier = group.RateMultiplier * group.PeakMultiplierAt(time.Now())
	b.AccountRateMultiplier = account.BillingRateMultiplier()
	b.KeyQuota, b.KeyRateLimit, b.AccountQuota = key.Quota > 0, key.HasRateLimits(), account.HasAnyQuotaLimit()
	if subscription != nil {
		b.SubscriptionID = &subscription.ID
	}
	b.Realtime = &QianwenRealtimeSnapshot{Region: region, PriceVersion: QianwenRealtimePriceVersion}
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

func QianwenRealtimeCharge(j *UpstreamJob) (*UsageLog, *UsageBillingCommand, error) {
	b := j.Billing
	r := b.Realtime
	if j.Platform != PlatformQianwen || r == nil || r.SessionID == "" || r.PriceVersion != QianwenRealtimePriceVersion || !IsQianwenRealtimeModel(b.Model) || !finitePositive(b.RateMultiplier) || !finitePositive(b.AccountRateMultiplier) {
		return nil, nil, ErrUpstreamJobTariff
	}
	expected := 0.143353 / 10000
	if r.Region == "singapore" {
		expected = 0.13 / 10000
	} else if r.Region != "beijing" {
		return nil, nil, ErrUpstreamJobTariff
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
	accountCost := QuantizeUsageBillingAmount(total * b.AccountRateMultiplier)
	if n > 0 && (!finitePositive(actual) || !finitePositive(accountCost)) {
		return nil, nil, ErrUpstreamJobTariff
	}
	mode, source, version := "realtime", "dashscope-official-usd-"+r.Region, r.PriceVersion
	tier := fmt.Sprintf("qwen3_tts_characters:%d", n)
	log := &UsageLog{UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, RequestID: j.PublicRequestID, Model: b.Model, RequestedModel: b.RequestedModel, UpstreamModel: &b.Model, GroupID: j.GroupID, SubscriptionID: b.SubscriptionID, TotalCost: total, ActualCost: actual, RateMultiplier: b.RateMultiplier, AccountRateMultiplier: &b.AccountRateMultiplier, AccountStatsCost: &accountCost, BillingMode: &mode, BillingTier: &tier, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
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
	return log, cmd, nil
}

func (s *UpstreamJobService) StartQianwenRealtime(ctx context.Context, j *UpstreamJob) (*UpstreamJob, error) {
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
	return repo.CreateRealtimeSession(ctx, j)
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
	copy := *session
	j := &copy
	j.ID = 0
	j.PublicRequestID = uuid.NewString()
	j.Family = "realtime_turn"
	j.Status = "submitted"
	j.FinishedAt = nil
	j.RequestFingerprint = identity
	j.SubmissionKey = identity
	j.SubmittedAt = s.now().UTC()
	j.ExpiresAt = j.SubmittedAt.Add(24 * time.Hour)
	j, _, err = s.repo.Create(ctx, j)
	if err != nil {
		return nil, err
	}
	conflict := false
	j, err = s.locked(ctx, j.PublicRequestID, func(stored *UpstreamJob, _ UpstreamJobTx) error {
		if stored.UpstreamTaskID != "" {
			conflict = stored.ErrorCode == "usage_conflict"
			old, _ := CanonicalUpstreamJobBody(stored.UsageJSON)
			if !bytes.Equal(old, canonical) {
				conflict = true
				stored.ErrorCode = "usage_conflict"
				stored.ErrorMessage = "Conflicting receipt; original usage retained"
				stored.ResultRef = string(canonical)
			}
			return nil
		}
		stored.UpstreamTaskID = responseID
		stored.UsageJSON = append(json.RawMessage(nil), canonical...)
		stored.Status = "running"
		stored.NextPollAt = s.now()
		stored.ResultRef = status
		if _, e := QianwenRealtimeCharacters(canonical); e != nil {
			s.finish(stored, "failed", "unsupported_usage", "Unexplained usage retained; not charged")
		}
		switch status {
		case "completed", "cancelled", "failed", "incomplete":
		default:
			s.finish(stored, "failed", "unsupported_usage", "Unexplained response status retained; not charged")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if conflict {
		return j, ErrQianwenRealtimeConflict
	}
	if j.Terminal() {
		return j, nil
	}
	return s.Poll(ctx, j.PublicRequestID)
}
