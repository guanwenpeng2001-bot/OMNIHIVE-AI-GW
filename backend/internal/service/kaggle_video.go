package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// Kaggle notebook video API (03_wan2.2_api.ipynb): async POST /v1/videos (202),
// GET /v1/videos/{id}, GET /v1/videos/{id}/content, statuses queued /
// in_progress / completed / failed / cancelled. Job ids look like vid_<hex>.
func ValidateKaggleVideoRequest(body []byte) error {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return fmt.Errorf("video generation requires a JSON object")
	}
	if model := gjson.GetBytes(body, "model"); model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return fmt.Errorf("model is required")
	}
	if prompt := gjson.GetBytes(body, "prompt"); prompt.Type != gjson.String || strings.TrimSpace(prompt.String()) == "" {
		return fmt.Errorf("prompt is required")
	}
	if width := gjson.GetBytes(body, "width"); width.Exists() && (width.Type != gjson.Number || width.Int() < 256 || width.Int()%32 != 0 || width.Float() != float64(width.Int())) {
		return fmt.Errorf("width must be a multiple of 32 and at least 256")
	}
	if height := gjson.GetBytes(body, "height"); height.Exists() && (height.Type != gjson.Number || height.Int() < 256 || height.Int()%32 != 0 || height.Float() != float64(height.Int())) {
		return fmt.Errorf("height must be a multiple of 32 and at least 256")
	}
	if frames := gjson.GetBytes(body, "num_frames"); frames.Exists() && (frames.Type != gjson.Number || frames.Int() < 5 || (frames.Int()-1)%4 != 0 || frames.Float() != float64(frames.Int())) {
		return fmt.Errorf("num_frames must be 4k+1 (e.g. 25, 49, 81, 121)")
	}
	return nil
}

func kaggleVideoAllowsUnpricedAdmission(job *UpstreamJob) bool {
	return job != nil && job.Family == "video" && (job.Platform == PlatformOpenRouter || job.Platform == PlatformKaggle)
}

func (gateway *OpenAIGatewayService) KaggleVideoBillingSnapshot(ctx context.Context, group *Group, account *Account, key *APIKey, body []byte, subscription *UserSubscription) UpstreamJobBilling {
	requested := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	mapped := requested
	if account != nil {
		mapped = account.GetMappedModel(requested)
	}
	billing := UpstreamJobBilling{Model: mapped, RequestedModel: requested, PricingState: PricingStateUnpriced}
	if account != nil {
		billing.AccountRateMultiplier = account.BillingRateMultiplier()
		billing.AccountQuota = account.HasAnyQuotaLimit()
	}
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
	cost, err := gateway.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: billing.Model, Provider: PlatformKaggle, Mode: BillingModePerRequest, Resolved: resolved, Resolver: resolver, Group: group, GroupID: &group.ID, RequestCount: 1, RateMultiplier: billing.RateMultiplier, PricingAt: time.Now()})
	if err != nil || cost == nil || (cost.PricingState != PricingStatePriced && cost.PricingState != PricingStateFree) {
		return billing
	}
	billing.PricingState, billing.PricingSnapshot = cost.PricingState, cost.PricingSnapshot
	return billing
}

