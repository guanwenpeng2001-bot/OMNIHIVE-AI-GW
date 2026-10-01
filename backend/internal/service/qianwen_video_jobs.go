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

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
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
	if !json.Valid(body) {
		return UpstreamJobBilling{}, fmt.Errorf("unsupported video model")
	}
	if isQianwenVaceVideoModel(mapped) {
		return qianwenVaceBillingSnapshot(group, account, key, model, mapped, body, subscription)
	}
	if !strings.HasPrefix(mapped, "wan2.") {
		return UpstreamJobBilling{}, fmt.Errorf("unsupported video model")
	}
	var resolution string
	var ok bool
	if strings.Contains(mapped, "-t2v") {
		// DashScope ignores resolution for text-to-video and defaults size to
		// 1080P. Pricing that ignored field admits a job we cannot safely settle.
		if gjson.GetBytes(body, "parameters.resolution").Exists() {
			return UpstreamJobBilling{}, fmt.Errorf("text-to-video requires parameters.size, not parameters.resolution")
		}
		resolution = videoUsageResolution(gjson.GetBytes(body, "parameters.size").String())
		ok = resolution != ""
	} else {
		if gjson.GetBytes(body, "parameters.size").Exists() {
			return UpstreamJobBilling{}, fmt.Errorf("this video model requires parameters.resolution, not parameters.size")
		}
		resolution, ok = LookupVideoBillingResolution(gjson.GetBytes(body, "parameters.resolution").String())
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
func isQianwenVideoJobModel(model string) bool {
	return strings.HasPrefix(model, "wan2.") || isQianwenVaceVideoModel(model)
}

func videoUsageResolution(size string) string {
	// Official Wan text-to-video tiers include square and 4:3 sizes whose
	// shorter edge is not the tier name. Keep these explicit, not estimated.
	// https://help.aliyun.com/en/model-studio/legacy-wan-text-to-video-api-reference
	switch size {
	case "624*624":
		return "480p"
	case "960*960", "1088*832", "832*1088":
		return "720p"
	case "1440*1440", "1632*1248", "1248*1632":
		return "1080p"
	}
	parts := strings.Split(size, "*")
	if len(parts) != 2 {
		return ""
	}
	width, widthErr := strconv.Atoi(parts[0])
	height, heightErr := strconv.Atoi(parts[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return ""
	}
	resolution, ok := LookupVideoBillingResolution(strconv.Itoa(min(width, height)) + "p")
	if !ok {
		return ""
	}
	return resolution
}

// The returned usage is authoritative: Wan 2.6 duration includes input plus
// output seconds; older Wan returns video_duration. Only single-video responses
// with a matching official resolution are currently interpretable.
func QianwenVideoJobCharge(upstreamJob *UpstreamJob) (usageLog *UsageLog, command *UsageBillingCommand, chargeErr error) {
	defer func() { recordJobPricingFailure(upstreamJob, chargeErr) }()
	if upstreamJob.Platform != PlatformQianwen || upstreamJob.Family != "video" || !isQianwenVideoJobModel(upstreamJob.Billing.Model) {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	billingSnapshot := upstreamJob.Billing
	if !finitePositive(billingSnapshot.UnitPrice) || !finitePositive(billingSnapshot.RateMultiplier) {
		return nil, nil, ErrUpstreamJobTariff
	}
	usage := gjson.ParseBytes(upstreamJob.UsageJSON)
	count := usage.Get("video_count")
	seconds := usage.Get("video_duration")
	if strings.HasPrefix(billingSnapshot.Model, "wan2.6") {
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
	if count.Type != gjson.Number || count.Float() != 1 || seconds.Type != gjson.Number || !finitePositive(seconds.Float()) || resolution != billingSnapshot.Resolution || upstreamJob.ResultRef == "" {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	total := QuantizeUsageBillingAmount(seconds.Float() * billingSnapshot.UnitPrice)
	actual := QuantizeUsageBillingAmount(total * billingSnapshot.RateMultiplier)
	if !finitePositive(total) || !finitePositive(actual) {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	mode := "video"
	source := "group_video_model_prices"
	version := "upstream-job-v1"
	log := &UsageLog{UserID: upstreamJob.UserID, APIKeyID: upstreamJob.APIKeyID, AccountID: upstreamJob.AccountID, RequestID: upstreamJob.PublicRequestID, Model: billingSnapshot.Model, RequestedModel: billingSnapshot.RequestedModel, UpstreamModel: &billingSnapshot.Model, GroupID: upstreamJob.GroupID, SubscriptionID: billingSnapshot.SubscriptionID, TotalCost: total, ActualCost: actual, RateMultiplier: billingSnapshot.RateMultiplier, AccountRateMultiplier: &billingSnapshot.AccountRateMultiplier, VideoCount: 1, VideoResolution: &billingSnapshot.Resolution, BillingMode: &mode, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
	// Existing audit column is integer; retain fractional official duration only in
	// usage_json instead of rounding a billing quantity into misleading metadata.
	if seconds.Float() == math.Trunc(seconds.Float()) && seconds.Float() <= math.MaxInt32 {
		duration := int(seconds.Float())
		log.VideoDurationSeconds = &duration
	}
	log.AccountStatsCost = &total
	cmd := &UsageBillingCommand{RequestID: upstreamJob.PublicRequestID, RequestFingerprint: upstreamJob.RequestFingerprint, UserID: upstreamJob.UserID, APIKeyID: upstreamJob.APIKeyID, AccountID: upstreamJob.AccountID, AccountType: AccountTypeAPIKey, Model: billingSnapshot.Model, MediaType: "video", BalanceCost: actual}
	if billingSnapshot.SubscriptionID != nil {
		log.BillingType = BillingTypeSubscription
		cmd.BillingType = BillingTypeSubscription
		cmd.SubscriptionID = billingSnapshot.SubscriptionID
		cmd.SubscriptionCost = actual
		cmd.BalanceCost = 0
	}
	if billingSnapshot.KeyQuota {
		cmd.APIKeyQuotaCost = actual
	}
	if billingSnapshot.KeyRateLimit {
		cmd.APIKeyRateLimitCost = actual
	}
	if billingSnapshot.AccountQuota {
		cmd.AccountQuotaCost = total * billingSnapshot.AccountRateMultiplier
	}
	if err := freezeJobPricing(upstreamJob, log, PricingSnapshot{UnitPrice: &billingSnapshot.UnitPrice, Units: seconds.Float(), SizeTier: billingSnapshot.Resolution}); err != nil {
		return nil, nil, err
	}
	return log, cmd, nil
}

// Use the same public-IP pinning as image downloads, with a separate unsigned
// direct transport: no account or environment proxy credentials are inherited.
var qianwenVideoContentTransport http.RoundTripper = httpclient.NewPublicTransport(&http.Transport{
	DisableCompression:  true,
	TLSHandshakeTimeout: 10 * time.Second,
	IdleConnTimeout:     90 * time.Second,
})

func isQianwenVideoResultHost(host string) bool {
	host = strings.ToLower(host)
	if strings.HasPrefix(host, "dashscope-result-") && strings.HasSuffix(host, ".aliyuncs.com") {
		return true // Retain the existing regional result-host policy.
	}
	// Official Wan results also use dashscope-<identifier> accelerated buckets:
	// https://help.aliyun.com/en/model-studio/wan-video-editing-api-reference
	bucket, accelerated := strings.CutSuffix(host, ".oss-accelerate.aliyuncs.com")
	if !accelerated || len(bucket) > 63 || !strings.HasPrefix(bucket, "dashscope-") {
		return false
	}
	identifier := strings.TrimPrefix(bucket, "dashscope-")
	if identifier == "" || strings.HasPrefix(identifier, "-") || strings.HasSuffix(identifier, "-") {
		return false
	}
	for _, character := range identifier {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}

// OpenQianwenVideoContent downloads a settled video result. Like the ASR
// transcript fetch, this is an unsigned request to a documented provider
// result host (regional dashscope-result or accelerated dashscope buckets, per the Wan
// API references): no account headers, cookies, proxy credentials or redirects
// cross this boundary. The caller must close Body.
func OpenQianwenVideoContent(ctx context.Context, rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || strings.Contains(rawURL, "#") || u.Host != u.Hostname() || !isQianwenVideoResultHost(u.Hostname()) {
		return nil, fmt.Errorf("unsupported video result origin")
	}
	req, err := http.NewRequestWithContext(withQianwenResultLookup(ctx, rawURL), http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("video result unavailable")
	}
	client := &http.Client{Transport: qianwenVideoContentTransport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("video result unavailable")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("video result HTTP %d", resp.StatusCode)
	}
	const maxVideoContentBytes = 256 << 20
	if resp.ContentLength < 0 || resp.ContentLength > maxVideoContentBytes {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("video result exceeds limit")
	}
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, resp.ContentLength), resp.Body}
	return resp, nil
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
	defer func() { _ = resp.Body.Close() }()
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
	if results := output.Get("results"); update.ResultRef == "" && results.IsArray() {
		update.ResultRef = results.Raw
	}
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
