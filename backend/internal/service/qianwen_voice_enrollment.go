package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/tidwall/gjson"
)

// Voice enrollment (Qwen-TTS voice cloning) on
// /api/v1/services/audio/tts/customization — synchronous JSON, no async header.
// Shapes verified against the official voice-clone HTTP API doc, 2026-09-22:
// https://help.aliyun.com/zh/model-studio/voice-clone-design-http-api
// Only qwen-voice-enrollment is admitted: it carries a documented Beijing list
// price (0.01 CNY per new voice, 2026-09-22 pricing page) billed per created
// clone; the older voice-enrollment (CosyVoice/Qwen-Audio-TTS) model has no
// verified current tariff and stays rejected. Billing requires the account's
// operator FX provenance (extra.qianwen_pricing_sync), identical to the async
// family tariff discipline.
const QianwenNativeVoiceEnrollmentPath = "/api/v1/services/audio/tts/customization"

const qianwenVoiceEnrollmentCloneCNY = 0.01

func isQianwenVoiceEnrollmentModel(model string) bool {
	return model == "qwen-voice-enrollment"
}

func qianwenVoiceEnrollmentAction(body []byte) string {
	return gjson.GetBytes(body, "input.action").String()
}

func parseQianwenVoiceEnrollmentBody(model string, body []byte) (bool, error) {
	if !strings.Contains(strings.ToLower(model), "voice-enrollment") {
		return false, nil
	}
	return true, validateQianwenVoiceEnrollmentBody(model, body)
}

func validateQianwenVoiceEnrollmentBody(model string, body []byte) error {
	if !isQianwenVoiceEnrollmentModel(model) {
		return fmt.Errorf("unsupported DashScope voice enrollment model: %s (only qwen-voice-enrollment has a verified tariff)", model)
	}
	if !gjson.ValidBytes(body) {
		return fmt.Errorf("invalid DashScope voice enrollment JSON")
	}
	action := qianwenVoiceEnrollmentAction(body)
	switch action {
	case "create", "list", "delete":
	default:
		return fmt.Errorf("unsupported DashScope voice enrollment action: %s", action)
	}
	unknown := false
	gjson.GetBytes(body, "input").ForEach(func(k, _ gjson.Result) bool {
		switch k.String() {
		case "action", "preferred_name", "target_model", "audio", "text", "language", "voice", "page_index", "page_size":
		default:
			unknown = true
		}
		return true
	})
	if unknown {
		return fmt.Errorf("unsupported DashScope voice enrollment input field")
	}
	switch action {
	case "create":
		name := gjson.GetBytes(body, "input.preferred_name")
		if name.Type != gjson.String || !validQianwenVoiceName(name.String()) {
			return fmt.Errorf("DashScope voice enrollment create requires input.preferred_name (digits/letters/underscore, up to 16 chars)")
		}
		target := gjson.GetBytes(body, "input.target_model")
		if target.Type != gjson.String || !strings.HasPrefix(target.String(), "qwen3-tts-vc-realtime") {
			return fmt.Errorf("DashScope voice enrollment create requires a documented qwen3-tts-vc-realtime target_model")
		}
		audio := gjson.GetBytes(body, "input.audio.data")
		if audio.Type != gjson.String || audio.String() == "" {
			return fmt.Errorf("DashScope voice enrollment create requires input.audio.data")
		}
		value := audio.String()
		if strings.HasPrefix(value, "data:") {
			if !strings.Contains(value, ";base64,") || len(value) > 30<<20 {
				return fmt.Errorf("DashScope voice enrollment audio data URL must be base64 within 20 MiB of audio")
			}
		} else if !qianwenAudioHTTPURL(value) {
			return fmt.Errorf("DashScope voice enrollment audio must be a data URL or an HTTP(S) URL")
		}
		if text := gjson.GetBytes(body, "input.text"); text.Exists() && text.Type != gjson.String {
			return fmt.Errorf("DashScope voice enrollment input.text must be a string")
		}
		if language := gjson.GetBytes(body, "input.language"); language.Exists() {
			switch language.String() {
			case "zh", "en", "de", "it", "pt", "es", "ja", "ko", "fr", "ru":
			default:
				return fmt.Errorf("unsupported DashScope voice enrollment language")
			}
		}
	case "list":
		for _, field := range []string{"page_index", "page_size"} {
			if v := gjson.GetBytes(body, "input."+field); v.Exists() && (v.Type != gjson.Number || v.Float() != float64(v.Int()) || v.Int() < 0 || v.Int() > 1000) {
				return fmt.Errorf("DashScope voice enrollment list requires integral %s", field)
			}
		}
	case "delete":
		voice := gjson.GetBytes(body, "input.voice")
		if voice.Type != gjson.String || strings.TrimSpace(voice.String()) == "" || len(voice.String()) > 256 {
			return fmt.Errorf("DashScope voice enrollment delete requires input.voice")
		}
	}
	return nil
}

