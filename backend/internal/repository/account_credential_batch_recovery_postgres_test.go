package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type recoveryUsageLogRepo struct{ service.UsageLogRepository }

func (*recoveryUsageLogRepo) GetAccountWindowStats(context.Context, int64, time.Time) (*usagestats.AccountStats, error) {
	return &usagestats.AccountStats{}, nil
}

// Only this in-memory transport sees synthetic credentials. No external request
// is made, even if a regression changes the requested URL or account.
type recoveryUsageUpstream struct {
	service.HTTPUpstream
	calls     atomic.Int32
	invalid   atomic.Bool
	accountID int64
	token     string
}

func (u *recoveryUsageUpstream) Do(r *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.calls.Add(1)
	if id != u.accountID || r.Header.Get("Authorization") != "Bearer "+u.token || r.URL.Path != "/api/v1/key" {
		u.invalid.Store(true)
		return nil, errors.New("unexpected synthetic usage request")
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":{"limit":100,"usage":12}}`))}, nil
}

func (u *recoveryUsageUpstream) DoWithTLS(r *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, proxy, id, concurrency)
}

func TestCredentialBatchRecoveryPostgresUsage(t *testing.T) {
	for _, failure := range []string{"revoked", "corrupt", "missing", "conflict"} {
		t.Run(failure, func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			f.ctx = domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("usage-recovery")))
			const secret = "recovery-usage-synthetic-material"
			passive := createBatchReferencedAccount(t, f, service.PlatformAnthropic, service.AccountTypeOAuth, map[string]any{"access_token": secret})
			active := createBatchReferencedAccount(t, f, service.PlatformOpenRouter, service.AccountTypeAPIKey, map[string]any{"api_key": secret})
			bad := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"refresh_token": secret})
			foreignCtx := domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("usage-recovery-foreign")))
			foreign := f.createAccount(t, foreignCtx, uniqueName("hidden-usage"), service.PlatformAnthropic, service.AccountTypeOAuth, map[string]any{"access_token": "foreign-usage-fixture"})
			_, err := f.secrets.Convert(foreignCtx, foreign.ID, f.accountRevision(t, foreign.ID))
			require.NoError(t, err)
			_, ciphertext, err := readCredentialSecret(f.ctx, f.db, bad.ID)
			require.NoError(t, err)
			switch failure {
			case "revoked":
				_, err = f.secrets.Revoke(f.ctx, bad.ID, 1)
			case "corrupt":
				f.writeInconsistentSecret(t, "UPDATE credential_secrets SET ciphertext='synthetic-corrupt-ciphertext' WHERE account_id=$1", bad.ID)
			case "missing":
				// Remove only this test's newly created secret, never baseline rows.
				_, err = f.db.ExecContext(f.ctx, "DELETE FROM credential_secrets WHERE account_id=$1", bad.ID)
			case "conflict":
				_, err = f.db.ExecContext(f.ctx, "UPDATE accounts SET credentials=jsonb_set(credentials,'{credential_secret_version}','99'::jsonb) WHERE id=$1", bad.ID)
			}
			require.NoError(t, err)
			strict, err := f.accounts.GetByIDs(f.ctx, []int64{passive.ID, bad.ID, active.ID})
			require.Error(t, err)
			require.Nil(t, strict, "general reads must still fail closed")
			upstream := &recoveryUsageUpstream{accountID: active.ID, token: secret}
			gateway := service.NewOpenAIGatewayService(f.accounts, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
			usageService := service.ProvideAccountUsageService(nil, nil, f.accounts, &recoveryUsageLogRepo{}, nil, nil, nil, nil, nil, nil, service.NewUsageCache(), nil, nil, gateway)
			usage, failures, err := usageService.GetUsageBatch(f.ctx, []int64{passive.ID, bad.ID, active.ID, bad.ID, foreign.ID, 999999999}, true)
			require.NoError(t, err)
			require.Len(t, usage, 2)
			require.Equal(t, "passive", usage[passive.ID].Source)
			require.Equal(t, "active", usage[active.ID].Source)
			require.Len(t, failures, 3)
			require.NotEmpty(t, failures[bad.ID])
			require.Equal(t, service.ErrAccountNotFound.Error(), failures[foreign.ID])
			require.Equal(t, service.ErrAccountNotFound.Error(), failures[999999999])
			require.EqualValues(t, 1, upstream.calls.Load())
			require.False(t, upstream.invalid.Load())
			body, err := json.Marshal(struct {
				Usage  map[int64]*service.UsageInfo
				Errors map[int64]string
			}{usage, failures})
			require.NoError(t, err)
			for _, material := range []string{secret, ciphertext, "synthetic-corrupt-ciphertext", "foreign-usage-fixture"} {
				if strings.Contains(string(body), material) {
					t.Fatal("credential material leaked into usage result")
				}
			}
		})
	}
}

