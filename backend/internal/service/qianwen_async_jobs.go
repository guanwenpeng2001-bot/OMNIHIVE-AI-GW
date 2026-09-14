package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const QianwenNativeASRPath = "/api/v1/services/audio/asr/transcription"

// Official Beijing prices, checked 2026-09-14:
// https://help.aliyun.com/en/model-studio/model-pricing
// FX is an operator-configured conversion, never a guessed market rate.
type QianwenAsyncSnapshot struct {
	QianwenPricingOptions
	UnitCNY  float64 `json:"unit_cny"`
	TaskType string  `json:"task_type"`
	Texture  string  `json:"texture,omitempty"`
	Geometry string  `json:"geometry,omitempty"`
}

func QianwenAsyncFamilyForPath(path string) string {
	switch path {
	case QianwenNativeVideoPath:
		return "video"
	case QianwenNative3DPath:
		return "3d"
	case QianwenNativeASRPath:
		return "asr"
	}
	return ""
}

func QianwenAsyncBillingSnapshot(family string, group *Group, account *Account, key *APIKey, body []byte, subscription *UserSubscription) (UpstreamJobBilling, error) {
	if family == "video" {
		return QianwenVideoBillingSnapshot(group, account, key, body, subscription)
	}
	billingSnapshot := UpstreamJobBilling{RequestedModel: gjson.GetBytes(body, "model").String(), AccountRateMultiplier: 1}
	billingSnapshot.Model = billingSnapshot.RequestedModel
	if account != nil {
		billingSnapshot.Model = account.GetMappedModel(billingSnapshot.Model)
	}
	asyncSnapshot, err := qianwenAsyncSpecification(family, billingSnapshot.Model, body)
	if err != nil {
		return billingSnapshot, err
	}
	billingSnapshot.Async = asyncSnapshot
	if group == nil {
		return billingSnapshot, ErrUpstreamJobTariff
	}
	billingSnapshot.RateMultiplier = group.RateMultiplier * group.PeakMultiplierAt(time.Now())
	if !finitePositive(billingSnapshot.RateMultiplier) {
		return billingSnapshot, ErrUpstreamJobTariff
	}
	// Before scheduling, validate the request. The selected account supplies FX.
	if account == nil {
		return billingSnapshot, nil
	}
	raw, _ := json.Marshal(account.Extra["qianwen_pricing_sync"])
	if json.Unmarshal(raw, &asyncSnapshot.QianwenPricingOptions) != nil || asyncSnapshot.Region != "asia-pacific-china" || !finitePositive(asyncSnapshot.USDPerCNY) || strings.TrimSpace(asyncSnapshot.FXSource) == "" {
		return billingSnapshot, fmt.Errorf("unsupported async tariff: Beijing pricing snapshot with explicit USD/CNY conversion and provenance required")
	}
	if _, err := time.Parse(time.RFC3339, asyncSnapshot.FXEffectiveAt); err != nil {
		return billingSnapshot, fmt.Errorf("unsupported async tariff: FX effective timestamp required")
	}
	base := DefaultQianwenDashScopeBaseURL
	if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok {
		if override, ok := bases["dashscope"].(string); ok && override != "" {
			base = override
		}
	}
	endpointURL, err := url.Parse(base)
	if err != nil || endpointURL.Scheme != "https" || (endpointURL.Hostname() != "dashscope.aliyuncs.com" && !strings.HasSuffix(endpointURL.Hostname(), ".cn-beijing.maas.aliyuncs.com")) {
		return billingSnapshot, fmt.Errorf("unsupported async tariff: verified Beijing endpoint required")
	}
	if family == "3d" && !strings.HasSuffix(endpointURL.Hostname(), ".cn-beijing.maas.aliyuncs.com") {
		return billingSnapshot, fmt.Errorf("3D requires the official Beijing workspace endpoint")
	}
	billingSnapshot.UnitPrice = asyncSnapshot.UnitCNY * asyncSnapshot.USDPerCNY
	if !finitePositive(billingSnapshot.UnitPrice) {
		return billingSnapshot, ErrUpstreamJobTariff
	}
	billingSnapshot.AccountRateMultiplier = account.BillingRateMultiplier()
	billingSnapshot.AccountQuota = account.HasAnyQuotaLimit()
	if key != nil {
		billingSnapshot.KeyQuota = key.Quota > 0
		billingSnapshot.KeyRateLimit = key.HasRateLimits()
	}
	if subscription != nil {
		billingSnapshot.SubscriptionID = &subscription.ID
	}
	return billingSnapshot, nil
}

