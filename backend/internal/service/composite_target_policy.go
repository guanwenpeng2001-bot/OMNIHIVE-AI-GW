package service

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// ErrCompositeTargetActionUnsupported is returned when a request resolves to a
// composite route whose ordered targets all lack a verified adapter for the
// requested action. The request fails explicitly instead of silently rerouting
// to a target that cannot serve it.
var ErrCompositeTargetActionUnsupported = infraerrors.BadRequest(
	"COMPOSITE_TARGET_ACTION_UNSUPPORTED",
	"no ordered composite target implements the requested action",
)

type compositeTargetAdvancementKey struct{}

// WithCompositeTargetAdvancement enables ordered-target advancement for the
// current request. Only a trunk that rewrites the outbound model to the target
// it selected may enable it: on every other entry point the request body still
// carries the model of the target resolved once by the middleware, so advancing
// would pair a request model with a different target's model.
func WithCompositeTargetAdvancement(ctx context.Context) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, compositeTargetAdvancementKey{}, true)
}

func compositeTargetAdvancementEnabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(compositeTargetAdvancementKey{}).(bool)
	return enabled
}

type compositeExcludedAccountsKey struct{}

// WithCompositeExcludedAccounts records the accounts the current failover loop
// already excluded, so ordered-target resolution can advance to the next target
// once every candidate of the earlier targets is excluded. The map is read, not
// copied: the caller keeps owning it.
func WithCompositeExcludedAccounts(ctx context.Context, excluded map[int64]struct{}) context.Context {
	if ctx == nil || len(excluded) == 0 {
		return ctx
	}
	return context.WithValue(ctx, compositeExcludedAccountsKey{}, excluded)
}

func compositeExcludedAccounts(ctx context.Context) map[int64]struct{} {
	if ctx == nil {
		return nil
	}
	excluded, _ := ctx.Value(compositeExcludedAccountsKey{}).(map[int64]struct{})
	return excluded
}

// compositeTargetActionImplemented reports whether one target platform has a
// verified adapter for the request action. An unknown action is not gated here:
// the gateway admission decides those requests.
func compositeTargetActionImplemented(target CompositeRouteTarget, action string) bool {
	action = strings.TrimSpace(action)
	if action == "" {
		return true
	}
	descriptor := domain.PlatformAction(domain.ActionIdentity{
		Platform:          target.TargetPlatform,
		HostedAccountType: target.AccountType,
	}, action)
	return descriptor.Status == domain.ActionImplemented
}

// compositeTargetActionGatedTargets keeps the declared order but drops targets
// that cannot serve the request action. A non-empty action with no remaining
// target is an explicit error rather than a silent single-target fallback.
func compositeTargetActionGatedTargets(targets []CompositeRouteTarget, action string) ([]CompositeRouteTarget, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	gated := make([]CompositeRouteTarget, 0, len(targets))
	for _, target := range targets {
		if compositeTargetActionImplemented(target, action) {
			gated = append(gated, target)
		}
	}
	if len(gated) == 0 && strings.TrimSpace(action) != "" {
		return nil, ErrCompositeTargetActionUnsupported
	}
	return gated, nil
}

// compositeTargetUpstreamModel resolves the model a target forwards upstream.
// An empty target model keeps the legacy "use the requested model" meaning.
func compositeTargetUpstreamModel(target CompositeRouteTarget, requestedModel string) string {
	if model := strings.TrimSpace(target.UpstreamModel); model != "" {
		return model
	}
	return strings.TrimSpace(requestedModel)
}

// compositeTargetLoadDenominator applies the target's scheduling weight to an
// account's load denominator. Weight shapes how much traffic a target attracts;
// the hard concurrency cap stays the account's own Concurrency and is never
// scaled by weight.
func compositeTargetLoadDenominator(base, weight int) int {
	if weight <= 0 || weight == CompositeTargetDefaultWeight {
		return base
	}
	scaled := (base*weight + CompositeTargetDefaultWeight/2) / CompositeTargetDefaultWeight
	if scaled < 1 {
		scaled = 1
	}
	return scaled
}

