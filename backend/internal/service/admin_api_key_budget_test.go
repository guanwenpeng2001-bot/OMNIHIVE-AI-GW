//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// budgetWriteRepoStub 同时充当 APIKeyRepository 与 APIKeyBudgetRepository，
// 与生产实现的两个接口关系一致（同一个 SQL 仓储）。
type budgetWriteRepoStub struct {
	APIKeyRepository
	policy       APIKeyBudgetPolicy
	policyWrites []APIKeyBudgetPolicy
	adjustments  []KeyBudgetAdjustmentCommand
	keyExists    bool
}

func (s *budgetWriteRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	if !s.keyExists {
		return nil, ErrAPIKeyNotFound
	}
	return &APIKey{ID: 7, UserID: 3}, nil
}

func (s *budgetWriteRepoStub) LoadAPIKeyBudgetStates(context.Context, []int64) (map[int64]APIKeyBudgetState, error) {
	return map[int64]APIKeyBudgetState{7: {Policy: s.policy}}, nil
}

func (s *budgetWriteRepoStub) SaveAPIKeyBudgetPolicy(_ context.Context, _ int64, policy APIKeyBudgetPolicy) error {
	s.policy = policy
	s.policyWrites = append(s.policyWrites, policy)
	return nil
}

func (s *budgetWriteRepoStub) GetAPIKeyBudgetState(context.Context, int64) (*APIKeyBudgetState, error) {
	return &APIKeyBudgetState{Policy: s.policy}, nil
}

func (s *budgetWriteRepoStub) AdjustAPIKeyBudgetUsage(_ context.Context, cmd *KeyBudgetAdjustmentCommand) (*APIKeyBudgetState, error) {
	s.adjustments = append(s.adjustments, *cmd)
	return &APIKeyBudgetState{Policy: s.policy, UsedUSD: cmd.DeltaUSD, OverrunUSD: cmd.DeltaUSD}, nil
}

func newBudgetWriteAdminService(repo *budgetWriteRepoStub) *adminServiceImpl {
	return &adminServiceImpl{apiKeyRepo: repo}
}

func TestAdminSetAPIKeyBudgetPolicy_OwnsTheEnforcedSwitch(t *testing.T) {
	repo := &budgetWriteRepoStub{keyExists: true}
	admin := newBudgetWriteAdminService(repo)

	// 自助路径无法把预算提升为 enforced。
	_, err := SaveAPIKeyBudgetPolicy(context.Background(), repo, 7, APIKeyBudgetPolicy{
		Mode: APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 600, Enforced: true,
	}, false)
	require.NoError(t, err)
	require.False(t, repo.policy.Enforced, "the owner may edit limits but never promote to enforced")

	// 管理端是唯一能置 budget_enforced 的路径。
	saved, err := admin.AdminSetAPIKeyBudgetPolicy(context.Background(), 7, APIKeyBudgetPolicy{
		Mode: APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 600, Enforced: true,
	})
	require.NoError(t, err)
	require.True(t, saved.Enforced)
	require.True(t, repo.policy.Enforced)
}

func TestAdminSetAPIKeyBudgetPolicy_ReusesOwnerValidation(t *testing.T) {
	repo := &budgetWriteRepoStub{keyExists: true}
	admin := newBudgetWriteAdminService(repo)

	_, err := admin.AdminSetAPIKeyBudgetPolicy(context.Background(), 7, APIKeyBudgetPolicy{
		Mode: APIKeyBudgetModeDuration, LimitUSD: -1, DurationSeconds: 600,
	})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid,
		"the admin write entry shares the owner-side validation instead of inventing its own rules")
	require.Empty(t, repo.policyWrites)

	_, err = admin.AdminSetAPIKeyBudgetPolicy(context.Background(), 7, APIKeyBudgetPolicy{
		Mode: APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 0,
	})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid, "an active budget still needs a real period")

	_, err = admin.AdminSetAPIKeyBudgetPolicy(context.Background(), 7, APIKeyBudgetPolicy{
		Mode: APIKeyBudgetModeCalendar, LimitUSD: 0,
	})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid, "a calendar budget needs a positive limit")
	require.Empty(t, repo.policyWrites)
}

func TestAdminSetAPIKeyBudgetPolicy_UnknownModeCollapsesToOff(t *testing.T) {
	// 既有规范化语义：未知模式收敛到 off（不限额），管理端不做例外。
	repo := &budgetWriteRepoStub{keyExists: true}
	admin := newBudgetWriteAdminService(repo)

	saved, err := admin.AdminSetAPIKeyBudgetPolicy(context.Background(), 7, APIKeyBudgetPolicy{
		Mode: "definitely-not-a-mode", LimitUSD: 5, Enforced: true,
	})
	require.NoError(t, err)
	require.Equal(t, APIKeyBudgetModeOff, saved.Mode)
	require.Zero(t, saved.LimitUSD)
	require.False(t, saved.Enforced, "an off policy is never enforced")
}

func TestAdminAdjustAPIKeyBudgetUsage_RequiresReasonAndNonZeroDelta(t *testing.T) {
	repo := &budgetWriteRepoStub{keyExists: true}
	admin := newBudgetWriteAdminService(repo)

	_, err := admin.AdminAdjustAPIKeyBudgetUsage(context.Background(), &KeyBudgetAdjustmentCommand{APIKeyID: 7, DeltaUSD: 1})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid, "an adjustment without a reason is refused")

	_, err = admin.AdminAdjustAPIKeyBudgetUsage(context.Background(), &KeyBudgetAdjustmentCommand{APIKeyID: 7, Reason: "goodwill"})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid, "a zero adjustment is refused")

	state, err := admin.AdminAdjustAPIKeyBudgetUsage(context.Background(), &KeyBudgetAdjustmentCommand{
		APIKeyID: 7, ActorUserID: 42, DeltaUSD: -2.5, Reason: "goodwill credit",
	})
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Len(t, repo.adjustments, 1)
	require.Equal(t, int64(42), repo.adjustments[0].ActorUserID)
	require.InDelta(t, -2.5, repo.adjustments[0].DeltaUSD, 1e-9)
	require.Equal(t, "goodwill credit", repo.adjustments[0].Reason)
	require.False(t, repo.adjustments[0].Now.IsZero(), "the adjustment is timestamped")
	require.WithinDuration(t, time.Now(), repo.adjustments[0].Now, time.Minute)
}

func TestAdminBudgetWriteEntry_RejectsUnknownKey(t *testing.T) {
	repo := &budgetWriteRepoStub{}
	admin := newBudgetWriteAdminService(repo)

	_, err := admin.AdminSetAPIKeyBudgetPolicy(context.Background(), 7, APIKeyBudgetPolicy{
		Mode: APIKeyBudgetModeDuration, LimitUSD: 5, DurationSeconds: 600,
	})
	require.Error(t, err)
	require.Empty(t, repo.policyWrites)
}
