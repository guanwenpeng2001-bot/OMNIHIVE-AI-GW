package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Synthetic fixtures from the official source structures, not live captures.
// Agent reset units come from ark-cli usage documentation (milliseconds).
const agentQuotaFixture = `{"Result":{"SeatAFPUsages":[{"SeatID":"other","AFPDaily":{"Quota":1,"Used":1}},{"SeatID":"seat","PlanType":"small","AFPFiveHour":{"Quota":100,"Used":10,"ResetTime":1782057600000},"AFPDaily":{"Quota":200,"Used":50,"ResetTime":1782057600},"AFPWeekly":{"Quota":0,"Used":20},"AFPMonthly":{"Quota":1000}}]}}`
const tencentQuotaFixture = `{"Response":{"TeamId":"team","ProductType":"enterprise","PackageInfo":{"TotalQuota":"9007199254740993.001","TotalUsed":"0","CycleQuota":"100.50","CurrentCycle":0,"SharedPool":"-1"},"TokenSummary":{"CycleSeq":1,"CycleStartTime":"2026-10-01T00:00:00Z","BillingItems":[{"BillingItem":"call_count","TotalQty":123},{"BillingItem":"input","TotalQty":9007199254740993}]}}}`

func agentQuotaAccount() *Account {
	account := volcengineUsageAccount()
	account.Credentials["base_url"] = "https://ark.cn-beijing.volces.com/api/plan/v3"
	account.Credentials["volcengine_plan_edition"] = "team"
	account.Credentials["volcengine_seat_id"] = "seat"
	return account
}

func tencentQuotaAccount() *Account {
	return &Account{ID: 17, Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"account_mode": "coding", "base_url": "https://tokenhub.tencentmaas.com/plan/v3", "api_key": "inference-secret",
		"tencent_query_secret_id": "cloud-access", "tencent_query_secret_key": "cloud-secret", "tencent_query_region": "ap-shanghai", "tencent_query_team_id": "team",
	}}
}

func TestCloudQuotaOfficialFixturesAndTransport(t *testing.T) {
	for _, provider := range []string{"agent", "tencent"} {
		for _, proxyMode := range []string{"direct", "proxy", "group"} {
			t.Run(provider+"/"+proxyMode, func(t *testing.T) {
				account, fixture, host := agentQuotaAccount(), agentQuotaFixture, "ark.cn-beijing.volcengineapi.com"
				if provider == "tencent" {
					account, fixture, host = tencentQuotaAccount(), tencentQuotaFixture, "tokenhub.tencentcloudapi.com"
				}
				proxyID, wantProxy := int64(3), ""
				if proxyMode == "proxy" {
					account.ProxyID, account.Proxy = &proxyID, &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
					wantProxy = "http://proxy.example:8080"
				}
				if proxyMode == "group" {
					account.ProxyGroupID = &proxyID
					account.egress = &accountEgress{target: EgressTarget{Resolved: true, URL: "http://proxy.example:8080", Source: EgressSourceGroupBinding}}
					wantProxy = "http://proxy.example:8080"
				}
				repo := &compatibleBalanceRepo{account: account}
				calls := 0
				upstream := compatibleBalanceHTTP{send: func(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
					calls++
					require.Equal(t, wantProxy, proxy)
					require.Equal(t, host, req.URL.Host)
					require.Equal(t, "/", req.URL.Path)
					require.Equal(t, "POST", req.Method)
					require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
					deadline, exists := req.Context().Deadline()
					require.True(t, exists)
					require.LessOrEqual(t, time.Until(deadline), cnQuotaUpstreamTimeout)
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.NotContains(t, string(body)+req.Header.Get("Authorization"), "inference-secret")
					if provider == "agent" {
						require.Equal(t, `{"SeatIDs":["seat"]}`, string(body))
						require.Equal(t, "Action=GetSeatAFPUsage&Version=2024-01-01", req.URL.RawQuery)
						require.Equal(t, "application/json; charset=utf-8", req.Header.Get("Content-Type"))
					} else {
						require.Equal(t, `{"TeamId":"team"}`, string(body))
						require.Empty(t, req.URL.RawQuery)
						require.Equal(t, "ap-shanghai", req.Header.Get("X-TC-Region"))
						require.Equal(t, "DescribeTokenPlan", req.Header.Get("X-TC-Action"))
						require.Equal(t, "2026-03-22", req.Header.Get("X-TC-Version"))
						require.Contains(t, req.Header.Get("Authorization"), "/tokenhub/tc3_request")
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fixture))}, nil
				}}
				result, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(volcengineQuotaTestContext(t), account.ID)
				require.NoError(t, err)
				require.True(t, result.Success, result.Error)
				require.True(t, result.Persisted)
				require.Equal(t, 1, calls)
				if provider == "agent" {
					require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 10, ResetAt: time.UnixMilli(1782057600000).UTC().Format(time.RFC3339)}, {Window: "daily", UsedPercent: 25, ResetAt: time.UnixMilli(1782057600).UTC().Format(time.RFC3339)}}, result.Tiers)
					require.Equal(t, 25.0, repo.writes[0]["volcengine_daily_used_percent"])
					require.Nil(t, repo.writes[0]["volcengine_weekly_used_percent"])
				} else {
					require.Empty(t, result.Tiers)
					require.Empty(t, result.Credits)
					require.Equal(t, "credits", result.TokenPlan.Unit)
					require.Equal(t, "9007199254740993.001", *result.TokenPlan.PackageInfo.TotalQuota)
					require.Nil(t, result.TokenPlan.PackageInfo.ExclusiveUsed)
					require.EqualValues(t, 0, *result.TokenPlan.PackageInfo.CurrentCycle)
					require.Equal(t, "call_count", *result.TokenPlan.TokenSummary.BillingItems[0].BillingItem)
					require.Equal(t, "9007199254740993", *result.TokenPlan.TokenSummary.BillingItems[1].TotalQty)
					encoded, err := json.Marshal(result)
					require.NoError(t, err)
					require.Contains(t, string(encoded), `"token_plan"`)
					require.Contains(t, string(encoded), `"total_qty":"9007199254740993"`)
					require.NotContains(t, string(encoded), `"exclusive_used"`)
				}
			})
		}
	}
}