// CompositeTargetForAttempt resolves the ordered target the next selection
// attempt should use for a request whose composite decision already sits on the
// context. Server trunks that receive the platform as a parameter use it so
// cross-platform fallback follows the same declared order as the scheduler.
// The caller must put the already-excluded accounts on the context with
// WithCompositeExcludedAccounts and enable advancement with
// WithCompositeTargetAdvancement; without it ok=false, so the caller keeps the
// target already resolved for the request.
func (s *GatewayService) CompositeTargetForAttempt(ctx context.Context, groupID *int64) (CompositeRouteTarget, bool, error) {
	gated, publicModel, err := compositeGatedTargetsForAttempt(ctx)
	if err != nil || len(gated) == 0 {
		return CompositeRouteTarget{}, false, err
	}
	if !compositeTargetAdvancementEnabled(ctx) {
		return CompositeRouteTarget{}, false, nil
	}
	target, ok := compositeWalkTargets(ctx, gated, func(walkCtx context.Context, candidate CompositeRouteTarget) bool {
		decision := CompositeRouteDecision{
			Matched:        true,
			PublicModel:    publicModel,
			TargetPlatform: candidate.TargetPlatform,
			AccountType:    candidate.AccountType,
			Targets:        gated,
		}
		return s.compositeTargetHasCandidate(walkCtx, groupID, decision, candidate, publicModel)
	})
	return target, ok, nil
}

// CompositeTargetForAttempt is the OpenAI trunk counterpart: it walks the same
// declared order using the OpenAI schedulable-account listing.
func (s *OpenAIGatewayService) CompositeTargetForAttempt(ctx context.Context, groupID *int64) (CompositeRouteTarget, bool, error) {
	gated, publicModel, err := compositeGatedTargetsForAttempt(ctx)
	if err != nil || len(gated) == 0 {
		return CompositeRouteTarget{}, false, err
	}
	if !compositeTargetAdvancementEnabled(ctx) {
		return CompositeRouteTarget{}, false, nil
	}
	target, ok := compositeWalkTargets(ctx, gated, func(walkCtx context.Context, candidate CompositeRouteTarget) bool {
		return s.compositeTargetHasCandidate(walkCtx, groupID, candidate, publicModel)
	})
	return target, ok, nil
}

// compositeGatedTargetsForAttempt returns the action-gated ordered targets of the
// current request plus the public model used for candidate model checks.
func compositeGatedTargetsForAttempt(ctx context.Context) ([]CompositeRouteTarget, string, error) {
	targets, ok := CompositeRouteTargetsFromContext(ctx)
	if !ok {
		return nil, "", nil
	}
	gated, err := compositeTargetActionGatedTargets(targets, gatewayRequiredAction(ctx))
	if err != nil {
		return nil, "", err
	}
	return gated, compositeRequestPublicModel(ctx, CompositeRouteDecision{}), nil
}

// compositeWalkTargets returns the first target with a candidate; when every
// target is exhausted the last one is returned so the existing "no available
// account" handling still applies.
func compositeWalkTargets(ctx context.Context, targets []CompositeRouteTarget, hasCandidate func(context.Context, CompositeRouteTarget) bool) (CompositeRouteTarget, bool) {
	if len(targets) == 0 {
		return CompositeRouteTarget{}, false
	}
	if len(targets) == 1 {
		return targets[0], true
	}
	for _, target := range targets {
		if hasCandidate(ctx, target) {
			return target, true
		}
	}
	return targets[len(targets)-1], true
}

// compositeTargetHasCandidate is the OpenAI trunk candidate check.
func (s *OpenAIGatewayService) compositeTargetHasCandidate(ctx context.Context, groupID *int64, target CompositeRouteTarget, requestedModel string) bool {
	targetCtx := WithCompositeRouteDecision(ctx, CompositeRouteDecision{
		Matched:        true,
		PublicModel:    requestedModel,
		TargetPlatform: target.TargetPlatform,
		AccountType:    target.AccountType,
		UpstreamModel:  target.UpstreamModel,
	})
	accounts, err := s.listSchedulableAccounts(targetCtx, groupID, target.TargetPlatform)
	if err != nil {
		return false
	}
	excluded := compositeExcludedAccounts(ctx)
	model := compositeTargetUpstreamModel(target, requestedModel)
	for index := range accounts {
		account := &accounts[index]
		if _, skip := excluded[account.ID]; skip {
			continue
		}
		if !compositeTargetAllowsAccount(target, account) {
			continue
		}
		if !accountAllowedByHostedRequest(targetCtx, account) {
			continue
		}
		if !account.IsSchedulableForModelWithContext(targetCtx, model) {
			continue
		}
		return true
	}
	return false
}

// WithCompositeTargetSelectionContext re-points the composite decision on the
// context at one ordered target and applies its affinity TTL, so a trunk that
// receives the platform as a parameter schedules and binds the session for
// exactly that target.
func WithCompositeTargetSelectionContext(ctx context.Context, target CompositeRouteTarget) context.Context {
	targets, _ := CompositeRouteTargetsFromContext(ctx)
	decision := CompositeRouteDecision{
		Matched:        true,
		PublicModel:    compositeRequestPublicModel(ctx, CompositeRouteDecision{}),
		TargetPlatform: target.TargetPlatform,
		AccountType:    target.AccountType,
		UpstreamModel:  target.UpstreamModel,
		Targets:        targets,
		SelectedTarget: &target,
	}
	return WithCompositeTargetStickyTTL(WithCompositeRouteDecision(ctx, decision))
}

