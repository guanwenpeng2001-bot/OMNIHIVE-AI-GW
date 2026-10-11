// Package openai provides helpers and types for OpenAI API integration.
package openai

import (
	_ "embed"
	"strings"
)

// Model represents an OpenAI model
type Model struct {
	CatalogSource string `json:"catalog_source,omitempty"`
	ID            string `json:"id"`
	Object        string `json:"object"`
	Created       int64  `json:"created"`
	OwnedBy       string `json:"owned_by"`
	Type          string `json:"type"`
	DisplayName   string `json:"display_name"`
}

// DefaultTestModel default model for testing OpenAI accounts
const DefaultTestModel = "gpt-5.4"

// CodexUsageProbeModel is the model used for OAuth Codex usage probes.
const CodexUsageProbeModel = "codex-auto-review"

// DefaultInstructions default instructions for non-Codex CLI requests.
// 内容为真实 Codex CLI 的 GPT-5-Codex base prompt（codex 系模型默认）。
//
//go:embed instructions.txt
var DefaultInstructions string

// instructionsGPT51 / instructionsGPT52 / instructionsGPT55 / instructionsGPT6Astra
// 为对应非 codex 模型的真实 Codex 编码 agent base prompt，用于模型感知的 instructions 选择。
// 未单独维护 prompt 的版本通过 latestCodexInstructions 回退到最新 prompt。
//
//go:embed instructions_gpt5_1.txt
var instructionsGPT51 string

//go:embed instructions_gpt5_2.txt
var instructionsGPT52 string

//go:embed instructions_gpt5_5.txt
var instructionsGPT55 string

// Source: openai/codex codex-rs/models-manager/models.json at 121f91fd5d9d.
//
//go:embed instructions_gpt6_astra.txt
var instructionsGPT6Astra string

// latestCodexInstructions 返回当前已知最新版本的 Codex base instructions，
// 当前为 GPT-6 Astra；若为空则依次回退到 GPT-5.5 和 DefaultInstructions 保证非空。
func latestCodexInstructions() string {
	if instructions := strings.TrimSpace(instructionsGPT6Astra); instructions != "" {
		return instructionsGPT6Astra
	}
	if v := strings.TrimSpace(instructionsGPT55); v != "" {
		return instructionsGPT55
	}
	return DefaultInstructions
}

// CanonicalizeOpenAIModelAliasSpelling normalizes provider prefixes, case,
// separators, and known compact spellings used by OpenAI model aliases.
func CanonicalizeOpenAIModelAliasSpelling(model string) string {
	model = strings.TrimSpace(model)
	if slash := strings.LastIndexByte(model, '/'); slash >= 0 {
		model = strings.TrimSpace(model[slash+1:])
	}
	model = strings.ToLower(model)
	if model == "" {
		return ""
	}

	normalized := strings.ReplaceAll(model, "_", "-")
	normalized = strings.Join(strings.Fields(normalized), "-")
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}

	if strings.HasPrefix(normalized, "gpt5") {
		normalized = "gpt-5" + strings.TrimPrefix(normalized, "gpt5")
	}
	if !strings.HasPrefix(normalized, "gpt-") && !strings.Contains(normalized, "codex") {
		return ""
	}

	replacements := []struct {
		from string
		to   string
	}{
		{"gpt-5.4mini", "gpt-5.4-mini"},
		{"gpt-5.4nano", "gpt-5.4-nano"},
		{"gpt-5.3-codexspark", "gpt-5.3-codex-spark"},
		{"gpt-5.3codexspark", "gpt-5.3-codex-spark"},
		{"gpt-5.3codex", "gpt-5.3-codex"},
	}
	for _, replacement := range replacements {
		normalized = strings.ReplaceAll(normalized, replacement.from, replacement.to)
	}
	return normalized
}

// CodexBaseInstructionsForModel 按模型返回最匹配的真实 Codex base instructions：
//   - gpt-6 系模型（含 gpt-6.1-sol、供应商前缀与日期变体）→ GPT-6 Astra prompt
//   - 含 "codex" 的模型（gpt-5-codex / gpt-5.x-codex / codex-max / spark 等）→ GPT-5-Codex prompt
//   - gpt-5.5 系非 codex 模型 → GPT-5.5 prompt
//   - gpt-5.2 系非 codex 模型 → GPT-5.2 prompt
//   - gpt-5.1 系非 codex 模型 → GPT-5.1 prompt
//   - 其它（含 gpt-5.3 / gpt-5.4 / 裸 gpt-5 / 未知模型）→ 回退到最新版本（当前 GPT-6 Astra）
//
// 任一专用 prompt 意外为空时回退链最终落到 DefaultInstructions，保证返回非空。
func CodexBaseInstructionsForModel(model string) string {
	canonical := CanonicalizeOpenAIModelAliasSpelling(model)
	switch {
	case strings.HasPrefix(canonical, "gpt-6"):
		if v := strings.TrimSpace(instructionsGPT6Astra); v != "" {
			return instructionsGPT6Astra
		}
	case strings.Contains(canonical, "codex"):
		return DefaultInstructions
	case strings.HasPrefix(canonical, "gpt-5.5"):
		if instructions := strings.TrimSpace(instructionsGPT55); instructions != "" {
			return instructionsGPT55
		}
	case strings.HasPrefix(canonical, "gpt-5.2"):
		if v := strings.TrimSpace(instructionsGPT52); v != "" {
			return instructionsGPT52
		}
	case strings.HasPrefix(canonical, "gpt-5.1"):
		if v := strings.TrimSpace(instructionsGPT51); v != "" {
			return instructionsGPT51
		}
	}
	return latestCodexInstructions()
}
