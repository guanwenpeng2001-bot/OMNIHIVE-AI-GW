package service

import (
	"context"
	"errors"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// recheckBatchCountingAccountRepo 统计 DB 访问次数，用于验证一轮选号只查一次库。
type recheckBatchCountingAccountRepo struct {
	stubOpenAIAccountRepo
	getByIDCalls  int
	getByIDsCalls int
	getByIDErr    error
	getByIDsErr   error
}

func (r *recheckBatchCountingAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.getByIDCalls++
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	return r.stubOpenAIAccountRepo.GetByID(ctx, id)
}

func (r *recheckBatchCountingAccountRepo) GetByIDs(ctx context.Context, ids []int64) ([]*Account, error) {
	r.getByIDsCalls++
	if r.getByIDsErr != nil {
		return nil, r.getByIDsErr
	}
	return r.stubOpenAIAccountRepo.GetByIDs(ctx, ids)
}

func recheckBatchTestAccount(id int64, concurrency int) Account {
	return Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: concurrency,
	}
}

func newRecheckBatchTestService(repo AccountRepository) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		accountRepo:       repo,
		cfg:               &config.Config{RunMode: config.RunModeSimple},
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{}},
	}
}

// 一轮选号的所有候选只触发一次 GetByIDs，复核读到的是刷新后的行。
func TestLoadOpenAIAccountRecheckBatchSingleQuery(t *testing.T) {
	stale := []Account{
		recheckBatchTestAccount(50001, 1),
		recheckBatchTestAccount(50002, 1),
		recheckBatchTestAccount(50003, 1),
	}
	refreshed := make([]Account, 0, len(stale))
	for _, account := range stale {
		updated := account
		updated.Concurrency = 9
		refreshed = append(refreshed, updated)
	}
	repo := &recheckBatchCountingAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: refreshed}}
	svc := newRecheckBatchTestService(repo)

	candidates := []*Account{&stale[0], &stale[1], &stale[2]}
	batch, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), candidates)
	require.NoError(t, err)
	require.Len(t, batch, 3)
	require.Equal(t, 1, repo.getByIDsCalls, "整轮候选只允许一次批量查询")
	require.Zero(t, repo.getByIDCalls, "载入批次阶段不得触发单条查询")

	ctx := withOpenAIAccountRecheckBatch(context.Background(), batch)
	for _, candidate := range candidates {
		latest, recheckErr := svc.recheckSelectedOpenAIAccountFromDBBeforeProfitErr(ctx, candidate, nil, PlatformOpenAI, "gpt-5.1", false, "")
		require.NoError(t, recheckErr)
		require.NotNil(t, latest)
		require.Equal(t, candidate.ID, latest.ID)
		require.Equal(t, 9, latest.Concurrency, "复核必须使用刷新后的行")
	}
	require.Zero(t, repo.getByIDCalls, "命中批次后不得再逐个候选查库")
	require.Equal(t, 1, repo.getByIDsCalls)
}

// 批量查询失败必须冒泡，而不是被当成候选被拒绝。
func TestLoadOpenAIAccountRecheckBatchPropagatesError(t *testing.T) {
	dbErr := errors.New("connection refused")
	repo := &recheckBatchCountingAccountRepo{getByIDsErr: dbErr}
	svc := newRecheckBatchTestService(repo)

	candidate := recheckBatchTestAccount(60001, 1)
	batch, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), []*Account{&candidate})
	require.Error(t, err)
	require.ErrorIs(t, err, dbErr)
	require.Contains(t, err.Error(), "load account recheck batch")
	require.Nil(t, batch)
	require.Equal(t, 1, repo.getByIDsCalls)
}

// 批次里缺失的候选表示账号已删除：拒绝该候选，但不算数据库故障。
func TestRecheckSelectedOpenAIAccountFromDBBeforeProfitRejectsCandidateMissingFromBatch(t *testing.T) {
	present := recheckBatchTestAccount(70001, 4)
	missing := recheckBatchTestAccount(70002, 4)
	repo := &recheckBatchCountingAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{present}}}
	svc := newRecheckBatchTestService(repo)

	batch, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), []*Account{&present, &missing})
	require.NoError(t, err)
	require.Len(t, batch, 2, "被请求的 ID 都要有条目，缺失者显式为 nil")
	ctx := withOpenAIAccountRecheckBatch(context.Background(), batch)

	latest, recheckErr := svc.recheckSelectedOpenAIAccountFromDBBeforeProfitErr(ctx, &present, nil, PlatformOpenAI, "gpt-5.1", false, "")
	require.NoError(t, recheckErr)
	require.NotNil(t, latest)
	require.Equal(t, present.ID, latest.ID)

	deleted, recheckErr := svc.recheckSelectedOpenAIAccountFromDBBeforeProfitErr(ctx, &missing, nil, PlatformOpenAI, "gpt-5.1", false, "")
	require.NoError(t, recheckErr, "批次内缺失表示账号已删除，属于拒绝候选而非数据库故障")
	require.Nil(t, deleted)
	require.Zero(t, repo.getByIDCalls, "批次内缺失的行不得退回单条查询")
	require.Equal(t, 1, repo.getByIDsCalls)
}

