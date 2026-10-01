package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"sort"
	"time"
)

// Bounded probes deliberately bypass ProbeAccount: connectivity tests may
// recover credentials, clear rate limits or update account health.
var ErrBoundedModelProbeUnsupported = errors.New("bounded model probe protocol unavailable")

type boundedModelProbeTarget struct {
	model    string
	endpoint string
}

// Rebuild the queue from persisted declarations and attempts, so cancellation
// and process restarts never refund or replay an uncertain attempt. A target is
// attempted at most once per UTC month; due targets still share the 5/50 budget.
func boundedModelProbeTargets(account *Account, now time.Time) []boundedModelProbeTarget {
	if account == nil || !account.IsActive() || !account.Schedulable || account.IsShadow() || account.IsSyntheticUITest() || (account.ExpiresAt != nil && !now.Before(*account.ExpiresAt)) {
		return nil
	}
	snapshot := account.GetUpstreamModelCatalog()
	state := account.ModelCatalogState()
	if snapshot == nil || snapshot.IdentityRevision != state.IdentityRevision || state.Result == "failed" || state.Result == "credentials_invalid" {
		return nil
	}
	observed, err := time.Parse(time.RFC3339Nano, snapshot.ObservedAt)
	if err != nil || observed.After(now) || !now.Before(observed.AddDate(0, 1, 0)) {
		return nil
	}
	var budget ModelProbeBudget
	if _, exists := account.Extra[ModelProbeBudgetExtraKey]; exists && !decodeCatalogExtra(account.Extra, ModelProbeBudgetExtraKey, &budget) {
		return nil
	}
	targets := []boundedModelProbeTarget{}
	seen := map[boundedModelProbeTarget]bool{}
	for _, model := range configuredModelMapping(account) {
		if !concreteMappedModel(account, model) || slices.Contains(snapshot.DiscoveredModels, model) {
			continue
		}
		for _, endpoint := range accountCatalogNativeTextEndpoints(account, model) {
			target := boundedModelProbeTarget{model: model, endpoint: endpoint}
			if seen[target] || !boundedModelProbeSupported(account, target) {
				continue
			}
			seen[target] = true
			due := true
			for _, declaration := range account.ModelAvailabilityDeclarations() {
				if declaration.Model != model || declaration.Endpoint != endpoint {
					continue
				}
				// Explicit revocation and stale identity never auto-renew.
				if declaration.RevokedAt != nil || declaration.IdentityRevision != state.IdentityRevision || declaration.RulesRevision != state.RulesRevision || declaration.ConfirmedAt.After(now) || now.Before(declaration.ConfirmedAt.AddDate(0, 1, 0)) {
					due = false
				}
			}
			for _, attempt := range budget.Attempts {
				if attempt.Model == model && attempt.Endpoint == endpoint && attempt.ReservedAt.UTC().Format("2006-01") >= now.UTC().Format("2006-01") {
					due = false
				}
			}
			if due {
				targets = append(targets, target)
			}
		}
	}
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].model == targets[right].model {
			return targets[left].endpoint < targets[right].endpoint
		}
		return targets[left].model < targets[right].model
	})
	return targets
}

func boundedModelProbeSupported(account *Account, target boundedModelProbeTarget) bool {
	// Only established API-key text contracts are enabled. Media has no audited
	// actual-cost/final-success contract here; OAuth can have recovery side effects.
	if metadata, exists := account.GetUpstreamModelMetadata(target.model); exists {
		if task := accountCatalogEndpointTask(account, target.model, metadata); task != "" && task != "text_generation" {
			return false
		}
	}
	return account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey &&
		(target.endpoint == CompositeRouteEndpointChatCompletions || target.endpoint == CompositeRouteEndpointResponses) &&
		!isCodexDedicatedMediaModel(target.model) && !isImageGenerationModel(target.model)
}

