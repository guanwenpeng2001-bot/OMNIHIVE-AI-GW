package service

import (
	"context"
	"slices"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
)

// ModelAvailabilityDiagnosis describes whether the requested model can be
// served by any persistently eligible account in the group (active with its
// schedulable setting enabled), ignoring transient state such as rate limits,
// overload, temporary unschedulability, and runtime blocks. Handlers use this
// on the "no available accounts" error path to distinguish 404
// model_not_found from 503 service_unavailable.
type ModelAvailabilityDiagnosis struct {
	// HasAccountsInPool is true if the group has at least one persistently
	// eligible account on the queried platform (or, for Anthropic/Gemini, on
	// the platform plus mixed-scheduled Antigravity accounts).
	HasAccountsInPool bool
	// HasModelSupport is true if at least one account's static model mapping
	// and explicit model scope admit the requested model.
	HasModelSupport bool
}

// ModelAvailabilityDiagnoser is implemented by gateway services that can
// report whether the requested model is configured to be served by any
// account. Both *GatewayService and *OpenAIGatewayService implement this so
// handlers in either package can share a single classifier.
type ModelAvailabilityDiagnoser interface {
	DiagnoseModelAvailabilityForPlatform(
		ctx context.Context,
		groupID *int64,
		requestedModel string,
		platform string,
	) ModelAvailabilityDiagnosis
}

// DiagnoseModelAvailabilityForPlatform inspects accounts enabled for scheduling
// by persistent configuration and returns whether the requested model is
// configured to be served by any of them. The dedicated repository query
// bypasses scheduler snapshots and deliberately ignores transient rate-limit,
// overload, temporary-unschedulable, expiry, quota, and runtime-block state.
//
// Safe to call on the error path: returns {true,true} on any internal failure
// or when the inputs preclude meaningful diagnosis (empty model, etc.), so
// callers stay on the 503 fallback branch.
func (s *GatewayService) DiagnoseModelAvailabilityForPlatform(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		// No model specified — cannot decide model_not_found. Caller falls back to 503.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	if strings.TrimSpace(platform) == "" {
		// Without a platform we cannot scope the lookup; bail out to the
		// 503 branch rather than make an unscoped scan.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	if s.accountRepo == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	useMixed := platform == PlatformAnthropic || platform == PlatformGemini
	platforms := []string{platform}
	if useMixed {
		platforms = append(platforms, PlatformAntigravity)
	}

	queryGroupID := groupID
	includeGrouped := false
	if useMixed {
		// Preserve the generic scheduler's scope rules: an explicit group wins
		// for mixed scheduling, while group-less simple mode scans all accounts.
		if groupID == nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
			includeGrouped = true
		}
	} else if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		queryGroupID = nil
		includeGrouped = true
	}

	accounts, err := s.accountRepo.ListModelAvailabilityCandidates(ctx, queryGroupID, platforms, includeGrouped)
	if err != nil {
		// Conservative fallback: pretend everything is fine so the caller
		// returns 503 (we don't want to flip to 404 just because a lookup
		// hiccup'd).
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	for i := range accounts {
		if useMixed && accounts[i].Platform == PlatformAntigravity && !accounts[i].IsMixedSchedulingEnabled() {
			continue
		}
		diag.HasAccountsInPool = true
		// A live request plan must not turn diagnosis into full admission:
		// expiry, runtime state and missing capability evidence are not a
		// persistent model exclusion. Only the saved mapping and scope apply.
		if s.isModelSupportedByAccount(&accounts[i], requestedModel) && accountModelScopeAllowsDiagnosis(ctx, &accounts[i], requestedModel) {
			diag.HasModelSupport = true
			return diag
		}
	}
	return diag
}

// accountModelScopeAllowsDiagnosis checks only an explicit account restriction.
// Reuse the forwarding name resolver so scope members are compared with the
// wire target, not the public alias. Do not require refreshed capability facts,
// load parent credentials, or call full binding: temporary failures stay 503/429.
func accountModelScopeAllowsDiagnosis(ctx context.Context, account *Account, requestedModel string) bool {
	var scope AccountModelAllowlist
	decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &scope)
	if !scope.Enabled {
		return true
	}
	if defaultMode, websocket := ctx.Value(openAIWebSocketSelectionKey{}).(string); websocket && account.ResolveOpenAIResponsesWebSocketV2Mode(defaultMode) == OpenAIWSIngressModePassthrough && configuredPassthroughModel(account, requestedModel) {
		// Match the relay's literal wire scope without admitting on transient
		// account state or fetching dynamic binding evidence.
		return slices.Contains(scope.Models, requestedModel)
	}
	if accountUsesHTTPResponsesPassthrough(ctx, account, "") && configuredPassthroughModel(account, normalizeAccountBindingWireModel(account, requestedModel)) {
		return slices.Contains(scope.Models, normalizeAccountBindingWireModel(account, requestedModel))
	}
	model, resolved := computeAccountBindingModel(ctx, account, requestedModel, AccountModelBindingOptions{})
	if !resolved {
		return false
	}
	for _, allowed := range scope.Models {
		if normalizeAccountBindingWireModel(account, allowed) == model {
			return true
		}
	}
	return false
}
