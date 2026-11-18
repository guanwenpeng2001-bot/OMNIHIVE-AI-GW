package service

import (
	"context"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

type commandCodeRecoveryRepo struct {
	AccountRepository
	permanent      int
	temporaryUntil time.Time
	rateUntil      time.Time
}

func commandCodeRecoveryAccount() *Account {
	return &Account{ID: 905, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fake-commandcode-key"}}
}

func (repo *commandCodeRecoveryRepo) SetError(context.Context, int64, string) error {
	repo.permanent++
	return nil
}
func (repo *commandCodeRecoveryRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, _ string) error {
	repo.temporaryUntil = until
	return nil
}
func (repo *commandCodeRecoveryRepo) SetRateLimited(_ context.Context, _ int64, until time.Time) error {
	repo.rateUntil = until
	return nil
}

func TestCommandCodeRecoverableResponses(t *testing.T) {
	for _, fixture := range []struct {
		name                 string
		status               int
		body                 string
		permanent, temporary bool
	}{
		{"invalid credentials", 401, `{"error":{"code":"INVALID_API_KEY"}}`, true, false},
		{"revoked credentials", 403, `{"error":{"code":"API_KEY_REVOKED"}}`, true, false},
		{"unknown authentication", 401, `<html>unauthorized</html>`, false, false},
		{"ordinary payment", 402, `{"error":{"message":"insufficient balance"}}`, false, true},
		{"edge challenge", 403, `<html>Cloudflare Just a moment</html>`, false, false},
		{"plan", 403, `{"error":{"code":"MODEL_NOT_IN_PLAN"}}`, false, false},
		{"unknown permission", 403, `{"error":{"message":"permission denied"}}`, false, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			repo := &commandCodeRecoveryRepo{}
			rates := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			gateway := &OpenAIGatewayService{rateLimitService: rates}
			before := time.Now()
			disabled := gateway.handleOpenAIAccountUpstreamError(context.Background(), commandCodeRecoveryAccount(), fixture.status, http.Header{}, []byte(fixture.body))
			require.Equal(t, fixture.permanent || fixture.temporary, disabled)
			require.Equal(t, fixture.permanent, repo.permanent == 1)
			if fixture.temporary {
				require.WithinDuration(t, before.Add(20*time.Minute), repo.temporaryUntil, time.Second)
				account := commandCodeRecoveryAccount()
				account.TempUnschedulableUntil = &repo.temporaryUntil
				require.False(t, account.IsSchedulable())
				elapsed := time.Now().Add(-time.Second)
				account.TempUnschedulableUntil = &elapsed
				require.True(t, account.IsSchedulable())
			} else {
				require.True(t, repo.temporaryUntil.IsZero())
			}
		})
	}
}

func TestCommandCodeExhaustedWindowsUseLatestReset(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	account := commandCodeRecoveryAccount()
	account.Extra = map[string]any{
		"commandcode_usage_supported":     true,
		"commandcode_5h_used_percent":     100.0,
		"commandcode_5h_reset_at":         now.Add(time.Hour).Format(time.RFC3339),
		"commandcode_weekly_used_percent": 100.0,
		"commandcode_weekly_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
	}
	repo := &commandCodeRecoveryRepo{}
	rates := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	rates.HandleUpstreamError(context.Background(), account, 429, http.Header{}, nil)
	require.Equal(t, now.Add(24*time.Hour), repo.rateUntil)
	account.Extra["commandcode_weekly_used_percent"] = 20.0
	rates.HandleUpstreamError(context.Background(), account, 429, http.Header{}, nil)
	require.Equal(t, now.Add(time.Hour), repo.rateUntil)
}

func TestCommandCodeFailoverPreservesExplicitTemporaryRules(t *testing.T) {
	for _, body := range []string{`{"error":{"code":"MODEL_NOT_IN_PLAN"}}`, `<html>Cloudflare Just a moment</html>`} {
		t.Run(body, func(t *testing.T) {
			account := commandCodeRecoveryAccount()
			account.Credentials["pool_mode"] = true
			account.Credentials["temp_unschedulable_enabled"] = true
			account.Credentials["temp_unschedulable_rules"] = []any{map[string]any{"error_code": float64(403), "keywords": []any{"MODEL_NOT_IN_PLAN", "Cloudflare"}, "duration_minutes": float64(3)}}
			repo := &commandCodeRecoveryRepo{}
			gateway := &OpenAIGatewayService{rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
			failure := gateway.failoverOpenAIUpstreamHTTPError(context.Background(), nil, account, &http.Response{StatusCode: 403, Header: http.Header{}}, []byte(body), body, "")
			require.NotNil(t, failure)
			require.WithinDuration(t, time.Now().Add(3*time.Minute), repo.temporaryUntil, time.Second)
			require.False(t, failure.RequestScopedTransient)
			require.Zero(t, repo.permanent)
		})
	}
}

func TestCommandCodeWindowSchedulingAndRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, fixture := range []struct {
		name      string
		supported any
		used      any
		reset     any
		window    bool
	}{
		{"valid", true, 100.0, now.Add(time.Hour).Format(time.RFC3339), true},
		{"expired", true, 100.0, now.Add(-time.Hour).Format(time.RFC3339), false},
		{"unsupported", false, 100.0, now.Add(time.Hour).Format(time.RFC3339), false},
		{"unknown support", nil, 100.0, now.Add(time.Hour).Format(time.RFC3339), false},
		{"missing reset", true, 100.0, nil, false},
		{"invalid reset", true, 100.0, "bad", false},
		{"unknown usage", true, nil, now.Add(time.Hour).Format(time.RFC3339), false},
		{"nonfinite usage", true, math.Inf(1), now.Add(time.Hour).Format(time.RFC3339), false},
		{"not exhausted", true, 20.0, now.Add(time.Hour).Format(time.RFC3339), false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			account := commandCodeRecoveryAccount()
			account.Extra = map[string]any{"commandcode_usage_supported": fixture.supported, "commandcode_5h_used_percent": fixture.used, "commandcode_5h_reset_at": fixture.reset}
			repo := &commandCodeRecoveryRepo{}
			rates := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			require.False(t, rates.HandleUpstreamError(context.Background(), account, 429, http.Header{"Anthropic-Ratelimit-Unified-Reset": []string{"9999999999"}}, []byte(`{"error":{"message":"rate limited"}}`)))
			decision := EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformCommandCode: 90}, now)
			require.Equal(t, fixture.window, decision.ShouldPause)
			if fixture.window {
				require.Equal(t, now.Add(time.Hour), repo.rateUntil)
				require.Equal(t, repo.rateUntil, *decision.Until)
				require.False(t, EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformCommandCode: 90}, repo.rateUntil).ShouldPause)
			} else {
				require.WithinDuration(t, time.Now().Add(time.Duration(defaultRateLimit429CooldownSeconds)*time.Second), repo.rateUntil, time.Second)
			}
			require.Zero(t, repo.permanent)
		})
	}
}

