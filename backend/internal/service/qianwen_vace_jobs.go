package service

import (
	"fmt"
	"time"

	"github.com/tidwall/gjson"
)

// VACE universal video editing on the shared video-synthesis endpoint,
// verified against the official Wan VACE guide, 2026-09-22:
// https://www.alibabacloud.com/help/en/model-studio/wan-vace-guide
// input.function selects the capability; this adapter admits video_edit
// (video.edit action) and video_extension (video.extend action) with their
// verbatim documented inputs. Beijing list price is 0.70 CNY/second at 720P
// (checked 2026-09-22); settlement still requires the operator-configured
// group per-second price, identical to other Wan models.
func isQianwenVaceVideoModel(model string) bool {
	return model == "wanx2.1-vace-plus" || model == "wan2.1-vace-plus"
}

func qianwenVaceBillingSnapshot(group *Group, account *Account, key *APIKey, model, mapped string, body []byte, subscription *UserSubscription) (UpstreamJobBilling, error) {
	bad := func() (UpstreamJobBilling, error) {
		return UpstreamJobBilling{}, fmt.Errorf("unsupported VACE model, function, input or pricing specification")
	}
	function := gjson.GetBytes(body, "input.function").String()
	switch function {
	case "video_edit":
		maskImage, maskVideo := gjson.GetBytes(body, "input.mask_image_url"), gjson.GetBytes(body, "input.mask_video_url")
		if qianwenHTTPURL(maskImage) == qianwenHTTPURL(maskVideo) {
			return bad()
		}
		if frame := gjson.GetBytes(body, "input.mask_frame_id"); frame.Exists() && (frame.Type != gjson.Number || frame.Float() != float64(frame.Int()) || frame.Int() < 0) {
			return bad()
		}
		if maskType := gjson.GetBytes(body, "input.mask_type"); maskType.Exists() && maskType.String() != "tracking" {
			return bad()
		}
		if ratio := gjson.GetBytes(body, "input.expand_ratio"); ratio.Exists() && (ratio.Type != gjson.Number || ratio.Float() < 0 || ratio.Float() > 1) {
			return bad()
		}
	case "video_extension":
		if !qianwenHTTPURL(gjson.GetBytes(body, "input.first_clip_url")) {
			return bad()
		}
	default:
		return bad()
	}
	if prompt := gjson.GetBytes(body, "input.prompt"); prompt.Exists() && (prompt.Type != gjson.String || len([]rune(prompt.String())) > 800) {
		return bad()
	}
	unknownInput := false
	gjson.GetBytes(body, "input").ForEach(func(k, _ gjson.Result) bool {
		switch k.String() {
		case "function", "prompt", "mask_image_url", "mask_video_url", "mask_frame_id", "mask_type", "expand_ratio", "first_clip_url":
		default:
			unknownInput = true
		}
		return true
	})
	if unknownInput {
		return bad()
	}
	// The only documented VACE pricing tier is 720P; output duration is fixed
	// by the provider and settled from authoritative usage evidence.
	if gjson.GetBytes(body, "parameters.resolution").String() != "720P" {
		return bad()
	}
	unknown := false
	gjson.GetBytes(body, "parameters").ForEach(func(k, _ gjson.Result) bool {
		switch k.String() {
		case "resolution", "prompt_extend", "watermark", "seed":
		default:
			unknown = true
		}
		return true
	})
	if unknown {
		return bad()
	}
	if group == nil {
		return UpstreamJobBilling{}, ErrUpstreamJobTariff
	}
	price := LookupVideoModelPrice(group.VideoModelPrices, mapped, "720p")
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
	b := UpstreamJobBilling{Model: mapped, RequestedModel: model, Resolution: "720p", UnitPrice: *price, RateMultiplier: multiplier, AccountRateMultiplier: 1}
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
