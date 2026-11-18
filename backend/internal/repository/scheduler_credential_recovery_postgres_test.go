package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// These tests can only write the task's dedicated synthetic database.
func schedulerRecoveryPostgresFixture(t *testing.T) *credentialSecretFixture {
	t.Helper()
	dsn := os.Getenv("SCHEDULER_RECOVERY_TEST_DSN")
	if dsn == "" {
		t.Skip("set SCHEDULER_RECOVERY_TEST_DSN to the isolated scheduler_recovery_test database")
	}
	parsed, err := url.Parse(dsn)
	require.True(t, err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") && parsed.Path == "/scheduler_recovery_test" &&
		(parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1") && parsed.Query().Get("dbname") == "" && parsed.Query().Get("host") == "", "only the dedicated local synthetic database is allowed")
	t.Setenv(credentialSecretPostgresEnv, dsn)
	f := newCredentialSecretFixture(t)
	f.ctx = domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("scheduler-recovery")))
	return f
}

type schedulerRecoverySnapshot struct {
	service.SchedulerCache
	accounts []*service.Account
}

func (s *schedulerRecoverySnapshot) GetSnapshot(context.Context, service.SchedulerBucket) ([]*service.Account, bool, error) {
	return s.accounts, true, nil
}
func (s *schedulerRecoverySnapshot) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	for _, a := range s.accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, nil
}

type schedulerRecoverySettings struct{ service.SettingRepository }

func (*schedulerRecoverySettings) GetValue(context.Context, string) (string, error) { return "", nil }

func (*schedulerRecoverySettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{"openai_advanced_scheduler_enabled": "true"}, nil
}

type schedulerRecoveryTrackingRepo struct {
	service.AccountRepository
	batchCalls          int
	singleIDs, batchIDs []int64
	beforeRead          func(context.Context, int64)
}

func (r *schedulerRecoveryTrackingRepo) GetByIDs(ctx context.Context, ids []int64) ([]*service.Account, error) {
	r.batchCalls++
	r.batchIDs = append([]int64(nil), ids...)
	return r.AccountRepository.GetByIDs(ctx, ids)
}
func (r *schedulerRecoveryTrackingRepo) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	r.singleIDs = append(r.singleIDs, id)
	if r.beforeRead != nil {
		r.beforeRead(ctx, id)
	}
	return r.AccountRepository.GetByID(ctx, id)
}

func schedulerRecoveryGateway(repo service.AccountRepository, accounts []*service.Account, redisClient *redis.Client) *service.OpenAIGatewayService {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	snapshot := service.NewSchedulerSnapshotService(&schedulerRecoverySnapshot{accounts: accounts}, nil, repo, nil, cfg)
	settings := service.NewSettingService(&schedulerRecoverySettings{}, cfg)
	limiter := service.NewRateLimitService(repo, nil, cfg, nil, nil)
	limiter.SetSettingService(settings)
	return service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, NewGatewayCache(redisClient), cfg, snapshot,
		service.NewConcurrencyService(NewConcurrencyCache(redisClient, 5, 30)), nil, limiter, nil, nil, nil, nil, nil, nil, nil, nil,
		settings, nil)
}

func createSchedulerRecoveryAccount(t *testing.T, f *credentialSecretFixture) *service.Account {
	t.Helper()
	a := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeAPIKey,
		map[string]any{"api_key": "scheduler-recovery-synthetic-only", "model_mapping": map[string]any{"gpt-5.1": "gpt-5.1"}})
	_, err := f.client.Account.UpdateOneID(a.ID).SetSchedulable(true).Save(f.ctx)
	require.NoError(t, err)
	a, err = f.accounts.GetByID(f.ctx, a.ID)
	require.NoError(t, err)
	return a
}

func schedulerRecoverySelect(t *testing.T, ctx context.Context, gateway *service.OpenAIGatewayService, path, session string) (*service.AccountSelectionResult, error) {
	t.Helper()
	if path == "legacy" {
		return gateway.SelectAccountWithLoadAwareness(ctx, nil, session, "gpt-5.1", nil)
	}
	selection, decision, err := gateway.SelectAccountWithScheduler(ctx, nil, "", session, "gpt-5.1", nil, service.OpenAIUpstreamTransportAny, false)
	if err == nil {
		require.Positive(t, decision.CandidateCount, "advanced scheduler must actually execute its load selection")
	}
	return selection, err
}

