package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type schedulerRecoveryRepo struct {
	AccountRepository
	listed              []Account
	accounts            map[int64]*Account
	batchErr            error
	batchHook           func(context.Context)
	read                func(context.Context, int64) (*Account, error)
	batchCalls          int
	batchIDs, singleIDs []int64
}

func (r *schedulerRecoveryRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return r.listed, nil
}

func (r *schedulerRecoveryRepo) GetByIDs(ctx context.Context, ids []int64) ([]*Account, error) {
	r.batchCalls++
	r.batchIDs = append([]int64(nil), ids...)
	if r.batchHook != nil {
		r.batchHook(ctx)
	}
	if r.batchErr != nil {
		return nil, r.batchErr
	}
	var accounts []*Account
	for _, id := range ids {
		if a := r.accounts[id]; a != nil {
			accounts = append(accounts, a)
		}
	}
	return accounts, nil
}

func (r *schedulerRecoveryRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.singleIDs = append(r.singleIDs, id)
	if r.read != nil {
		return r.read(ctx, id)
	}
	if a := r.accounts[id]; a != nil {
		return a, nil
	}
	return nil, ErrAccountNotFound.WithCause(sql.ErrNoRows)
}

func schedulerRecoveryFixture() (*OpenAIGatewayService, *schedulerRecoveryRepo, []*Account, *int64) {
	groupID := int64(10107)
	primary, backup := recheckBatchTestAccount(34201, 1), recheckBatchTestAccount(34202, 1)
	primary.GroupIDs, backup.GroupIDs = []int64{groupID}, []int64{groupID}
	primary.Priority, backup.Priority = 0, 10
	accounts := []*Account{&primary, &backup}
	repo := &schedulerRecoveryRepo{accounts: map[int64]*Account{primary.ID: &primary, backup.ID: &backup}, listed: []Account{primary, backup}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg,
		schedulerSnapshot:  &SchedulerSnapshotService{accountRepo: repo, cache: &openAISnapshotCacheStub{snapshotAccounts: accounts, accountsByID: repo.accounts}},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	return svc, repo, accounts, &groupID
}

func recoverySelect(ctx context.Context, svc *OpenAIGatewayService, groupID *int64, path, session string) (*AccountSelectionResult, error) {
	if path == "legacy" {
		return svc.SelectAccountWithLoadAwareness(ctx, groupID, session, "gpt-5.1", nil)
	}
	selected, _, err := newDefaultOpenAIAccountScheduler(svc, nil).Select(ctx, OpenAIAccountScheduleRequest{GroupID: groupID, Platform: PlatformOpenAI, RequestedModel: "gpt-5.1", SessionHash: session})
	return selected, err
}

func TestOpenAIRecheckRecoverySelectsHealthyBackup(t *testing.T) {
	for _, path := range []string{"legacy", "advanced"} {
		for _, failure := range []error{ErrCredentialSecretRevoked, ErrCredentialSecretInvalid, ErrCredentialSecretConflict, ErrCredentialSecretNotFound} {
			t.Run(path+"/"+fmt.Sprint(failure), func(t *testing.T) {
				svc, repo, accounts, group := schedulerRecoveryFixture()
				repo.batchErr = failure
				repo.read = func(_ context.Context, id int64) (*Account, error) {
					if id == accounts[0].ID {
						return nil, failure
					}
					return repo.accounts[id], nil
				}
				var acquired []int64
				svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired})
				selected, err := recoverySelect(context.Background(), svc, group, path, "")
				require.NoError(t, err)
				require.Equal(t, accounts[1].ID, selected.Account.ID)
				require.Equal(t, []int64{accounts[1].ID}, acquired, "bad reference must not reserve capacity")
				require.Equal(t, 1, repo.batchCalls)
				if selected.ReleaseFunc != nil {
					selected.ReleaseFunc()
				}
			})
		}
	}
}

