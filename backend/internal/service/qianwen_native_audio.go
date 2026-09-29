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
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const qianwenNativeAudioTimeout = 3 * time.Minute

// Only these synchronous models use the verified characters/seconds schemas.
// Legacy Qwen-TTS uses audio tokens; filetrans and realtime need other lifecycles.
func qianwenNativeAudioMode(model string) string {
	switch model {
	case "qwen3-tts-flash", "qwen3-tts-flash-2025-11-27", "qwen3-tts-flash-2025-09-18",
		"qwen3-tts-instruct-flash", "qwen3-tts-instruct-flash-2026-01-26":
		return "tts"
	case "qwen3-asr-flash", "qwen3-asr-flash-2025-09-08", "qwen3-asr-flash-2026-02-10",
		"qwen3-asr-flash-us", "qwen3-asr-flash-2025-09-08-us":
		return "stt"
	}
	return ""
}

func isQianwenNativeAudioFamily(model string) bool {
	model = strings.ToLower(model)
	return strings.Contains(model, "tts") || strings.Contains(model, "asr") ||
		strings.Contains(model, "audio") || strings.HasPrefix(model, "cosyvoice") ||
		strings.HasPrefix(model, "paraformer") || strings.HasPrefix(model, "sensevoice") ||
		strings.HasPrefix(model, "sambert") || strings.HasPrefix(model, "qwen-voice") || model == "voice-enrollment"
}

func canonicalQianwenAudioBody(body []byte) []byte {
	if !json.Valid(body) {
		return body
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return body
	}
	canonical, _ := json.Marshal(value)
	return canonical
}

func parseQianwenNativeAudioBody(model string, body []byte) (bool, error) {
	if !isQianwenNativeAudioFamily(model) {
		return false, nil
	}
	return true, validateQianwenNativeAudioInput(model, canonicalQianwenAudioBody(body))
}

func validateQianwenNativeAudioInput(model string, body []byte) error {
	mode := qianwenNativeAudioMode(model)
	if mode == "" {
		return fmt.Errorf("unsupported DashScope audio model %s: requires a separate usage or asynchronous/WebSocket lifecycle adapter", model)
	}
	if !gjson.ValidBytes(body) {
		return fmt.Errorf("invalid DashScope audio JSON")
	}
	for _, path := range []string{"parameters.enable_thinking", "parameters.modalities", "parameters.incremental_output", "parameters.enable_search", "input.prompt", "input.file_urls", "input.file_url"} {
		if gjson.GetBytes(body, path).Exists() {
			return fmt.Errorf("unsupported DashScope audio parameter: %s", path)
		}
	}
	explicit, err := qianwenCacheMode(body)
	if err != nil {
		return err
	}
	if explicit {
		return fmt.Errorf("unsupported DashScope audio explicit caching")
	}
	if mode == "tts" {
		text, voice := gjson.GetBytes(body, "input.text"), gjson.GetBytes(body, "input.voice")
		if text.Type != gjson.String || strings.TrimSpace(text.String()) == "" || utf8.RuneCountInString(text.String()) > 600 {
			return fmt.Errorf("DashScope TTS requires input.text with 1–600 characters")
		}
		if voice.Type != gjson.String || strings.TrimSpace(voice.String()) == "" {
			return fmt.Errorf("DashScope TTS requires input.voice")
		}
		if gjson.GetBytes(body, "input.messages").Exists() {
			return fmt.Errorf("unsupported DashScope TTS input.messages")
		}
		return nil
	}
	if gjson.GetBytes(body, "input.text").Exists() {
		return fmt.Errorf("unsupported DashScope ASR input.text")
	}
	messages := gjson.GetBytes(body, "input.messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return fmt.Errorf("DashScope ASR requires input.messages")
	}
	audioCount := 0
	for _, message := range messages.Array() {
		role := message.Get("role").String()
		if role != "user" && role != "system" {
			return fmt.Errorf("unsupported DashScope ASR message role")
		}
		content := message.Get("content")
		if !content.IsArray() || len(content.Array()) == 0 {
			return fmt.Errorf("DashScope ASR requires content parts")
		}
		for _, part := range content.Array() {
			if !part.IsObject() || len(part.Map()) != 1 {
				return fmt.Errorf("unsupported DashScope ASR content part")
			}
			if audio := part.Get("audio"); audio.Exists() {
				if role != "user" || audio.Type != gjson.String || !qianwenAudioHTTPURL(audio.String()) {
					return fmt.Errorf("DashScope ASR requires an HTTP(S) audio URL in a user message")
				}
				audioCount++
			} else if text := part.Get("text"); text.Type != gjson.String || role != "system" {
				return fmt.Errorf("unsupported DashScope ASR content: only user audio and system text are supported")
			}
		}
	}
	if audioCount != 1 {
		return fmt.Errorf("DashScope ASR requires exactly one audio URL")
	}
	return nil
}

