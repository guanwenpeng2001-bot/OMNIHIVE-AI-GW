package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func nativeReasoningGroup(c *gin.Context, platform string) *Group {
	if c == nil {
		return nil
	}
	value, _ := c.Get("api_key")
	key, ok := value.(*APIKey)
	if !ok || key.Group == nil || (key.Group.Platform != platform && key.Group.Platform != PlatformComposite) {
		return nil
	}
	return key.Group
}

// The Qianwen policy exposes two levels: low disables thinking, high enables
// it. Only documented switchable models participate; no effort field is sent.
func prepareQianwenThinkingWithPolicy(ctx context.Context, c *gin.Context, source, outbound []byte, model, path string) ([]byte, *bool, error) {
	body, mode, err := prepareQianwenThinking(source, outbound, model, path)
	if err != nil || mode == nil || qianwenDefaultThinking(model) == nil {
		return body, mode, err
	}
	group := nativeReasoningGroup(c, PlatformQianwen)
	if group == nil {
		return body, mode, nil
	}
	effort := "low"
	if *mode {
		effort = "high"
	}
	policyBody, _ := sjson.SetBytes([]byte(`{}`), "model", model)
	policyBody, _ = sjson.SetBytes(policyBody, "reasoning_effort", effort)
	applied, _, err := ApplyReasoningEffortPolicy(policyBody, binaryReasoningCeiling(group.MaxReasoningEffort), group.ReasoningEffortMappings, group.MaxReasoningEffortOverLimit)
	if err != nil {
		return nil, nil, err
	}
	effective := gjson.GetBytes(applied, "reasoning_effort").String()
	if effective != "low" && effective != "high" {
		return nil, nil, fmt.Errorf("reasoning effort %q unsupported for Qianwen thinking switch", effective)
	}
	enabled := effective == "high"
	body, err = sjson.SetBytes(body, path, enabled)
	return body, &enabled, err
}

func normalizeCommandCodeReasoningWithPolicy(c *gin.Context, body []byte, model string) ([]byte, error) {
	group := nativeReasoningGroup(c, PlatformCommandCode)
	if group == nil {
		return body, nil
	}
	ceiling := group.MaxReasoningEffort
	// Keep the known binary ceiling for this existing native integration. For
	// other models, unknown capability metadata must not erase client controls.
	if strings.EqualFold(strings.TrimSpace(model), "moonshotai/kimi-k2.7-code-highspeed") {
		ceiling = binaryReasoningCeiling(ceiling)
	}
	applied, _, err := ApplyReasoningEffortPolicy(body, ceiling, group.ReasoningEffortMappings, group.MaxReasoningEffortOverLimit)
	return applied, err
}

func nativePolicyErrorResponse(err error) (int, string) {
	if errors.Is(err, ErrUnverifiedModelPrice) {
		return http.StatusBadRequest, "model_pricing_unavailable"
	}
	if IsReasoningEffortPolicyDenied(err) {
		return http.StatusForbidden, "permission_error"
	}
	return http.StatusBadRequest, "invalid_request_error"
}

// Composite groups can configure intermediate ceilings for other providers.
// Choose the greatest supported binary level no higher than that ceiling.
func binaryReasoningCeiling(ceiling string) string {
	rank, ok := reasoningEffortRank(ceiling)
	high, _ := reasoningEffortRank("high")
	if !ok {
		return ceiling
	}
	low, _ := reasoningEffortRank("low")
	if rank < low {
		return ceiling
	}
	if rank < high {
		return "low"
	}
	return "high"
}
