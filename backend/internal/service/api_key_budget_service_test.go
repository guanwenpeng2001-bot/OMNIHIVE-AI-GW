//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The budget persistence port is implemented by the API key repository in
// production; this in-memory extension keeps service tests free of SQL.
func (s *apiKeyRepoStub) LoadAPIKeyBudgetStates(ctx context.Context, apiKeyIDs []int64) (map[int64]APIKeyBudgetState, error) {
	if s.budgetLoadErr != nil {
		return nil, s.budgetLoadErr
	}
	out := make(map[int64]APIKeyBudgetState, len(apiKeyIDs))
	for _, id := range apiKeyIDs {
		if state, ok := s.budgetStates[id]; ok {
			out[id] = state
		}
	}
	return out, nil
}

func (s *apiKeyRepoStub) SaveAPIKeyBudgetPolicy(ctx context.Context, apiKeyID int64, policy APIKeyBudgetPolicy) error {
	if s.budgetSaveErr != nil {
		return s.budgetSaveErr
	}
	s.savedBudgetPolicies = append(s.savedBudgetPolicies, policy.Normalize())
	state := s.budgetStates[apiKeyID]
	state.Policy = policy.Normalize()
	if state.LimitUSD == 0 {
		state.LimitUSD = policy.LimitUSD
	}
	s.budgetStates[apiKeyID] = state
	return nil
}

func (s *apiKeyRepoStub) GetAPIKeyBudgetState(ctx context.Context, apiKeyID int64) (*APIKeyBudgetState, error) {
	state, ok := s.budgetStates[apiKeyID]
	if !ok {
		return &APIKeyBudgetState{}, nil
	}
	return &state, nil
}

func (s *apiKeyRepoStub) AdjustAPIKeyBudgetUsage(ctx context.Context, cmd *KeyBudgetAdjustmentCommand) (*APIKeyBudgetState, error) {
	if cmd == nil {
		return nil, errors.New("nil adjustment")
	}
	s.adjustments = append(s.adjustments, *cmd)
	state := s.budgetStates[cmd.APIKeyID]
	state.UsedUSD += cmd.DeltaUSD
	if state.UsedUSD < 0 {
		state.UsedUSD = 0
	}
	s.budgetStates[cmd.APIKeyID] = state
	return &state, nil
}

func newBudgetServiceStub(t *testing.T, policy APIKeyBudgetPolicy, used float64) (*APIKeyService, *apiKeyRepoStub) {
	t.Helper()
	repo := &apiKeyRepoStub{
		apiKey: &APIKey{ID: 7, UserID: 3, Name: "budget-key", Status: StatusActive, BudgetPolicy: policy},
		budgetStates: map[int64]APIKeyBudgetState{
			7: NewAPIKeyBudgetState(policy, nil, nil, used, 0, 0),
		},
	}
	return &APIKeyService{apiKeyRepo: repo}, repo
}

func TestAPIKeyService_UpdateRejectsOwnerUsageReset(t *testing.T) {
	service, _ := newBudgetServiceStub(t, APIKeyBudgetPolicy{}, 0)

	_, err := service.Update(context.Background(), 7, 3, UpdateAPIKeyRequest{ResetQuota: budgetBoolPtr(true)})
	require.ErrorIs(t, err, ErrAPIKeyUsageResetForbidden)

	_, err = service.Update(context.Background(), 7, 3, UpdateAPIKeyRequest{ResetRateLimitUsage: budgetBoolPtr(true)})
	require.ErrorIs(t, err, ErrAPIKeyUsageResetForbidden)
}

func TestAPIKeyService_UpdateRejectsEnforcedBudgetChangeAndKeepsUsage(t *testing.T) {
	enforced := APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 20, DurationSeconds: 3600, Enforced: true}
	service, repo := newBudgetServiceStub(t, enforced, 4)

	_, err := service.Update(context.Background(), 7, 3, UpdateAPIKeyRequest{BudgetLimit: budgetFloatPtr(5)})
	require.ErrorIs(t, err, ErrAPIKeyBudgetEnforced)

	_, err = service.Update(context.Background(), 7, 3, UpdateAPIKeyRequest{ClearBudget: budgetBoolPtr(true)})
	require.ErrorIs(t, err, ErrAPIKeyBudgetEnforced)

	require.Empty(t, repo.savedBudgetPolicies, "an enforced budget must never be written by the owner path")
	require.InDelta(t, 4, repo.budgetStates[7].UsedUSD, 1e-9)
}

func TestAPIKeyService_UpdateKeepsConsumedUsageWhenLimitChanges(t *testing.T) {
	selfSet := APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 10, DurationSeconds: 3600}
	service, repo := newBudgetServiceStub(t, selfSet, 6)

	updated, err := service.Update(context.Background(), 7, 3, UpdateAPIKeyRequest{BudgetLimit: budgetFloatPtr(25)})
	require.NoError(t, err)
	require.InDelta(t, 25, updated.BudgetPolicy.LimitUSD, 1e-9)
	require.Len(t, repo.savedBudgetPolicies, 1)
	require.InDelta(t, 6, repo.budgetStates[7].UsedUSD, 1e-9, "changing the limit never zeroes consumed usage")
}

