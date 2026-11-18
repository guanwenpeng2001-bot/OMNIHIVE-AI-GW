package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type boundedProbeRepository struct {
	AccountRepository // Any unexpected health mutation panics rather than passing.
	account           Account
	now               time.Time
	budget            ModelProbeBudget
	events            []string
	claimError        error
	completeError     error
	locked            bool
}

func (repository *boundedProbeRepository) GetByID(context.Context, int64) (*Account, error) {
	return &repository.account, nil
}

func (repository *boundedProbeRepository) ListActive(context.Context) ([]Account, error) {
	return []Account{repository.account}, nil
}

func (repository *boundedProbeRepository) TryLockAccountModelCatalog(context.Context, int64) (func(), error) {
	if repository.locked {
		return nil, ErrAccountModelCatalogConflict
	}
	repository.locked = true
	return func() { repository.locked = false }, nil
}

func (repository *boundedProbeRepository) ReserveModelProbe(ctx context.Context, accountID int64, baseline AccountModelCatalogBaseline, model, endpoint string) (*ModelProbeAttempt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if baseline != repository.account.ModelCatalogBaseline() {
		return nil, ErrAccountModelCatalogConflict
	}
	attempt := ModelProbeAttempt{ID: fmt.Sprintf("attempt-%d", repository.budget.Revision), AccountID: accountID, Model: model, Endpoint: endpoint, ReservedAt: repository.now}
	if err := repository.budget.Reserve(attempt); err != nil {
		return nil, err
	}
	repository.account.Extra[ModelProbeBudgetExtraKey] = repository.budget
	repository.events = append(repository.events, "reserve")
	return &attempt, nil
}

func (repository *boundedProbeRepository) ClaimModelProbe(ctx context.Context, accountID int64, attemptID string) (*ModelProbeAttempt, error) {
	if repository.claimError != nil {
		return nil, repository.claimError
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for index := range repository.budget.Attempts {
		attempt := &repository.budget.Attempts[index]
		if attempt.ID == attemptID && attempt.AccountID == accountID && attempt.ClaimedAt == nil {
			attempt.ClaimedAt = &repository.now
			repository.account.Extra[ModelProbeBudgetExtraKey] = repository.budget
			repository.events = append(repository.events, "claim")
			return attempt, nil
		}
	}
	return nil, ErrModelProbeAttemptClaimed
}

func (repository *boundedProbeRepository) CompleteModelProbe(ctx context.Context, accountID int64, attemptID string, outcome ModelProbeOutcome) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if repository.completeError != nil {
		return repository.completeError
	}
	for index := range repository.budget.Attempts {
		attempt := &repository.budget.Attempts[index]
		if attempt.ID == attemptID && attempt.AccountID == accountID {
			attempt.CompletedAt = &repository.now
			attempt.Outcome = string(outcome)
			repository.account.Extra[ModelProbeBudgetExtraKey] = repository.budget
			repository.events = append(repository.events, "complete:"+string(outcome))
			return nil
		}
	}
	return ErrModelProbeAttemptClaimed
}

type boundedProbeTransport struct {
	HTTPUpstream
	repository    *boundedProbeRepository
	status        int
	body          string
	requestModels []string
	paths         []string
	failure       error
	cancel        context.CancelFunc
}

func (transport *boundedProbeTransport) DoWithTLS(request *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	transport.repository.events = append(transport.repository.events, "send")
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		return nil, err
	}
	transport.requestModels = append(transport.requestModels, payload.Model)
	transport.paths = append(transport.paths, request.URL.Path)
	if transport.cancel != nil {
		transport.cancel()
	}
	if transport.failure != nil {
		return nil, transport.failure
	}
	return &http.Response{StatusCode: transport.status, Body: io.NopCloser(strings.NewReader(transport.body)), Header: make(http.Header)}, nil
}

func boundedProbeFixture() (*AccountTestService, *boundedProbeRepository, *boundedProbeTransport) {
	now := time.Date(2026, time.September, 19, 0, 0, 0, 0, time.UTC)
	repository := &boundedProbeRepository{now: now, account: Account{
		ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"alias": "private-model"}},
		Extra:       map[string]any{UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: true, ObservedAt: now.Format(time.RFC3339Nano), DiscoveredModels: []string{"native-model"}}},
	}}
	transport := &boundedProbeTransport{repository: repository, status: 200, body: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"}
	service := &AccountTestService{accountRepo: repository, httpUpstream: transport, cfg: &config.Config{}}
	return service, repository, transport
}

