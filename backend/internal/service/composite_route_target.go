package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

const (
	// CompositeTargetLatencyDefault keeps the pre-existing account selection order.
	CompositeTargetLatencyDefault = "default"
	// CompositeTargetLatencyLowLatency biases selection towards the upstream
	// account with the best recently observed first-token latency. Schedulers
	// without a latency signal keep their existing order.
	CompositeTargetLatencyLowLatency = "low_latency"

	// CompositeTargetDefaultWeight keeps one target equivalent to the legacy
	// single-target route; weights only compare targets of the same route.
	CompositeTargetDefaultWeight = 100
	// CompositeTargetMaxCount bounds one route's fallback list.
	CompositeTargetMaxCount = 16
	// CompositeTargetMinAffinityTTLSeconds and CompositeTargetMaxAffinityTTLSeconds
	// bound the optional per-target sticky-session TTL.
	CompositeTargetMinAffinityTTLSeconds = 60
	CompositeTargetMaxAffinityTTLSeconds = 86400
)

var compositeTargetConstraintPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var (
	ErrCompositeTargetInvalid = infraerrors.BadRequest("COMPOSITE_TARGET_INVALID", "invalid composite route target")
)

// CompositeRouteTarget is one ordered fallback target of a composite model
// route. Targets of one route share the public model and the request action;
// only the concrete provider, upstream model, constraints and scheduling
// preferences differ. Position preserves the admin-declared order.
type CompositeRouteTarget struct {
	Position           int    `json:"position"`
	TargetPlatform     string `json:"target_platform"`
	AccountType        string `json:"account_type,omitempty"`
	UpstreamModel      string `json:"upstream_model,omitempty"`
	Region             string `json:"region,omitempty"`
	Provider           string `json:"provider,omitempty"`
	LatencyPreference  string `json:"latency_preference,omitempty"`
	SchedulingWeight   int    `json:"scheduling_weight"`
	AffinityTTLSeconds int    `json:"affinity_ttl_seconds,omitempty"`
	Enabled            bool   `json:"enabled"`
}

func normalizeCompositeTargetLatencyPreference(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CompositeTargetLatencyLowLatency:
		return CompositeTargetLatencyLowLatency
	default:
		return CompositeTargetLatencyDefault
	}
}