func TestOpenAIRecheckRecoverySelectionPathsReuseVerifiedRows(t *testing.T) {
	for _, path := range []string{"legacy", "advanced", "weighted"} {
		for _, mode := range []string{"acquire", "load-error", "wait", "no-snapshot"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				svc, repo, accounts, group := schedulerRecoveryFixture()
				// Referenced snapshot rows normally trigger an authoritative single
				// read. No such read may bypass the completed recovery batch.
				for _, a := range accounts {
					a.Credentials = map[string]any{CredentialSecretReferenceKey: "synthetic-reference", CredentialSecretVersionKey: 1}
				}
				fresh := *accounts[1]
				fresh.Concurrency = 7
				fresh.Credentials = map[string]any{"api_key": "synthetic-fresh-material"}
				repo.batchErr = ErrCredentialSecretInvalid
				repo.read = func(_ context.Context, id int64) (*Account, error) {
					if id == accounts[0].ID {
						return nil, ErrCredentialSecretRevoked
					}
					return &fresh, nil
				}
				var acquired []int64
				concurrency := schedulerTestConcurrencyCache{acquiredIDs: &acquired}
				if mode == "load-error" {
					concurrency.loadBatchErr = errors.New("synthetic load cache failure")
				}
				if mode == "wait" {
					concurrency.acquireResults = map[int64]bool{accounts[1].ID: false}
				}
				if mode == "no-snapshot" {
					svc.schedulerSnapshot = nil
				}
				svc.concurrencyService = NewConcurrencyService(concurrency)
				var selected *AccountSelectionResult
				var err error
				if path == "weighted" {
					selected, _, err = newDefaultOpenAIAccountScheduler(svc, nil).Select(context.Background(), OpenAIAccountScheduleRequest{
						GroupID: group, Platform: PlatformOpenAI, RequestedModel: "gpt-5.1", StickyWeighted: true, StickyAccountID: accounts[0].ID,
					})
				} else {
					selected, err = recoverySelect(context.Background(), svc, group, path, "")
				}
				require.NoError(t, err)
				require.Equal(t, fresh.ID, selected.Account.ID)
				require.Equal(t, 7, selected.Account.Concurrency)
				require.True(t, selected.Account.Credentials["api_key"] == fresh.Credentials["api_key"], "must retain recovered credentials")
				require.NotContains(t, acquired, accounts[0].ID)
				require.Equal(t, []int64{accounts[0].ID, accounts[1].ID}, repo.singleIDs, "neither rejects nor healthy rows may be hydrated again")
				if mode == "wait" {
					require.Equal(t, fresh.ID, selected.WaitPlan.AccountID)
					require.Equal(t, 7, selected.WaitPlan.MaxConcurrency)
				}
				if selected.ReleaseFunc != nil {
					selected.ReleaseFunc()
				}
			})
		}
	}
}

func TestOpenAIRecheckRecoveryNormalSelectionRemainsBatched(t *testing.T) {
	for _, path := range []string{"legacy", "advanced"} {
		t.Run(path, func(t *testing.T) {
			svc, repo, accounts, group := schedulerRecoveryFixture()
			for _, a := range accounts {
				a.Credentials = map[string]any{CredentialSecretReferenceKey: "synthetic-reference", CredentialSecretVersionKey: 1}
			}
			selected, err := recoverySelect(context.Background(), svc, group, path, "")
			require.NoError(t, err)
			require.NotNil(t, selected.Account)
			require.Equal(t, 1, repo.batchCalls)
			require.Empty(t, repo.singleIDs)
			if selected.ReleaseFunc != nil {
				selected.ReleaseFunc()
			}
		})
	}
}

func TestOpenAIRecheckRecoveryAllUnavailable(t *testing.T) {
	for _, path := range []string{"legacy", "advanced"} {
		t.Run(path, func(t *testing.T) {
			svc, repo, accounts, group := schedulerRecoveryFixture()
			repo.batchErr = ErrCredentialSecretInvalid
			repo.read = func(context.Context, int64) (*Account, error) { return nil, ErrCredentialSecretRevoked }
			var acquired []int64
			svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired})
			selection, err := recoverySelect(context.Background(), svc, group, path, "")
			require.Error(t, err)
			require.Nil(t, selection)
			require.Empty(t, acquired)
			require.Len(t, repo.singleIDs, len(accounts))
		})
	}
}

func TestOpenAIRecheckRecoveryFreshIneligibleCannotTakeTopK(t *testing.T) {
	svc, repo, accounts, group := schedulerRecoveryFixture()
	accounts[0].Priority = 20 // cached healthy backup wins top-K before this read
	fresh := *accounts[0]
	fresh.Status, fresh.Schedulable, fresh.Priority = "inactive", false, -100
	repo.accounts[accounts[0].ID] = &fresh
	svc.cfg.Gateway.OpenAIWS.LBTopK = 1
	svc.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 1
	var acquired []int64
	svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired})
	selected, err := recoverySelect(context.Background(), svc, group, "advanced", "")
	require.NoError(t, err)
	require.Equal(t, accounts[1].ID, selected.Account.ID)
	require.NotContains(t, acquired, fresh.ID)
	if selected.ReleaseFunc != nil {
		selected.ReleaseFunc()
	}
}