func qianwenAsyncSpecification(family, model string, body []byte) (*QianwenAsyncSnapshot, error) {
	a := &QianwenAsyncSnapshot{}
	r := gjson.ParseBytes(body)
	bad := func() (*QianwenAsyncSnapshot, error) {
		return nil, fmt.Errorf("unsupported %s model, input or pricing specification", family)
	}
	if !json.Valid(body) || !r.IsObject() {
		return bad()
	}
	if family == "asr" {
		if model != "paraformer-v2" && model != "paraformer-8k-v2" && model != "paraformer-v1" && model != "paraformer-8k-v1" && model != "paraformer-mtl-v1" {
			return bad()
		}
		files := r.Get("input.file_urls")
		if !files.IsArray() || len(files.Array()) != 1 || !qianwenHTTPURL(files.Array()[0]) {
			return bad()
		}
		if c := r.Get("parameters.channel_id"); c.Exists() && (!c.IsArray() || len(c.Array()) != 1 || c.Array()[0].Type != gjson.Number || c.Array()[0].Float() != 0) {
			return bad()
		}
		a.UnitCNY, a.TaskType = 0.00008, "single-file-channel-0"
		return a, nil
	}
	if family != "3d" || (model != "Tripo/Tripo-H3.1" && model != "Tripo/Tripo-P1.0") {
		return bad()
	}
	n := 0
	for _, field := range []string{"prompt", "image", "images"} {
		v := r.Get("input." + field)
		if !v.Exists() {
			continue
		}
		n++
		switch field {
		case "prompt":
			if v.Type != gjson.String || strings.TrimSpace(v.String()) == "" || len([]rune(v.String())) > 1024 {
				return bad()
			}
			a.TaskType = "text-to-3d"
		case "image":
			if !qianwenHTTPURL(v) {
				return bad()
			}
			a.TaskType = "image-to-3d"
		case "images":
			if !v.IsArray() || len(v.Array()) != 4 {
				return bad()
			}
			valid := 0
			for _, item := range v.Array() {
				if !item.IsObject() {
					return bad()
				}
				if len(item.Map()) == 0 {
					continue
				}
				if (item.Get("type").String() != "png" && item.Get("type").String() != "jpeg") || !qianwenHTTPURL(item.Get("file_token")) {
					return bad()
				}
				valid++
			}
			if valid < 2 {
				return bad()
			}
			a.TaskType = "multi-image-to-3d"
		}
	}
	if n != 1 {
		return bad()
	}
	a.Geometry, a.Texture = "standard", "standard"
	if v := r.Get("parameters.geometry_quality"); v.Exists() {
		if model != "Tripo/Tripo-H3.1" || (v.String() != "standard" && v.String() != "ultra") {
			return bad()
		}
		a.Geometry = v.String()
	}
	if v := r.Get("parameters.texture_quality"); v.Exists() {
		if v.String() != "standard" && v.String() != "detailed" {
			return bad()
		}
		a.Texture = v.String()
	}
	for _, field := range []string{"texture", "pbr"} {
		if v := r.Get("parameters." + field); v.Exists() && v.Type != gjson.True && v.Type != gjson.False {
			return bad()
		}
	}
	if r.Get("parameters.texture").Type == gjson.False && r.Get("parameters.pbr").Type == gjson.False {
		a.Texture = "none"
	}
	// Reject unknown options: they may introduce an unpriced generation mode.
	unknown := false
	r.Get("parameters").ForEach(func(k, v gjson.Result) bool {
		switch k.String() {
		case "texture", "pbr", "texture_quality", "geometry_quality":
		default:
			unknown = true
		}
		return true
	})
	if unknown {
		return bad()
	}
	units := 1
	if model == "Tripo/Tripo-P1.0" {
		units = 3
	}
	if a.TaskType != "text-to-3d" {
		units++
	}
	if a.Geometry == "ultra" {
		units += 2
	}
	if a.Texture == "standard" {
		units++
	}
	if a.Texture == "detailed" {
		units += 2
	}
	a.UnitCNY = float64(units) * 0.7
	return a, nil
}

