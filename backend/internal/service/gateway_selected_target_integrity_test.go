package service

import (
	"context"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

type selectedTargetSessionCache struct {
	SessionLimitCache
	rejected []int64
}

func (cache *selectedTargetSessionCache) RegisterSession(_ context.Context, accountID int64, _ string, _ int, _ time.Duration) (bool, error) {
	cache.rejected = append(cache.rejected, accountID)
	return false, nil
}
func (*selectedTargetSessionCache) GetWindowCostBatch(context.Context, []int64) (map[int64]float64, error) {
	return nil, nil
}

type selectedTargetConcurrencyCache struct {
	compositeTargetRecordingConcurrencyCache
	busy     bool
	released []int64
}

func (cache *selectedTargetConcurrencyCache) AcquireAccountSlot(ctx context.Context, accountID int64, concurrency int, requestID string) (bool, error) {
	if cache.busy {
		return false, nil
	}
	return cache.compositeTargetRecordingConcurrencyCache.AcquireAccountSlot(ctx, accountID, concurrency, requestID)
}
func (cache *selectedTargetConcurrencyCache) ReleaseAccountSlot(_ context.Context, accountID int64, _ string) error {
	cache.released = append(cache.released, accountID)
	return nil
}

func TestGatewaySelectedTargetSurvivesSessionReselection(t *testing.T) {
	for _, scenario := range []string{"different_platform", "same_platform", "wait_plan", "exhausted", "unselected", "hosted_scope"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Gateway.Scheduling.LoadBatchEnabled = false
			first := compositeOrderedTargetTestAccount(901, PlatformAnthropic, AccountTypeOAuth)
			first.Extra = map[string]any{"max_sessions": 1}
			first.Credentials = map[string]any{"model_mapping": map[string]string{"claude-sonnet-4-6": "claude-sonnet-4-6"}}
			second := compositeOrderedTargetTestAccount(902, PlatformOpenAI, AccountTypeAPIKey)
			if scenario == "same_platform" {
				second.Platform = PlatformAnthropic
			}
			second.Credentials = map[string]any{"model_mapping": map[string]string{"wire-second": "wire-second"}}
			if scenario == "unselected" {
				second.Credentials = map[string]any{"model_mapping": map[string]string{"unrelated": "unrelated"}}
			}
			if scenario == "hosted_scope" {
				second.Type = AccountTypeOAuth
			}
			targets := []CompositeRouteTarget{
				{Position: 0, TargetPlatform: first.Platform, AccountType: AccountTypeOAuth, UpstreamModel: "claude-sonnet-4-6", Enabled: true},
				{Position: 1, TargetPlatform: second.Platform, AccountType: AccountTypeAPIKey, UpstreamModel: "wire-second", Enabled: true},
			}
			repo := &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{first.Platform: {first}}}
			if scenario != "exhausted" {
				repo.byPlatform[second.Platform] = append(repo.byPlatform[second.Platform], second)
			}
			sessions := &selectedTargetSessionCache{}
			slots := &selectedTargetConcurrencyCache{busy: scenario == "wait_plan"}
			svc := &GatewayService{cfg: cfg, accountRepo: repo, sessionLimitCache: sessions, concurrencyService: NewConcurrencyService(slots)}
			group := &Group{ID: 900, Hydrated: true, Platform: PlatformComposite, Status: StatusActive}
			ctx := WithCompositeTargetAdvancement(WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, PublicModel: "Exact.Public-Alias", TargetPlatform: first.Platform, UpstreamModel: targets[0].UpstreamModel, Targets: targets, SelectedTarget: &targets[0]}))
			ctx = svc.withGroupContext(ctx, group)
			excluded := map[int64]struct{}{}
			selection, err := svc.SelectAccountWithLoadAwareness(ctx, &group.ID, "new-session", targets[0].UpstreamModel, excluded, "", 0)
			require.Equal(t, []int64{901}, sessions.rejected)
			require.Empty(t, excluded, "internal retries must not mutate caller exclusions")
			if scenario == "exhausted" || scenario == "unselected" || scenario == "hosted_scope" {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, selection)
				return
			}
			require.NoError(t, err)
			require.Equal(t, int64(902), selection.Account.ID)
			require.Equal(t, &targets[1], selection.CompositeTarget)
			require.Equal(t, scenario != "wait_plan", selection.Acquired)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			if scenario == "wait_plan" {
				require.Equal(t, int64(902), selection.WaitPlan.AccountID)
			} else {
				require.Equal(t, []int64{901, 902}, slots.released)
			}
			public, _ := RequestedPublicModelFromContext(ctx)
			require.Equal(t, "Exact.Public-Alias", public)
		})
	}
}

func TestGatewaySelectedTargetLoadBatch(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	account := compositeOrderedTargetTestAccount(902, PlatformOpenAI, AccountTypeAPIKey)
	account.Credentials = map[string]any{"model_mapping": map[string]string{"wire-second": "wire-second"}}
	targets := []CompositeRouteTarget{
		{Position: 0, TargetPlatform: PlatformOpenAI, AccountType: AccountTypeOAuth, UpstreamModel: "wire-first", Enabled: true},
		{Position: 1, TargetPlatform: PlatformOpenAI, AccountType: AccountTypeAPIKey, UpstreamModel: "wire-second", Enabled: true},
	}
	slots := &compositeTargetRecordingConcurrencyCache{}
	svc := &GatewayService{cfg: cfg, accountRepo: &compositeOrderedTargetAccountRepo{byPlatform: map[string][]Account{PlatformOpenAI: {account}}}, concurrencyService: NewConcurrencyService(slots)}
	group := &Group{ID: 900, Hydrated: true, Platform: PlatformComposite, Status: StatusActive}
	ctx := WithCompositeTargetAdvancement(WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, PublicModel: "Exact.Public-Alias", TargetPlatform: PlatformOpenAI, UpstreamModel: "wire-first", Targets: targets, SelectedTarget: &targets[0]}))
	ctx = svc.withGroupContext(ctx, group)
	selection, err := svc.SelectAccountWithLoadAwareness(ctx, &group.ID, "", "wire-first", nil, "", 0)
	require.NoError(t, err)
	require.Equal(t, int64(902), selection.Account.ID)
	require.Equal(t, &targets[1], selection.CompositeTarget)
	require.True(t, selection.Acquired)
	selection.ReleaseFunc()
	require.Contains(t, slots.batchDenominators, int64(902))
}