func TestCloudQuotaNoOutboundBoundaries(t *testing.T) {
	for _, provider := range []string{"agent", "tencent"} {
		for _, boundary := range []string{"missing_key", "missing_identity", "personal", "international", "payg", "custom", "mixed", "bad_key", "missing_region", "bad_region"} {
			t.Run(provider+"/"+boundary, func(t *testing.T) {
				account := agentQuotaAccount()
				if provider == "tencent" {
					account = tencentQuotaAccount()
				}
				switch boundary {
				case "missing_key":
					delete(account.Credentials, "volcengine_secret_access_key")
					delete(account.Credentials, "tencent_query_secret_key")
				case "missing_identity":
					delete(account.Credentials, "volcengine_seat_id")
					delete(account.Credentials, "tencent_query_team_id")
				case "personal":
					account.Credentials["volcengine_plan_edition"] = "personal"
					if provider == "tencent" {
						account.Credentials["base_url"] = "https://api.lkeap.cloud.tencent.com/plan/v3"
					}
				case "international":
					account.Credentials["base_url"] = "https://tokenhub-intl.tencentcloudmaas.com/plan/v3"
				case "payg":
					account.Credentials["account_mode"] = "payg"
				case "custom":
					account.Credentials["base_url"] = "https://custom.example/v1"
				case "mixed":
					account.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://other.example/v1"}
				case "bad_key":
					account.Credentials["volcengine_access_key_id"] = "bad\r\ninjected"
					account.Credentials["tencent_query_secret_id"] = "bad\r\ninjected"
				case "missing_region":
					if provider == "agent" {
						return
					}
					delete(account.Credentials, "tencent_query_region")
				case "bad_region":
					if provider == "agent" {
						return
					}
					account.Credentials["tencent_query_region"] = "ap-shanghai\r\ninjected"
				}
				calls := 0
				upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) { calls++; return nil, nil }}
				result, err := NewCNProviderQuotaService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryUsage(volcengineQuotaTestContext(t), account.ID)
				require.Zero(t, calls)
				if err == nil {
					require.False(t, result.Success)
				}
			})
		}
	}
}