// ActiveCompositeTargetFromContext returns the ordered target the current
// selection attempt is using: the pinned selected target, otherwise the target
// matching the platform resolved for the request, otherwise the first declared
// target.
func ActiveCompositeTargetFromContext(ctx context.Context) (CompositeRouteTarget, bool) {
	if pinned, ok := compositeSelectedTargetFromContext(ctx); ok {
		return pinned, true
	}
	targets, ok := CompositeRouteTargetsFromContext(ctx)
	if !ok {
		return CompositeRouteTarget{}, false
	}
	if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok {
		for _, target := range targets {
			if target.TargetPlatform == platform {
				return target, true
			}
		}
	}
	return targets[0], true
}

// compositeRequestPublicModel prefers the public model recorded for the request.
func compositeRequestPublicModel(ctx context.Context, decision CompositeRouteDecision) string {
	if model, ok := RequestedPublicModelFromContext(ctx); ok {
		return model
	}
	return decision.PublicModel
}

// compositeDecisionForTarget re-points a decision at one ordered target while
// keeping the full ordered list for downstream handlers. The chosen target is
// pinned on the decision so same-platform targets stay distinguishable.
func compositeDecisionForTarget(decision CompositeRouteDecision, target CompositeRouteTarget, requestedModel string) CompositeRouteDecision {
	decision.TargetPlatform = target.TargetPlatform
	decision.AccountType = target.AccountType
	decision.UpstreamModel = compositeTargetUpstreamModel(target, requestedModel)
	decision.Targets = append([]CompositeRouteTarget(nil), decision.Targets...)
	selected := target
	decision.SelectedTarget = &selected
	return decision
}

// compositeTargetHasCandidate reports whether one target still has an account
// that is not excluded, satisfies the target constraints and can serve the
// target model. It reuses the existing schedulable-account listing and model
// evidence checks instead of building a parallel resolution chain.
func (s *GatewayService) compositeTargetHasCandidate(ctx context.Context, groupID *int64, decision CompositeRouteDecision, target CompositeRouteTarget, requestedModel string) bool {
	targetDecision := compositeDecisionForTarget(decision, target, requestedModel)
	targetCtx := WithCompositeRouteDecision(ctx, targetDecision)
	accounts, _, err := s.listSchedulableAccounts(targetCtx, groupID, target.TargetPlatform, false)
	if err != nil {
		return false
	}
	excluded := compositeExcludedAccounts(ctx)
	model := compositeTargetUpstreamModel(target, requestedModel)
	for index := range accounts {
		account := &accounts[index]
		if _, skip := excluded[account.ID]; skip {
			continue
		}
		if !compositeTargetAllowsAccount(target, account) {
			continue
		}
		if !s.isAccountSchedulableForModelSelection(targetCtx, account, model) {
			continue
		}
		return true
	}
	return false
}

// selectCompositeDecisionTarget walks the ordered targets and returns the first
// one that still has a candidate. A single target keeps the existing selection
// path untouched (including its wait-queue behavior); when every target is
// exhausted the last one is returned so the existing "no available account"
// handling still applies. Walking requires WithCompositeTargetAdvancement: the
// declared order is only meaningful for a caller that also sends the selected
// target's upstream model.
func (s *GatewayService) selectCompositeDecisionTarget(ctx context.Context, groupID *int64, decision CompositeRouteDecision, requestedModel, action string) (CompositeRouteDecision, error) {
	if !compositeTargetAdvancementEnabled(ctx) {
		// 只有能在本次尝试内改写模型的主干才推进目标：其它入口的请求体仍带着中间件
		// 解析出的目标模型，推进会把请求模型与另一个目标的目标模型错配。这些入口保持
		// 既有单目标语义，动作由既有的账号级动作证据与分派检查把关。
		return decision, nil
	}
	targets, err := compositeTargetActionGatedTargets(decision.Targets, action)
	if err != nil {
		return decision, err
	}
	if len(targets) == 0 {
		return decision, nil
	}
	decision.Targets = targets
	publicModel := compositeRequestPublicModel(ctx, decision)
	if len(targets) == 1 {
		return compositeDecisionForTarget(decision, targets[0], publicModel), nil
	}
	for _, target := range targets {
		if s.compositeTargetHasCandidate(ctx, groupID, decision, target, publicModel) {
			return compositeDecisionForTarget(decision, target, publicModel), nil
		}
	}
	return compositeDecisionForTarget(decision, targets[len(targets)-1], publicModel), nil
}