// 没有批次时保留单条 GetByID 的遗留行为。
func TestRecheckSelectedOpenAIAccountFromDBLegacyPathWithoutBatch(t *testing.T) {
	stale := recheckBatchTestAccount(80001, 1)
	refreshed := stale
	refreshed.Concurrency = 6
	repo := &recheckBatchCountingAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{refreshed}}}
	svc := newRecheckBatchTestService(repo)

	latest := svc.recheckSelectedOpenAIAccountFromDBBeforeProfit(context.Background(), &stale, nil, PlatformOpenAI, "gpt-5.1", false, "")
	require.NotNil(t, latest)
	require.Equal(t, 6, latest.Concurrency)
	require.Equal(t, 1, repo.getByIDCalls)
	require.Zero(t, repo.getByIDsCalls)
}

// 遗留入口继续吞掉数据库错误；Err 版本把它交给调用方，账号不存在仍算拒绝候选。
func TestRecheckSelectedOpenAIAccountFromDBErrorSemantics(t *testing.T) {
	account := recheckBatchTestAccount(80002, 1)
	dbErr := errors.New("connection refused")
	repo := &recheckBatchCountingAccountRepo{getByIDErr: dbErr}
	svc := newRecheckBatchTestService(repo)

	require.Nil(t, svc.recheckSelectedOpenAIAccountFromDBBeforeProfit(context.Background(), &account, nil, PlatformOpenAI, "gpt-5.1", false, ""),
		"遗留入口保持吞错语义")

	latest, err := svc.recheckSelectedOpenAIAccountFromDBBeforeProfitErr(context.Background(), &account, nil, PlatformOpenAI, "gpt-5.1", false, "")
	require.ErrorIs(t, err, dbErr)
	require.Nil(t, latest)

	notFoundSvc := newRecheckBatchTestService(&recheckBatchCountingAccountRepo{getByIDErr: ErrAccountNotFound})
	gone, goneErr := notFoundSvc.recheckSelectedOpenAIAccountFromDBBeforeProfitErr(context.Background(), &account, nil, PlatformOpenAI, "gpt-5.1", false, "")
	require.NoError(t, goneErr, "账号不存在属于拒绝候选，不是数据库故障")
	require.Nil(t, gone)
}

// 选号入口必须把批量查询失败当作错误返回，而不是悄悄少一批候选。
func TestOpenAIRecheckBatchErrorSurfacesFromSelectionEntryPoints(t *testing.T) {
	groupID := int64(10107)
	stalePrimary := &Account{ID: 34201, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
	staleBackup := &Account{ID: 34202, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
	dbErr := errors.New("connection refused")
	repo := &recheckBatchCountingAccountRepo{getByIDsErr: dbErr}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	svc := &OpenAIGatewayService{
		accountRepo: repo,
		cfg:         cfg,
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			snapshotAccounts: []*Account{stalePrimary, staleBackup},
			accountsByID:     map[int64]*Account{stalePrimary.ID: stalePrimary, staleBackup.ID: staleBackup},
		}},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	_, err := svc.SelectAccountWithLoadAwareness(context.Background(), &groupID, "", "gpt-5.1", nil)
	require.ErrorIs(t, err, dbErr, "负载感知选号入口不得吞掉批量查询失败")

	scheduler := newDefaultOpenAIAccountScheduler(svc, nil)
	_, _, schedErr := scheduler.Select(context.Background(), OpenAIAccountScheduleRequest{
		GroupID: &groupID, Platform: PlatformOpenAI, RequestedModel: "gpt-5.1",
	})
	require.ErrorIs(t, schedErr, dbErr, "高级调度器入口不得吞掉批量查询失败")

	require.Equal(t, 2, repo.getByIDsCalls)
	require.Zero(t, repo.getByIDCalls, "入口失败前不得退回逐个候选查库")
}
