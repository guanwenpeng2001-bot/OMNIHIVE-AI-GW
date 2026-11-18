package service

import (
	"context"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

// WithResolvedTargetPlatform stores the concrete provider chosen for a request
// made through a composite group.
func WithResolvedTargetPlatform(ctx context.Context, platform string) context.Context {
	platform = strings.TrimSpace(platform)
	if ctx == nil || platform == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxkey.ResolvedTargetPlatform, platform)
}

// ResolvedTargetPlatformFromContext returns the concrete provider chosen for
// the current request, if one was resolved.
func ResolvedTargetPlatformFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	platform, ok := ctx.Value(ctxkey.ResolvedTargetPlatform).(string)
	platform = strings.TrimSpace(platform)
	if !ok || platform == "" {
		return "", false
	}
	return platform, true
}

func WithCompositeRouteDecision(ctx context.Context, decision CompositeRouteDecision) context.Context {
	if ctx == nil || !decision.Matched {
		return ctx
	}
	ctx = WithResolvedTargetPlatform(ctx, decision.TargetPlatform)
	if len(decision.Targets) > 0 {
		ctx = WithCompositeRouteTargets(ctx, decision.Targets)
	}
	ctx = WithCompositeSelectedTarget(ctx, decision.SelectedTarget)
	requiredType := domain.ResolveAccountTypeIdentity(decision.TargetPlatform, decision.AccountType).RequiredAccountType()
	ctx = context.WithValue(ctx, hostedRouteAccountTypeKey{}, requiredType)
	if model := strings.TrimSpace(decision.UpstreamModel); model != "" {
		ctx = context.WithValue(ctx, ctxkey.ResolvedUpstreamModel, model)
	}
	if model := strings.TrimSpace(decision.PublicModel); model != "" {
		ctx = context.WithValue(ctx, ctxkey.RequestedPublicModel, model)
	}
	if source := strings.TrimSpace(decision.Source); source != "" {
		ctx = context.WithValue(ctx, ctxkey.CompositeRouteSource, source)
	}
	return ctx
}

type compositeRouteTargetsKey struct{}

type compositeSelectedTargetKey struct{}

// WithCompositeRouteTargets records the ordered fallback targets resolved for
// the request so schedulers and handlers can advance to the next target.
func WithCompositeRouteTargets(ctx context.Context, targets []CompositeRouteTarget) context.Context {
	if ctx == nil || len(targets) == 0 {
		return ctx
	}
	return context.WithValue(ctx, compositeRouteTargetsKey{}, append([]CompositeRouteTarget(nil), targets...))
}

// WithCompositeSelectedTarget pins the ordered target this attempt selected. It
// cannot be derived from the platform alone: one route may declare several
// targets of the same platform (for example one Bedrock target per region).
func WithCompositeSelectedTarget(ctx context.Context, target *CompositeRouteTarget) context.Context {
	if ctx == nil || target == nil {
		return ctx
	}
	pinned := *target
	return context.WithValue(ctx, compositeSelectedTargetKey{}, pinned)
}

func compositeSelectedTargetFromContext(ctx context.Context) (CompositeRouteTarget, bool) {
	if ctx == nil {
		return CompositeRouteTarget{}, false
	}
	target, ok := ctx.Value(compositeSelectedTargetKey{}).(CompositeRouteTarget)
	return target, ok
}

// CompositeRouteTargetsFromContext returns the ordered fallback targets of the
// current composite request, if one was resolved.
func CompositeRouteTargetsFromContext(ctx context.Context) ([]CompositeRouteTarget, bool) {
	if ctx == nil {
		return nil, false
	}
	targets, ok := ctx.Value(compositeRouteTargetsKey{}).([]CompositeRouteTarget)
	if !ok || len(targets) == 0 {
		return nil, false
	}
	return targets, true
}

type hostedRouteAccountTypeKey struct{}

func RequiredRouteAccountType(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requiredType, _ := ctx.Value(hostedRouteAccountTypeKey{}).(string)
	return requiredType
}

func ResolvedUpstreamModelFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	model, ok := ctx.Value(ctxkey.ResolvedUpstreamModel).(string)
	model = strings.TrimSpace(model)
	if !ok || model == "" {
		return "", false
	}
	return model, true
}

func RequestedPublicModelFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	model, ok := ctx.Value(ctxkey.RequestedPublicModel).(string)
	model = strings.TrimSpace(model)
	if !ok || model == "" {
		return "", false
	}
	return model, true
}

func CompositeRouteSourceFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	source, ok := ctx.Value(ctxkey.CompositeRouteSource).(string)
	source = strings.TrimSpace(source)
	if !ok || source == "" {
		return "", false
	}
	return source, true
}