func (service *AccountTestService) runBoundedModelProbes(ctx context.Context, accountID int64, now time.Time) error {
	repository, ok := service.accountRepo.(ModelProbeBudgetRepository)
	locker, lockOK := service.accountRepo.(AccountModelCatalogLocker)
	if !ok || !lockOK || service.httpUpstream == nil {
		return ErrBoundedModelProbeUnsupported
	}
	// Reuse catalog serialization across replicas, without a second queue store.
	releaseBudget, err := service.acquireCatalogApplyBudget(ctx)
	if err != nil {
		return err
	}
	defer releaseBudget()
	unlock, err := locker.TryLockAccountModelCatalog(ctx, accountID)
	if err != nil {
		return err
	}
	defer unlock()
	account, err := service.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	for _, target := range boundedModelProbeTargets(account, now) {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Successful completion advances rules CAS: reload for each reservation.
		account, err = service.accountRepo.GetByID(ctx, accountID)
		if err != nil {
			return err
		}
		if !slices.Contains(boundedModelProbeTargets(account, now), target) {
			continue
		}
		request, err := service.boundedModelProbeRequest(ctx, account, target)
		if err != nil {
			return err
		}
		if _, err := account.EgressProxyURL(ctx); err != nil {
			return err
		}
		attempt, err := repository.ReserveModelProbe(ctx, accountID, account.ModelCatalogBaseline(), target.model, target.endpoint)
		if err != nil {
			return err
		}
		if _, err = repository.ClaimModelProbe(ctx, accountID, attempt.ID); err != nil {
			return err
		}
		// No retries after claim, including on network failure or cancellation.
		outcome := service.executeBoundedModelProbe(ctx, account, target, request)
		if err := repository.CompleteModelProbe(ctx, accountID, attempt.ID, outcome); err != nil {
			return err
		}
	}
	return nil
}

func (service *AccountTestService) boundedModelProbeRequest(ctx context.Context, account *Account, target boundedModelProbeTarget) (*http.Request, error) {
	if !boundedModelProbeSupported(account, target) || account.GetOpenAIProtocolAPIKey() == "" {
		return nil, ErrBoundedModelProbeUnsupported
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	normalized, err := service.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, ErrBoundedModelProbeUnsupported
	}
	apiURL := buildOpenAIChatCompletionsURL(normalized)
	payload := createOpenAIChatCompletionsTestPayload(target.model, "hi")
	if target.endpoint == CompositeRouteEndpointResponses {
		apiURL = buildOpenAIResponsesURLForPlatform(account.Platform, normalized)
		payload = createOpenAITestPayload(target.model, false)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI), http.MethodPost, apiURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Authorization", "Bearer "+account.GetOpenAIProtocolAPIKey())
	account.ApplyHeaderOverrides(request.Header)
	return request, nil
}

func (service *AccountTestService) executeBoundedModelProbe(ctx context.Context, account *Account, target boundedModelProbeTarget, request *http.Request) ModelProbeOutcome {
	if ctx.Err() != nil {
		return ModelProbeTemporary
	}
	proxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return ModelProbeTemporary
	}
	response, err := accountHTTPUpstream(service.httpUpstream, account).DoWithTLS(request, proxyURL, account.ID, account.Concurrency, service.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil || response == nil || response.Body == nil {
		return ModelProbeTemporary
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		explicitMissing := readErr == nil && json.Unmarshal(body, &envelope) == nil && envelope.Error.Code == "model_not_found"
		return ClassifyModelProbeOutcome(response.StatusCode, true, explicitMissing, false)
	}
	completed := false
	probe := &accountProbeContext{ctx: ctx, emit: func(event TestEvent) {
		if event.Type == "test_complete" {
			completed = event.Success
		}
	}}
	// Reuse the native terminal-event parsers, without health-test side effects.
	body := io.LimitReader(response.Body, 1<<20)
	if target.endpoint == CompositeRouteEndpointResponses {
		err = service.processOpenAIStream(probe, body)
	} else {
		err = service.processOpenAIChatCompletionsStream(probe, body)
	}
	return ClassifyModelProbeOutcome(response.StatusCode, true, false, completed && err == nil && ctx.Err() == nil)
}
