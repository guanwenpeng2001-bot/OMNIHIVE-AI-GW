package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// OpenAI Sora video lifecycle on the shared upstream-job trunk: admission tariff
// snapshot, owner-bound submission, polling and settlement. Protocol evidence
// (checked 2026-09-21): the official video generation guide
// (developers.openai.com/api/docs/guides/video-generation) and the official
// OpenAPI specification (openai/openai-openapi@master): VideoResource,
// VideoStatus, VideoSize, VideoSeconds and the CreateVideo*Body schemas. The
// Videos API is announced to shut down on 2026-09-24; the adapter mirrors the
// documented contract without inventing successor behavior.

// openAIVideoModelFamily reduces dated variants onto the documented priced
// families. Unknown families stay unbillable instead of borrowing a price.
func openAIVideoModelFamily(model string) string {
	switch {
	case model == "sora-2" || model == "sora-2-pro":
		return model
	case strings.HasPrefix(model, "sora-2-pro-"):
		return "sora-2-pro"
	case strings.HasPrefix(model, "sora-2-"):
		return "sora-2"
	}
	return ""
}

// openAIVideoSizeTier maps the exact documented size strings onto the shared
// price tiers. The guide sells the 1024-wide exports as the 1080p class; the
// wire size strings pass through untouched and unknown sizes are never guessed
// into a cheaper bucket.
func openAIVideoSizeTier(size string) (string, bool) {
	switch strings.TrimSpace(size) {
	case "720x1280", "1280x720":
		return VideoBillingResolution720P, true
	case "1024x1792", "1792x1024":
		return VideoBillingResolution1080P, true
	}
	return "", false
}

// OpenAIVideoBillingSnapshot freezes the admission tariff for one video job.
// Generation resolves the documented model/size defaults; edit and extend
// inherit the owner-checked source job's model and resolution and re-freeze the
// current group price, never the stale source price.
func OpenAIVideoBillingSnapshot(group *Group, account *Account, key *APIKey, body []byte, subscription *UserSubscription, source *UpstreamJob) (UpstreamJobBilling, error) {
	billingSnapshot := UpstreamJobBilling{AccountRateMultiplier: 1}
	var model, resolution string
	if source != nil {
		if source.Platform != PlatformOpenAI || source.Family != "video" || source.Status != "succeeded" || openAIVideoModelFamily(source.Billing.Model) == "" {
			return billingSnapshot, ErrUpstreamJobTariff
		}
		model, resolution = source.Billing.Model, source.Billing.Resolution
		billingSnapshot.RequestedModel = source.Billing.RequestedModel
	} else {
		requested := gjson.GetBytes(body, "model").String()
		billingSnapshot.RequestedModel = requested
		model = requested
		if model == "" {
			// Documented default: CreateVideo*Body model defaults to sora-2.
			model = "sora-2"
		}
		if account != nil {
			model = account.GetMappedModel(model)
		}
		if openAIVideoModelFamily(model) == "" {
			return billingSnapshot, fmt.Errorf("unsupported OpenAI video model")
		}
		size := gjson.GetBytes(body, "size").String()
		if size == "" {
			// Documented default: size defaults to 720x1280.
			size = "720x1280"
		}
		tier, ok := openAIVideoSizeTier(size)
		if !ok {
			return billingSnapshot, fmt.Errorf("unsupported OpenAI video size")
		}
		resolution = tier
	}
	if group == nil {
		return billingSnapshot, ErrUpstreamJobTariff
	}
	// Price cards are keyed by documented family; dated variants share the family
	// tariff while snapshot.Model keeps the exact mapped upstream ID.
	price := LookupVideoModelPrice(group.VideoModelPrices, openAIVideoModelFamily(model), resolution)
	if price == nil || !finitePositive(*price) {
		return billingSnapshot, ErrUpstreamJobTariff
	}
	multiplier := group.RateMultiplier * group.PeakMultiplierAt(time.Now())
	if group.VideoRateIndependent {
		multiplier = group.VideoRateMultiplier
	}
	if !finitePositive(multiplier) {
		return billingSnapshot, ErrUpstreamJobTariff
	}
	billingSnapshot.Model = model
	billingSnapshot.Resolution = resolution
	billingSnapshot.UnitPrice = *price
	billingSnapshot.RateMultiplier = multiplier
	if account != nil {
		billingSnapshot.AccountRateMultiplier = account.BillingRateMultiplier()
		billingSnapshot.AccountQuota = account.HasAnyQuotaLimit()
	}
	if key != nil {
		billingSnapshot.KeyQuota = key.Quota > 0
		billingSnapshot.KeyRateLimit = key.HasRateLimits()
	}
	if subscription != nil {
		billingSnapshot.SubscriptionID = &subscription.ID
	}
	return billingSnapshot, nil
}

