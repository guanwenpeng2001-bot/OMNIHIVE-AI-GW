package repository

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type membershipTrackingRepository struct {
	service.AccountRepository
	calls int
}

func (r *membershipTrackingRepository) ListModelAvailabilityCandidates(ctx context.Context, group *int64, platforms []string, all bool) ([]service.Account, error) {
	r.calls++
	return r.AccountRepository.ListModelAvailabilityCandidates(ctx, group, platforms, all)
}

func (r *membershipTrackingRepository) ListSchedulableByGroupIDAndPlatform(ctx context.Context, g int64, p string) ([]service.Account, error) {
	r.calls++
	return r.AccountRepository.ListSchedulableByGroupIDAndPlatform(ctx, g, p)
}

// The launcher validates both Docker labels. This test additionally pins the
// database, loopback ports and an otherwise empty Redis logical database.
func TestSchedulerResourcesScopePostgresRedis(t *testing.T) {
	dsn := os.Getenv("SCHEDULER_RESOURCES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated scheduler_resources_test and Redis fixture")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "postgres", u.Scheme)
	require.Equal(t, "127.0.0.1:55480", u.Host)
	require.Equal(t, "/scheduler_resources_test", u.Path)
	require.Equal(t, "sslmode=disable", u.RawQuery)
	require.Equal(t, "127.0.0.1:56380", os.Getenv("SCHEDULER_RESOURCES_REDIS_ADDR"))
	t.Setenv(credentialSecretPostgresEnv, dsn)
	f := newCredentialSecretFixture(t)
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:56380", DB: 14, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	size, err := client.DBSize(ctx).Result()
	require.NoError(t, err)
	require.Zero(t, size, "fixture DB14 must be unused; never flush another task's data")
	require.True(t, client.SetNX(ctx, "scheduler-resources-fixture-owner", t.Name(), time.Hour).Val())
	t.Cleanup(func() { _ = client.Del(ctx, "scheduler-resources-fixture-owner").Err() })
	cache := NewSchedulerCache(client)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Gateway.Scheduling.DbFallbackEnabled = true
	repo := &membershipTrackingRepository{AccountRepository: f.accounts}
	svc := service.NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)
	workspace := f.createWorkspace(t, uniqueName("membership-owner"))
	foreignWorkspace := f.createWorkspace(t, uniqueName("membership-foreign"))
	ownerCtx := domain.WithWorkspaceScope(ctx, workspace)
	foreignCtx := domain.WithWorkspaceScope(ctx, foreignWorkspace)
	// Own database rows are retained as synthetic evidence; no business database
	// is reachable through this fixture.
	newGroup := func() int64 {
		var id int64
		require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,workspace_id) VALUES($1,'composite',$2) RETURNING id`, uniqueName("membership-group"), workspace).Scan(&id))
		return id
	}
	for _, field := range []string{"rate_limit_reset_at", "overload_until", "temp_unschedulable_until"} {
		t.Run(field, func(t *testing.T) {
			group := newGroup()
			otherGroup := newGroup()
			bucket := service.SchedulerBucket{GroupID: group, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
			makeAccount := func(accountCtx context.Context, groupID int64) *service.Account {
				a := f.createAccount(t, accountCtx, uniqueName("membership-account"), service.PlatformOpenAI, service.AccountTypeAPIKey, map[string]any{"api_key": "synthetic-membership-only"})
				_, err := f.db.ExecContext(ctx, `UPDATE accounts SET schedulable=true WHERE id=$1`, a.ID)
				require.NoError(t, err)
				_, err = f.db.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, a.ID, groupID)
				require.NoError(t, err)
				return a
			}
			ready := makeAccount(ownerCtx, group)
			cooling := makeAccount(ownerCtx, group)
			// Cross-workspace group bindings are rejected by the database itself.
			foreign := f.createAccount(t, foreignCtx, uniqueName("membership-foreign"), service.PlatformOpenAI, service.AccountTypeAPIKey, map[string]any{"api_key": "synthetic-foreign"})
			_, crossErr := f.db.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, foreign.ID, group)
			require.Error(t, crossErr)
			_ = makeAccount(ownerCtx, otherGroup)
			until := time.Now().Add(-time.Second)
			_, err := f.db.ExecContext(ctx, fmt.Sprintf("UPDATE accounts SET %s=$1 WHERE id=$2", field), until, cooling.ID)
			require.NoError(t, err)
			// Capture keys before cleanup so only this test's exact account/bucket keys
			// are removed. The global registry removes only our membership.
			t.Cleanup(func() {
				for _, a := range []*service.Account{ready, cooling, foreign} {
					_ = client.Del(ctx, fmt.Sprint(schedulerAccountPrefix, a.ID), fmt.Sprint(schedulerAccountMetaPrefix, a.ID), fmt.Sprint(schedulerAccountLastUsedPrefix, a.ID)).Err()
				}
				for _, prefix := range []string{schedulerActivePrefix, schedulerReadyPrefix, schedulerVersionPrefix, schedulerEpochPrefix, schedulerRetiredPrefix} {
					_ = client.Del(ctx, schedulerBucketKey(prefix, bucket)).Err()
				}
				keys, _ := client.Keys(ctx, schedulerSnapshotPrefix+bucket.String()+":*").Result()
				if len(keys) > 0 {
					_ = client.Del(ctx, keys...).Err()
				}
				_ = client.SRem(ctx, schedulerBucketSetKey, bucket.String()).Err()
			})
			// Unscoped worker builds the shared pool. Scoped requests may read it but
			// must never publish a partial-workspace replacement.
			accounts, _, err := svc.ListSchedulableAccounts(ctx, &group, service.PlatformOpenAI, false)
			require.NoError(t, err)
			require.Len(t, accounts, 2)
			stored, hit, err := cache.GetSnapshot(ctx, bucket)
			require.NoError(t, err)
			require.True(t, hit)
			require.Len(t, stored, 2)
			before := repo.calls
			denied, _, deniedErr := svc.ListSchedulableAccounts(foreignCtx, &group, service.PlatformOpenAI, false)
			require.NoError(t, deniedErr)
			require.Empty(t, denied)
			accounts, _, err = svc.ListSchedulableAccounts(ownerCtx, &group, service.PlatformOpenAI, false)
			require.NoError(t, err)
			require.Len(t, accounts, 2)
			accounts, _, err = svc.ListSchedulableAccounts(ownerCtx, &group, service.PlatformOpenAI, false)
			require.NoError(t, err)
			require.Len(t, accounts, 2)
			require.Equal(t, before, repo.calls, "scoped warm-cache reads must not query the database")
			for _, a := range accounts {
				require.Equal(t, workspace, a.WorkspaceID)
				require.Contains(t, a.GroupIDs, group)
			}
			// A second cache client observes the same stored scheduling revocation.
			fresh, err := f.accounts.GetByID(ownerCtx, cooling.ID)
			require.NoError(t, err)
			fresh.Schedulable = false
			require.NoError(t, cache.SetAccount(ctx, fresh))
			second := service.NewSchedulerSnapshotService(NewSchedulerCache(client), nil, repo, nil, cfg)
			accounts, _, err = second.ListSchedulableAccounts(ownerCtx, &group, service.PlatformOpenAI, false)
			require.NoError(t, err)
			require.Len(t, accounts, 2)
			var updated *service.Account
			for i := range accounts {
				if accounts[i].ID == cooling.ID {
					updated = &accounts[i]
				}
			}
			require.NotNil(t, updated)
			require.False(t, updated.Schedulable)
			require.False(t, updated.IsSchedulable(), "candidate admission must see the revoked scheduling flag")
			// Scoped cold-cache reads remain scoped and cannot republish shared pools.
			require.NoError(t, cache.RetireBucket(ctx, bucket))
			_, err = cache.ReopenBucket(ctx, bucket)
			require.NoError(t, err)
			accounts, _, err = second.ListSchedulableAccounts(ownerCtx, &group, service.PlatformOpenAI, false)
			require.NoError(t, err)
			require.Len(t, accounts, 2)
			_, hit, err = cache.GetSnapshot(ctx, bucket)
			require.NoError(t, err)
			require.False(t, hit)
		})
	}
}

// Newly retaining a cooling member must not make its unusable credential
// poison an otherwise healthy pool during a cold load.
func TestSchedulerMembershipCoolingCredentialIsolationPostgres(t *testing.T) {
	dsn := os.Getenv("SCHEDULER_RESOURCES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires dedicated scheduler_resources_test")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:55480", u.Host)
	require.Equal(t, "/scheduler_resources_test", u.Path)
	t.Setenv(credentialSecretPostgresEnv, dsn)
	f := newCredentialSecretFixture(t)
	workspace := f.createWorkspace(t, uniqueName("cooling-credential"))
	f.ctx = domain.WithWorkspaceScope(f.ctx, workspace)
	var group int64
	require.NoError(t, f.db.QueryRowContext(f.ctx, `INSERT INTO groups(name,platform,workspace_id) VALUES($1,'composite',$2) RETURNING id`, uniqueName("cooling-credential-group"), workspace).Scan(&group))
	healthy := f.createAccount(t, f.ctx, uniqueName("healthy"), service.PlatformOpenAI, service.AccountTypeAPIKey, map[string]any{"api_key": "synthetic-healthy"})
	bad := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeAPIKey, map[string]any{"api_key": "synthetic-revoked"})
	for _, a := range []*service.Account{healthy, bad} {
		_, err = f.db.ExecContext(f.ctx, `UPDATE accounts SET schedulable=true WHERE id=$1`, a.ID)
		require.NoError(t, err)
		_, err = f.db.ExecContext(f.ctx, `INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, a.ID, group)
		require.NoError(t, err)
	}
	_, err = f.db.ExecContext(f.ctx, `UPDATE accounts SET rate_limit_reset_at=NOW()+interval '1 hour' WHERE id=$1`, bad.ID)
	require.NoError(t, err)
	_, err = f.secrets.Revoke(f.ctx, bad.ID, 1)
	require.NoError(t, err)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Gateway.Scheduling.DbFallbackEnabled = true
	svc := service.NewSchedulerSnapshotService(nil, nil, f.accounts, nil, cfg)
	got, _, err := svc.ListSchedulableAccounts(f.ctx, &group, service.PlatformOpenAI, false)
	require.NoError(t, err, "retaining a revoked cooling member must not block a healthy account")
	require.Len(t, got, 1)
	require.Equal(t, healthy.ID, got[0].ID)
}
