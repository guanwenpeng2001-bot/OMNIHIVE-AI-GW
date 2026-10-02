package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Evidence: official OpenRouter video submit, poll and content schemas checked
// 2026-10-02. Poll usage publishes cost, not delivered seconds. Only an explicit
// per-request retail tariff can settle one completed artifact without guessing.
func ValidateOpenRouterVideoRequest(body []byte) error {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return fmt.Errorf("video generation requires a JSON object")
	}
	if model := gjson.GetBytes(body, "model"); model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return fmt.Errorf("model is required")
	}
	if gjson.GetBytes(body, "previous_job_id").Exists() || gjson.GetBytes(body, "video").Exists() {
		return fmt.Errorf("video continuation requires an owner-bound adapter and is not implemented")
	}
	if gjson.GetBytes(body, "callback_url").Exists() {
		return fmt.Errorf("video callbacks are not supported; use the owner-bound status endpoint")
	}
	if duration := gjson.GetBytes(body, "duration"); duration.Exists() && (duration.Type != gjson.Number || duration.Int() < 1 || duration.Float() != float64(duration.Int())) {
		return fmt.Errorf("duration must be a positive integer")
	}
	if seconds := gjson.GetBytes(body, "seconds"); seconds.Exists() {
		duration, err := strconv.Atoi(seconds.String())
		if err != nil || duration < 1 || (seconds.Type != gjson.String && seconds.Type != gjson.Number) {
			return fmt.Errorf("seconds must be a positive integer or integer string")
		}
		if native := gjson.GetBytes(body, "duration"); native.Exists() && native.Int() != int64(duration) {
			return fmt.Errorf("seconds and duration conflict")
		}
	}
	return nil
}

func (gateway *OpenAIGatewayService) OpenRouterVideoBillingSnapshot(ctx context.Context, group *Group, account *Account, key *APIKey, body []byte, subscription *UserSubscription) UpstreamJobBilling {
	requested := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	billing := UpstreamJobBilling{Model: account.GetMappedModel(requested), RequestedModel: requested, Resolution: gjson.GetBytes(body, "resolution").String(), PricingState: PricingStateUnpriced, AccountRateMultiplier: account.BillingRateMultiplier(), AccountQuota: account.HasAnyQuotaLimit()}
	if key != nil {
		billing.KeyQuota = key.Quota > 0
		billing.KeyRateLimit = key.HasRateLimits()
	}
	if subscription != nil {
		billing.SubscriptionID = &subscription.ID
	}
	if group == nil || gateway.billingService == nil {
		return billing
	}
	billing.RateMultiplier = group.RateMultiplier * group.PeakMultiplierAt(time.Now())
	if group.VideoRateIndependent {
		billing.RateMultiplier = group.VideoRateMultiplier
	}
	resolver := gateway.resolver
	if resolver == nil {
		resolver = NewModelPricingResolver(nil, gateway.billingService)
	}
	resolved := resolver.Resolve(ctx, PricingInput{Model: billing.Model, Mode: BillingModePerRequest, GroupID: &group.ID, Group: group})
	if resolved == nil || resolved.Mode != BillingModePerRequest || (resolved.Source != PricingSourceGroup && resolved.Source != PricingSourceChannel) {
		return billing
	}
	cost, err := gateway.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: billing.Model, Provider: PlatformOpenRouter, Mode: BillingModePerRequest, Resolved: resolved, Resolver: resolver, Group: group, GroupID: &group.ID, RequestCount: 1, RateMultiplier: billing.RateMultiplier, PricingAt: time.Now()})
	if err != nil || cost == nil || (cost.PricingState != PricingStatePriced && cost.PricingState != PricingStateFree) {
		return billing
	}
	billing.PricingState, billing.PricingSnapshot = cost.PricingState, cost.PricingSnapshot
	return billing
}