func TestSchedulerRecoveryPostgresHealthyBackupAndScope(t *testing.T) {
	for _, failure := range []string{"revoked", "corrupt", "conflict"} {
		t.Run(failure, func(t *testing.T) {
			f := schedulerRecoveryPostgresFixture(t)
			bad, good := createSchedulerRecoveryAccount(t, f), createSchedulerRecoveryAccount(t, f)
			foreignContext := domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("scheduler-foreign")))
			localContext := f.ctx
			f.ctx = foreignContext
			foreign := createSchedulerRecoveryAccount(t, f)
			f.ctx = localContext
			var err error
			switch failure {
			case "revoked":
				_, err = f.secrets.Revoke(f.ctx, bad.ID, 1)
			case "corrupt":
				_, err = f.db.ExecContext(f.ctx, "UPDATE credential_secrets SET ciphertext='synthetic-corrupt-envelope' WHERE account_id=$1", bad.ID)
			case "conflict":
				_, err = f.db.ExecContext(f.ctx, "UPDATE accounts SET credentials=jsonb_set(credentials,'{credential_secret_version}','99'::jsonb) WHERE id=$1", bad.ID)
			}
			require.NoError(t, err)
			_, err = f.secrets.Rotate(f.ctx, good.ID, 1, map[string]any{"api_key": "scheduler-recovery-rotated-synthetic"})
			require.NoError(t, err)
			strict, err := f.accounts.GetByIDs(f.ctx, []int64{bad.ID, good.ID})
			require.Error(t, err)
			require.Nil(t, strict, "general reads must remain fail-closed")
			for _, path := range []string{"legacy", "advanced"} {
				t.Run(path, func(t *testing.T) {
					server := miniredis.RunT(t)
					redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
					t.Cleanup(func() { _ = redisClient.Close() })
					repo := &schedulerRecoveryTrackingRepo{AccountRepository: f.accounts}
					gateway := schedulerRecoveryGateway(repo, []*service.Account{bad, good, foreign}, redisClient)
					selected, err := schedulerRecoverySelect(t, f.ctx, gateway, path, "")
					require.NoError(t, err)
					require.Equal(t, good.ID, selected.Account.ID)
					require.EqualValues(t, 2, service.CredentialVersion(selected.Account.Credentials))
					require.Equal(t, []int64{bad.ID, good.ID}, repo.batchIDs)
					require.Equal(t, repo.batchIDs, repo.singleIDs, "one recovery read per local candidate; no later hydration read")
					require.Equal(t, 1, repo.batchCalls)
					if selected.ReleaseFunc != nil {
						selected.ReleaseFunc()
					}
					fresh, err := f.accounts.GetByID(f.ctx, good.ID)
					require.NoError(t, err)
					require.Equal(t, fresh.CredentialRevision, selected.Account.CredentialRevision)
					require.ErrorIs(t, f.accounts.UpdateCredentialsCAS(f.ctx, good, good.Credentials), service.ErrCredentialSecretConflict)
					_, err = f.accounts.GetByID(f.ctx, foreign.ID)
					require.ErrorIs(t, err, service.ErrAccountNotFound)
				})
			}
		})
	}
}

func TestSchedulerRecoveryPostgresStickyVersionCAS(t *testing.T) {
	f := schedulerRecoveryPostgresFixture(t)
	bad, good := createSchedulerRecoveryAccount(t, f), createSchedulerRecoveryAccount(t, f)
	_, err := f.secrets.Revoke(f.ctx, bad.ID, 1)
	require.NoError(t, err)
	for _, path := range []string{"legacy", "advanced"} {
		for _, replace := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/replace=%v", path, replace), func(t *testing.T) {
				server := miniredis.RunT(t)
				client := redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { _ = client.Close() })
				cache := NewGatewayCache(client)
				const session = "v2:binding:hmac:scheduler-recovery:synthetic"
				key := buildSessionKey(0, session)
				old, winner := fmt.Sprintf("v2:%d:observed", bad.ID), fmt.Sprintf("v2:%d:competitor", good.ID)
				require.NoError(t, client.Set(f.ctx, key, old, time.Minute).Err())
				ctx := service.WithAffinityBindingState(f.ctx)
				_, err := cache.GetSessionAccountID(ctx, 0, session)
				require.NoError(t, err)
				repo := &schedulerRecoveryTrackingRepo{AccountRepository: f.accounts}
				repo.beforeRead = func(readCtx context.Context, id int64) {
					require.Same(t, service.AffinityBindingStateFromContext(ctx), service.AffinityBindingStateFromContext(readCtx))
					if replace && id == bad.ID {
						require.NoError(t, client.Set(ctx, key, winner, time.Minute).Err())
					}
				}
				gateway := schedulerRecoveryGateway(repo, []*service.Account{bad, good}, client)
				selected, err := schedulerRecoverySelect(t, ctx, gateway, path, session)
				require.NoError(t, err)
				require.Equal(t, good.ID, selected.Account.ID)
				if selected.ReleaseFunc != nil {
					selected.ReleaseFunc()
				}
				// Recovery must neither replace the caller's observed nonce nor
				// gain permission to delete/overwrite a concurrent winner.
				observed, _ := service.AffinityBindingStateFromContext(ctx).Versions.Load(key)
				require.Equal(t, old, observed)
				require.NoError(t, cache.DeleteSessionAccountID(ctx, 0, session))
				require.NoError(t, cache.SetSessionAccountID(service.WithAffinityAdmission(ctx), 0, session, good.ID, 5*time.Minute))
				value, err := server.Get(key)
				require.NoError(t, err)
				if replace {
					require.Equal(t, winner, value)
					require.Equal(t, time.Minute, server.TTL(key))
				} else {
					require.Contains(t, value, fmt.Sprintf("v2:%d:", good.ID))
					require.Equal(t, 5*time.Minute, server.TTL(key))
				}
			})
		}
	}
}