func TestCloudQuotaFailuresNeverPublishOrInvalidateInference(t *testing.T) {
	for _, provider := range []string{"agent", "tencent"} {
		for _, failure := range []string{"permission", "auth", "redirect", "large", "wrong_identity", "malformed", "missing_fields", "stale", "region"} {
			t.Run(provider+"/"+failure, func(t *testing.T) {
				account, fixture := agentQuotaAccount(), agentQuotaFixture
				if provider == "tencent" {
					account, fixture = tencentQuotaAccount(), tencentQuotaFixture
				}
				status := 200
				repo := &compatibleBalanceRepo{account: account}
				switch failure {
				case "permission":
					status = 403
					fixture = `{"Response":{"Error":{"Code":"UnauthorizedOperation","Message":"cloud-secret"}},"ResponseMetadata":{"Error":{"Code":"AccessDenied"}}}`
				case "auth":
					status = 401
				case "redirect":
					status = 302
				case "large":
					fixture = strings.Repeat(" ", cnQuotaMaxBodyBytes+1)
				case "wrong_identity":
					fixture = strings.ReplaceAll(fixture, `"seat"`, `"wrong"`)
					fixture = strings.ReplaceAll(fixture, `"team"`, `"wrong"`)
				case "malformed":
					fixture = `{`
				case "missing_fields":
					fixture = `{"Response":{"TeamId":"team","ProductType":"enterprise"},"Result":{}}`
				case "stale":
					repo.persistErr = ErrAccountRuntimeIdentityChanged
				case "region":
					fixture = `{"Response":{"Error":{"Code":"InvalidParameter.Region"}},"ResponseMetadata":{"Error":{"Code":"InvalidParameter.Region"}}}`
				}
				calls := 0
				upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(fixture))}, nil
				}}
				service := NewCNProviderQuotaService(repo, nil, upstream, nil)
				result, err := service.QueryUsage(volcengineQuotaTestContext(t), account.ID)
				require.NoError(t, err)
				require.False(t, result.Success)
				require.False(t, result.Persisted)
				require.Nil(t, result.TokenPlan)
				require.Empty(t, result.Tiers)
				require.NotContains(t, result.Error, "cloud-secret")
				require.Equal(t, 1, calls)
				if failure == "permission" || failure == "auth" {
					fetcher := &ChannelMonitorQuotaFetcher{cnQuota: service}
					snapshot := fetcher.fetchCNQuota(volcengineQuotaTestContext(t), account, time.Now())
					require.False(t, snapshot.CredentialInvalid)
				}
			})
		}
	}
}

func TestCloudQuotaInFlightIdentityRotation(t *testing.T) {
	for _, field := range []string{"tencent_query_secret_id", "tencent_query_secret_key", "tencent_query_region", "tencent_query_team_id", "usage_query_token", "usage_query_user_id"} {
		t.Run(field, func(t *testing.T) {
			account := tencentQuotaAccount()
			repo := &volcengineIdentityGuardRepo{compatibleBalanceRepo: compatibleBalanceRepo{account: account}, current: account}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				rotated := *account
				rotated.Credentials = maps.Clone(account.Credentials)
				rotated.Credentials[field] = "changed"
				repo.current = &rotated
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tencentQuotaFixture))}, nil
			}}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsage(volcengineQuotaTestContext(t), account.ID)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.Nil(t, result.TokenPlan)
			require.Empty(t, repo.writes)
		})
	}
}

func TestTencentSignatureFixedCanonicalVector(t *testing.T) {
	request, err := http.NewRequest("POST", "https://tokenhub.tencentcloudapi.com/", nil)
	require.NoError(t, err)
	body := []byte(`{"TeamId":"team"}`)
	now := time.Unix(1720000000, 0)
	signTencentTokenPlan(request, body, "cloud-access", "cloud-secret", "ap-shanghai", now)
	payloadDigest := sha256.Sum256(body)
	canonical := "POST\n/\n\ncontent-type:application/json\nhost:tokenhub.tencentcloudapi.com\n\ncontent-type;host\n" + hex.EncodeToString(payloadDigest[:])
	canonicalDigest := sha256.Sum256([]byte(canonical))
	key := []byte("TC3cloud-secret")
	for _, part := range []string{"2024-07-03", "tokenhub", "tc3_request"} {
		digest := hmac.New(sha256.New, key)
		_, writeErr := digest.Write([]byte(part))
		require.NoError(t, writeErr)
		key = digest.Sum(nil)
	}
	digest := hmac.New(sha256.New, key)
	_, writeErr := digest.Write([]byte("TC3-HMAC-SHA256\n1720000000\n2024-07-03/tokenhub/tc3_request\n" + hex.EncodeToString(canonicalDigest[:])))
	require.NoError(t, writeErr)
	require.Equal(t, "TC3-HMAC-SHA256 Credential=cloud-access/2024-07-03/tokenhub/tc3_request, SignedHeaders=content-type;host, Signature="+hex.EncodeToString(digest.Sum(nil)), request.Header.Get("Authorization"))
}