// SubmitOpenAIVideo forwards one bounded video submission (generate, edit or
// extend) and translates the returned job object. For extensions the caller
// pins the requested segment length (documented values 4, 8, 12, 16, 20 per
// CreateVideoExtend*Body) so settlement prices only the newly generated
// seconds, not the stitched total.
func (s *OpenAIGatewayService) SubmitOpenAIVideo(ctx context.Context, account *Account, action, contentType string, body []byte, mappedModel string, billableSeconds int) (UpstreamJobUpdate, error) {
	switch action {
	case "video.generate":
		billableSeconds = 0
	case "video.edit":
		billableSeconds = 0
	case "video.extend":
		switch billableSeconds {
		case 4, 8, 12, 16, 20:
		default:
			return UpstreamJobUpdate{}, errors.New("video extension requires a documented seconds value")
		}
		// The billable segment must be the same seconds value the upstream
		// receives; a JSON body that disagrees is a caller bug, not a discount.
		if mediaType, _, mediaErr := mime.ParseMediaType(contentType); mediaErr == nil && mediaType == "application/json" {
			if requested := gjson.GetBytes(body, "seconds").String(); requested != strconv.Itoa(billableSeconds) {
				return UpstreamJobUpdate{}, errors.New("video extension billable seconds must match the request")
			}
		}
	default:
		return UpstreamJobUpdate{}, errors.New("unverified OpenAI video submission")
	}
	response, err := s.ForwardOpenAIAction(ctx, account, action, contentType, body, mappedModel)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	const videoResponseLimit = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(response.Body, videoResponseLimit+1))
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if len(raw) > videoResponseLimit {
		return UpstreamJobUpdate{}, errors.New("OpenAI video response exceeds limit")
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return UpstreamJobUpdate{}, fmt.Errorf("OpenAI upstream temporarily unavailable: %d", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return openAIResourceFailure(response.StatusCode, raw), nil
	}
	return parseOpenAIVideoResource(raw, billableSeconds)
}

// PollOpenAIVideo revalidates the owner-bound account and reads one video job.
// A completed job carries usage evidence; extension jobs keep the admission-time
// billable segment recorded at submission.
func (s *OpenAIGatewayService) PollOpenAIVideo(ctx context.Context, j *UpstreamJob) (UpstreamJobUpdate, error) {
	if j == nil || j.Family != "video" {
		return UpstreamJobUpdate{}, ErrUpstreamJobUnsupportedUsage
	}
	account, err := s.openAIResourceAccount(ctx, j)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	billableSeconds := 0
	if recorded := gjson.GetBytes(j.UsageJSON, "billable_seconds"); recorded.Type == gjson.Number {
		billableSeconds = int(recorded.Int())
	}
	raw, status, err := s.openAIResourceRequest(ctx, account, http.MethodGet, "/videos/"+url.PathEscape(j.UpstreamTaskID), "", nil)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return openAIResourceFailure(status, raw), nil
	}
	return parseOpenAIVideoResource(raw, billableSeconds)
}

// parseOpenAIVideoResource translates the documented VideoResource lifecycle:
// queued/in_progress/completed/failed. Terminal success without interpretable
// usage is evidence for review, never a guessed charge.
func parseOpenAIVideoResource(raw []byte, billableSeconds int) (UpstreamJobUpdate, error) {
	if !gjson.ValidBytes(raw) {
		return UpstreamJobUpdate{}, errors.New("invalid OpenAI video JSON")
	}
	identifier := gjson.GetBytes(raw, "id").String()
	if !openAIResourceID(identifier) {
		return UpstreamJobUpdate{}, errors.New("invalid OpenAI video ID")
	}
	status := gjson.GetBytes(raw, "status").String()
	update := UpstreamJobUpdate{TaskID: identifier, ProviderStatus: status}
	switch status {
	case "queued":
		update.Status = "submitted"
	case "in_progress":
		update.Status = "running"
	case "completed":
		usage, ok := openAIVideoUsage(raw, billableSeconds)
		if !ok {
			update.Status = "failed"
			update.ErrorCode = "unsupported_usage"
			update.ErrorMessage = "Unrecognized OpenAI video usage; no settlement"
			return update, nil
		}
		update.Status = "succeeded"
		update.ResultRef = identifier
		update.Usage = usage
	case "failed":
		update.Status = "failed"
		update.ErrorCode = gjson.GetBytes(raw, "error.code").String()
		update.ErrorMessage = sanitizeUpstreamErrorMessage(gjson.GetBytes(raw, "error.message").String())
	default:
		return UpstreamJobUpdate{}, errors.New("unverified OpenAI video status")
	}
	return update, nil
}