func normalizeCompositeTargetConstraint(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// normalizeCompositeRouteTargets validates the admin-declared fallback list and
// rewrites positions to the declared order. A nil list means "keep the current
// targets"; an empty non-nil list clears them back to the legacy single target.
func normalizeCompositeRouteTargets(targets []CompositeRouteTarget) ([]CompositeRouteTarget, error) {
	if len(targets) > CompositeTargetMaxCount {
		return nil, infraerrors.BadRequest("COMPOSITE_TARGET_INVALID", "too many ordered targets (max 16)")
	}
	out := make([]CompositeRouteTarget, 0, len(targets))
	enabled := 0
	for index, target := range targets {
		target.Position = index
		target.TargetPlatform = strings.ToLower(strings.TrimSpace(target.TargetPlatform))
		target.AccountType = strings.TrimSpace(target.AccountType)
		target.UpstreamModel = strings.TrimSpace(target.UpstreamModel)
		target.Region = normalizeCompositeTargetConstraint(target.Region)
		target.Provider = normalizeCompositeTargetConstraint(target.Provider)
		target.LatencyPreference = normalizeCompositeTargetLatencyPreference(target.LatencyPreference)

		if !isConcreteRequestPlatform(target.TargetPlatform) {
			return nil, infraerrors.BadRequest("COMPOSITE_TARGET_INVALID", "target_platform must be a concrete provider")
		}
		if err := domain.ValidateHostedRestriction(target.TargetPlatform, target.AccountType); err != nil {
			// 与其余目标校验失败保持同一 HTTP 形状（400 + 稳定 reason），
			// 而不是把领域层的裸错误冒泡成 500。
			return nil, infraerrors.BadRequest(
				"COMPOSITE_TARGET_INVALID",
				fmt.Sprintf("account_type %q is not valid for target_platform %q", target.AccountType, target.TargetPlatform),
			)
		}
		if target.SchedulingWeight < 0 || target.SchedulingWeight > 10000 {
			return nil, infraerrors.BadRequest("COMPOSITE_TARGET_INVALID", "scheduling_weight must be between 0 and 10000")
		}
		if target.SchedulingWeight == 0 {
			target.SchedulingWeight = CompositeTargetDefaultWeight
		}
		if target.AffinityTTLSeconds != 0 &&
			(target.AffinityTTLSeconds < CompositeTargetMinAffinityTTLSeconds || target.AffinityTTLSeconds > CompositeTargetMaxAffinityTTLSeconds) {
			return nil, infraerrors.BadRequest("COMPOSITE_TARGET_INVALID", "affinity_ttl_seconds must be 0 or between 60 and 86400")
		}
		if target.Region != "" && !compositeTargetConstraintPattern.MatchString(target.Region) {
			return nil, infraerrors.BadRequest("COMPOSITE_TARGET_INVALID", "region must match [A-Za-z0-9][A-Za-z0-9._-]{0,63}")
		}
		if target.Provider != "" && !compositeTargetConstraintPattern.MatchString(target.Provider) {
			return nil, infraerrors.BadRequest("COMPOSITE_TARGET_INVALID", "provider must match [A-Za-z0-9][A-Za-z0-9._-]{0,63}")
		}
		if target.Enabled {
			enabled++
		}
		out = append(out, target)
	}
	if len(out) > 0 && enabled == 0 {
		return nil, infraerrors.BadRequest("COMPOSITE_TARGET_INVALID", "at least one ordered target must be enabled")
	}
	return out, nil
}

// EffectiveTargets returns the fallback list the runtime should use. Routes
// without ordered targets fall back to one target synthesized from the legacy
// columns, so pre-existing single-target routes keep their exact behavior.
func (r CompositeModelRoute) EffectiveTargets() []CompositeRouteTarget {
	if len(r.Targets) == 0 {
		return []CompositeRouteTarget{{
			Position:          0,
			TargetPlatform:    r.TargetPlatform,
			AccountType:       r.AccountType,
			UpstreamModel:     r.UpstreamModel,
			LatencyPreference: CompositeTargetLatencyDefault,
			SchedulingWeight:  CompositeTargetDefaultWeight,
			Enabled:           true,
		}}
	}
	out := make([]CompositeRouteTarget, 0, len(r.Targets))
	for _, target := range r.Targets {
		if !target.Enabled {
			continue
		}
		out = append(out, target)
	}
	return out
}

// PrimaryTarget returns the first enabled target, which is mirrored into the
// route's legacy columns for rollback compatibility.
func (r CompositeModelRoute) PrimaryTarget() (CompositeRouteTarget, bool) {
	targets := r.EffectiveTargets()
	if len(targets) == 0 {
		return CompositeRouteTarget{}, false
	}
	return targets[0], true
}

// AffinityTTLDuration returns the configured sticky-session TTL. Zero means the
// caller keeps the legacy default.
func (t CompositeRouteTarget) AffinityTTLDuration() time.Duration {
	if t.AffinityTTLSeconds < CompositeTargetMinAffinityTTLSeconds || t.AffinityTTLSeconds > CompositeTargetMaxAffinityTTLSeconds {
		return 0
	}
	return time.Duration(t.AffinityTTLSeconds) * time.Second
}

// accountRoutingRegion resolves the region an account actually serves from its
// explicitly configured attributes. Region is not a first-class account column;
// hosted platforms carry it in credentials (Bedrock aws_region, Vertex location).
func accountRoutingRegion(account *Account) string {
	if account == nil {
		return ""
	}
	for _, key := range []string{"region"} {
		if value := strings.TrimSpace(account.GetExtraString(key)); value != "" {
			return normalizeCompositeTargetConstraint(value)
		}
	}
	for _, key := range []string{"region", "aws_region", "location", "vertex_location", "vertex_region"} {
		if value := strings.TrimSpace(account.GetCredential(key)); value != "" {
			return normalizeCompositeTargetConstraint(value)
		}
	}
	return ""
}

// accountRoutingProvider resolves the upstream provider an account belongs to.
// An explicit provider/vendor attribute wins; otherwise the account platform is
// the provider identity used by this gateway.
func accountRoutingProvider(account *Account) string {
	if account == nil {
		return ""
	}
	if value := strings.TrimSpace(account.GetExtraString("provider")); value != "" {
		return normalizeCompositeTargetConstraint(value)
	}
	if value := strings.TrimSpace(account.GetExtraString("vendor")); value != "" {
		return normalizeCompositeTargetConstraint(value)
	}
	for _, key := range []string{"provider", "vendor"} {
		if value := strings.TrimSpace(account.GetCredential(key)); value != "" {
			return normalizeCompositeTargetConstraint(value)
		}
	}
	return normalizeCompositeTargetConstraint(account.Platform)
}

// compositeTargetAllowsAccount reports whether an account satisfies one target's
// declared region/provider/hosted-type constraints. Unknown attributes do not
// satisfy an explicit constraint, so a constrained target fails closed instead
// of leaking traffic to an unverified account.
func compositeTargetAllowsAccount(target CompositeRouteTarget, account *Account) bool {
	if account == nil {
		return false
	}
	requiredType := domain.ResolveAccountTypeIdentity(target.TargetPlatform, strings.TrimSpace(target.AccountType)).RequiredAccountType()
	if requiredType != "" && account.Type != requiredType {
		return false
	}
	if target.Region != "" && accountRoutingRegion(account) != target.Region {
		return false
	}
	if target.Provider != "" && accountRoutingProvider(account) != target.Provider {
		return false
	}
	return true
}