func validateQianwenNativeAudioOperation(operation QianwenNativeOperation, model string, body []byte, stream bool) (bool, error) {
	if !isQianwenNativeAudioFamily(model) {
		canonical := canonicalQianwenAudioBody(body)
		hasAudio := gjson.GetBytes(canonical, "input.text").Exists()
		gjson.GetBytes(canonical, "input.messages").ForEach(func(_, message gjson.Result) bool {
			message.Get("content").ForEach(func(_, part gjson.Result) bool {
				hasAudio = hasAudio || part.Get("audio").Exists()
				return true
			})
			return true
		})
		if hasAudio {
			return true, fmt.Errorf("unsupported DashScope audio input for non-audio mapped model")
		}
		return false, nil
	}
	body = canonicalQianwenAudioBody(body)
	if operation != QianwenNativeMultimodal || stream || gjson.GetBytes(body, "stream").Bool() {
		return true, fmt.Errorf("unsupported DashScope audio operation: only synchronous multimodal-generation is supported")
	}
	return true, validateQianwenNativeAudioInput(model, body)
}

func qianwenAudioHTTPURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}

func applyQianwenNativeAudioHeaders(req *http.Request, model string) {
	if qianwenNativeAudioMode(model) == "" {
		return
	}
	req.Header.Del("X-DashScope-SSE")
	req.Header.Del("X-DashScope-Async")
	req.Header.Set("Accept", "application/json")
}

// This is a wire adapter, called inside the existing native Chat admission,
// scheduling, failover and queued RecordUsage lifecycle. No jobs are submitted.
func (s *OpenAIGatewayService) forwardQianwenNativeAudio(ctx context.Context, c *gin.Context, account *Account, operation QianwenNativeOperation, body []byte, stream bool) (*OpenAIForwardResult, error) {
	started := time.Now()
	clientCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, qianwenNativeAudioTimeout)
	defer cancel()
	model := gjson.GetBytes(canonicalQianwenAudioBody(body), "model").String()
	mapped := account.GetMappedModel(model)
	v, _ := c.Get("api_key")
	key, _ := v.(*APIKey)
	if _, err := s.qianwenNativeAudioPrice(ctx, key, mapped); err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "unsupported_error", err.Error())
		return nil, err
	}
	req, err := s.BuildQianwenNativeRequest(ctx, account, operation, body, stream)
	if err != nil {
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, req.URL.Path)
	SetOpsUpstreamModel(c, mapped)
	resp, err := s.doOpenAIUpstream(req, upstreamModelsProxyURL(account), account)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { cancel(); _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return s.handleChatCompletionsErrorResponse(resp, c, account, model)
	}
	protocolError := func(err error) (*OpenAIForwardResult, error) {
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", err.Error())
		return nil, err
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		return protocolError(fmt.Errorf("unexpected DashScope audio status or content type; synchronous JSON required"))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, qianwenNativeMaxFrame+1))
	if err != nil {
		return protocolError(err)
	}
	if len(data) > qianwenNativeMaxFrame {
		return protocolError(fmt.Errorf("DashScope audio response exceeds size limit"))
	}
	result := &OpenAIForwardResult{Model: model, BillingModel: mapped, UpstreamModel: mapped, UpstreamEndpoint: req.URL.Path, RequestID: resp.Header.Get("X-Request-Id"), UpstreamHeaders: resp.Header.Clone()}
	if err := observeQianwenNativeAudio(data, result); err != nil {
		return protocolError(err)
	}
	c.Data(resp.StatusCode, "application/json", data)
	result.Duration = time.Since(started)
	result.ClientDisconnect = clientCtx.Err() != nil
	return result, nil
}

func observeQianwenNativeAudio(body []byte, result *OpenAIForwardResult) error {
	body = canonicalQianwenAudioBody(body)
	if err := observeQianwenNative(body, result); err != nil {
		return err
	}
	if status := gjson.GetBytes(body, "status_code"); status.Exists() && (status.Type != gjson.Number || status.Float() != 200) {
		return fmt.Errorf("DashScope audio response reports an unsuccessful status")
	}
	if gjson.GetBytes(body, "output.task_id").Exists() || gjson.GetBytes(body, "output.task_status").Exists() {
		return fmt.Errorf("unsupported asynchronous DashScope audio response")
	}
	mode := qianwenNativeAudioMode(result.UpstreamModel)
	field, divisor := "seconds", 3600.0
	switch mode {
	case "tts":
		field, divisor = "characters", 1_000_000
		if gjson.GetBytes(body, "output.finish_reason").String() != "stop" || !qianwenAudioHTTPURL(gjson.GetBytes(body, "output.audio.url").String()) {
			return fmt.Errorf("DashScope TTS response requires a completed audio URL")
		}
	case "stt":
		choices := gjson.GetBytes(body, "output.choices")
		if !choices.IsArray() || len(choices.Array()) != 1 || choices.Array()[0].Get("finish_reason").String() != "stop" {
			return fmt.Errorf("DashScope ASR response requires a completed transcript")
		}
		parts := choices.Array()[0].Get("message.content")
		if !parts.IsArray() || len(parts.Array()) != 1 || parts.Array()[0].Get("text").Type != gjson.String {
			return fmt.Errorf("DashScope ASR response requires text content")
		}
	default:
		return fmt.Errorf("unsupported DashScope audio usage model")
	}
	units := gjson.GetBytes(body, "usage."+field)
	value := units.Float()
	if units.Type != gjson.Number || !positiveFinite(value) || math.Trunc(value) != value || value > 1<<53 {
		return fmt.Errorf("DashScope audio response requires positive integral usage.%s", field)
	}
	// Audio quantities must never be represented as text tokens.
	result.Usage = OpenAIUsage{}
	result.AudioUsage = &AudioUsage{Mode: mode, DurationOrUnits: value / divisor}
	return nil
}
