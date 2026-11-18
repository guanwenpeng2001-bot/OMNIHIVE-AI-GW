package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

type usageCredentialReadRepo struct {
	AccountRepository
	batchErr  error
	accounts  map[int64]*Account
	read      func(context.Context, int64) (*Account, error)
	batchIDs  []int64
	singleIDs []int64
}

func (r *usageCredentialReadRepo) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	r.batchIDs = append(r.batchIDs, ids...)
	if r.batchErr != nil {
		return nil, r.batchErr
	}
	var out []*Account
	for _, id := range ids {
		if a := r.accounts[id]; a != nil {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *usageCredentialReadRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.singleIDs = append(r.singleIDs, id)
	if r.read != nil {
		return r.read(ctx, id)
	}
	if a := r.accounts[id]; a != nil {
		return a, nil
	}
	return nil, ErrAccountNotFound
}

func TestUsageCredentialFallbackMixedResults(t *testing.T) {
	for _, credentialErr := range []error{ErrCredentialSecretInvalid, ErrCredentialSecretNotFound, ErrCredentialSecretRevoked, ErrCredentialSecretConflict} {
		t.Run(fmt.Sprint(credentialErr), func(t *testing.T) {
			const canary = "usage-credential-fixture-not-for-output"
			var logged bytes.Buffer
			previousLogger, previousWriter := slog.Default(), log.Writer()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
			log.SetOutput(&logged)
			t.Cleanup(func() { slog.SetDefault(previousLogger); log.SetOutput(previousWriter) })
			passive := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": canary}, Extra: map[string]any{"passive_usage_7d_utilization": 0.2}}
			active := &Account{ID: 2, Platform: PlatformOpenRouter, Type: AccountTypeAPIKey, Status: StatusActive, Credentials: map[string]any{"api_key": canary}}
			repo := &usageCredentialReadRepo{batchErr: fmt.Errorf("%s: %w", canary, credentialErr), accounts: map[int64]*Account{1: passive, 2: active}}
			repo.read = func(_ context.Context, id int64) (*Account, error) {
				if id == 3 {
					return nil, fmt.Errorf("%s: %w", canary, credentialErr)
				}
				if id == 4 {
					return nil, ErrAccountNotFound.WithCause(sql.ErrNoRows)
				}
				return repo.accounts[id], nil
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":{"limit":100,"usage":12}}`))}}
			cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true}}}
			svc := &AccountUsageService{accountRepo: repo, usageLogRepo: &usageBatchLogRepoStub{}, cache: NewUsageCache(), openRouterGateway: &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}}
			usage, failures, err := svc.GetUsageBatch(context.Background(), []int64{1, 3, 2, 3, 4, 0, -1}, true)
			require.NoError(t, err)
			require.Len(t, usage, 2)
			require.Equal(t, "passive", usage[1].Source)
			require.Equal(t, "active", usage[2].Source)
			require.Equal(t, credentialErr.Error(), failures[3])
			require.Equal(t, ErrAccountNotFound.Error(), failures[4])
			require.Equal(t, []int64{1, 3, 2, 4}, repo.batchIDs)
			require.Equal(t, repo.batchIDs, repo.singleIDs)
			require.Len(t, upstream.requests, 1)
			if upstream.lastReq.Header.Get("Authorization") != "Bearer "+canary {
				t.Fatal("active usage did not receive resolved credentials")
			}
			body, err := json.Marshal(struct {
				Usage  map[int64]*UsageInfo
				Errors map[int64]string
			}{usage, failures})
			require.NoError(t, err)
			if strings.Contains(string(body), canary) {
				t.Fatal("credential material leaked into batch result")
			}
			if strings.Contains(logged.String(), canary) {
				t.Fatal("credential material leaked into logs")
			}
		})
	}
}