func (gateway *OpenAIGatewayService) openRouterVideoAccount(ctx context.Context, job *UpstreamJob) (*Account, error) {
	if job == nil || job.Platform != PlatformOpenRouter || job.Family != "video" || !openAIResourceID(job.UpstreamTaskID) {
		return nil, ErrUpstreamJobNotFound
	}
	account, err := gateway.accountRepo.GetByID(ctx, job.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Platform != PlatformOpenRouter || account.Type != AccountTypeAPIKey || !account.IsActive() {
		return nil, errors.New("OpenRouter video account unavailable")
	}
	if err := account.RestoreJobEgress(ctx, job.Billing.Egress); err != nil {
		return nil, err
	}
	return account, nil
}

func (gateway *OpenAIGatewayService) openRouterVideoRequest(ctx context.Context, account *Account, method, path string, body []byte) (*http.Response, error) {
	if account == nil || account.Platform != PlatformOpenRouter || account.Type != AccountTypeAPIKey {
		return nil, ErrUpstreamJobNotFound
	}
	credential := strings.TrimSpace(account.GetCredential("api_key"))
	if credential == "" {
		return nil, fmt.Errorf("OpenRouter key missing")
	}
	base, err := gateway.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), method, account.buildOpenAIEndpointURL(base, "/v1"+path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	account.ApplyHeaderOverrides(request.Header)
	request.Header.Set("Authorization", "Bearer "+credential)
	proxy, err := account.EgressProxyURL(ctx)
	if err != nil {
		return nil, err
	}
	return gateway.doOpenAIUpstream(request, proxy, account)
}

func parseOpenRouterVideo(raw []byte) (UpstreamJobUpdate, error) {
	if !gjson.ValidBytes(raw) {
		return UpstreamJobUpdate{}, fmt.Errorf("invalid OpenRouter video response")
	}
	identifier := gjson.GetBytes(raw, "id").String()
	if !openAIResourceID(identifier) {
		return UpstreamJobUpdate{}, fmt.Errorf("invalid OpenRouter job ID")
	}
	status := gjson.GetBytes(raw, "status").String()
	update := UpstreamJobUpdate{TaskID: identifier, ProviderStatus: status}
	switch status {
	case "pending":
		update.Status = "submitted"
	case "in_progress":
		update.Status = "running"
	case "completed":
		update.Status = "succeeded"
		update.ResultRef = identifier
		evidence := map[string]any{"provider_status": status}
		if usage := gjson.GetBytes(raw, "usage"); usage.IsObject() {
			evidence["provider_usage"] = json.RawMessage(usage.Raw)
		}
		if urls := gjson.GetBytes(raw, "unsigned_urls"); urls.IsArray() {
			valid := true
			for _, entry := range urls.Array() {
				parsed, err := url.Parse(entry.String())
				if entry.Type != gjson.String || err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
					valid = false
				}
			}
			if valid {
				evidence["artifact_count"] = len(urls.Array())
			}
		}
		update.Usage, _ = json.Marshal(evidence)
	case "failed", "cancelled":
		update.Status = "failed"
		update.ErrorCode = "upstream_" + status
		update.ErrorMessage = sanitizeUpstreamErrorMessage(gjson.GetBytes(raw, "error").String())
	case "expired":
		update.Status = "expired"
	default:
		return UpstreamJobUpdate{}, fmt.Errorf("unknown OpenRouter video status")
	}
	return update, nil
}

func (gateway *OpenAIGatewayService) SubmitOpenRouterVideo(ctx context.Context, account *Account, body []byte, model string) (UpstreamJobUpdate, error) {
	if err := ValidateOpenRouterVideoRequest(body); err != nil {
		return UpstreamJobUpdate{}, err
	}
	outbound := ReplaceModelInBody(body, model)
	if seconds := gjson.GetBytes(body, "seconds"); seconds.Exists() {
		duration, _ := strconv.Atoi(seconds.String()) // validated above; request quantity only, never billing evidence.
		outbound, _ = sjson.SetBytes(outbound, "duration", duration)
		outbound, _ = sjson.DeleteBytes(outbound, "seconds")
	}
	response, err := gateway.openRouterVideoRequest(ctx, account, http.MethodPost, "/videos", outbound)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return UpstreamJobUpdate{}, fmt.Errorf("invalid video submission response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return openAIResourceFailure(response.StatusCode, raw), nil
	}
	update, err := parseOpenRouterVideo(raw)
	if err != nil {
		return update, err
	}
	// A submission acknowledgement cannot be charged. Poll confirms terminal state.
	if update.Status == "succeeded" {
		update.Status = "submitted"
		update.ResultRef = ""
		update.Usage = nil
	}
	return update, nil
}