func TestCloudQuotaMonitorDoesNotInventAvailability(t *testing.T) {
	account := tencentQuotaAccount()
	upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tencentQuotaFixture))}, nil
	}}
	service := NewCNProviderQuotaService(&compatibleBalanceRepo{account: account}, nil, upstream, nil)
	fetcher := &ChannelMonitorQuotaFetcher{cnQuota: service}
	snapshot := fetcher.fetchCNQuota(context.Background(), account, time.Now())
	require.False(t, snapshot.Success)
	require.True(t, snapshot.Unsupported)
	require.False(t, snapshot.CredentialInvalid)
	require.Nil(t, snapshot.Balance)
	require.Empty(t, snapshot.Tiers)
}

func TestVolcengineAgentWindowSemantics(t *testing.T) {
	fixture := `{"Result":{"SeatAFPUsages":[{"SeatID":"seat","AFPFiveHour":{"Quota":10,"Used":0},"AFPDaily":{"Quota":10,"Used":20},"AFPWeekly":{"Quota":10,"Used":3},"AFPMonthly":{"Quota":10,"Used":4}}]}}`
	tiers, _, status := parseVolcengineAgentUsage([]byte(fixture), "seat")
	require.Equal(t, "ok", status)
	require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 0}, {Window: "daily", UsedPercent: 200}, {Window: "weekly", UsedPercent: 30}, {Window: "monthly", UsedPercent: 40}}, tiers)
	for name, fixture := range map[string]string{
		"duplicate":     `{"Result":{"SeatAFPUsages":[{"SeatID":"seat"},{"SeatID":"seat"}]}}`,
		"string amount": `{"Result":{"SeatAFPUsages":[{"SeatID":"seat","AFPDaily":{"Quota":"10","Used":3}}]}}`,
		"infinite":      `{"Result":{"SeatAFPUsages":[{"SeatID":"seat","AFPDaily":{"Quota":10,"Used":1e999}}]}}`,
		"bad reset":     `{"Result":{"SeatAFPUsages":[{"SeatID":"seat","AFPDaily":{"Quota":10,"Used":3,"ResetTime":1.5}}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, status := parseVolcengineAgentUsage([]byte(fixture), "seat")
			require.Equal(t, "invalid_response", status)
		})
	}
	for _, amounts := range []string{`"Quota":0,"Used":50`, `"Quota":-1,"Used":50`, `"Quota":100`, `"Used":0`} {
		tiers, _, status := parseVolcengineAgentUsage([]byte(`{"Result":{"SeatAFPUsages":[{"SeatID":"seat","AFPDaily":{`+amounts+`}}]}}`), "seat")
		require.Equal(t, "ok", status)
		require.Empty(t, tiers)
	}
}

func TestVolcengineAgentSeatClassification(t *testing.T) {
	for _, scenario := range []struct{ name, items, status string }{
		{"empty", `[]`, "no_seat"},
		{"other seat", `[{"SeatID":"other","AFPDaily":{"Quota":10,"Used":2}}]`, "no_seat"},
		{"empty seat", `[{"SeatID":""}]`, "no_seat"},
		{"duplicate", `[{"SeatID":"seat"},{"SeatID":"seat"}]`, "invalid_response"},
		{"object instead of array", `{}`, "invalid_response"},
		{"non object", `[null]`, "invalid_response"},
		{"missing seat", `[{}]`, "invalid_response"},
		{"non string seat", `[{"SeatID":42}]`, "invalid_response"},
		{"malformed window", `[{"SeatID":"other","AFPDaily":[]}]`, "invalid_response"},
		{"bad amount", `[{"SeatID":"other","AFPDaily":{"Quota":10,"Used":"2"}}]`, "invalid_response"},
		{"bad reset", `[{"SeatID":"other","AFPDaily":{"Quota":10,"Used":2,"ResetTime":"bad"}}]`, "invalid_response"},
		{"valid followed by malformed", `[{"SeatID":"seat","AFPDaily":{"Quota":10,"Used":2}},null]`, "invalid_response"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := agentQuotaAccount()
			repo := &compatibleBalanceRepo{account: account}
			calls := 0
			body := `{"Result":{"SeatAFPUsages":` + scenario.items + `}}`
			tiers, plan, status := parseVolcengineAgentUsage([]byte(body), "seat")
			require.Equal(t, scenario.status, status)
			require.Empty(t, tiers)
			require.Empty(t, plan)
			upstream := compatibleBalanceHTTP{send: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				require.Equal(t, "GetSeatAFPUsage", req.URL.Query().Get("Action"))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			}}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, nil).QueryUsageForAccount(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, scenario.status, result.Status)
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			require.Empty(t, result.Tiers)
			require.Empty(t, repo.writes)
			require.Equal(t, 1, calls)
		})
	}
}

func TestCloudQuotaRuntimeObservationsClearWithoutFinancialMutation(t *testing.T) {
	account := tencentQuotaAccount()
	account.Extra = map[string]any{"volcengine_daily_used_percent": 90, "volcengine_daily_reset_at": "old", "hunyuan_token_plan": map[string]any{"unit": "tokens"}, "hunyuan_usage_updated_at": "old", "quota_used": 42}
	ClearAccountRuntimeObservations(account)
	require.Equal(t, map[string]any{"quota_used": 42}, account.Extra)
	require.Equal(t, "inference-secret", account.Credentials["api_key"])
}

func TestTencentEnterpriseAutoAndBusinessPermission(t *testing.T) {
	for _, body := range []string{strings.ReplaceAll(tencentQuotaFixture, `"enterprise"`, `"enterprise-auto"`), `{"Response":{"Error":{"Code":"UnauthorizedOperation.UnauthorizedOperation","Message":"cloud-secret"}}}`} {
		account := tencentQuotaAccount()
		upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		}}
		result, err := NewCNProviderQuotaService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryUsage(volcengineQuotaTestContext(t), account.ID)
		require.NoError(t, err)
		if strings.Contains(body, "UnauthorizedOperation") {
			require.Equal(t, "permission_denied", result.Status)
			require.True(t, result.CredentialValid)
			require.NotContains(t, result.Error, "cloud-secret")
		} else {
			require.True(t, result.Success)
			require.Equal(t, "tokens", result.TokenPlan.Unit)
		}
	}
}

func TestTencentQuotaIntegerBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, fields string
		valid        bool
	}{
		{"zero", `"TokenSummary":{"CycleSeq":0,"BillingItems":[{"TotalQty":0}]}`, true},
		{"max quantity", `"TokenSummary":{"BillingItems":[{"TotalQty":9223372036854775807}]}`, true},
		{"null quantity", `"TokenSummary":{"BillingItems":[{"TotalQty":null}]}`, true},
		{"missing quantity", `"TokenSummary":{"BillingItems":[{"BillingItem":"input"}]}`, true},
		{"quantity overflow", `"TokenSummary":{"BillingItems":[{"TotalQty":9223372036854775808}]}`, false},
		{"fractional quantity", `"TokenSummary":{"BillingItems":[{"TotalQty":0.5}]}`, false},
		{"string quantity", `"TokenSummary":{"BillingItems":[{"TotalQty":"5"}]}`, false},
		{"negative cycle", `"PackageInfo":{"CurrentCycle":-1}`, false},
		{"unsafe cycle", `"PackageInfo":{"RemainCycles":9007199254740992}`, false},
		{"unsafe summary cycle", `"TokenSummary":{"CycleSeq":9007199254740992}`, false},
		{"safe cycle", `"PackageInfo":{"TotalCycles":9007199254740991}`, true},
		{"numeric amount", `"PackageInfo":{"TotalQuota":10}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := tencentQuotaAccount()
			body := `{"Response":{"TeamId":"team","ProductType":"enterprise",` + test.fields + `}}`
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			}}
			result, err := NewCNProviderQuotaService(&compatibleBalanceRepo{account: account}, nil, upstream, nil).QueryUsage(volcengineQuotaTestContext(t), account.ID)
			require.NoError(t, err)
			require.Equal(t, test.valid, result.Success, result.Error)
			if !test.valid {
				require.False(t, result.Persisted)
				require.Nil(t, result.TokenPlan)
				return
			}
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			switch test.name {
			case "zero":
				require.Contains(t, string(encoded), `"total_qty":"0"`)
				require.Contains(t, string(encoded), `"cycle_seq":0`)
			case "max quantity":
				require.Contains(t, string(encoded), `"total_qty":"9223372036854775807"`)
			case "null quantity", "missing quantity":
				require.NotContains(t, string(encoded), `"total_qty"`)
			}
		})
	}
}