// DetectModelPlatform maps common public model IDs to the concrete provider
// platform used by sub2api. It intentionally returns false for ambiguous model
// names so composite groups fail closed instead of guessing.
func DetectModelPlatform(model string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return "", false
	}

	normalized = strings.TrimPrefix(normalized, "models/")
	if slash := strings.IndexByte(normalized, '/'); slash > 0 {
		provider := strings.TrimSpace(normalized[:slash])
		rest := strings.TrimSpace(normalized[slash+1:])
		switch provider {
		case "qianwen", "dashscope":
			return PlatformQianwen, true
		case "anthropic", "claude":
			return PlatformAnthropic, true
		case "openai", "chatgpt":
			return PlatformOpenAI, true
		case "google", "google-ai-studio", "gemini":
			return PlatformGemini, true
		case "xai", "x-ai", "grok":
			return PlatformGrok, true
		case "kimi", "moonshot":
			return PlatformKimi, true
		case "zhipu", "glm", "bigmodel":
			return PlatformZhipu, true
		case "deepseek":
			return PlatformDeepseek, true
		case "minimax":
			return PlatformMiniMax, true
		}
		if rest != "" {
			normalized = strings.TrimPrefix(rest, "models/")
		}
	}

	if qianwenSharedModelRequiresRoute(normalized) {
		return "", false
	}
	switch {
	case strings.HasPrefix(normalized, "anthropic.claude-"),
		strings.HasPrefix(normalized, "claude-"):
		return PlatformAnthropic, true
	case strings.HasPrefix(normalized, "gpt-"),
		strings.HasPrefix(normalized, "chatgpt-"),
		strings.HasPrefix(normalized, "codex-"),
		strings.HasPrefix(normalized, "text-embedding-"),
		strings.HasPrefix(normalized, "text-moderation-"),
		strings.HasPrefix(normalized, "omni-moderation-"),
		strings.HasPrefix(normalized, "dall-e-"),
		strings.HasPrefix(normalized, "gpt-image-"),
		strings.HasPrefix(normalized, "tts-"),
		strings.HasPrefix(normalized, "whisper-"),
		hasOpenAISeriesPrefix(normalized):
		return PlatformOpenAI, true
	case strings.HasPrefix(normalized, "gemini-"),
		strings.HasPrefix(normalized, "learnlm-"):
		return PlatformGemini, true
	case normalized == "grok" || strings.HasPrefix(normalized, "grok-"):
		return PlatformGrok, true
	case normalized == "k3",
		normalized == "k3-256k",
		strings.HasPrefix(normalized, "kimi-"),
		strings.HasPrefix(normalized, "moonshot-"):
		return PlatformKimi, true
	case strings.HasPrefix(normalized, "glm-"):
		return PlatformZhipu, true
	case strings.HasPrefix(normalized, "deepseek-"):
		return PlatformDeepseek, true
	case strings.HasPrefix(normalized, "minimax-"),
		strings.HasPrefix(normalized, "abab5"),
		strings.HasPrefix(normalized, "abab6"),
		strings.HasPrefix(normalized, "abab7"):
		return PlatformMiniMax, true
	default:
		return "", false
	}
}

func hasOpenAISeriesPrefix(model string) bool {
	for _, prefix := range []string{"o1", "o3", "o4", "o5"} {
		if model == prefix || strings.HasPrefix(model, prefix+"-") {
			return true
		}
	}
	return false
}

func (s *GatewayService) resolveCompositeRouteDecision(ctx context.Context, group *Group, requestedModel, endpoint string) (CompositeRouteDecision, bool, error) {
	if group == nil || group.Platform != PlatformComposite {
		return CompositeRouteDecision{}, false, nil
	}
	decision, matched, err := s.buildCompositeRouteDecision(ctx, group, requestedModel, endpoint)
	if err != nil || !matched {
		return decision, matched, err
	}
	selected, err := s.selectCompositeDecisionTarget(ctx, &group.ID, decision, requestedModel, gatewayRequiredAction(ctx))
	if err != nil {
		return selected, false, err
	}
	return selected, true, nil
}

func (s *GatewayService) buildCompositeRouteDecision(ctx context.Context, group *Group, requestedModel, endpoint string) (CompositeRouteDecision, bool, error) {
	if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok {
		// Routing may already have rewritten requestedModel to the upstream name.
		// Keep the original public name for downstream model authorization.
		publicModel := requestedModel
		if originalModel, modelOK := RequestedPublicModelFromContext(ctx); modelOK {
			publicModel = originalModel
		}
		upstreamModel := requestedModel
		if resolvedModel, modelOK := ResolvedUpstreamModelFromContext(ctx); modelOK {
			upstreamModel = resolvedModel
		}
		source := CompositeRouteSourceDetector
		if resolvedSource, sourceOK := CompositeRouteSourceFromContext(ctx); sourceOK {
			source = resolvedSource
		}
		decision := CompositeRouteDecision{
			Matched:        true,
			Source:         source,
			GroupID:        group.ID,
			PublicModel:    publicModel,
			TargetPlatform: platform,
			AccountType:    RequiredRouteAccountType(ctx),
			UpstreamModel:  upstreamModel,
			Endpoint:       normalizeCompositeRouteEndpoint(endpoint),
		}
		if targets, ok := CompositeRouteTargetsFromContext(ctx); ok {
			decision.Targets = targets
		}
		return decision, true, nil
	}
	decision, err := s.compositeResolver.Resolve(ctx, group.ID, requestedModel, endpoint)
	if err != nil {
		return decision, false, err
	}
	return decision, decision.Matched, nil
}

func isConcreteRequestPlatform(platform string) bool { return domain.IsConcretePlatform(platform) }

// Both fresh and sticky selections must enforce hosting restrictions.
func accountAllowedByHostedRequest(ctx context.Context, account *Account) bool {
	if account == nil {
		return false
	}
	if ctx == nil {
		return true
	}
	if requiredType := RequiredRouteAccountType(ctx); requiredType != "" && account.Type != requiredType {
		return false
	}
	if group, ok := ctx.Value(ctxkey.Group).(*Group); ok && group != nil {
		if requiredType := group.RequiredAccountType(); requiredType != "" && account.Type != requiredType {
			return false
		}
	}
	return true
}