func (gateway *OpenAIGatewayService) PollOpenRouterVideo(ctx context.Context, job *UpstreamJob) (UpstreamJobUpdate, error) {
	account, err := gateway.openRouterVideoAccount(ctx, job)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	response, err := gateway.openRouterVideoRequest(ctx, account, http.MethodGet, "/videos/"+url.PathEscape(job.UpstreamTaskID), nil)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return UpstreamJobUpdate{}, fmt.Errorf("invalid video poll response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return UpstreamJobUpdate{}, fmt.Errorf("OpenRouter video poll status %d", response.StatusCode)
	}
	return parseOpenRouterVideo(raw)
}

func (gateway *OpenAIGatewayService) OpenOpenRouterVideoContent(ctx context.Context, job *UpstreamJob) (*http.Response, error) {
	account, err := gateway.openRouterVideoAccount(ctx, job)
	if err != nil {
		return nil, err
	}
	if job.Status != "succeeded" || job.ResultRef != job.UpstreamTaskID {
		return nil, ErrUpstreamJobNotFound
	}
	response, err := gateway.openRouterVideoRequest(ctx, account, http.MethodGet, "/videos/"+url.PathEscape(job.UpstreamTaskID)+"/content", nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "video/") || response.ContentLength > GatewayResourceDownloadLimit {
		response.Body.Close()
		return nil, fmt.Errorf("invalid OpenRouter video content")
	}
	response.Body = struct {
		io.Reader
		io.Closer
	}{&gatewayResourceBoundedReader{reader: response.Body, limit: GatewayResourceDownloadLimit}, response.Body}
	return response, nil
}

func OpenRouterVideoJobCharge(job *UpstreamJob) (*UsageLog, *UsageBillingCommand, error) {
	if job == nil || job.Platform != PlatformOpenRouter || job.Family != "video" || job.ResultRef != job.UpstreamTaskID {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	usage := gjson.ParseBytes(job.UsageJSON)
	// Only a documented terminal artifact establishes a completed per-request unit.
	if usage.Get("provider_status").String() != "completed" || usage.Get("artifact_count").Int() != 1 || usage.Get("provider_usage.cost").Type != gjson.Number || !validBillingNumber(usage.Get("provider_usage.cost").Float()) {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	var snapshot PricingSnapshot
	if json.Unmarshal([]byte(job.Billing.PricingSnapshot), &snapshot) != nil || snapshot.Mode != string(BillingModePerRequest) || (job.Billing.PricingState != PricingStatePriced && job.Billing.PricingState != PricingStateFree) {
		return nil, nil, ErrUpstreamJobTariff
	}
	cost, err := RecalculatePricingSnapshot(job.Billing.PricingSnapshot)
	if err != nil {
		return nil, nil, err
	}
	mode := string(BillingModePerRequest)
	source := snapshot.Source
	log := &UsageLog{UserID: job.UserID, APIKeyID: job.APIKeyID, AccountID: job.AccountID, RequestID: job.PublicRequestID, Model: job.Billing.Model, RequestedModel: job.Billing.RequestedModel, UpstreamModel: &job.Billing.Model, GroupID: job.GroupID, SubscriptionID: job.Billing.SubscriptionID, TotalCost: cost.TotalCost, ActualCost: cost.ActualCost, RateMultiplier: job.Billing.RateMultiplier, AccountRateMultiplier: &job.Billing.AccountRateMultiplier, VideoCount: 1, BillingMode: &mode, PriceSource: &source, PricingState: cost.PricingState, PricingSnapshot: cost.PricingSnapshot, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
	log.AccountStatsCost = &cost.TotalCost
	command := gatewayResourceBillingCommand(job, job.Billing, cost.ActualCost, cost.TotalCost*job.Billing.AccountRateMultiplier)
	command.MediaType = "video"
	return log, command, nil
}
