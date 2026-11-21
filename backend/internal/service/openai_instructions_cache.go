package service

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

func supportsInstructionsCacheHoist(model string) bool {
	version, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-")
	if !ok {
		return false
	}
	version, _, _ = strings.Cut(version, "-")
	majorText, minorText, hasMinor := strings.Cut(version, ".")
	major, err := strconv.ParseUint(majorText, 10, 32)
	if err != nil {
		return false
	}
	var minor uint64
	if hasMinor {
		minor, err = strconv.ParseUint(minorText, 10, 32)
		if err != nil {
			return false
		}
	}
	return major > 6 || major == 6 && minor >= 2
}

func sameDeveloperInstructions(item gjson.Result, text string) bool {
	kind := item.Get("type")
	if (kind.Exists() && kind.String() != "message") || item.Get("role").String() != "developer" {
		return false
	}
	content := item.Get("content")
	if content.Type == gjson.String {
		return content.Str == text
	}
	part := content.Get("0")
	return content.IsArray() && !content.Get("1").Exists() && part.Get("type").String() == "input_text" && part.Get("text").Type == gjson.String && part.Get("text").Str == text
}

func hoistOpenAIInstructions(account *Account, model string, compact bool, body []byte) ([]byte, bool) {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey || account.UsesOpenAICodexProtocol() || compact || !supportsInstructionsCacheHoist(model) {
		return body, false
	}
	if gjson.GetBytes(body, "store").Type != gjson.False || gjson.GetBytes(body, "previous_response_id").Exists() || gjson.GetBytes(body, "conversation").Exists() {
		return body, false
	}
	instructions := gjson.GetBytes(body, "instructions")
	if instructions.Type != gjson.String || strings.TrimSpace(instructions.Str) == "" {
		return body, false
	}
	input := gjson.GetBytes(body, "input")
	developer := `{"type":"message","role":"developer","content":[{"type":"input_text","text":` + instructions.Raw + `}]}`
	var replacement string
	switch {
	case input.Type == gjson.String:
		replacement = "[" + developer + `,{"type":"message","role":"user","content":[{"type":"input_text","text":` + input.Raw + `}]}]`
	case input.IsArray():
		first := input.Get("0")
		if !sameDeveloperInstructions(first, instructions.Str) {
			separator := ""
			if first.Exists() {
				separator = ","
			}
			replacement = "[" + developer + separator + input.Raw[1:]
		}
	default:
		return body, false
	}
	updated := body
	var err error
	if replacement != "" {
		updated, err = sjson.SetRawBytes(body, "input", []byte(replacement))
		if err != nil {
			return body, false
		}
	}
	updated, err = sjson.DeleteBytes(updated, "instructions")
	if err != nil {
		return body, false
	}
	return updated, true
}

func (s *OpenAIGatewayService) applyInstructionsCacheHoist(c *gin.Context, account *Account, model string, body []byte) ([]byte, bool) {
	if s.cfg != nil && !s.cfg.Gateway.OpenAIInstructionsCacheHoist {
		return body, false
	}
	updated, changed := hoistOpenAIInstructions(account, model, isOpenAIResponsesCompactPath(c), body)
	if changed && c != nil && c.Request != nil {
		ctx := c.Request.Context()
		log := logger.FromContext(ctx).With(zap.Bool("openai_instructions_cache_hoisted", true))
		log.Debug("Moved Responses instructions to first developer message", zap.String("upstream_model", model))
		c.Request = c.Request.WithContext(logger.IntoContext(ctx, log))
	}
	return updated, changed
}
