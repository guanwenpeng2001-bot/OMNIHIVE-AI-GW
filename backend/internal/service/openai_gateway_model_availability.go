package service

import (
	"context"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
)

// DiagnoseModelAvailabilityForPlatform reports whether the requested model
// is configured to be served by any persistently eligible OpenAI-compatible
// account in the group for the given platform (e.g. PlatformOpenAI,
// PlatformGrok). The platform scopes the candidate pool so distinct
// OpenAI-compatible platforms do not cross-contaminate diagnosis results.
// The query bypasses scheduler snapshots and ignores transient runtime state.
//
// Safe to call on the error path: returns {true,true} on any internal
// failure or when the inputs preclude meaningful diagnosis (empty model,
// nil service), so callers stay on the 503 fallback branch.
func (s *OpenAIGatewayService) DiagnoseModelAvailabilityForPlatform(
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
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	if s.accountRepo == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	platform = NormalizeOpenAICompatiblePlatform(platform)
	queryGroupID := groupID
	includeGrouped := false
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		queryGroupID = nil
		includeGrouped = true
	}
	accounts, err := s.accountRepo.ListModelAvailabilityCandidates(
		ctx,
		queryGroupID,
		[]string{platform},
		includeGrouped,
	)
	if err != nil {
		// Conservative fallback so the caller keeps returning 503; we do not
		// want a transient lookup failure to flip into 404 model_not_found.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	for i := range accounts {
		diag.HasAccountsInPool = true
		// Mapping and explicit scope are persistent configuration; runtime
		// availability and capability evidence must not decide a 404 here.
		if accounts[i].IsModelSupported(requestedModel) && accountModelScopeAllowsDiagnosis(ctx, &accounts[i], requestedModel) {
			if gatewayRequiredAction(ctx) == "embeddings" {
				account, err := accountForCatalogSelection(ctx, s.accountRepo, &accounts[i])
				if err != nil {
					return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
				}
				parent, err := loadCatalogCredentialAccount(ctx, s.accountRepo, account)
				if err != nil {
					return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
				}
				if !AccountSupportsResolvedModel(ctx, account, requestedModel, AccountModelBindingOptions{Endpoint: "embeddings", CredentialAccount: parent}) {
					continue
				}
			}
			diag.HasModelSupport = true
			return diag
		}
	}
	return diag
}