func TestAPIKeyService_UpdateCannotPromoteToEnforced(t *testing.T) {
	selfSet := APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 10, DurationSeconds: 3600}
	service, repo := newBudgetServiceStub(t, selfSet, 0)

	saved, err := service.SaveBudgetPolicy(context.Background(), 7, APIKeyBudgetPolicy{
		Mode:            APIKeyBudgetModeDuration,
		LimitUSD:        10,
		DurationSeconds: 3600,
		Enforced:        true,
	}, false)
	require.NoError(t, err)
	require.False(t, saved.Enforced)
	require.False(t, repo.savedBudgetPolicies[0].Enforced)
}

func TestAPIKeyService_AdjustBudgetUsageRequiresAuditReason(t *testing.T) {
	service, repo := newBudgetServiceStub(t, APIKeyBudgetPolicy{Mode: APIKeyBudgetModeDuration, LimitUSD: 50, DurationSeconds: 3600}, 5)

	_, err := service.AdjustBudgetUsage(context.Background(), &KeyBudgetAdjustmentCommand{APIKeyID: 7, ActorUserID: 99, DeltaUSD: -3})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid)

	_, err = service.AdjustBudgetUsage(context.Background(), &KeyBudgetAdjustmentCommand{APIKeyID: 7, ActorUserID: 99, DeltaUSD: 0, Reason: "zero"})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid)

	state, err := service.AdjustBudgetUsage(context.Background(), &KeyBudgetAdjustmentCommand{
		APIKeyID: 7, ActorUserID: 99, DeltaUSD: -3, Reason: "goodwill credit after upstream incident",
	})
	require.NoError(t, err)
	require.InDelta(t, 2, state.UsedUSD, 1e-9, "an audited credit reduces consumed usage without zeroing history")
	require.Len(t, repo.adjustments, 1)
	require.Equal(t, int64(99), repo.adjustments[0].ActorUserID)
	require.Equal(t, "goodwill credit after upstream incident", repo.adjustments[0].Reason)
}

func TestAPIKeyBudgetPolicyFromUpdateRequestMergesPartialChanges(t *testing.T) {
	current := APIKeyBudgetPolicy{Mode: APIKeyBudgetModeCalendar, LimitUSD: 10, CalendarUnit: APIKeyBudgetCalendarWeek, Timezone: "Asia/Shanghai"}

	next, changed, err := apiKeyBudgetPolicyFromUpdateRequest(current, UpdateAPIKeyRequest{BudgetLimit: budgetFloatPtr(30)})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, APIKeyBudgetCalendarWeek, next.CalendarUnit)
	require.Equal(t, "Asia/Shanghai", next.EffectiveTimezoneName())
	require.InDelta(t, 30, next.LimitUSD, 1e-9)

	next, changed, err = apiKeyBudgetPolicyFromUpdateRequest(current, UpdateAPIKeyRequest{LegacyBudgetAnchor: budgetStringPtr(APIKeyLegacyBudgetAnchor5h)})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, APIKeyBudgetModeDuration, next.Mode)
	require.Equal(t, int64((5*time.Hour)/time.Second), next.DurationSeconds)
	require.Equal(t, APIKeyLegacyBudgetAnchor5h, LegacyBudgetAnchorForPolicy(next))

	_, _, err = apiKeyBudgetPolicyFromUpdateRequest(current, UpdateAPIKeyRequest{LegacyBudgetAnchor: budgetStringPtr("2h")})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid)

	cleared, changed, err := apiKeyBudgetPolicyFromUpdateRequest(current, UpdateAPIKeyRequest{ClearBudget: budgetBoolPtr(true)})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, APIKeyBudgetModeOff, cleared.Mode)

	unchanged, changed, err := apiKeyBudgetPolicyFromUpdateRequest(current, UpdateAPIKeyRequest{})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, current, unchanged)
}

func TestAPIKeyBudgetPolicyFromCreateRequestKeepsLegacyAnchor(t *testing.T) {
	policy, err := apiKeyBudgetPolicyFromCreateRequest(CreateAPIKeyRequest{LegacyBudgetAnchor: APIKeyLegacyBudgetAnchor7d, BudgetLimit: 40})
	require.NoError(t, err)
	require.Equal(t, APIKeyLegacyBudgetAnchor7d, LegacyBudgetAnchorForPolicy(policy))
	require.InDelta(t, 40, policy.LimitUSD, 1e-9)

	_, err = apiKeyBudgetPolicyFromCreateRequest(CreateAPIKeyRequest{LegacyBudgetAnchor: "3d", BudgetLimit: 40})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid)

	off, err := apiKeyBudgetPolicyFromCreateRequest(CreateAPIKeyRequest{})
	require.NoError(t, err)
	require.False(t, off.IsActive())

	custom, err := apiKeyBudgetPolicyFromCreateRequest(CreateAPIKeyRequest{
		BudgetPeriodMode:      APIKeyBudgetModeDuration,
		BudgetLimit:           8,
		BudgetDurationSeconds: 1800,
		BudgetAnchor:          APIKeyBudgetAnchorDay,
		BudgetTimezone:        "Asia/Shanghai",
	})
	require.NoError(t, err)
	require.True(t, custom.IsActive())
	require.Equal(t, int64(1800), custom.DurationSeconds)
	require.Equal(t, APIKeyBudgetAnchorDay, custom.Anchor)
}

func budgetBoolPtr(v bool) *bool        { return &v }
func budgetFloatPtr(v float64) *float64 { return &v }
func budgetStringPtr(v string) *string  { return &v }
