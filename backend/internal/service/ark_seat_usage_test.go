package service

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// Synthetic fixtures follow the public explorer and BytePlus document schemas.
const arkSeatFixture = `{"Result":{"SeatID":"seat-1","ShortTermUsage":42.5,"WeeklyUsage":18.3,"MonthlyUsage":166.7,"ShortTermResetMilestone":1785492000123,"WeeklyResetMilestone":1785686400000,"MonthlyResetMilestone":1785513600000}}`

func byteplusUsageAccount() *Account {
	return &Account{ID: 17, Platform: "byteplus", Type: AccountTypeAPIKey, Credentials: map[string]any{
		"account_mode": "coding", "base_url": "https://ark.ap-southeast.bytepluses.com/api/coding/v3", "api_key": "inference-secret", "billing_plan": "team",
		"byteplus_access_key_id": "test-access", "byteplus_secret_access_key": "test-secret", "byteplus_seat_id": "seat-1",
	}}
}

func arkTeamAccount(provider string) *Account {
	if provider == "byteplus" {
		return byteplusUsageAccount()
	}
	account := volcengineUsageAccount()
	account.Credentials["volcengine_plan_edition"], account.Credentials["volcengine_seat_id"] = "team", "seat-1"
	return account
}

func TestArkSeatParser(test *testing.T) {
	tiers, status := parseArkSeatUsage([]byte(arkSeatFixture), "seat-1")
	require.Equal(test, "ok", status)
	require.Len(test, tiers, 3)
	require.Equal(test, 166.7, tiers[2].UsedPercent)
	require.Equal(test, time.UnixMilli(1785492000123).UTC().Format(time.RFC3339Nano), tiers[0].ResetAt)
	for name, fields := range map[string]string{
		"nested":      `"SeatInfoUsage":{"UserID":756,"ShortTermUsage":0,"WeeklyUsage":5,"MonthlyUsage":100}`,
		"equal merge": `"UserID":"756","ShortTermUsage":0,"WeeklyUsage":5.0,"WeeklyResetMilestone":1785686400000,"SeatInfoUsage":{"UserID":756,"ShortTermUsage":0.0,"WeeklyUsage":5,"MonthlyUsage":100}`,
		"sentinel":    `"ShortTermUsage":0,"ShortTermResetMilestone":-1,"WeeklyUsage":5,"WeeklyResetMilestone":null,"MonthlyUsage":100,"MonthlyResetMilestone":0`,
	} {
		test.Run(name, func(test *testing.T) {
			tiers, status := parseArkSeatUsage([]byte(`{"Result":{`+fields+`}}`), "seat-1")
			require.Equal(test, "ok", status)
			require.Len(test, tiers, 3)
			require.Empty(test, tiers[0].ResetAt)
			require.Empty(test, tiers[2].ResetAt)
			if name == "equal merge" {
				require.NotEmpty(test, tiers[1].ResetAt)
			} else {
				require.Empty(test, tiers[1].ResetAt)
			}
		})
	}
	for name, fields := range map[string]string{
		"seat type":         `"SeatID":1,"ShortTermUsage":1`,
		"seat null":         `"SeatID":null,"ShortTermUsage":1`,
		"metadata conflict": `"ShortTermUsage":1,"ProjectName":"a","SeatInfoUsage":{"ProjectName":"b"}`,
		"large exponent":    `"ShortTermUsage":1e-999999999`,
		"empty":             ``, "legacy": `"QuotaUsage":[{"Level":"session","Percent":2,"ResetTimestamp":-1}]`,
		"conflict":              `"ShortTermUsage":1,"SeatInfoUsage":{"ShortTermUsage":2}`,
		"reset conflict":        `"ShortTermUsage":1,"WeeklyResetMilestone":123,"SeatInfoUsage":{"WeeklyResetMilestone":124}`,
		"wrong seat bad window": `"SeatID":"other","ShortTermUsage":-1`,
		"wrong seat bad reset":  `"SeatID":"other","ShortTermUsage":1,"ShortTermResetMilestone":"bad"`,
		"wrong seat conflict":   `"SeatID":"other","ShortTermUsage":1,"SeatInfoUsage":{"ShortTermUsage":2}`,
		"wrong seat no windows": `"SeatID":"other"`,
		"negative":              `"ShortTermUsage":-1`, "null usage": `"ShortTermUsage":null`,
		"string": `"ShortTermUsage":"3"`, "overflow": `"ShortTermUsage":1e999`,
		"reset fractional":        `"ShortTermUsage":3,"ShortTermResetMilestone":1.2`,
		"reset int64 overflow":    `"ShortTermUsage":3,"ShortTermResetMilestone":9223372036854775808`,
		"reset negative overflow": `"ShortTermUsage":3,"ShortTermResetMilestone":-9223372036854775809`,
		"reset calendar overflow": `"ShortTermUsage":3,"ShortTermResetMilestone":9223372036854775807`,
		"reset string":            `"ShortTermUsage":3,"ShortTermResetMilestone":"123"`,
		"nested array":            `"ShortTermUsage":1,"SeatInfoUsage":[]`,
	} {
		test.Run(name, func(test *testing.T) {
			tiers, status := parseArkSeatUsage([]byte(`{"Result":{`+fields+`}}`), "seat-1")
			require.Equal(test, "invalid_response", status)
			require.Empty(test, tiers)
		})
	}
	// Even small positive timestamps are milliseconds, not seconds.
	tiers, status = parseArkSeatUsage([]byte(`{"Result":{"WeeklyUsage":5,"WeeklyResetMilestone":1000}}`), "seat-1")
	require.Equal(test, "ok", status)
	require.Equal(test, "1970-01-01T00:00:01Z", tiers[0].ResetAt)
}