func (gateway *OpenAIGatewayService) kaggleVideoAccount(ctx context.Context, job *UpstreamJob) (*Account, error) {
	if job == nil || job.Platform != PlatformKaggle || job.Family != "video" || !openAIResourceID(job.UpstreamTaskID) {
		return nil, ErrUpstreamJobNotFound
	}
	account, err := gateway.accountRepo.GetByID(ctx, job.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Platform != PlatformKaggle || account.Type != AccountTypeAPIKey || !account.IsActive() {
		return nil, errors.New("Kaggle video account unavailable")
	}
	if err := account.RestoreJobEgress(ctx, job.Billing.Egress); err != nil {
		return nil, err
	}
	return account, nil
}

func (gateway *OpenAIGatewayService) kaggleVideoRequest(ctx context.Context, account *Account, method, path string, body []byte) (*http.Response, error) {
	if account == nil || account.Platform != PlatformKaggle || account.Type != AccountTypeAPIKey {
		return nil, ErrUpstreamJobNotFound
	}
	credential := strings.TrimSpace(account.GetCredential("api_key"))
	if credential == "" {
		return nil, fmt.Errorf("Kaggle key missing")
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

func parseKaggleVideo(raw []byte) (UpstreamJobUpdate, error) {
	raw = bytes.TrimSpace(raw)
	if !gjson.ValidBytes(raw) {
		return UpstreamJobUpdate{}, fmt.Errorf("invalid Kaggle video response")
	}
	identifier := gjson.GetBytes(raw, "id").String()
	if !openAIResourceID(identifier) {
		return UpstreamJobUpdate{}, fmt.Errorf("invalid Kaggle job ID")
	}
	status := gjson.GetBytes(raw, "status").String()
	update := UpstreamJobUpdate{TaskID: identifier, ProviderStatus: status}
	switch status {
	case "queued":
		update.Status = "submitted"
	case "in_progress":
		update.Status = "running"
	case "completed":
		update.Status = "succeeded"
		update.ResultRef = identifier
		evidence := map[string]any{"provider_status": status}
		if usage := gjson.GetBytes(raw, "seconds"); usage.Exists() {
			evidence["provider_seconds"] = usage.Value()
		}
		if progress := gjson.GetBytes(raw, "progress"); progress.Exists() {
			evidence["progress"] = progress.Value()
		}
		update.Usage, _ = json.Marshal(evidence)
	case "failed", "cancelled":
		update.Status = "failed"
		update.ErrorCode = "upstream_" + status
		update.ErrorMessage = sanitizeUpstreamErrorMessage(gjson.GetBytes(raw, "error").String())
	default:
		return UpstreamJobUpdate{}, fmt.Errorf("unknown Kaggle video status")
	}
	return update, nil
}

func (gateway *OpenAIGatewayService) SubmitKaggleVideo(ctx context.Context, account *Account, body []byte, model string) (UpstreamJobUpdate, error) {
	if err := ValidateKaggleVideoRequest(body); err != nil {
		return UpstreamJobUpdate{}, err
	}
	outbound := ReplaceModelInBody(body, model)
	response, err := gateway.kaggleVideoRequest(ctx, account, http.MethodPost, "/videos", outbound)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Debug("provider response cleanup failed")
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return UpstreamJobUpdate{}, fmt.Errorf("invalid video submission response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return openAIResourceFailure(response.StatusCode, raw), nil
	}
	update, err := parseKaggleVideo(raw)
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

func (gateway *OpenAIGatewayService) PollKaggleVideo(ctx context.Context, job *UpstreamJob) (UpstreamJobUpdate, error) {
	account, err := gateway.kaggleVideoAccount(ctx, job)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	response, err := gateway.kaggleVideoRequest(ctx, account, http.MethodGet, "/videos/"+url.PathEscape(job.UpstreamTaskID), nil)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Debug("provider response cleanup failed")
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return UpstreamJobUpdate{}, fmt.Errorf("invalid video poll response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return UpstreamJobUpdate{}, fmt.Errorf("Kaggle video poll status %d", response.StatusCode)
	}
	return parseKaggleVideo(raw)
}

func (gateway *OpenAIGatewayService) OpenKaggleVideoContent(ctx context.Context, job *UpstreamJob) (*http.Response, error) {
	account, err := gateway.kaggleVideoAccount(ctx, job)
	if err != nil {
		return nil, err
	}
	if job.Status != "succeeded" || job.ResultRef != job.UpstreamTaskID {
		return nil, ErrUpstreamJobNotFound
	}
	response, err := gateway.kaggleVideoRequest(ctx, account, http.MethodGet, "/videos/"+url.PathEscape(job.UpstreamTaskID)+"/content", nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "video/") || response.ContentLength > GatewayResourceDownloadLimit {
		if err := response.Body.Close(); err != nil {
			slog.Debug("provider response cleanup failed")
		}
		return nil, fmt.Errorf("invalid Kaggle video content")
	}
	response.Body = struct {
		io.Reader
		io.Closer
	}{&gatewayResourceBoundedReader{reader: response.Body, limit: GatewayResourceDownloadLimit}, response.Body}
	return response, nil
}

func KaggleVideoJobCharge(job *UpstreamJob) (*UsageLog, *UsageBillingCommand, error) {
	if job == nil || job.Platform != PlatformKaggle || job.Family != "video" || job.ResultRef != job.UpstreamTaskID {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	usage := gjson.ParseBytes(job.UsageJSON)
	if usage.Get("provider_status").String() != "completed" {
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