func TestCommandCodeErrorPolicyPrecedence(t *testing.T) {
	for _, mode := range []string{"pool", "excluded code", "temporary rule"} {
		t.Run(mode, func(t *testing.T) {
			account := commandCodeRecoveryAccount()
			switch mode {
			case "pool":
				account.Credentials["pool_mode"] = true
			case "excluded code":
				account.Credentials["custom_error_codes_enabled"] = true
				account.Credentials["custom_error_codes"] = []any{float64(500)}
			case "temporary rule":
				account.Credentials["temp_unschedulable_enabled"] = true
				account.Credentials["temp_unschedulable_rules"] = []any{map[string]any{"error_code": float64(403), "keywords": []any{"MODEL_NOT_IN_PLAN"}, "duration_minutes": float64(3)}}
			}
			repo := &commandCodeRecoveryRepo{}
			gateway := &OpenAIGatewayService{rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
			status, body := 402, `{"error":{"message":"insufficient balance"}}`
			if mode == "temporary rule" {
				status, body = 403, `{"error":{"code":"MODEL_NOT_IN_PLAN"}}`
			}
			result := gateway.handleOpenAIAccountUpstreamError(context.Background(), account, status, http.Header{}, []byte(body))
			require.Equal(t, mode == "temporary rule", result)
			require.Zero(t, repo.permanent)
			if mode == "temporary rule" {
				require.WithinDuration(t, time.Now().Add(3*time.Minute), repo.temporaryUntil, time.Second)
			} else {
				require.True(t, repo.temporaryUntil.IsZero())
			}
		})
	}
}