func TestArkSeatResponseMissingRequestedSeat(test *testing.T) {
	for _, fields := range []string{
		`"SeatID":"","ShortTermUsage":1`,
		`"SeatID":"other","ShortTermUsage":1`,
		`"ShortTermUsage":1,"SeatInfoUsage":{"SeatID":"other"}`,
	} {
		tiers, status := parseArkSeatUsage([]byte(`{"Result":{`+fields+`}}`), "seat-1")
		require.Equal(test, "no_seat", status)
		require.Empty(test, tiers)
	}
}

func TestArkSeatMissingRequestedSeatService(test *testing.T) {
	for _, provider := range []string{"byteplus", "volcengine"} {
		test.Run(provider, func(test *testing.T) {
			account := arkTeamAccount(provider)
			repo := &compatibleBalanceRepo{account: account}
			calls := 0
			upstream := compatibleBalanceHTTP{send: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				require.Equal(test, "GetSeatInfoUsage", req.URL.Query().Get("Action"))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.ReplaceAll(arkSeatFixture, "seat-1", "other")))}, nil
			}}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsageForAccount(context.Background(), account)
			require.NoError(test, err)
			require.Equal(test, "no_seat", result.Status)
			require.False(test, result.Success)
			require.False(test, result.Persisted)
			require.Empty(test, result.Tiers)
			require.Empty(test, repo.writes)
			require.Equal(test, 1, calls)
		})
	}
}