// openAIVideoUsage accepts only the documented completed-video measurements.
// Seconds arrive as a string; sizes must match the documented tier map; the
// model must reduce to a priced family.
func openAIVideoUsage(raw []byte, billableSeconds int) (json.RawMessage, bool) {
	seconds, err := strconv.Atoi(gjson.GetBytes(raw, "seconds").String())
	size := gjson.GetBytes(raw, "size").String()
	_, sizeOK := openAIVideoSizeTier(size)
	model := gjson.GetBytes(raw, "model").String()
	// A single clip extends at most six documented segments to 120 seconds.
	if err != nil || seconds <= 0 || seconds > 120 || !sizeOK || openAIVideoModelFamily(model) == "" {
		return nil, false
	}
	usage := map[string]any{"seconds": seconds, "size": size, "model": model}
	if billableSeconds > 0 {
		if billableSeconds > seconds {
			return nil, false
		}
		usage["billable_seconds"] = billableSeconds
	}
	encoded, err := json.Marshal(usage)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

// OpenAIVideoJobCharge settles one completed OpenAI video job from the frozen
// admission tariff and the recorded usage. Quantity is the billable segment for
// extensions and the full duration otherwise; any mismatch between evidence and
// the frozen offer blocks settlement instead of repricing after the fact.
func OpenAIVideoJobCharge(j *UpstreamJob) (usageLog *UsageLog, command *UsageBillingCommand, chargeErr error) {
	defer func() { recordJobPricingFailure(j, chargeErr) }()
	if j == nil || j.Platform != PlatformOpenAI || j.Family != "video" {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	billingSnapshot := j.Billing
	if openAIVideoModelFamily(billingSnapshot.Model) == "" || !finitePositive(billingSnapshot.UnitPrice) || !finitePositive(billingSnapshot.RateMultiplier) {
		return nil, nil, ErrUpstreamJobTariff
	}
	if !json.Valid(j.UsageJSON) {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	usage := gjson.ParseBytes(j.UsageJSON)
	seconds := usage.Get("seconds")
	sizeTier, sizeOK := openAIVideoSizeTier(usage.Get("size").String())
	if seconds.Type != gjson.Number || !finitePositive(seconds.Float()) || seconds.Float() > 120 || !sizeOK || sizeTier != billingSnapshot.Resolution || openAIVideoModelFamily(usage.Get("model").String()) != openAIVideoModelFamily(billingSnapshot.Model) || j.ResultRef != j.UpstreamTaskID {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	quantity := seconds.Float()
	if billable := usage.Get("billable_seconds"); billable.Exists() {
		if billable.Type != gjson.Number || !finitePositive(billable.Float()) || billable.Float() > seconds.Float() {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
		quantity = billable.Float()
	}
	total := QuantizeUsageBillingAmount(quantity * billingSnapshot.UnitPrice)
	actual := QuantizeUsageBillingAmount(total * billingSnapshot.RateMultiplier)
	if !finitePositive(total) || !finitePositive(actual) {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	mode := "video"
	source := "group_video_model_prices"
	version := "openai-video-v1"
	log := &UsageLog{UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, RequestID: j.PublicRequestID, Model: billingSnapshot.Model, RequestedModel: billingSnapshot.RequestedModel, UpstreamModel: &billingSnapshot.Model, GroupID: j.GroupID, SubscriptionID: billingSnapshot.SubscriptionID, TotalCost: total, ActualCost: actual, RateMultiplier: billingSnapshot.RateMultiplier, AccountRateMultiplier: &billingSnapshot.AccountRateMultiplier, VideoCount: 1, VideoResolution: &billingSnapshot.Resolution, BillingMode: &mode, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
	// The audit column is integer; fractional official duration stays in
	// usage_json rather than being rounded into misleading metadata.
	if quantity == math.Trunc(quantity) && quantity <= math.MaxInt32 {
		duration := int(quantity)
		log.VideoDurationSeconds = &duration
	}
	log.AccountStatsCost = &total
	cmd := &UsageBillingCommand{RequestID: j.PublicRequestID, RequestFingerprint: j.RequestFingerprint, UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, AccountType: AccountTypeAPIKey, Model: billingSnapshot.Model, MediaType: "video", BalanceCost: actual}
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
	if err := freezeJobPricing(j, log, PricingSnapshot{UnitPrice: &billingSnapshot.UnitPrice, Units: quantity, SizeTier: billingSnapshot.Resolution}); err != nil {
		return nil, nil, err
	}
	return log, cmd, nil
}