func validQianwenVoiceName(name string) bool {
	if name == "" || len(name) > 16 {
		return false
	}
	for _, r := range name {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validateQianwenVoiceEnrollmentOperation(operation QianwenNativeOperation, model string, body []byte, stream bool) (bool, error) {
	voiceModel := strings.Contains(strings.ToLower(model), "voice-enrollment")
	if operation == QianwenNativeVoiceEnrollment {
		if stream || gjson.GetBytes(body, "stream").Bool() {
			return true, fmt.Errorf("DashScope voice enrollment does not support streaming")
		}
		return true, validateQianwenVoiceEnrollmentBody(model, body)
	}
	if voiceModel {
		return true, fmt.Errorf("DashScope voice enrollment model %s requires the tts/customization endpoint", model)
	}
	return false, nil
}

// observeQianwenVoiceEnrollment validates the per-action output and carries
// the billed clone count (usage.count) for the voice billing hook. Failed
// creations never reach this point: upstream errors are passed through before.
func observeQianwenVoiceEnrollment(body []byte, action string, result *OpenAIForwardResult) error {
	if !gjson.ValidBytes(body) {
		return fmt.Errorf("invalid DashScope voice enrollment response JSON")
	}
	if err := rejectQianwenNativeAsyncResponse(body); err != nil {
		return err
	}
	if id := gjson.GetBytes(body, "request_id").String(); id != "" {
		result.RequestID = id
	}
	if code := gjson.GetBytes(body, "code").String(); code != "" {
		return fmt.Errorf("DashScope voice enrollment response error: %s", code)
	}
	count := gjson.GetBytes(body, "usage.count")
	if count.Type != gjson.Number || count.Float() != float64(count.Int()) || count.Int() < 0 || count.Int() > 1 {
		return fmt.Errorf("DashScope voice enrollment response requires usage.count 0 or 1")
	}
	switch action {
	case "create":
		if voice := gjson.GetBytes(body, "output.voice"); voice.Type != gjson.String || strings.TrimSpace(voice.String()) == "" {
			return fmt.Errorf("DashScope voice enrollment create response requires output.voice")
		}
		if count.Int() != 1 {
			return fmt.Errorf("DashScope voice enrollment create requires usage.count 1")
		}
	case "list":
		if !gjson.GetBytes(body, "output.voice_list").IsArray() {
			return fmt.Errorf("DashScope voice enrollment list response requires output.voice_list")
		}
	case "delete":
		if voice := gjson.GetBytes(body, "output.voice"); voice.Type != gjson.String || strings.TrimSpace(voice.String()) == "" {
			return fmt.Errorf("DashScope voice enrollment delete response requires output.voice")
		}
	}
	result.AudioUsage = &AudioUsage{Mode: "voice_clone", DurationOrUnits: float64(count.Int())}
	return nil
}

// isQianwenVoiceEnrollmentResult distinguishes the customization endpoint from
// the audio synthesis results billed by the characters/seconds pipeline.
func isQianwenVoiceEnrollmentResult(result *OpenAIForwardResult) bool {
	return result != nil && result.AudioUsage != nil && result.AudioUsage.Mode == "voice_clone" &&
		strings.HasSuffix(result.UpstreamEndpoint, "/services/audio/tts/customization")
}

// qianwenVoiceEnrollmentPrice resolves the documented Beijing per-clone tariff
// with the account's operator FX before any upstream resource is consumed.
func qianwenVoiceEnrollmentPrice(account *Account) (float64, error) {
	if account == nil {
		return 0, ErrModelPricingUnavailable
	}
	var opts QianwenPricingOptions
	raw, _ := json.Marshal(account.Extra["qianwen_pricing_sync"])
	if json.Unmarshal(raw, &opts) != nil || opts.Region != "asia-pacific-china" || !positiveFinite(opts.USDPerCNY) || strings.TrimSpace(opts.FXSource) == "" {
		return 0, fmt.Errorf("%w: DashScope voice enrollment requires Beijing pricing with explicit USD/CNY conversion provenance", ErrModelPricingUnavailable)
	}
	return qianwenVoiceEnrollmentCloneCNY * opts.USDPerCNY, nil
}

// Persist the provider clone count in the existing BillingTier text field so a
// usage row is auditable without pretending that created voices are tokens.
func recordQianwenVoiceEnrollmentUnits(log *UsageLog, result *OpenAIForwardResult) {
	if !isQianwenVoiceEnrollmentResult(result) {
		return
	}
	label := fmt.Sprintf("qianwen_voice_v1:clones=%g", result.AudioUsage.DurationOrUnits)
	log.BillingTier = &label
	if log.TotalCost > 0 {
		log.RateMultiplier = log.ActualCost / log.TotalCost
	}
}

func (s *OpenAIGatewayService) calculateQianwenVoiceEnrollmentCost(result *OpenAIForwardResult, multiplier float64) (*CostBreakdown, error) {
	clones := result.AudioUsage.DurationOrUnits
	if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return nil, fmt.Errorf("invalid DashScope voice enrollment rate multiplier")
	}
	if clones == 0 {
		// list/delete are documented free operations (usage.count 0).
		return &CostBreakdown{PriceSource: QianwenPricingSource, PriceCardVersion: "voice-enrollment-2026-09-22", BillingMode: string(BillingModePerRequest)}, nil
	}
	if result.QianwenVoiceCloneUSDPerClone == nil || !positiveFinite(*result.QianwenVoiceCloneUSDPerClone) {
		return nil, fmt.Errorf("%w: DashScope voice enrollment clone price was not resolved before submission", ErrModelPricingUnavailable)
	}
	total := QuantizeUsageBillingAmount(clones * *result.QianwenVoiceCloneUSDPerClone)
	actual := QuantizeUsageBillingAmount(total * multiplier)
	if !positiveFinite(total) {
		return nil, fmt.Errorf("invalid DashScope voice enrollment cost")
	}
	return &CostBreakdown{
		PriceSource:      QianwenPricingSource,
		PriceCardVersion: "voice-enrollment-2026-09-22",
		BillingMode:      string(BillingModePerRequest),
		TotalCost:        total,
		ActualCost:       actual,
	}, nil
}