func TestOpenAIRecheckRecoveryEmptyBatchKeepsOutOfBucketWeightedSticky(t *testing.T) {
	svc, repo, accounts, group := schedulerRecoveryFixture()
	// The bucket only contains a disappeared row. A still-live sticky account
	// outside that stale bucket must retain the existing validated fallback.
	svc.schedulerSnapshot.cache = &openAISnapshotCacheStub{snapshotAccounts: accounts[:1], accountsByID: repo.accounts}
	delete(repo.accounts, accounts[0].ID)
	selected, _, err := newDefaultOpenAIAccountScheduler(svc, nil).Select(context.Background(), OpenAIAccountScheduleRequest{
		GroupID: group, Platform: PlatformOpenAI, RequestedModel: "gpt-5.1", StickyWeighted: true, StickyAccountID: accounts[1].ID,
	})
	require.NoError(t, err)
	require.Equal(t, accounts[1].ID, selected.Account.ID)
	if selected.ReleaseFunc != nil {
		selected.ReleaseFunc()
	}
}

func TestOpenAIRecheckRecoverySelectionInfrastructureFailure(t *testing.T) {
	for _, path := range []string{"legacy", "advanced"} {
		for _, phase := range []string{"batch", "recovery"} {
			t.Run(path+"/"+phase, func(t *testing.T) {
				svc, repo, accounts, group := schedulerRecoveryFixture()
				failure := errors.Join(ErrCredentialSecretInvalid, errors.New("synthetic driver secret"))
				repo.batchErr = failure
				if phase == "recovery" {
					repo.batchErr = ErrCredentialSecretInvalid
					repo.read = func(_ context.Context, id int64) (*Account, error) {
						if id == accounts[0].ID {
							return repo.accounts[id], nil
						}
						return nil, failure
					}
				}
				var acquired []int64
				svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired})
				selected, err := recoverySelect(context.Background(), svc, group, path, "")
				require.ErrorIs(t, err, failure)
				require.NotContains(t, err.Error(), "synthetic driver secret")
				require.Nil(t, selected)
				require.Empty(t, acquired)
			})
		}
	}
}

func TestOpenAIRecheckRecoveryMovedCandidateCannotConsumeTopK(t *testing.T) {
	svc, repo, accounts, group := schedulerRecoveryFixture()
	accounts[0].Priority = 20
	fresh := *accounts[0]
	fresh.GroupIDs = []int64{*group + 1}
	fresh.Priority = -100
	repo.accounts[accounts[0].ID] = &fresh
	svc.cfg.Gateway.OpenAIWS.LBTopK = 1
	svc.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 1

	selected, err := recoverySelect(context.Background(), svc, group, "advanced", "")
	require.NoError(t, err, "a moved account must not consume top-K ahead of a healthy in-group backup")
	require.NotNil(t, selected)
	require.Equal(t, accounts[1].ID, selected.Account.ID)
	if selected.ReleaseFunc != nil {
		selected.ReleaseFunc()
	}
}