func TestCredentialBatchRecoveryPostgresCandidateTransaction(t *testing.T) {
	for _, maxConnections := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d connections", maxConnections), func(t *testing.T) {
			f := newCredentialSecretFixture(t)
			f.ctx = domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("candidate-tx")))
			inactive := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"refresh_token": "candidate-transaction-fixture"})
			_, err := f.client.Account.UpdateOneID(inactive.ID).SetStatus("inactive").Save(f.ctx)
			require.NoError(t, err)
			active := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"refresh_token": "candidate-active-fixture"})
			options := service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformOpenAI}, Limit: 1, ActiveOnly: true, RequireRefreshToken: true}
			f.db.SetMaxOpenConns(maxConnections)
			tx, err := f.client.Tx(f.ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			txCtx := dbent.NewTxContext(f.ctx, tx)
			_, err = tx.Client().Account.UpdateOneID(inactive.ID).SetStatus(service.StatusActive).Save(txCtx)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(txCtx, 2*time.Second)
			defer cancel()
			page, readErr := f.accounts.ListOAuthRefreshCandidatePage(ctx, options)
			// Release the transaction even when reproducing the pre-fix timeout.
			if readErr != nil {
				_ = tx.Rollback()
				t.Fatalf("candidate scan failed in caller transaction: %v", readErr)
			}
			require.Len(t, page.Accounts, 1)
			require.Equal(t, inactive.ID, page.Accounts[0].ID, "scan must see uncommitted activation")
			require.True(t, page.HasMore)
			options.AfterID = page.NextAfterID
			page, err = f.accounts.ListOAuthRefreshCandidatePage(ctx, options)
			require.NoError(t, err)
			require.Len(t, page.Accounts, 1)
			require.Equal(t, active.ID, page.Accounts[0].ID)
			options.AfterID = page.NextAfterID
			page, err = f.accounts.ListOAuthRefreshCandidatePage(ctx, options)
			require.NoError(t, err)
			require.Empty(t, page.Accounts)
			require.False(t, page.HasMore)
			require.NoError(t, tx.Rollback())
			options.AfterID = 0
			page, err = f.accounts.ListOAuthRefreshCandidatePage(f.ctx, options)
			require.NoError(t, err)
			require.Len(t, page.Accounts, 1)
			require.Equal(t, active.ID, page.Accounts[0].ID, "rollback must keep original account inactive")
			options.AfterID = page.NextAfterID
			page, err = f.accounts.ListOAuthRefreshCandidatePage(f.ctx, options)
			require.NoError(t, err)
			require.Empty(t, page.Accounts)
			require.False(t, page.HasMore)
		})
	}
}

func TestCredentialBatchRecoveryPostgresInfrastructureErrors(t *testing.T) {
	f := newCredentialSecretFixture(t)
	f.ctx = domain.WithWorkspaceScope(f.ctx, f.createWorkspace(t, uniqueName("recovery-db-error")))
	good := createBatchReferencedAccount(t, f, service.PlatformAnthropic, service.AccountTypeOAuth, map[string]any{"access_token": "infrastructure-fixture"})
	bad := createBatchReferencedAccount(t, f, service.PlatformOpenAI, service.AccountTypeOAuth, map[string]any{"refresh_token": "infrastructure-fixture"})
	_, err := f.secrets.Revoke(f.ctx, bad.ID, 1)
	require.NoError(t, err)
	for _, phase := range []string{"batch", "recovery"} {
		for _, failure := range []error{errors.New("synthetic database read failure"), context.Canceled, context.DeadlineExceeded} {
			t.Run(phase+"/"+failure.Error(), func(t *testing.T) {
				repo, driver := countingCredentialRepository(f)
				if phase == "batch" {
					driver.secretReadError = failure
				} else {
					// The strict batch first encounters the real revoked secret. Its
					// next (single-account) secret read fails at the driver boundary.
					driver.beforeSecretRead = func() {
						driver.beforeSecretRead = func() { driver.secretReadError = failure }
					}
				}
				svc := service.NewAccountUsageService(repo, &recoveryUsageLogRepo{}, nil, nil, nil, nil, nil, nil, service.NewUsageCache(), nil, nil)
				usage, failures, err := svc.GetUsageBatch(f.ctx, []int64{good.ID, bad.ID}, false)
				require.ErrorIs(t, err, failure)
				require.Nil(t, usage)
				require.Nil(t, failures)
				if phase == "batch" {
					require.EqualValues(t, 1, driver.secretQueries.Load())
				} else {
					require.EqualValues(t, 2, driver.secretQueries.Load())
				}
			})
		}
	}
}
