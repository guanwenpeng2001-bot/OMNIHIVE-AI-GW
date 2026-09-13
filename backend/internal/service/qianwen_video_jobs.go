package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// Scope this first adapter to the documented legacy Wan 2.x protocol. Newer
// models and different tariff dimensions need explicit support, not guesswork.
func QianwenVideoBillingSnapshot(group *Group, account *Account, key *APIKey, body []byte, subscription *UserSubscription) (UpstreamJobBilling, error) {
	model := gjson.GetBytes(body, "model").String()
	mapped := model
	if account != nil {
		mapped = account.GetMappedModel(model)
	}
	if !json.Valid(body) || !strings.HasPrefix(mapped, "wan2.") {
		return UpstreamJobBilling{}, fmt.Errorf("unsupported video model")
	}
	resolution, ok := LookupVideoBillingResolution(gjson.GetBytes(body, "parameters.resolution").String())
	if !ok { // text-to-video uses parameters.size; never silently default a tier.
		resolution = videoUsageResolution(gjson.GetBytes(body, "parameters.size").String())
		ok = resolution != ""
	}
	if group == nil || !ok {
		return UpstreamJobBilling{}, ErrUpstreamJobTariff
	}
	price := LookupVideoModelPrice(group.VideoModelPrices, mapped, resolution)
	if price == nil || !finitePositive(*price) {
		return UpstreamJobBilling{}, ErrUpstreamJobTariff
	}
	multiplier := group.RateMultiplier * group.PeakMultiplierAt(time.Now())
	if group.VideoRateIndependent {
		multiplier = group.VideoRateMultiplier
	}
	if !finitePositive(multiplier) {
		return UpstreamJobBilling{}, ErrUpstreamJobTariff
	}
	b := UpstreamJobBilling{Model: mapped, RequestedModel: model, Resolution: resolution, UnitPrice: *price, RateMultiplier: multiplier, AccountRateMultiplier: 1}
	if key != nil {
		b.KeyQuota = key.Quota > 0
		b.KeyRateLimit = key.HasRateLimits()
	}
	if account != nil {
		b.AccountRateMultiplier = account.BillingRateMultiplier()
		b.AccountQuota = account.HasAnyQuotaLimit()
	}
	if subscription != nil {
		b.SubscriptionID = &subscription.ID
	}
	return b, nil
}
func videoUsageResolution(size string) string {
	parts := strings.Split(size, "*")
	if len(parts) != 2 {
		return ""
	}
	w, e1 := strconv.Atoi(parts[0])
	h, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || w <= 0 || h <= 0 {
		return ""
	}
	resolution, ok := LookupVideoBillingResolution(strconv.Itoa(min(w, h)) + "p")
	if !ok {
		return ""
	}
	return resolution
}