func TestOpenAIRecheckRecoveryBatchContract(t *testing.T) {
	t.Run("normal batch deduplicates without fallback", func(t *testing.T) {
		svc, repo, accounts, _ := schedulerRecoveryFixture()
		batch, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), []*Account{nil, {ID: -1}, accounts[1], accounts[0], accounts[1], {ID: 999}})
		require.NoError(t, err)
		require.Equal(t, []int64{accounts[1].ID, accounts[0].ID, 999}, repo.batchIDs)
		require.Empty(t, repo.singleIDs)
		require.Nil(t, batch[999])
	})
	t.Run("recovery preserves nil entries and never re-reads rejected IDs", func(t *testing.T) {
		svc, repo, accounts, _ := schedulerRecoveryFixture()
		repo.batchErr = ErrCredentialSecretInvalid
		repo.read = func(_ context.Context, id int64) (*Account, error) {
			if id == accounts[0].ID {
				return nil, ErrCredentialSecretRevoked
			}
			if id == 998 {
				return nil, nil
			}
			if id == 999 {
				return nil, ErrAccountNotFound.WithCause(sql.ErrNoRows)
			}
			return repo.accounts[id], nil
		}
		batch, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), []*Account{accounts[0], accounts[1], accounts[0], {ID: 998}, {ID: 999}})
		require.NoError(t, err)
		require.Len(t, batch, 4)
		require.Equal(t, []int64{accounts[0].ID, accounts[1].ID, 998, 999}, repo.singleIDs)
		count := len(repo.singleIDs)
		ctx := withOpenAIAccountRecheckBatch(context.Background(), batch)
		for _, id := range []int64{accounts[0].ID, 998, 999} {
			got, err := svc.latestOpenAIAccountForRecheck(ctx, id)
			require.NoError(t, err)
			require.Nil(t, got)
		}
		require.Len(t, repo.singleIDs, count)
	})
	t.Run("same workspace and affinity context", func(t *testing.T) {
		svc, repo, accounts, _ := schedulerRecoveryFixture()
		ctx := WithAffinityBindingState(domain.WithWorkspaceScope(context.Background(), 72))
		state := AffinityBindingStateFromContext(ctx)
		repo.batchErr = ErrCredentialSecretInvalid
		repo.read = func(readCtx context.Context, id int64) (*Account, error) {
			workspace, scoped := domain.WorkspaceScope(readCtx)
			require.True(t, scoped)
			require.EqualValues(t, 72, workspace)
			require.Same(t, state, AffinityBindingStateFromContext(readCtx))
			if id == accounts[0].ID {
				return nil, ErrAccountNotFound
			}
			a := *repo.accounts[id]
			a.WorkspaceID = 72
			return &a, nil
		}
		batch, err := svc.loadOpenAIAccountRecheckBatch(ctx, accounts)
		require.NoError(t, err)
		require.Nil(t, batch[accounts[0].ID])
		require.EqualValues(t, 72, batch[accounts[1].ID].WorkspaceID)
	})
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject repository boundary violation/%v", foreign), func(t *testing.T) {
			svc, repo, accounts, _ := schedulerRecoveryFixture()
			repo.batchErr = ErrCredentialSecretInvalid
			repo.read = func(_ context.Context, id int64) (*Account, error) {
				if foreign {
					return &Account{ID: id, WorkspaceID: 73}, nil
				}
				return &Account{ID: id + 1, WorkspaceID: 72}, nil
			}
			batch, err := svc.loadOpenAIAccountRecheckBatch(domain.WithWorkspaceScope(context.Background(), 72), accounts)
			require.Error(t, err)
			require.Nil(t, batch)
			require.Len(t, repo.singleIDs, 1)
		})
	}
}

func TestOpenAIRecheckRecoveryInfrastructureAndSecrecy(t *testing.T) {
	const canary = "scheduler-synthetic-secret-not-for-output"
	var logged bytes.Buffer
	priorLogger, priorWriter := slog.Default(), log.Writer()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	log.SetOutput(&logged)
	t.Cleanup(func() { slog.SetDefault(priorLogger); log.SetOutput(priorWriter) })
	dbErr := errors.New(canary)
	failures := []error{dbErr, context.Canceled, context.DeadlineExceeded, errors.Join(ErrCredentialSecretInvalid, dbErr),
		fmt.Errorf("wrapped: %w", errors.Join(ErrCredentialSecretRevoked, dbErr)), ErrCredentialSecretInvalid.WithCause(dbErr),
		ErrAccountNotFound.WithCause(dbErr), errors.Join(ErrCredentialSecretRevoked, context.Canceled), ErrCredentialSecretConflict.WithCause(context.DeadlineExceeded)}
	for _, phase := range []string{"batch", "recovery"} {
		for index, failure := range failures {
			t.Run(fmt.Sprintf("%s/%d", phase, index), func(t *testing.T) {
				svc, repo, accounts, _ := schedulerRecoveryFixture()
				repo.batchErr = failure
				if phase == "recovery" {
					repo.batchErr = fmt.Errorf("%s: %w", canary, ErrCredentialSecretInvalid)
					repo.read = func(_ context.Context, id int64) (*Account, error) {
						if id == accounts[0].ID {
							return repo.accounts[id], nil
						}
						return nil, failure
					}
				}
				batch, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), accounts)
				require.ErrorIs(t, err, failure)
				require.Nil(t, batch, "infrastructure failure cannot publish earlier healthy rows")
				require.NotContains(t, err.Error(), canary)
				if phase == "batch" {
					require.Empty(t, repo.singleIDs)
				} else {
					require.Len(t, repo.singleIDs, 2)
				}
			})
		}
	}
	svc, repo, accounts, _ := schedulerRecoveryFixture()
	repo.batchErr = fmt.Errorf("%s: %w", canary, ErrCredentialSecretInvalid)
	repo.read = func(context.Context, int64) (*Account, error) {
		return nil, fmt.Errorf("%s: %w", canary, ErrCredentialSecretRevoked)
	}
	batch, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), accounts)
	require.NoError(t, err)
	require.Len(t, batch, 2)
	require.Nil(t, batch[accounts[0].ID])
	require.NotContains(t, logged.String(), canary)
}

