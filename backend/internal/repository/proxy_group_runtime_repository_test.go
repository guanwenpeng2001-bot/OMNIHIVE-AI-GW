//go:build unit

package repository

import (
	"context"
	"encoding/json"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type postgresRuntimeCache struct {
	service.ProxyGroupCache
	bindings map[[2]int64]int64
	calls    int
}

func (c *postgresRuntimeCache) GetBinding(_ context.Context, g, a int64) (int64, error) {
	c.calls++
	return c.bindings[[2]int64{g, a}], nil
}
func (c *postgresRuntimeCache) TrySetBindingNX(_ context.Context, g, a, p int64, _ time.Duration) (int64, error) {
	c.calls++
	if a <= 0 {
		panic("account zero")
	}
	key := [2]int64{g, a}
	if c.bindings[key] == 0 {
		c.bindings[key] = p
	}
	return c.bindings[key], nil
}
func (c *postgresRuntimeCache) TouchBindingIfValue(_ context.Context, g, a, p int64, _ time.Duration) (bool, error) {
	c.calls++
	return c.bindings[[2]int64{g, a}] == p, nil
}
func (c *postgresRuntimeCache) DeleteBindingIfValue(_ context.Context, g, a, p int64) (bool, error) {
	c.calls++
	key := [2]int64{g, a}
	if c.bindings[key] != p {
		return false, nil
	}
	delete(c.bindings, key)
	return true, nil
}
func (c *postgresRuntimeCache) GetCooldown(context.Context, int64) (*service.ProxyCooldown, error) {
	c.calls++
	return nil, nil
}

func TestProxyGroupRuntimeDedicatedPostgres(t *testing.T) {
	db, client := proxyGroupTestPostgres(t)
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := dbent.NewTxContext(context.Background(), tx)
	workspace, err := tx.Client().Workspace.Create().SetName("p2-runtime-" + uuid.NewString()).Save(ctx)
	require.NoError(t, err)
	ctx = domain.WithWorkspaceScope(ctx, workspace.ID)
	groups := NewProxyGroupRepository(client)
	proxies := NewProxyRepository(client, db)
	group := &service.ProxyGroup{Name: "runtime", WorkspaceID: workspace.ID, Status: service.StatusActive, FallbackMode: service.FallbackModeNone}
	require.NoError(t, groups.Create(ctx, group))
	node := &service.Proxy{Name: "fixture", WorkspaceID: workspace.ID, Protocol: "http", Host: "proxy-fixture.invalid", Port: 8080, Status: service.StatusActive}
	require.NoError(t, proxies.Create(ctx, node))
	require.NoError(t, groups.ReplaceMembers(ctx, group.ID, []int64{node.ID}))
	cache := &postgresRuntimeCache{bindings: map[[2]int64]int64{}}
	runtime := service.NewProxyGroupRuntime(groups, proxies, cache)
	accounts := newAccountRepositoryWithSQL(client, db, nil)
	accounts.egressRuntime = runtime
	account := &service.Account{Name: "p2-account", WorkspaceID: workspace.ID, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{}, ProxyGroupID: &group.ID, Status: service.StatusActive, Schedulable: true, Concurrency: 1}
	require.NoError(t, accounts.Create(ctx, account))
	loaded, err := accounts.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, group.ID, *loaded.ProxyGroupID)
	url, err := loaded.EgressProxyURL(ctx)
	require.NoError(t, err)
	require.Equal(t, node.URL(), url)
	require.Nil(t, loaded.ProxyID)
	metadata := buildSchedulerMetadataAccount(*loaded)
	require.Equal(t, workspace.ID, metadata.WorkspaceID)
	require.Equal(t, group.ID, *metadata.ProxyGroupID)
	full, _, err := marshalSchedulerCacheAccount(*loaded)
	require.NoError(t, err)
	require.NotContains(t, string(full), "group_binding")
	decoded, err := decodeCachedAccount(string(full))
	require.NoError(t, err)
	require.Equal(t, group.ID, *decoded.ProxyGroupID)
	// The SQL bulk path clears both fields and legacy fallback bookkeeping.
	zero := int64(0)
	_, err = accounts.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{ProxyID: &zero, ProxyGroupID: &zero})
	require.NoError(t, err)
	loaded, err = accounts.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Nil(t, loaded.ProxyGroupID)
	require.Nil(t, loaded.ProxyID)
	calls := cache.calls
	url, err = loaded.EgressProxyURL(ctx)
	require.NoError(t, err)
	require.Empty(t, url)
	require.Equal(t, calls, cache.calls)
	_, err = accounts.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{ProxyGroupID: &group.ID})
	require.NoError(t, err)
	_, err = accounts.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{ProxyID: &node.ID})
	require.Error(t, err)
}

func TestBatchImageRuntimeSnapshotDedicatedPostgres(t *testing.T) {
	db, _ := proxyGroupTestPostgres(t)
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	migration, err := migrations.FS.ReadFile("281_batch_image_egress_snapshot.sql")
	require.NoError(t, err)
	_, err = tx.Exec(string(migration))
	require.NoError(t, err)
	var userID int64
	require.NoError(t, tx.QueryRow("INSERT INTO users(email,password_hash) VALUES($1,$2) RETURNING id", "p2-batch-"+uuid.NewString()+"@fixture.invalid", "fixture-hash").Scan(&userID))
	route := &service.ProxyGroupJobEgress{ProxyGroupID: 7, ProxyID: 11, Source: service.EgressSourceGroupBinding}
	repo := &batchImageRepository{sql: tx}
	job, err := repo.CreateBatchImageJob(context.Background(), service.CreateBatchImageJobParams{BatchID: uuid.NewString(), UserID: userID, Provider: service.BatchImageProviderGeminiAPI, Model: "fixture", Status: service.BatchImageJobStatusCreated, Currency: "USD", EgressSnapshot: route, GroupRateMultiplier: 1, AccountRateMultiplier: 1, BatchDiscountMultiplier: 1, HoldMultiplier: 1})
	require.NoError(t, err)
	require.Equal(t, route, job.EgressSnapshot)
	loaded, err := repo.GetBatchImageJobByBatchID(context.Background(), job.BatchID)
	require.NoError(t, err)
	require.Equal(t, route, loaded.EgressSnapshot)
	public, err := json.Marshal(loaded)
	require.NoError(t, err)
	require.NotContains(t, string(public), "egress")
	require.NotContains(t, string(public), "proxy_group_id")
	var stored string
	require.NoError(t, tx.QueryRow("SELECT egress_snapshot::text FROM batch_image_jobs WHERE id=$1", job.ID).Scan(&stored))
	require.JSONEq(t, `{"proxy_group_id":7,"proxy_id":11,"source":"group_binding"}`, stored)
}