func qianwenHTTPURL(v gjson.Result) bool {
	u, err := url.Parse(v.String())
	return v.Type == gjson.String && err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

func (s *OpenAIGatewayService) SubmitQianwenAsync(ctx context.Context, account *Account, family string, body []byte) (UpstreamJobUpdate, error) {
	if family == "video" {
		return s.SubmitQianwenVideo(ctx, account, body)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return UpstreamJobUpdate{}, err
	}
	if account == nil {
		return UpstreamJobUpdate{}, ErrUpstreamJobTariff
	}
	model := account.GetMappedModel(gjson.GetBytes(body, "model").String())
	if _, err := qianwenAsyncSpecification(family, model, body); err != nil {
		return UpstreamJobUpdate{}, err
	}
	payload["model"], _ = json.Marshal(model)
	body, _ = json.Marshal(payload)
	path := QianwenNativeASRPath
	if family == "3d" {
		path = QianwenNative3DPath
	}
	return s.qianwenVideoRequest(ctx, account, http.MethodPost, strings.TrimPrefix(path, "/api/v1"), body)
}

func (s *OpenAIGatewayService) PollQianwenAsyncJob(ctx context.Context, j *UpstreamJob) (UpstreamJobUpdate, error) {
	if j.Family != "3d" && j.Family != "asr" {
		return s.PollQianwenJob(ctx, j)
	}
	a, err := s.QianwenResourceAccount(ctx, j, false)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	u, err := s.qianwenVideoRequest(ctx, a, http.MethodGet, "/tasks/"+url.PathEscape(j.UpstreamTaskID), nil)
	if err != nil || u.Status != "succeeded" || u.TaskID != j.UpstreamTaskID || j.Family != "asr" {
		return u, err
	}
	return completeQianwenASR(ctx, u, fetchQianwenTranscript)
}

func completeQianwenASR(ctx context.Context, u UpstreamJobUpdate, fetch func(context.Context, string) ([]byte, error)) (UpstreamJobUpdate, error) {
	results := gjson.Parse(u.ResultRef)
	if !results.IsArray() || len(results.Array()) != 1 || results.Get("0.subtask_status").String() != "SUCCEEDED" {
		u.Status, u.ErrorCode, u.ErrorMessage = "failed", "unsupported_usage", "ASR requires a successful single-file result; no settlement"
		return u, nil
	}
	raw, err := fetch(ctx, results.Get("0.transcription_url").String())
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	transcripts := gjson.GetBytes(raw, "transcripts")
	if !json.Valid(raw) || !transcripts.IsArray() || len(transcripts.Array()) != 1 || transcripts.Get("0.channel_id").Type != gjson.Number || transcripts.Get("0.channel_id").Float() != 0 || transcripts.Get("0.text").Type != gjson.String {
		u.Status, u.ErrorCode, u.ErrorMessage = "failed", "unsupported_usage", "Unrecognized ASR transcript; no settlement"
		return u, nil
	}
	// Persist only completed text and its channel, not the source audio URL or sentences.
	result, _ := json.Marshal([]map[string]any{{"channel_id": 0, "text": transcripts.Get("0.text").String()}})
	u.ResultRef = string(result)
	return u, nil
}

// Provider result downloads are unsigned requests to one documented OSS bucket.
// No account headers, cookies, proxy credentials or redirects cross this boundary.
func fetchQianwenTranscript(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host != "dashscope-result-bj.oss-cn-beijing.aliyuncs.com" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("unsupported ASR result origin")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ASR result unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ASR result HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, fmt.Errorf("ASR result exceeds limit or is unreadable")
	}
	return raw, nil
}