func TestOpenAIRecheckRecoveryBoundsAndCancellation(t *testing.T) {
	t.Run("healthy batch is not subject to recovery cap", func(t *testing.T) {
		svc, repo, _, _ := schedulerRecoveryFixture()
		var candidates []*Account
		for i := 1; i <= openAIAccountRecoveryMaxAccounts+1; i++ {
			a := &Account{ID: int64(i)}
			candidates = append(candidates, a)
			repo.accounts[a.ID] = a
		}
		batch, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), candidates)
		require.NoError(t, err)
		require.Len(t, batch, len(candidates))
		require.Empty(t, repo.singleIDs)
	})
	for _, size := range []int{openAIAccountRecoveryMaxAccounts, openAIAccountRecoveryMaxAccounts + 1} {
		t.Run(fmt.Sprintf("quantity/%d", size), func(t *testing.T) {
			svc, repo, _, _ := schedulerRecoveryFixture()
			repo.batchErr = fmt.Errorf("synthetic-hidden: %w", ErrCredentialSecretInvalid)
			repo.read = func(context.Context, int64) (*Account, error) { return nil, ErrCredentialSecretRevoked }
			candidates := make([]*Account, 0, size*2)
			for i := 1; i <= size; i++ {
				a := &Account{ID: int64(i)}
				candidates = append(candidates, a, a)
			}
			batch, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), candidates)
			if size > openAIAccountRecoveryMaxAccounts {
				require.ErrorIs(t, err, ErrCredentialSecretInvalid)
				require.NotContains(t, err.Error(), "synthetic-hidden")
				require.Nil(t, batch)
				require.Empty(t, repo.singleIDs)
			} else {
				require.NoError(t, err)
				require.Len(t, batch, size)
				require.Len(t, repo.singleIDs, size)
			}
		})
	}
	for _, phase := range []string{"before", "batch", "single"} {
		t.Run("cancel/"+phase, func(t *testing.T) {
			svc, repo, accounts, _ := schedulerRecoveryFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo.batchErr = ErrCredentialSecretInvalid
			switch phase {
			case "before":
				cancel()
			case "batch":
				repo.batchHook = func(context.Context) { cancel() }
			case "single":
				repo.read = func(context.Context, int64) (*Account, error) { cancel(); return accounts[0], nil }
			}
			batch, err := svc.loadOpenAIAccountRecheckBatch(ctx, accounts)
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, batch)
			if phase == "before" {
				require.Zero(t, repo.batchCalls)
			}
			if phase == "single" {
				require.Len(t, repo.singleIDs, 1)
			} else {
				require.Empty(t, repo.singleIDs)
			}
		})
	}
	t.Run("deadline inherits tighter caller budget", func(t *testing.T) {
		svc, repo, accounts, _ := schedulerRecoveryFixture()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()
		repo.batchErr = ErrCredentialSecretInvalid
		repo.read = func(ctx context.Context, _ int64) (*Account, error) { <-ctx.Done(); return nil, ctx.Err() }
		batch, err := svc.loadOpenAIAccountRecheckBatch(ctx, accounts)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Nil(t, batch)
		require.Len(t, repo.singleIDs, 1)
	})
	t.Run("recovery installs its own deadline", func(t *testing.T) {
		svc, repo, accounts, _ := schedulerRecoveryFixture()
		repo.batchErr = ErrCredentialSecretInvalid
		repo.read = func(ctx context.Context, _ int64) (*Account, error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), openAIAccountRecoveryTimeout)
			return nil, ErrCredentialSecretRevoked
		}
		_, err := svc.loadOpenAIAccountRecheckBatch(context.Background(), accounts)
		require.NoError(t, err)
	})
}