func TestUsageCredentialFallbackInfrastructureErrors(t *testing.T) {
	databaseErr := errors.New("fixture database unavailable")
	for _, phase := range []string{"batch", "single"} {
		for _, failure := range []error{databaseErr, context.Canceled, context.DeadlineExceeded, errors.Join(ErrCredentialSecretInvalid, databaseErr), ErrCredentialSecretInvalid.WithCause(databaseErr), errors.Join(ErrAccountNotFound, databaseErr), ErrAccountNotFound.WithCause(databaseErr), ErrAccountNotFound.WithCause(context.Canceled)} {
			t.Run(phase+"/"+fmt.Sprint(failure), func(t *testing.T) {
				repo := &usageCredentialReadRepo{batchErr: failure}
				if phase == "single" {
					repo.batchErr = ErrCredentialSecretInvalid
					repo.read = func(_ context.Context, id int64) (*Account, error) {
						if id == 1 {
							return &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}, nil
						}
						return nil, failure
					}
				}
				svc := &AccountUsageService{accountRepo: repo}
				usage, failures, err := svc.GetUsageBatch(context.Background(), []int64{1, 2}, false)
				require.ErrorIs(t, err, failure)
				require.Nil(t, usage)
				require.Nil(t, failures)
				if phase == "batch" {
					require.Empty(t, repo.singleIDs)
				} else {
					require.Equal(t, []int64{1, 2}, repo.singleIDs)
				}
			})
		}
	}
}

func TestUsageCredentialFallbackBoundsAndCancellation(t *testing.T) {
	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		repo := &usageCredentialReadRepo{batchErr: ErrCredentialSecretInvalid}
		_, _, err := (&AccountUsageService{accountRepo: repo}).GetUsageBatch(ctx, []int64{1}, false)
		require.ErrorIs(t, err, context.Canceled)
		require.Empty(t, repo.batchIDs)
		require.Empty(t, repo.singleIDs)
	})
	t.Run("no credential error no fallback", func(t *testing.T) {
		repo := &usageCredentialReadRepo{}
		_, failures, err := (&AccountUsageService{accountRepo: repo}).GetUsageBatch(context.Background(), []int64{1, 1}, false)
		require.NoError(t, err)
		require.Equal(t, ErrAccountNotFound.Error(), failures[1])
		require.Empty(t, repo.singleIDs)
	})
	t.Run("hard query bound", func(t *testing.T) {
		repo := &usageCredentialReadRepo{batchErr: ErrCredentialSecretInvalid}
		ids := make([]int64, 1001)
		for i := range ids {
			ids[i] = int64(i + 1)
		}
		_, _, err := (&AccountUsageService{accountRepo: repo}).GetUsageBatch(context.Background(), ids, false)
		require.ErrorIs(t, err, ErrCredentialSecretInvalid)
		require.Empty(t, repo.singleIDs)
	})
	t.Run("all unavailable at query bound", func(t *testing.T) {
		repo := &usageCredentialReadRepo{batchErr: ErrCredentialSecretRevoked, read: func(context.Context, int64) (*Account, error) { return nil, ErrCredentialSecretRevoked }}
		ids := make([]int64, 1000)
		for i := range ids {
			ids[i] = int64(i + 1)
		}
		usage, failures, err := (&AccountUsageService{accountRepo: repo}).GetUsageBatch(context.Background(), ids, false)
		require.NoError(t, err)
		require.Empty(t, usage)
		require.Len(t, failures, len(ids))
		require.Equal(t, ids, repo.singleIDs)
	})
	t.Run("cancel during read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		repo := &usageCredentialReadRepo{batchErr: ErrCredentialSecretInvalid}
		repo.read = func(context.Context, int64) (*Account, error) { cancel(); return nil, ErrCredentialSecretRevoked }
		usage, failures, err := (&AccountUsageService{accountRepo: repo}).GetUsageBatch(ctx, []int64{1, 2}, false)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, usage)
		require.Nil(t, failures)
		require.Equal(t, []int64{1}, repo.singleIDs)
	})
	t.Run("overall deadline", func(t *testing.T) {
		setUsageBatchTestTimeouts(t, time.Second, 30*time.Millisecond)
		repo := &usageCredentialReadRepo{batchErr: ErrCredentialSecretInvalid}
		repo.read = func(ctx context.Context, _ int64) (*Account, error) { <-ctx.Done(); return nil, ctx.Err() }
		started := time.Now()
		usage, failures, err := (&AccountUsageService{accountRepo: repo}).GetUsageBatch(context.Background(), []int64{1, 2}, false)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Nil(t, usage)
		require.Nil(t, failures)
		require.Equal(t, []int64{1}, repo.singleIDs)
		require.Less(t, time.Since(started), time.Second)
	})
}
