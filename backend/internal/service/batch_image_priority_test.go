//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// d2c2b46c9 orders by ascending priority, then ascending ID. Exercise every
// repository permutation so ties cannot accidentally depend on query order.
func TestBatchImagePublicService_AccountPriorityPermutations(t *testing.T) {
	for _, tt := range []struct {
		name       string
		priorities [3]int
		wantID     int64
	}{
		{"ascending", [3]int{1, 5, 9}, 101},
		{"descending", [3]int{9, 5, 1}, 303},
		{"equal", [3]int{5, 5, 5}, 101},
		{"preferred tie", [3]int{9, 1, 1}, 202},
		{"zero", [3]int{9, 0, 5}, 202},
	} {
		for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
			t.Run(fmt.Sprintf("%s/%v", tt.name, order), func(t *testing.T) {
				svc, _, _, _, _ := newTestBatchImagePublicService(true)
				accounts := make([]Account, 0, 3)
				for _, index := range order {
					account := testBatchImageAccount(int64(101*(index+1)), AccountTypeAPIKey)
					account.Priority = tt.priorities[index]
					accounts = append(accounts, account)
				}
				svc.AccountRepo = &publicBatchImageAccountRepo{accounts: accounts}
				svc.ProviderRegistry = NewBatchImageProviderRegistry(NewGeminiAPIBatchImageProvider(nil))
				provider, account, err := svc.selectProviderAndAccount(context.Background(), testBatchImageOwner(), BatchImageProviderGeminiAPI, "gemini-2.5-flash-image")
				require.NoError(t, err)
				require.Equal(t, BatchImageProviderGeminiAPI, provider.Name())
				require.Equal(t, tt.wantID, account.ID)
			})
		}
	}
}

func TestBatchImagePublicService_AccountPriorityPreservesEligibility(t *testing.T) {
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	for _, tt := range []struct {
		name    string
		disable func(*Account)
	}{
		{"unschedulable", func(a *Account) { a.Schedulable = false }},
		{"inactive", func(a *Account) { a.Status = "disabled" }},
		{"expired", func(a *Account) { a.AutoPauseOnExpired, a.ExpiresAt = true, &past }},
		{"overloaded", func(a *Account) { a.OverloadUntil = &future }},
		{"rate limited", func(a *Account) { a.RateLimitResetAt = &future }},
		{"temporarily unavailable", func(a *Account) { a.TempUnschedulableUntil = &future }},
		{"quota exceeded", func(a *Account) { a.Extra = map[string]any{"quota_limit": 1.0, "quota_used": 1.0} }},
		{"unsupported model", func(a *Account) { a.Credentials["model_mapping"] = map[string]any{"other-model": "other-model"} }},
		{"unsupported account type", func(a *Account) { a.Type = AccountTypeOAuth }},
		{"missing credential", func(a *Account) { delete(a.Credentials, "api_key") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _, _, _ := newTestBatchImagePublicService(true)
			preferred := testBatchImageAccount(101, AccountTypeAPIKey)
			preferred.Priority = 0
			tt.disable(&preferred)
			fallback := testBatchImageAccount(202, AccountTypeAPIKey)
			repo := &publicBatchImageAccountRepo{accounts: []Account{preferred, fallback}}
			svc.AccountRepo = repo
			// Use the real provider's qualification check; the public-service fake
			// deliberately accepts all account types and credentials.
			svc.ProviderRegistry = NewBatchImageProviderRegistry(NewGeminiAPIBatchImageProvider(nil))
			_, account, err := svc.selectProviderAndAccount(context.Background(), testBatchImageOwner(), BatchImageProviderGeminiAPI, "gemini-2.5-flash-image")
			require.NoError(t, err)
			require.Equal(t, fallback.ID, account.ID)

			repo.accounts = []Account{preferred}
			provider, account, err := svc.selectProviderAndAccount(context.Background(), testBatchImageOwner(), BatchImageProviderGeminiAPI, "gemini-2.5-flash-image")
			require.ErrorIs(t, err, ErrBatchImageNoAccountAvailable)
			require.Nil(t, provider)
			require.Nil(t, account)
		})
	}
}
