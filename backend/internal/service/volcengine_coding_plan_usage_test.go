package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Synthetic fixture of the narrow response contract, not a live capture.
const volcengineUsageFixture = `{"Result":{"QuotaUsage":[{"Level":"monthly","Percent":120,"ResetTimestamp":1784303999000},{"Level":"session","Percent":0,"ResetTimestamp":-1},{"Level":"weekly","Percent":12.5,"ResetTimestamp":1782057600}]}}`

func volcengineUsageAccount() *Account {
	return &Account{ID: 17, Platform: PlatformVolcengine, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"account_mode": "coding", "base_url": "https://ark.cn-beijing.volces.com/api/coding/v3", "api_key": "inference-secret",
		"volcengine_access_key_id": "test-access", "volcengine_secret_access_key": "test-secret",
	}}
}

func TestVolcengineSyntheticParser(t *testing.T) {
	tiers, status := parseVolcengineCodingUsage([]byte(volcengineUsageFixture))
	require.Equal(t, "ok", status)
	require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 0}, {Window: "weekly", UsedPercent: 12.5, ResetAt: time.Unix(1782057600, 0).UTC().Format(time.RFC3339)}, {Window: "monthly", UsedPercent: 120, ResetAt: time.Unix(1784303999, 0).UTC().Format(time.RFC3339)}}, tiers)
	for name, body := range map[string]string{
		"missing": `{}`, "cli": `{"viewer":{},"items":[{"periods":[{"label":"5h","percent":25}]}]}`,
		"null": `{"Result":{"QuotaUsage":null}}`, "object": `{"Result":{"QuotaUsage":{}}}`,
		"missing percent": `{"Result":{"QuotaUsage":[{"Level":"session","ResetTimestamp":-1}]}}`,
		"string percent":  `{"Result":{"QuotaUsage":[{"Level":"session","Percent":"0","ResetTimestamp":-1}]}}`,
		"overflow":        `{"Result":{"QuotaUsage":[{"Level":"session","Percent":1e999,"ResetTimestamp":-1}]}}`,
		"unknown":         `{"Result":{"QuotaUsage":[{"Level":"daily","Percent":0,"ResetTimestamp":-1}]}}`,
		"missing reset":   `{"Result":{"QuotaUsage":[{"Level":"weekly","Percent":0}]}}`,
		"invalid reset":   `{"Result":{"QuotaUsage":[{"Level":"weekly","Percent":0,"ResetTimestamp":1e99}]}}`,
		"duplicate":       `{"Result":{"QuotaUsage":[{"Level":"weekly","Percent":0,"ResetTimestamp":-1},{"Level":"weekly","Percent":50,"ResetTimestamp":-1}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			tiers, status := parseVolcengineCodingUsage([]byte(body))
			require.Equal(t, "invalid_response", status)
			require.Empty(t, tiers)
		})
	}
	tiers, status = parseVolcengineCodingUsage([]byte(`{"Result":{"QuotaUsage":[]}}`))
	require.Equal(t, "not_subscribed", status)
	require.Empty(t, tiers)
	tiers, status = parseVolcengineCodingUsage([]byte(`{"Result":{"QuotaUsage":[{"Level":"WEEKLY","Percent":0,"ResetTimestamp":0},{"Level":"future","Percent":10}]}}`))
	require.Equal(t, "ok", status)
	require.Equal(t, []CNQuotaTier{{Window: "weekly"}}, tiers)
}

func TestVolcenginePersonalRequestAndSnapshot(t *testing.T) {
	for _, binding := range []string{"default", "proxy", "group"} {
		t.Run(binding, func(t *testing.T) {
			a := volcengineUsageAccount()
			a.Extra = map[string]any{"header_overrides": map[string]any{"Authorization": "inference-secret", "X-Secret": "inference-secret"}}
			a.Credentials[credKeyHeaderOverrideEnabled] = true
			a.Credentials[credKeyHeaderOverrides] = map[string]any{"x-secret": "inference-secret"}
			inferenceHeaders := http.Header{}
			// Validate the configured override on an eligible inference profile.
			// Volcengine is currently ineligible; control-plane isolation must also
			// remain safe if inference override eligibility is extended later.
			inferenceAccount := *a
			inferenceAccount.Platform = PlatformOpenAI
			inferenceAccount.ApplyHeaderOverrides(inferenceHeaders)
			require.Equal(t, "inference-secret", getHeaderRaw(inferenceHeaders, "x-secret"))
			id := int64(4)
			wantProxy := ""
			if binding == "proxy" {
				a.ProxyID, a.Proxy = &id, &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
				wantProxy = "http://proxy.example:8080"
			}
			if binding == "group" {
				a.ProxyGroupID = &id
				a.egress = &accountEgress{target: EgressTarget{Resolved: true, URL: "http://proxy.example:8080", Source: EgressSourceGroupBinding}}
				wantProxy = "http://proxy.example:8080"
			}
			repo := &compatibleBalanceRepo{account: a}
			calls := 0
			upstream := compatibleBalanceHTTP{send: func(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
				calls++
				require.Equal(t, "POST", req.Method)
				require.Equal(t, "https://open.volcengineapi.com/?Action=GetCodingPlanUsage&Version=2024-01-01", req.URL.String())
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, `{}`, string(body))
				require.Equal(t, "application/json", req.Header.Get("Content-Type"))
				require.Contains(t, req.Header.Get("Authorization"), "Credential=test-access/")
				require.NotContains(t, req.Header, "X-Secret")
				require.Empty(t, getHeaderRaw(req.Header, "x-secret"))
				require.NotContains(t, req.Header.Get("Authorization"), "inference-secret")
				require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
				deadline, ok := req.Context().Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), cnQuotaUpstreamTimeout)
				require.Equal(t, wantProxy, proxy)
				require.Equal(t, a.ID, accountID)
				require.Equal(t, 1, concurrency)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(volcengineUsageFixture))}, nil
			}}
			r, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(volcengineQuotaTestContext(t), a.ID)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.True(t, r.Success)
			require.True(t, r.Persisted)
			require.Equal(t, "ok", r.Status)
			require.Equal(t, "personal", r.PlanLevel)
			require.Len(t, repo.writes, 1)
			require.Equal(t, 120.0, repo.writes[0]["volcengine_monthly_used_percent"])
			require.Nil(t, repo.writes[0]["volcengine_5h_reset_at"])
		})
	}
}

func TestVolcengineNoRequestBoundaries(t *testing.T) {
	for _, scenario := range []struct {
		name, status string
		mutate       func(*Account)
	}{
		{"missing access", "missing_credentials", func(a *Account) { delete(a.Credentials, "volcengine_access_key_id") }},
		{"missing secret", "missing_credentials", func(a *Account) { delete(a.Credentials, "volcengine_secret_access_key") }},
		{"payg", "unsupported_profile", func(a *Account) { a.Credentials["account_mode"] = "payg" }},
		{"agent", "no_verified_query", func(a *Account) { a.Credentials["base_url"] = "https://ark.cn-beijing.volces.com/api/plan/v3" }},
		{"custom", "unsupported_profile", func(a *Account) { a.Credentials["base_url"] = "https://relay.example/api/coding/v3" }},
		{"mixed", "unsupported_profile", func(a *Account) {
			a.Credentials["api_base_urls"] = map[string]any{"anthropic": "https://relay.example/api/coding"}
		}},
		{"byteplus", "no_verified_query", func(a *Account) { a.Platform = "byteplus" }},
		{"foreign root", "unsupported_profile", func(a *Account) { a.Credentials["base_url"] = "https://ark.ap-southeast.bytepluses.com/api/coding/v3" }},
		{"bad edition", "unsupported_profile", func(a *Account) { a.Credentials["volcengine_plan_edition"] = "agent" }},
		{"header injection", "invalid_credentials", func(a *Account) { a.Credentials["volcengine_access_key_id"] = "evil\r\nheader" }},
		{"unresolved proxy", "upstream_error", func(a *Account) { id := int64(9); a.ProxyID = &id }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a := volcengineUsageAccount()
			scenario.mutate(a)
			repo := &compatibleBalanceRepo{account: a}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				t.Fatal("unexpected upstream request")
				return nil, nil
			}}
			r, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(volcengineQuotaTestContext(t), a.ID)
			require.NoError(t, err)
			require.Equal(t, scenario.status, r.Status)
			require.False(t, r.Success)
			require.Empty(t, repo.writes)
		})
	}
}

func TestVolcengineFailurePreservesSnapshotAndSecrets(t *testing.T) {
	for _, scenario := range []struct {
		name, body, status string
		http               int
		team, invalid      bool
	}{

		{"permission", `{"ResponseMetadata":{"Error":{"Code":"AccessDenied","Message":"test-secret"}}}`, "permission_denied", 200, false, false},
		{"signature", `{"Error":{"Code":"SignatureDoesNotMatch","Message":"inference-secret"}}`, "invalid_credentials", 200, false, true},
		{"authentication", `{"Error":{"Code":"AuthFailure"}}`, "invalid_credentials", 200, false, true},
		{"403", `test-secret`, "permission_denied", 403, false, false},
		{"401", `test-secret`, "invalid_credentials", 401, false, true},
		{"429", `test-secret`, "upstream_error", 429, false, false},
		{"503", `test-secret`, "upstream_error", 503, false, false},
		{"redirect", `test-secret`, "upstream_error", 302, false, false},
		{"bad body", `test-secret`, "invalid_response", 200, false, false},
		{"missing field", `{"Result":{}}`, "invalid_response", 200, false, false},
		{"empty quota", `{"Result":{"QuotaUsage":[]}}`, "not_subscribed", 200, false, false},
		{"oversized", strings.Repeat("x", cnQuotaMaxBodyBytes+1), "invalid_response", 200, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a := volcengineUsageAccount()
			a.Extra = map[string]any{"volcengine_5h_used_percent": 67.0}
			if scenario.team {
				a.Credentials["volcengine_plan_edition"] = "team"
			}
			repo := &compatibleBalanceRepo{account: a}
			calls := 0
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: scenario.http, Body: io.NopCloser(strings.NewReader(scenario.body))}, nil
			}}
			r, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(volcengineQuotaTestContext(t), a.ID)
			require.NoError(t, err)
			require.Equal(t, scenario.status, r.Status)
			require.Equal(t, !scenario.invalid, r.CredentialValid)
			require.False(t, r.Success)
			require.False(t, r.Persisted)
			require.Empty(t, r.Tiers)
			require.Empty(t, repo.writes)
			require.Equal(t, 1, calls)
			require.Equal(t, 67.0, a.Extra["volcengine_5h_used_percent"])
			data, err := json.Marshal(r)
			require.NoError(t, err)
			require.NotContains(t, string(data), "test-secret")
			require.NotContains(t, string(data), "inference-secret")
		})
	}
}

func TestVolcengineTransportAndPolicy(t *testing.T) {
	a := volcengineUsageAccount()
	repo := &compatibleBalanceRepo{account: a}
	calls := 0
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		calls++
		return nil, errors.New("test-secret network detail")
	}}
	s := NewCNProviderQuotaService(repo, nil, upstream, nil)
	r, err := s.QueryUsage(volcengineQuotaTestContext(t), a.ID)
	require.NoError(t, err)
	require.Equal(t, "upstream_error", r.Status)
	require.NotContains(t, r.Error, "test-secret")
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"example.invalid"}
	s.cfg = cfg
	r, err = s.QueryUsage(volcengineQuotaTestContext(t), a.ID)
	require.NoError(t, err)
	require.Equal(t, "url_rejected", r.Status)
	require.Equal(t, 1, calls)
}

func TestVolcengineQuerySecretsLifecycle(t *testing.T) {
	a := volcengineUsageAccount()
	merged := MergePreservingSensitiveCreds(a.Credentials, map[string]any{"volcengine_plan_edition": "team"})
	require.Equal(t, "test-access", merged["volcengine_access_key_id"])
	require.Equal(t, "test-secret", merged["volcengine_secret_access_key"])
	inline, secrets := SplitCredentialSecrets(merged)
	require.NotContains(t, inline, "volcengine_access_key_id")
	require.NotContains(t, inline, "volcengine_secret_access_key")
	require.Equal(t, "team", inline["volcengine_plan_edition"])
	metadata := credentialTestMetadata()
	ciphertext, err := EncryptCredentialSecret(credentialTestEncryptor{}, metadata, secrets)
	require.NoError(t, err)
	require.NotContains(t, ciphertext, "test-secret")
	resolved, err := ResolveCredentialSecret(context.Background(), credentialTestEncryptor{}, metadata, ciphertext)
	require.NoError(t, err)
	require.Equal(t, secrets, resolved)
	rotated := MergePreservingSensitiveCreds(merged, map[string]any{"volcengine_secret_access_key": "rotated"})
	require.Equal(t, "rotated", rotated["volcengine_secret_access_key"])
	require.Equal(t, "test-access", rotated["volcengine_access_key_id"])
	cleared := MergePreservingSensitiveCreds(rotated, map[string]any{"volcengine_access_key_id": nil, "volcengine_secret_access_key": nil})
	require.Nil(t, cleared["volcengine_access_key_id"])
	require.Nil(t, cleared["volcengine_secret_access_key"])
	require.Equal(t, "inference-secret", cleared["api_key"])
}

func volcengineQuotaTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestVolcengineStaleIdentityCannotPublishQuota(t *testing.T) {
	a := volcengineUsageAccount()
	repo := &compatibleBalanceRepo{account: a, persistErr: ErrAccountRuntimeIdentityChanged}
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(volcengineUsageFixture))}, nil
	}}
	r, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(volcengineQuotaTestContext(t), a.ID)
	require.NoError(t, err)
	require.False(t, r.Success)
	require.False(t, r.Persisted)
	require.Empty(t, r.Tiers)
	require.Equal(t, "upstream_error", r.Status)
}

func TestVolcengineIdentityChangesClearOnlyRuntimeObservations(t *testing.T) {
	for _, field := range []string{"volcengine_access_key_id", "volcengine_secret_access_key", "volcengine_plan_edition", "volcengine_seat_id", "base_url", "account_mode"} {
		t.Run(field, func(t *testing.T) {
			a := volcengineUsageAccount()
			before := AccountRuntimeIdentityKey(a)
			a.Credentials[field] = "changed"
			require.NotEqual(t, before, AccountRuntimeIdentityKey(a))
			a.Extra = map[string]any{"volcengine_5h_used_percent": 90.0, "volcengine_weekly_reset_at": "2026-10-04T00:00:00Z", "volcengine_monthly_used_percent": 80.0, "volcengine_usage_updated_at": "old", "quota_used": 42.0}
			ClearAccountRuntimeObservations(a)
			require.Equal(t, map[string]any{"quota_used": 42.0}, a.Extra)
			require.Equal(t, "inference-secret", a.Credentials["api_key"])
		})
	}
}

type volcengineIdentityGuardRepo struct {
	compatibleBalanceRepo
	current *Account
}

func (r *volcengineIdentityGuardRepo) UpdateAccountRuntimeSnapshot(_ context.Context, expected *Account, updates map[string]any, _ *time.Time) error {
	if AccountRuntimeIdentityKey(expected) != AccountRuntimeIdentityKey(r.current) {
		return ErrAccountRuntimeIdentityChanged
	}
	r.writes = append(r.writes, updates)
	return nil
}

func TestVolcengineInflightRotationDoesNotWriteOrReturnOldTiers(t *testing.T) {
	a := volcengineUsageAccount()
	repo := &volcengineIdentityGuardRepo{compatibleBalanceRepo: compatibleBalanceRepo{account: a}, current: a}
	started, release := make(chan struct{}), make(chan struct{})
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(volcengineUsageFixture))}, nil
	}}
	s := NewCNProviderQuotaService(repo, nil, upstream, nil)
	result := make(chan *CNProviderQuotaProbeResult, 1)
	go func() { r, _ := s.QueryUsage(volcengineQuotaTestContext(t), a.ID); result <- r }()
	<-started
	rotated := *a
	rotated.Credentials = maps.Clone(a.Credentials)
	rotated.Credentials["volcengine_secret_access_key"] = "rotated-secret"
	repo.current = &rotated
	close(release)
	r := <-result
	require.NotNil(t, r)
	require.False(t, r.Success)
	require.Empty(t, r.Tiers)
	require.Empty(t, repo.writes)
}