func TestSchedulerRecoveryPostgresInfrastructureFailure(t *testing.T) {
	f := schedulerRecoveryPostgresFixture(t)
	bad, good := createSchedulerRecoveryAccount(t, f), createSchedulerRecoveryAccount(t, f)
	_, err := f.secrets.Revoke(f.ctx, bad.ID, 1)
	require.NoError(t, err)
	for _, path := range []string{"legacy", "advanced"} {
		for _, phase := range []string{"batch", "recovery"} {
			t.Run(path+"/"+phase, func(t *testing.T) {
				repo, driver := countingCredentialRepository(f)
				failure := errors.Join(service.ErrCredentialSecretInvalid, errors.New("synthetic infrastructure failure"))
				if phase == "batch" {
					driver.secretReadError = failure
				} else {
					driver.beforeSecretRead = func() { driver.beforeSecretRead = func() { driver.secretReadError = failure } }
				}
				server := miniredis.RunT(t)
				client := redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { _ = client.Close() })
				gateway := schedulerRecoveryGateway(repo, []*service.Account{bad, good}, client)
				selected, err := schedulerRecoverySelect(t, f.ctx, gateway, path, "")
				require.ErrorIs(t, err, failure)
				require.Nil(t, selected)
				require.NotContains(t, err.Error(), "synthetic infrastructure failure")
				if phase == "batch" {
					require.EqualValues(t, 1, driver.secretQueries.Load())
				} else {
					require.EqualValues(t, 2, driver.secretQueries.Load())
				}
			})
		}
	}
}

func TestSchedulerRecoveryPostgresCallerTransaction(t *testing.T) {
	f := schedulerRecoveryPostgresFixture(t)
	bad, good := createSchedulerRecoveryAccount(t, f), createSchedulerRecoveryAccount(t, f)
	_, err := f.secrets.Revoke(f.ctx, bad.ID, 1)
	require.NoError(t, err)
	f.db.SetMaxOpenConns(1)
	for _, path := range []string{"legacy", "advanced"} {
		t.Run(path, func(t *testing.T) {
			tx, err := f.client.Tx(f.ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			ctx, cancel := context.WithTimeout(dbent.NewTxContext(f.ctx, tx), 4*time.Second)
			defer cancel()
			_, err = tx.Client().Account.UpdateOneID(good.ID).SetConcurrency(9).Save(ctx)
			require.NoError(t, err)
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			repo := &schedulerRecoveryTrackingRepo{AccountRepository: f.accounts}
			gateway := schedulerRecoveryGateway(repo, []*service.Account{bad, good}, client)
			selected, selectErr := schedulerRecoverySelect(t, ctx, gateway, path, "")
			rollbackErr := tx.Rollback()
			require.NoError(t, selectErr)
			require.NoError(t, rollbackErr)
			require.Equal(t, 9, selected.Account.Concurrency, "fallback must see caller's uncommitted row on its sole connection")
			require.Equal(t, []int64{bad.ID, good.ID}, repo.singleIDs)
			if selected.ReleaseFunc != nil {
				selected.ReleaseFunc()
			}
			stored, err := f.accounts.GetByID(f.ctx, good.ID)
			require.NoError(t, err)
			require.Equal(t, good.Concurrency, stored.Concurrency, "rollback must not persist the transaction's candidate change")
		})
	}
}