// The returned usage is authoritative: Wan 2.6 duration includes input plus
// output seconds; older Wan returns video_duration. Only single-video responses
// with a matching official resolution are currently interpretable.
func QianwenVideoJobCharge(j *UpstreamJob) (*UsageLog, *UsageBillingCommand, error) {
	if j.Platform != PlatformQianwen || j.Family != "video" || !strings.HasPrefix(j.Billing.Model, "wan2.") {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	b := j.Billing
	if !finitePositive(b.UnitPrice) || !finitePositive(b.RateMultiplier) {
		return nil, nil, ErrUpstreamJobTariff
	}
	usage := gjson.ParseBytes(j.UsageJSON)
	count := usage.Get("video_count")
	seconds := usage.Get("video_duration")
	if strings.HasPrefix(b.Model, "wan2.6") {
		seconds = usage.Get("duration")
	}
	resolution := videoUsageResolution(usage.Get("video_ratio").String())
	if size := usage.Get("size").String(); size != "" {
		resolution = videoUsageResolution(size)
	}
	if sr := usage.Get("SR"); sr.Exists() {
		tier, ok := LookupVideoBillingResolution(sr.String() + "p")
		if !ok || (resolution != "" && resolution != tier) {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
		resolution = tier
	}
	if count.Type != gjson.Number || count.Float() != 1 || seconds.Type != gjson.Number || !finitePositive(seconds.Float()) || resolution != b.Resolution || j.ResultRef == "" {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	total := QuantizeUsageBillingAmount(seconds.Float() * b.UnitPrice)
	actual := QuantizeUsageBillingAmount(total * b.RateMultiplier)
	if !finitePositive(total) || !finitePositive(actual) {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	mode := "video"
	source := "group_video_model_prices"
	version := "upstream-job-v1"
	log := &UsageLog{UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, RequestID: j.PublicRequestID, Model: b.Model, RequestedModel: b.RequestedModel, UpstreamModel: &b.Model, GroupID: j.GroupID, SubscriptionID: b.SubscriptionID, TotalCost: total, ActualCost: actual, RateMultiplier: b.RateMultiplier, AccountRateMultiplier: &b.AccountRateMultiplier, VideoCount: 1, VideoResolution: &b.Resolution, BillingMode: &mode, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
	// Existing audit column is integer; retain fractional official duration only in
	// usage_json instead of rounding a billing quantity into misleading metadata.
	if seconds.Float() == math.Trunc(seconds.Float()) && seconds.Float() <= math.MaxInt32 {
		duration := int(seconds.Float())
		log.VideoDurationSeconds = &duration
	}
	accountCost := total * b.AccountRateMultiplier
	log.AccountStatsCost = &accountCost
	cmd := &UsageBillingCommand{RequestID: j.PublicRequestID, RequestFingerprint: j.RequestFingerprint, UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, AccountType: AccountTypeAPIKey, Model: b.Model, MediaType: "video", BalanceCost: actual}
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
		cmd.AccountQuotaCost = total * b.AccountRateMultiplier
	}
	return log, cmd, nil
}

func (s *OpenAIGatewayService) SubmitQianwenVideo(ctx context.Context, account *Account, body []byte) (UpstreamJobUpdate, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return UpstreamJobUpdate{}, err
	}
	model := gjson.GetBytes(body, "model").String()
	payload["model"], _ = json.Marshal(account.GetMappedModel(model))
	body, _ = json.Marshal(payload)
	return s.qianwenVideoRequest(ctx, account, http.MethodPost, "/services/aigc/video-generation/video-synthesis", body)
}
func (s *OpenAIGatewayService) PollQianwenVideo(ctx context.Context, j *UpstreamJob) (UpstreamJobUpdate, error) {
	if j.Platform != PlatformQianwen || j.Family != "video" {
		return UpstreamJobUpdate{}, ErrUpstreamJobUnsupportedUsage
	}
	account, err := s.accountRepo.GetByID(ctx, j.AccountID)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	return s.qianwenVideoRequest(ctx, account, http.MethodGet, "/tasks/"+url.PathEscape(j.UpstreamTaskID), nil)
}
func (s *OpenAIGatewayService) qianwenVideoRequest(ctx context.Context, account *Account, method, path string, body []byte) (UpstreamJobUpdate, error) {
	if account == nil || account.Platform != PlatformQianwen || account.Type != AccountTypeAPIKey || strings.TrimSpace(account.GetCNAPIKey()) == "" {
		return UpstreamJobUpdate{}, fmt.Errorf("video requires a Qianwen API-key account")
	}
	base := DefaultQianwenDashScopeBaseURL
	if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok {
		if override, ok := bases["dashscope"].(string); ok && strings.TrimSpace(override) != "" {
			base = strings.TrimSpace(override)
		}
	}
	base, err := s.validateUpstreamBaseURL(base)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	account.ApplyHeaderOverrides(req.Header)
	req.Header.Set("Authorization", "Bearer "+account.GetCNAPIKey())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Del("X-DashScope-SSE")
	if method == http.MethodPost {
		req.Header.Set("X-DashScope-Async", "enable")
	} else {
		req.Header.Del("X-DashScope-Async")
	}
	resp, err := s.doOpenAIUpstream(req, upstreamModelsProxyURL(account), account)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return UpstreamJobUpdate{}, fmt.Errorf("upstream temporarily unavailable: %d", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UpstreamJobUpdate{Status: "failed", ErrorCode: "upstream_rejected", ErrorMessage: fmt.Sprintf("DashScope rejected the request (HTTP %d)", resp.StatusCode)}, nil
	}
	if !json.Valid(raw) {
		return UpstreamJobUpdate{}, fmt.Errorf("invalid upstream JSON")
	}
	output := gjson.GetBytes(raw, "output")
	update := UpstreamJobUpdate{TaskID: output.Get("task_id").String(), ResultRef: output.Get("video_url").String(), Usage: json.RawMessage(gjson.GetBytes(raw, "usage").Raw)}
	switch output.Get("task_status").String() {
	case "PENDING":
		update.Status = "submitted"
	case "RUNNING":
		update.Status = "running"
	case "SUCCEEDED":
		update.Status = "succeeded"
	case "FAILED", "CANCELED":
		update.Status = "failed"
		update.ErrorCode = "upstream_failed"
		update.ErrorMessage = "DashScope task failed"
	case "UNKNOWN":
		update.Status = "expired"
	default:
		update.Status = "unsupported"
	}
	return update, nil
}