func TestArkSeatRequestAndPartialSnapshot(test *testing.T) {
	for _, provider := range []string{"byteplus", "volcengine"} {
		for _, response := range []string{arkSeatFixture, `{"Result":{"SeatInfoUsage":{"ShortTermUsage":0}}}`} {
			test.Run(provider+response, func(test *testing.T) {
				account := arkTeamAccount(provider)
				account.Extra = map[string]any{provider + "_weekly_used_percent": 90.0, provider + "_5h_reset_at": "old"}
				id := int64(4)
				account.ProxyID, account.Proxy = &id, &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
				account.Credentials[credKeyHeaderOverrideEnabled] = true
				account.Credentials[credKeyHeaderOverrides] = map[string]any{"x-secret": "inference-secret"}
				repo := &compatibleBalanceRepo{account: account}
				calls := 0
				upstream := compatibleBalanceHTTP{send: func(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
					calls++
					region, host := "cn-beijing", "ark.cn-beijing.volcengineapi.com"
					if provider == "byteplus" {
						region, host = "ap-southeast-1", "ark.ap-southeast-1.byteplusapi.com"
					}
					require.Equal(test, host, req.URL.Host)
					require.Equal(test, "/", req.URL.Path)
					require.Equal(test, "Action=GetSeatInfoUsage&Version=2024-01-01", req.URL.RawQuery)
					require.Equal(test, http.MethodPost, req.Method)
					require.Equal(test, "http://proxy.example:8080", proxy)
					body, err := io.ReadAll(req.Body)
					require.NoError(test, err)
					require.JSONEq(test, `{"SeatID":"seat-1"}`, string(body))
					require.Contains(test, req.Header.Get("Authorization"), "/"+region+"/ark/request")
					require.Contains(test, req.Header.Get("Authorization"), "SignedHeaders=host;x-content-sha256;x-date,")
					require.NotContains(test, req.Header.Get("Authorization"), "content-type")
					raw, _ := json.Marshal(req.Header)
					require.NotContains(test, string(raw), "inference-secret")
					require.True(test, HTTPUpstreamRedirectsDisabled(req.Context()))
					deadline, ok := req.Context().Deadline()
					require.True(test, ok)
					require.LessOrEqual(test, time.Until(deadline), cnQuotaUpstreamTimeout)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response))}, nil
				}}
				probeResult, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(context.Background(), account.ID)
				require.NoError(test, err)
				require.True(test, probeResult.Success)
				require.True(test, probeResult.Persisted)
				require.Equal(test, 1, calls)
				require.Equal(test, provider, probeResult.Provider)
				require.Equal(test, "coding_plan", probeResult.Source)
				require.Equal(test, "team", probeResult.PlanLevel)
				require.Len(test, repo.writes, 1)
				if response != arkSeatFixture {
					require.Nil(test, repo.writes[0][provider+"_weekly_used_percent"])
					require.Nil(test, repo.writes[0][provider+"_5h_reset_at"])
				}
			})
		}
	}
}

func TestArkSeatZeroOutboundGates(test *testing.T) {
	for _, provider := range []string{"byteplus", "volcengine"} {
		for _, scenario := range []struct {
			name, status string
			mutate       func(*Account)
		}{
			{"seat", "no_seat", func(account *Account) { delete(account.Credentials, provider+"_seat_id") }},
			{"access", "missing_credentials", func(account *Account) { delete(account.Credentials, provider+"_access_key_id") }},
			{"secret", "missing_credentials", func(account *Account) { delete(account.Credentials, provider+"_secret_access_key") }},
			{"mode", "unsupported_profile", func(account *Account) { account.Credentials["account_mode"] = "payg" }},
			{"custom", "unsupported_profile", func(account *Account) { account.Credentials["base_url"] = "https://relay.example/api/coding/v3" }},
			{"region", "unsupported_profile", func(account *Account) {
				if provider == "byteplus" {
					account.Credentials["base_url"] = "https://ark.cn-beijing.volces.com/api/coding/v3"
				} else {
					account.Credentials["base_url"] = "https://ark.ap-southeast.bytepluses.com/api/coding/v3"
				}
			}},
			{"mixed", "unsupported_profile", func(account *Account) {
				account.Credentials["api_base_urls"] = map[string]any{"anthropic": "https://relay.example/api/coding"}
			}},
			{"proxy", "upstream_error", func(account *Account) { id := int64(9); account.ProxyID = &id }},
		} {
			test.Run(provider+scenario.name, func(test *testing.T) {
				account := arkTeamAccount(provider)
				scenario.mutate(account)
				repo := &compatibleBalanceRepo{account: account}
				upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
					test.Fatal("unexpected outbound")
					return nil, nil
				}}
				probeResult, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(context.Background(), account.ID)
				require.NoError(test, err)
				require.Equal(test, scenario.status, probeResult.Status)
				require.Empty(test, repo.writes)
			})
		}
	}
	for _, plan := range []string{"personal", "", "unknown"} {
		account := byteplusUsageAccount()
		account.Credentials["billing_plan"] = plan
		upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
			test.Fatal("unexpected outbound")
			return nil, nil
		}}
		probeResult, err := NewCNProviderQuotaService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryUsage(context.Background(), account.ID)
		require.NoError(test, err)
		require.Equal(test, "no_verified_query", probeResult.Status)
	}
}

