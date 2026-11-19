package protocol

import "context"

type ConversionMode string

const (
	Native   ConversionMode = "native"
	Bridge   ConversionMode = "bridge"
	Rejected ConversionMode = "rejected"
)

const (
	AnthropicMessages = "anthropic_messages"
	GeminiGenerate    = "gemini_generate"
	GeminiCountTokens = "gemini_count_tokens"
	GeminiEmbedding   = "gemini_embedding"
)

type Diagnostic struct {
	Field    string `json:"field"`
	Reason   string `json:"reason"`
	Severity string `json:"severity"`
}

type ConversionPlan struct {
	Mode           ConversionMode `json:"mode"`
	SourceProtocol string         `json:"source_protocol"`
	TargetProtocol string         `json:"target_protocol"`
	Diagnostics    []Diagnostic   `json:"diagnostics"`
}

type conversionPlanKey struct{}

func WithConversionPlan(ctx context.Context, plan ConversionPlan) context.Context {
	plan.Diagnostics = append([]Diagnostic{}, plan.Diagnostics...)
	return context.WithValue(ctx, conversionPlanKey{}, plan)
}

func ConversionPlanFromContext(ctx context.Context) (ConversionPlan, bool) {
	plan, ok := ctx.Value(conversionPlanKey{}).(ConversionPlan)
	if ok {
		plan.Diagnostics = append([]Diagnostic{}, plan.Diagnostics...)
	}
	return plan, ok
}

func GeminiNativePlan(action string) ConversionPlan {
	target := GeminiGenerate
	switch action {
	case "countTokens":
		target = GeminiCountTokens
	case "embedContent", "batchEmbedContents":
		target = GeminiEmbedding
	}
	return ConversionPlan{Mode: Native, SourceProtocol: target, TargetProtocol: target, Diagnostics: []Diagnostic{}}
}
