package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Uses only the exclusively assigned job_test database, never an application DB.
func capabilityPostgres(t *testing.T) (*sql.DB, *dbent.Client, service.AccountRepository) {
	t.Helper()
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to exclusive job_test")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var database string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database))
	require.Equal(t, "job_test", database)
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	return db, client, repository.NewAccountRepository(client, db, nil)
}

type capabilityPoolUpstream struct {
	entered chan int64
	block   bool
}

func (u *capabilityPoolUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	if u.block {
		select {
		case u.entered <- id:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
}

func (u *capabilityPoolUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestAccountModelCatalogApplyPostgresPoolBudget(t *testing.T) {
	db, client, repo := capabilityPostgres(t)
	for _, size := range []int{2, 4} {
		t.Run(fmt.Sprintf("pool_%d", size), func(t *testing.T) {
			db.SetMaxOpenConns(size)
			db.SetMaxIdleConns(size)
			accounts := make([]*service.Account, 2)
			for i := range accounts {
				row, err := client.Account.Create().SetName("catalog-pool").SetPlatform(service.PlatformDeepseek).SetType(service.AccountTypeAPIKey).SetCredentials(map[string]any{"api_key": "test", "base_url": "https://api.deepseek.com"}).Save(context.Background())
				require.NoError(t, err)
				accounts[i], err = repo.GetByID(context.Background(), row.ID)
				require.NoError(t, err)
			}
			upstream := &capabilityPoolUpstream{entered: make(chan int64, 2), block: true}
			cfg := &config.Config{}
			cfg.Database.MaxOpenConns = size
			svc := service.NewAccountTestService(repo, nil, nil, nil, nil, upstream, cfg, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			results := make(chan error, 2)
			for _, account := range accounts {
				go func(a *service.Account) { _, err := svc.ApplyAccountModelCatalog(ctx, a, nil); results <- err }(account)
			}
			for i := 0; i < size/2; i++ {
				select {
				case <-upstream.entered:
				case <-ctx.Done():
					t.Fatal("apply failed to enter discovery before deadline")
				}
			}
			if size == 2 {
				select {
				case <-upstream.entered:
					t.Fatal("second account exceeded shared pool budget")
				case <-time.After(100 * time.Millisecond):
				}
			}
			queryCtx, queryCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			require.NoError(t, db.PingContext(queryCtx), "ordinary queries retain a free connection during discovery")
			queryCancel()
			// Deadline cancels both active discovery and any waiter that has no permit.
			for range accounts {
				select {
				case err := <-results:
					require.Error(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("apply did not release on timeout")
				}
			}
			require.Eventually(t, func() bool { return db.Stats().InUse == 0 }, time.Second, 10*time.Millisecond)
			require.NoError(t, db.PingContext(context.Background()))
			upstream.block = false
			// Reuse the same service: permits and advisory locks must both be released.
			for _, account := range accounts {
				go func(a *service.Account) {
					_, err := svc.ApplyAccountModelCatalog(context.Background(), a, nil)
					results <- err
				}(account)
			}
			for range accounts {
				select {
				case err := <-results:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("apply budget was not reusable")
				}
			}
			require.Eventually(t, func() bool { return db.Stats().InUse == 0 }, time.Second, 10*time.Millisecond)
		})
	}
}

func TestCapabilityDefaultsPostgresManualTokenReconfirmation(t *testing.T) {
	_, client, repo := capabilityPostgres(t)
	ctx := context.Background()
	row, err := client.Account.Create().SetName("catalog-oauth").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).SetCredentials(map[string]any{"access_token": "old", "chatgpt_account_id": "stable"}).SetExtra(map[string]any{service.UpstreamModelCatalogStateExtraKey: service.AccountModelCatalogState{IdentityRevision: 3, RulesRevision: 7}}).Save(ctx)
	require.NoError(t, err)
	existing, err := repo.GetByID(ctx, row.ID)
	require.NoError(t, err)
	declaration, err := decodeModelCapabilityDefaults(json.RawMessage(`{"schema_version":1,"reason":"owner reconfirmed","models":{"private-id":{"input_modalities":[],"output_modalities":[],"reasoning":true,"reasoning_control":{"kind":"unknown","effort_values":[]}}}}`))
	require.NoError(t, err)
	credentials := map[string]any{"access_token": "new", "chatgpt_account_id": "stable"}
	extra := map[string]any{modelCapabilityDefaultsKey: declaration}
	require.NoError(t, prepareUpdatedModelCapabilityDefaults(existing, "", credentials, extra, "admin:42"))
	edit := *existing
	edit.Credentials, edit.Extra = credentials, extra
	writer := repo.(interface {
		UpdateWithAccountBillingSettings(context.Context, *service.Account, *bool, *bool, *float64) error
	})
	require.NoError(t, writer.UpdateWithAccountBillingSettings(ctx, &edit, nil, nil, nil))
	saved, err := repo.GetByID(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, int64(4), saved.ModelCatalogState().IdentityRevision)
	require.NoError(t, validateSavedModelCapabilityIdentity(saved))
	cfg := &config.Config{}
	upstream := &capabilityPoolUpstream{}
	svc := service.NewAccountTestService(repo, nil, nil, nil, nil, upstream, cfg, nil)
	_, err = svc.SyncUpstreamModelCatalog(ctx, saved)
	require.NoError(t, err, "reconfirmation is immediately previewable")
	applied, err := svc.ApplyAccountModelCatalog(ctx, saved, nil)
	require.NoError(t, err, "reconfirmation is immediately applicable")
	require.True(t, applied.Applied)
	saved, err = repo.GetByID(ctx, row.ID)
	require.NoError(t, err)
	saved.Credentials["access_token"] = "automatic-renewal"
	require.NoError(t, repo.Update(ctx, saved))
	renewed, err := repo.GetByID(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, int64(4), renewed.ModelCatalogState().IdentityRevision, "same-subject automatic renewal keeps identity")
	require.NoError(t, validateSavedModelCapabilityIdentity(renewed))
}
