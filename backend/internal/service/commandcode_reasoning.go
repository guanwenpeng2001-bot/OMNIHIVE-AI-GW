package service

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Command Code passes reasoning options to heterogeneous providers. Only send
// capabilities demonstrated for the concrete model; unknown models omit effort.
// Kimi low/high were verified against this exact ID in the protocol audit.
func commandCodeReasoningEffortAllowed(model, effort string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "moonshotai/kimi-k2.7-code-highspeed":
		return effort == "low" || effort == "high"
	}
	return false
}

func normalizeCommandCodeReasoning(body []byte, model string) ([]byte, error) {
	v := gjson.GetBytes(body, "reasoning_effort")
	if !v.Exists() || commandCodeReasoningEffortAllowed(model, v.String()) {
		return body, nil
	}
	return sjson.DeleteBytes(body, "reasoning_effort")
}