func TestBoundedModelProbeLifecycle(test *testing.T) {
	test.Run("reserve claim send complete and exact mapping target", func(test *testing.T) {
		service, repository, transport := boundedProbeFixture()
		// The concrete target is also a mapping key; it must not be mapped twice.
		repository.account.Credentials["model_mapping"] = map[string]any{"alias": "private-model", "private-model": "native-model", "wildcard": "ignored-*"}
		require.NoError(test, service.runBoundedModelProbes(context.Background(), 42, repository.now))
		require.Equal(test, []string{"private-model"}, transport.requestModels)
		require.Equal(test, []string{"reserve", "claim", "send", "complete:success"}, repository.events)
		require.Equal(test, 1, repository.budget.DailyUsed)
		require.False(test, repository.locked)
	})

	test.Run("selection preserves complete fresh discovery boundary", func(test *testing.T) {
		for _, fixture := range []struct {
			name   string
			change func(*Account)
		}{
			{"missing discovery", func(account *Account) { delete(account.Extra, UpstreamModelCatalogExtraKey) }},
			{"partial discovery", func(account *Account) {
				snapshot := account.GetUpstreamModelCatalog()
				snapshot.Complete = false
				account.Extra[UpstreamModelCatalogExtraKey] = snapshot
			}},
			{"stale discovery", func(account *Account) {
				snapshot := account.GetUpstreamModelCatalog()
				snapshot.ObservedAt = "2026-08-19T00:00:00Z"
				account.Extra[UpstreamModelCatalogExtraKey] = snapshot
			}},
			{"future discovery", func(account *Account) {
				snapshot := account.GetUpstreamModelCatalog()
				snapshot.ObservedAt = "2026-10-01T00:00:00Z"
				account.Extra[UpstreamModelCatalogExtraKey] = snapshot
			}},
			{"failed discovery", func(account *Account) {
				account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "failed"}
			}},
			{"changed identity", func(account *Account) {
				account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 2}
			}},
			{"discovered target", func(account *Account) {
				snapshot := account.GetUpstreamModelCatalog()
				snapshot.DiscoveredModels = []string{"private-model"}
				account.Extra[UpstreamModelCatalogExtraKey] = snapshot
			}},
			{"disabled account", func(account *Account) { account.Schedulable = false }},
			{"oauth", func(account *Account) { account.Type = AccountTypeOAuth }},
			{"media without cost contract", func(account *Account) { account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-image-1"} }},
			{"unsupported platform", func(account *Account) { account.Platform = PlatformGrok }},
			{"corrupt budget", func(account *Account) { account.Extra[ModelProbeBudgetExtraKey] = "invalid" }},
		} {
			test.Run(fixture.name, func(test *testing.T) {
				service, repository, transport := boundedProbeFixture()
				fixture.change(&repository.account)
				require.NoError(test, service.runBoundedModelProbes(context.Background(), 42, repository.now))
				require.Empty(test, transport.requestModels)
				require.Zero(test, repository.budget.DailyUsed)
			})
		}
	})

	test.Run("classify terminal and explicit errors without health writes", func(test *testing.T) {
		for _, fixture := range []struct {
			name    string
			status  int
			body    string
			failure error
			outcome ModelProbeOutcome
		}{
			{"explicit missing 404", 404, `{"error":{"code":"model_not_found"}}`, nil, ModelProbeMissing},
			{"explicit missing 400", 400, `{"error":{"code":"model_not_found"}}`, nil, ModelProbeMissing},
			{"wrong path 404", 404, `<html>not found</html>`, nil, ModelProbeTemporary},
			{"bad request 400", 400, `{"error":{"code":"invalid_request_error"}}`, nil, ModelProbeTemporary},
			{"rate limit", 429, `{"error":{"code":"model_not_found"}}`, nil, ModelProbeTemporary},
			{"quota", 400, `{"error":{"code":"insufficient_quota"}}`, nil, ModelProbeTemporary},
			{"unauthorized", 401, `{"error":{"code":"invalid_api_key"}}`, nil, ModelProbeTemporary},
			{"network", 0, "", errors.New("network unavailable"), ModelProbeTemporary},
			{"partial stream", 200, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n", nil, ModelProbeTemporary},
			{"done only", 200, "data: [DONE]\n\n", nil, ModelProbeTemporary},
		} {
			test.Run(fixture.name, func(test *testing.T) {
				service, repository, transport := boundedProbeFixture()
				transport.status, transport.body, transport.failure = fixture.status, fixture.body, fixture.failure
				declaration := []ModelAvailabilityDeclaration{{Model: "private-model", Endpoint: "chat_completions", ConfirmedAt: repository.now.AddDate(0, -2, 0), EvidenceKind: ModelEvidenceProbe}}
				repository.account.Extra[ModelAvailabilityExtraKey] = declaration
				require.NoError(test, service.runBoundedModelProbes(context.Background(), 42, repository.now))
				require.Equal(test, string(fixture.outcome), repository.budget.Attempts[0].Outcome)
				require.Equal(test, StatusActive, repository.account.Status)
				require.Equal(test, declaration, repository.account.Extra[ModelAvailabilityExtraKey], "only repository completion owns declaration writes")
			})
		}
	})

	test.Run("responses uses native completion parser", func(test *testing.T) {
		service, repository, transport := boundedProbeFixture()
		repository.account.Extra["responses_supported"] = true
		target := boundedModelProbeTarget{model: "private-model", endpoint: CompositeRouteEndpointResponses}
		request, err := service.boundedModelProbeRequest(context.Background(), &repository.account, target)
		require.NoError(test, err)
		transport.body = "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture\",\"status\":\"completed\",\"output\":[]}}\n\n"
		require.Equal(test, ModelProbeSucceeded, service.executeBoundedModelProbe(context.Background(), &repository.account, target, request))
		require.Equal(test, []string{"/v1/responses"}, transport.paths)
	})

	test.Run("budget refusal prevents outbound request", func(test *testing.T) {
		for _, used := range []struct{ daily, monthly int }{{5, 5}, {0, 50}} {
			service, repository, transport := boundedProbeFixture()
			repository.budget = ModelProbeBudget{Day: "2026-09-19", Month: "2026-09", DailyUsed: used.daily, MonthlyUsed: used.monthly}
			require.ErrorIs(test, service.runBoundedModelProbes(context.Background(), 42, repository.now), ErrModelProbeBudgetExceeded)
			require.Empty(test, transport.requestModels)
		}
	})

	test.Run("queue carries excess due targets to next day", func(test *testing.T) {
		service, repository, transport := boundedProbeFixture()
		mapping := map[string]any{}
		for index := 0; index < 6; index++ {
			mapping[fmt.Sprintf("alias-%d", index)] = fmt.Sprintf("private-model-%d", index)
		}
		repository.account.Credentials["model_mapping"] = mapping
		runner := &ScheduledTestRunnerService{accountTestSvc: service}
		runner.processBoundedModelProbeQueue(context.Background(), repository.now)
		require.Len(test, transport.requestModels, 5)
		require.Equal(test, 5, repository.budget.DailyUsed)
		runner.processBoundedModelProbeQueue(context.Background(), repository.now)
		require.Len(test, transport.requestModels, 5)
		repository.now = repository.now.Add(24 * time.Hour)
		runner.processBoundedModelProbeQueue(context.Background(), repository.now)
		require.Len(test, transport.requestModels, 6)
		require.Equal(test, 1, repository.budget.DailyUsed)
		require.Equal(test, 6, repository.budget.MonthlyUsed)
	})

	test.Run("known nontext metadata never spends text budget", func(test *testing.T) {
		service, repository, transport := boundedProbeFixture()
		repository.account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"private-model": {TaskType: "video_generation"}}})
		require.NoError(test, service.runBoundedModelProbes(context.Background(), 42, repository.now))
		require.Empty(test, transport.requestModels)
		require.Zero(test, repository.budget.MonthlyUsed)
	})

	test.Run("claim failure and unknown completion survive restart", func(test *testing.T) {
		for _, failure := range []string{"claim", "complete", "cancel"} {
			test.Run(failure, func(test *testing.T) {
				service, repository, transport := boundedProbeFixture()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if failure == "claim" {
					repository.claimError = ErrModelProbeAttemptClaimed
				}
				if failure == "complete" {
					repository.completeError = errors.New("persistence unavailable")
				}
				if failure == "cancel" {
					transport.cancel = cancel
				}
				require.Error(test, service.runBoundedModelProbes(ctx, 42, repository.now))
				before := len(transport.requestModels)
				if failure == "claim" {
					require.Zero(test, before)
				}
				repository.claimError, repository.completeError, transport.cancel = nil, nil, nil
				restarted := &AccountTestService{accountRepo: repository, httpUpstream: transport, cfg: &config.Config{}}
				require.NoError(test, restarted.runBoundedModelProbes(context.Background(), 42, repository.now.Add(time.Hour)))
				require.Len(test, transport.requestModels, before)
				require.Equal(test, 1, repository.budget.DailyUsed)
			})
		}
	})

	test.Run("monthly queue respects expiry revocation and budget", func(test *testing.T) {
		service, repository, transport := boundedProbeFixture()
		confirmed := repository.now.AddDate(0, -1, 0).Add(time.Hour)
		repository.account.Extra[ModelAvailabilityExtraKey] = []ModelAvailabilityDeclaration{{AccountID: 42, Model: "private-model", Endpoint: "chat_completions", ConfirmedAt: confirmed, EvidenceKind: ModelEvidenceProbe}}
		runner := &ScheduledTestRunnerService{accountTestSvc: service}
		runner.runBoundedModelProbeQueue() // Disabled by default, no account reads.
		runner.processBoundedModelProbeQueue(context.Background(), repository.now)
		require.Empty(test, transport.requestModels)
		repository.now = repository.now.Add(time.Hour)
		runner.processBoundedModelProbeQueue(context.Background(), repository.now)
		require.Len(test, transport.requestModels, 1)
		runner.processBoundedModelProbeQueue(context.Background(), repository.now.Add(time.Hour))
		require.Len(test, transport.requestModels, 1)
		// A fresh full discovery is required again in the next month.
		repository.now = repository.now.AddDate(0, 1, 0)
		snapshot := repository.account.GetUpstreamModelCatalog()
		snapshot.ObservedAt = repository.now.Format(time.RFC3339Nano)
		repository.account.Extra[UpstreamModelCatalogExtraKey] = snapshot
		runner.processBoundedModelProbeQueue(context.Background(), repository.now)
		require.Len(test, transport.requestModels, 2)
		require.Equal(test, 1, repository.budget.MonthlyUsed)
		declarations := repository.account.ModelAvailabilityDeclarations()
		declarations[0].RevokedAt = &confirmed
		repository.account.Extra[ModelAvailabilityExtraKey] = declarations
		require.Empty(test, boundedModelProbeTargets(&repository.account, repository.now))
	})

	test.Run("five per day fifty per month media one and eight", func(test *testing.T) {
		for _, endpoint := range []string{"chat_completions", "images"} {
			budget := ModelProbeBudget{}
			dailyLimit, monthlyLimit := 5, 50
			if endpoint == "images" {
				dailyLimit, monthlyLimit = 1, 8
			}
			for day := 1; day <= monthlyLimit/dailyLimit; day++ {
				for count := 0; count < dailyLimit; count++ {
					require.NoError(test, budget.Reserve(ModelProbeAttempt{Endpoint: endpoint, ReservedAt: time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC)}))
				}
				require.ErrorIs(test, budget.Reserve(ModelProbeAttempt{Endpoint: endpoint, ReservedAt: time.Date(2026, 9, day, 1, 0, 0, 0, time.UTC)}), ErrModelProbeBudgetExceeded)
			}
			require.Equal(test, monthlyLimit, budget.MonthlyUsed)
			require.ErrorIs(test, budget.Reserve(ModelProbeAttempt{Endpoint: endpoint, ReservedAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)}), ErrModelProbeBudgetExceeded)
			require.NoError(test, budget.Reserve(ModelProbeAttempt{Endpoint: endpoint, ReservedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}))
			require.Equal(test, 1, budget.MonthlyUsed)
		}
	})
}