func TestArkSeatFailureAndCAS(test *testing.T) {
	for _, provider := range []string{"byteplus", "volcengine"} {
		for _, code := range []int{200, 401, 403, 302, 500} {
			account := arkTeamAccount(provider)
			repo := &compatibleBalanceRepo{account: account}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(`{"Result":{"QuotaUsage":[]}}`))}, nil
			}}
			probeResult, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(context.Background(), account.ID)
			require.NoError(test, err)
			require.False(test, probeResult.Success)
			require.Empty(test, probeResult.Tiers)
			require.Empty(test, repo.writes)
		}
		account := arkTeamAccount(provider)
		repo := &compatibleBalanceRepo{account: account, persistErr: ErrAccountRuntimeIdentityChanged}
		upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(arkSeatFixture))}, nil
		}}
		probeResult, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(context.Background(), account.ID)
		require.NoError(test, err)
		require.False(test, probeResult.Success)
		require.False(test, probeResult.Persisted)
		require.Empty(test, probeResult.Tiers)
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"example.invalid"}
		probeResult, err = NewCNProviderQuotaService(repo, nil, upstream, cfg).QueryUsage(context.Background(), account.ID)
		require.NoError(test, err)
		require.Equal(test, "url_rejected", probeResult.Status)
	}
}

func TestByteplusIdentityAndSnapshotInvalidation(test *testing.T) {
	for _, field := range []string{"byteplus_access_key_id", "byteplus_secret_access_key", "byteplus_seat_id", "billing_plan", "base_url", "account_mode"} {
		account := byteplusUsageAccount()
		before := AccountRuntimeIdentityKey(account)
		account.Credentials = maps.Clone(account.Credentials)
		account.Credentials[field] = "changed"
		require.NotEqual(test, before, AccountRuntimeIdentityKey(account))
		account.Extra = map[string]any{"byteplus_5h_used_percent": 90, "byteplus_weekly_reset_at": "old", "byteplus_monthly_used_percent": 60, "byteplus_usage_updated_at": "old", "quota_used": 42}
		ClearAccountRuntimeObservations(account)
		require.Equal(test, map[string]any{"quota_used": 42}, account.Extra)
		require.Equal(test, "inference-secret", account.Credentials["api_key"])
	}
}

func TestArkSeatInflightIdentityChangesDiscardTiers(test *testing.T) {
	for _, provider := range []string{"byteplus", "volcengine"} {
		for _, field := range []string{provider + "_secret_access_key", provider + "_seat_id", "billing_plan", "base_url"} {
			account := arkTeamAccount(provider)
			repo := &volcengineIdentityGuardRepo{compatibleBalanceRepo: compatibleBalanceRepo{account: account}, current: account}
			started, release := make(chan struct{}), make(chan struct{})
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				close(started)
				<-release
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(arkSeatFixture))}, nil
			}}
			quotaService := NewCNProviderQuotaService(repo, nil, upstream, nil)
			result := make(chan *CNProviderQuotaProbeResult, 1)
			go func() {
				probeResult, _ := quotaService.QueryUsage(context.Background(), account.ID)
				result <- probeResult
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				test.Fatal("request never started")
			}
			rotated := *account
			rotated.Credentials = maps.Clone(account.Credentials)
			rotated.Credentials[field] = "changed"
			repo.current = &rotated
			close(release)
			probeResult := <-result
			require.NotNil(test, probeResult)
			require.False(test, probeResult.Success)
			require.Empty(test, probeResult.Tiers)
			require.Empty(test, repo.writes)
		}
	}
}

func TestArkSeatRejectsUnsafeAccountAndResponse(test *testing.T) {
	for _, provider := range []string{"byteplus", "volcengine"} {
		account := arkTeamAccount(provider)
		account.Type = AccountTypeOAuth
		upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
			test.Fatal("non API key account must not send")
			return nil, nil
		}}
		probeResult, err := NewCNProviderQuotaService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryUsage(context.Background(), account.ID)
		if err == nil {
			require.False(test, probeResult.Success)
		}
		for _, body := range []string{strings.Repeat("x", cnQuotaMaxBodyBytes+1), `{"Result":{"ShortTermUsage":1,"SeatInfoUsage":{"ShortTermUsage":2}}}`, `{"ResponseMetadata":{"Error":{"Code":"AccessDenied","Message":"test-secret"}}}`} {
			account = arkTeamAccount(provider)
			repo := &compatibleBalanceRepo{account: account}
			upstream = compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			}}
			probeResult, err = NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(context.Background(), account.ID)
			require.NoError(test, err)
			require.False(test, probeResult.Success)
			require.Empty(test, repo.writes)
			require.NotContains(test, probeResult.Error, "test-secret")
		}
	}
}
