package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type cnBalanceResponseUpstream struct {
	calls      int
	request    *http.Request
	statusCode int
	body       string
}

func (u *cnBalanceResponseUpstream) Do(
	req *http.Request,
	_ string,
	_ int64,
	_ int,
) (*http.Response, error) {
	u.calls++
	u.request = req
	return &http.Response{
		StatusCode: u.statusCode,
		Body:       io.NopCloser(strings.NewReader(u.body)),
		Header:     make(http.Header),
	}, nil
}

func (u *cnBalanceResponseUpstream) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type cnBalanceProbeRepo struct {
	AccountRepository
	account     *Account
	extraWrites []map[string]any
	persistErr  error
}

func (r *cnBalanceProbeRepo) GetByID(_ context.Context, _ int64) (*Account, error) {
	return r.account, nil
}

func (r *cnBalanceProbeRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.extraWrites = append(r.extraWrites, updates)
	return nil
}

func newDeepSeekBalanceProbeAccount() *Account {
	return &Account{
		ID:       42,
		Platform: PlatformDeepseek,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"account_mode": AccountModePayG,
			"api_key":      "sk-test",
			"base_url":     "https://api.deepseek.com",
		},
	}
}

func TestCNProviderBalanceService_DeepSeekInvalidBalancePayloadDoesNotBecomeZero(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{
			name:      "missing balance infos",
			body:      `{"data":{"models":["deepseek-v4-flash"]}}`,
			wantError: "missing balance_infos",
		},
		{
			name:      "empty balance infos",
			body:      `{"is_available":true,"balance_infos":[]}`,
			wantError: "no valid balance entries",
		},
		{
			name:      "invalid balance value",
			body:      `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"not-a-number"}]}`,
			wantError: "no valid balance entries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &cnBalanceProbeRepo{account: newDeepSeekBalanceProbeAccount()}
			upstream := &cnBalanceResponseUpstream{statusCode: http.StatusOK, body: tt.body}
			svc := NewCNProviderBalanceService(repo, nil, upstream, nil)

			result, err := svc.QueryBalance(context.Background(), repo.account.ID)

			require.NoError(t, err)
			require.Equal(t, 1, upstream.calls, "the malformed payload must reach the balance parser")
			require.Equal(t, "https://api.deepseek.com/user/balance", upstream.request.URL.String())
			require.Equal(t, "Bearer sk-test", upstream.request.Header.Get("Authorization"))
			require.NotNil(t, result)
			require.False(t, result.Success)
			require.Contains(t, result.Error, tt.wantError)
			require.Empty(t, result.Balances)
			require.Empty(t, repo.extraWrites, "invalid upstream balance payload must not persist a synthetic zero balance")
		})
	}
}

func TestCNProviderBalanceService_DeepSeekValidZeroBalanceRemainsSuccessful(t *testing.T) {
	repo := &cnBalanceProbeRepo{account: newDeepSeekBalanceProbeAccount()}
	upstream := &cnBalanceResponseUpstream{
		statusCode: http.StatusOK,
		body:       `{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"0"}]}`,
	}
	svc := NewCNProviderBalanceService(repo, nil, upstream, nil)

	result, err := svc.QueryBalance(context.Background(), repo.account.ID)

	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls, "the valid zero must come from the upstream payload")
	require.Equal(t, "https://api.deepseek.com/user/balance", upstream.request.URL.String())
	require.Equal(t, "Bearer sk-test", upstream.request.Header.Get("Authorization"))
	require.NotNil(t, result)
	require.True(t, result.Success)
	require.False(t, result.Available)
	require.Equal(t, "CNY", result.Currency)
	require.Zero(t, result.Balance)
	require.Len(t, result.Balances, 1)
	require.Len(t, repo.extraWrites, 1, "a valid upstream zero balance must still be persisted")
}

func (r *cnBalanceProbeRepo) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, end *time.Time) error {
	if r.persistErr != nil {
		return r.persistErr
	}
	if AccountRuntimeIdentityKey(expected) != AccountRuntimeIdentityKey(r.account) {
		return ErrAccountRuntimeIdentityChanged
	}
	return r.UpdateExtra(ctx, expected.ID, updates)
}

func TestCNProviderBalanceService_NativePersistenceIdentity(t *testing.T) {
	for _, provider := range []string{PlatformDeepseek, PlatformKimi} {
		for name, persistErr := range map[string]error{
			"success":              nil,
			"identity":             ErrAccountRuntimeIdentityChanged,
			"wrapped identity":     fmt.Errorf("private details: %w", ErrAccountRuntimeIdentityChanged),
			"ordinary store error": errors.New("store unavailable"),
		} {
			t.Run(provider+"/"+name, func(t *testing.T) {
				account := newDeepSeekBalanceProbeAccount()
				account.Platform = provider
				body := `{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"98"},{"currency":"USD","total_balance":"3.5"}]}`
				wantEntries := []CNProviderBalanceEntry{{Currency: "CNY", Balance: 98}, {Currency: "USD", Balance: 3.5}}
				if provider == PlatformKimi {
					account.Credentials["base_url"] = "https://api.moonshot.cn/v1"
					body = `{"code":0,"data":{"available_balance":98}}`
					wantEntries = wantEntries[:1]
				}
				repo := &cnBalanceProbeRepo{account: account, persistErr: persistErr}
				upstream := &cnBalanceResponseUpstream{statusCode: http.StatusOK, body: body}
				result, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryBalance(context.Background(), account.ID)
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, StatusActive, account.Status)
				if errors.Is(persistErr, ErrAccountRuntimeIdentityChanged) {
					require.ErrorIs(t, err, ErrAccountRuntimeIdentityChanged)
					require.Same(t, persistErr, err)
					require.Nil(t, result, "obsolete observations must not escape the native service")
					require.Empty(t, repo.extraWrites)
					return
				}
				require.NoError(t, err)
				require.True(t, result.Success)
				require.Equal(t, persistErr == nil, result.Persisted)
				require.Equal(t, 98.0, result.Balance)
				require.Equal(t, "CNY", result.Currency)
				require.Equal(t, wantEntries, result.Balances)
				require.Equal(t, provider == PlatformKimi, result.Available)
				if persistErr != nil {
					require.Empty(t, repo.extraWrites)
				} else {
					require.Len(t, repo.extraWrites, 1)
				}
			})
		}
	}
}