func QianwenAsyncJobCharge(upstreamJob *UpstreamJob) (usageLog *UsageLog, command *UsageBillingCommand, chargeErr error) {
	defer func() { recordJobPricingFailure(upstreamJob, chargeErr) }()
	if upstreamJob.Family == "realtime_turn" {
		return QianwenRealtimeCharge(upstreamJob)
	}
	if upstreamJob.Family != "3d" && upstreamJob.Family != "asr" {
		return QianwenJobCharge(upstreamJob)
	}
	billingSnapshot, asyncSnapshot := upstreamJob.Billing, upstreamJob.Billing.Async
	if upstreamJob.Platform != PlatformQianwen || asyncSnapshot == nil || !finitePositive(billingSnapshot.UnitPrice) || !finitePositive(billingSnapshot.RateMultiplier) || !finitePositive(asyncSnapshot.UnitCNY) || !finitePositive(asyncSnapshot.USDPerCNY) || math.Abs(billingSnapshot.UnitPrice-asyncSnapshot.UnitCNY*asyncSnapshot.USDPerCNY) > 1e-12 {
		return nil, nil, ErrUpstreamJobTariff
	}
	usage, result := gjson.ParseBytes(upstreamJob.UsageJSON), gjson.Parse(upstreamJob.ResultRef)
	if !json.Valid(upstreamJob.UsageJSON) || !result.IsArray() || len(result.Array()) != 1 {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	quantity := 1.0
	if upstreamJob.Family == "3d" {
		texture, geometry := usage.Get("texture_quality").String(), usage.Get("geometry_quality").String()
		// texture_quality may retain its default even when texture and PBR are
		// disabled. The documented base-only output proves the untextured tier;
		// do not invent a mandatory provider texture_quality="none" value.
		if asyncSnapshot.Texture == "none" && qianwenHTTPURL(result.Get("0.base_model_url")) && !result.Get("0.pbr_model_url").Exists() && (texture == "" || texture == "none" || texture == "standard" || texture == "detailed") {
			texture = "none"
		}
		if geometry == "" {
			geometry = "standard"
		}
		if usage.Get("count").Type != gjson.Number || usage.Get("count").Float() != 1 || usage.Get("3d_task_type").String() != asyncSnapshot.TaskType || geometry != asyncSnapshot.Geometry || texture != asyncSnapshot.Texture {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
		if !qianwenHTTPURL(result.Get("0.pbr_model_url")) && !qianwenHTTPURL(result.Get("0.base_model_url")) {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
	} else {
		duration := usage.Get("duration")
		if duration.Type != gjson.Number || !finitePositive(duration.Float()) || result.Get("0.text").Type != gjson.String || result.Get("0.channel_id").Type != gjson.Number || result.Get("0.channel_id").Float() != 0 {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
		quantity = duration.Float()
	}
	total := QuantizeUsageBillingAmount(quantity * billingSnapshot.UnitPrice)
	actual := QuantizeUsageBillingAmount(total * billingSnapshot.RateMultiplier)
	if !finitePositive(total) || !finitePositive(actual) {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	mode, source, version := upstreamJob.Family, "dashscope-cn-standard", "async-families-2026-09-14"
	tier := fmt.Sprintf("qianwen_%s_v1:units=%g", upstreamJob.Family, quantity)
	log := &UsageLog{UserID: upstreamJob.UserID, APIKeyID: upstreamJob.APIKeyID, AccountID: upstreamJob.AccountID, RequestID: upstreamJob.PublicRequestID, Model: billingSnapshot.Model, RequestedModel: billingSnapshot.RequestedModel, UpstreamModel: &billingSnapshot.Model, GroupID: upstreamJob.GroupID, SubscriptionID: billingSnapshot.SubscriptionID, TotalCost: total, ActualCost: actual, RateMultiplier: billingSnapshot.RateMultiplier, AccountRateMultiplier: &billingSnapshot.AccountRateMultiplier, BillingMode: &mode, BillingTier: &tier, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
	accountCost := total * billingSnapshot.AccountRateMultiplier
	log.AccountStatsCost = &total
	cmd := &UsageBillingCommand{RequestID: upstreamJob.PublicRequestID, RequestFingerprint: upstreamJob.RequestFingerprint, UserID: upstreamJob.UserID, APIKeyID: upstreamJob.APIKeyID, AccountID: upstreamJob.AccountID, AccountType: AccountTypeAPIKey, Model: billingSnapshot.Model, MediaType: upstreamJob.Family, BalanceCost: actual}
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
		cmd.AccountQuotaCost = accountCost
	}
	if err := freezeJobPricing(upstreamJob, log, PricingSnapshot{UnitPrice: &billingSnapshot.UnitPrice, Units: quantity, SizeTier: upstreamJob.Family}); err != nil {
		return nil, nil, err
	}
	return log, cmd, nil
}
