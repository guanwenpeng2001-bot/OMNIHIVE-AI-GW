package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWorkBuddyLateQuotaAndBoundedPreamble(t *testing.T) {
	for _, payload := range []string{`{"error":{"code":14018}}`, `{"code":14018,"msg":"Credits exhausted"}`} {
		a := wbAccount("cn", "", AccountTypeOAuth)
		svc := &OpenAIGatewayService{}
		first := strings.Split(wbSSE, "\n\n")[0] + "\n\n"
		resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(first + "data: " + payload + "\n\n"))}
		_, err := svc.normalizeWorkBuddyResponse(t.Context(), a, resp, false)
		var failure *UpstreamFailoverError
		require.ErrorAs(t, err, &failure)
		require.True(t, svc.isOpenAIAccountRuntimeBlocked(a))
	}
	svc := &OpenAIGatewayService{}
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + strings.Repeat("x", (4<<20)+1)))}
	_, err := svc.normalizeWorkBuddyResponse(t.Context(), wbAccount("cn", "", AccountTypeOAuth), resp, true)
	require.ErrorContains(t, err, "too large")
}

func TestWorkBuddyEmptySystemAndMalformedConfig(t *testing.T) {
	for _, content := range []string{`null`, `"  "`, `[]`, `[{"type":"text","text":""}]`} {
		raw, err := prepareWorkBuddyChat([]byte(`{"messages":[{"role":"system","content":` + content + `},{"role":"user","content":"hello"}]}`))
		require.NoError(t, err)
		require.Equal(t, "You are a helpful assistant.", gjson.GetBytes(raw, "messages.0.content").String())
	}
	for _, raw := range []string{`{}`, `{"code":0}`, `{"code":0,"data":null}`, `{"data":{"models":[]}}`, `{"code":0,"data":"wrong"}`} {
		_, _, err := extractWorkBuddyModelCatalog([]byte(raw), "cn")
		require.Error(t, err, raw)
		require.Equal(t, raw, string(workBuddyConfigFallback([]byte(raw), wbAccount("cn", "", AccountTypeAPIKey))))
	}
}

func TestWorkBuddyUsageOmittedExtrasAndEnterprise(t *testing.T) {
	var sum map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"CreditGroups":[{"Kind":"plan_base","CycleTotalCapacity":500,"CycleUsedCapacity":50}],"Packages":[]}`), &sum))
	details := []map[string]any{}
	for _, code := range workBuddyExtraCodes[:2] {
		details = append(details, map[string]any{"PackageCode": code, "CycleCapacitySizePrecise": float64(20), "CycleCapacityRemainPrecise": float64(12)})
	}
	snap := parseWorkBuddyUsage(sum, details, "cn")
	require.Equal(t, 40.0, snap.ExtraTotal)
	require.Equal(t, 24.0, snap.ExtraRemaining)
	require.Equal(t, 474.0, snap.TotalRemaining)
	for _, enterprise := range []bool{false, true} {
		t.Run(fmt.Sprint(enterprise), func(t *testing.T) {
			calls := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/billing/meter/get-user-resource-summary":
					if enterprise {
						fmt.Fprint(w, `{"code":0,"data":{"Packages":[]}}`)
					} else {
						fmt.Fprint(w, `{"code":0,"data":{"CreditGroups":[{"Kind":"plan_base","CycleTotalCapacity":500,"CycleUsedCapacity":50}]}}`)
					}
				case "/billing/meter/get-enterprise-user-usage":
					require.Equal(t, "team-1", r.Header.Get("X-Enterprise-Id"))
					fmt.Fprint(w, `{"code":0,"data":{"limitNum":1000,"credit":125,"cycleResetTime":"2030-12-01 00:00:00"}}`)
				default:
					w.WriteHeader(503)
				}
			}))
			defer server.Close()
			a := wbAccount("cn", server.URL, AccountTypeOAuth)
			if enterprise {
				a.Credentials["enterprise_id"] = "team-1"
			}
			got, err := NewWorkBuddyOAuthService(wbConfig(server.URL), nil).FetchWorkBuddyUsage(t.Context(), a)
			require.NoError(t, err)
			if enterprise {
				require.Len(t, calls, 2)
				require.Equal(t, "Enterprise", got.Plan)
				require.Equal(t, 875.0, got.TotalRemaining)
				require.Equal(t, "2030-11-30T16:00:00Z", got.BaseResetAt)
			} else {
				require.Equal(t, 450.0, got.TotalRemaining)
			}
		})
	}
}

type wbProxyRepo struct {
	ProxyRepository
	proxy *Proxy
}

func (r wbProxyRepo) GetByID(context.Context, int64) (*Proxy, error) { return r.proxy, nil }
func TestWorkBuddyProxyAndRefreshRegistration(t *testing.T) {
	calls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.True(t, r.URL.IsAbs())
		require.Equal(t, "/v2/plugin/auth/state", r.URL.Path)
		fmt.Fprint(w, `{"code":0,"data":{"state":"s","authUrl":"https://www.workbuddy.cn/login"}}`)
	}))
	defer proxy.Close()
	u, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	repo := wbProxyRepo{proxy: &Proxy{ID: 1, Protocol: "http", Host: u.Hostname(), Port: port, Status: StatusActive}}
	svc := NewWorkBuddyOAuthService(wbConfig(proxy.URL), repo)
	svc.endpoints = map[string]string{"cn": "http://127.0.0.1:1"}
	id := int64(1)
	_, err = svc.Start(t.Context(), "cn", &id, "actor")
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	refresh := NewWorkBuddyTokenRefresher(svc)
	a := wbAccount("global", "", AccountTypeOAuth)
	a.Credentials["expires_at"] = time.Now().Add(time.Hour).UnixMilli()
	require.True(t, refresh.CanRefresh(a))
	require.False(t, refresh.NeedsRefresh(a, time.Minute))
	a.Credentials["expires_at"] = time.Now().Add(10 * time.Second).Format(time.RFC3339)
	require.True(t, refresh.NeedsRefresh(a, time.Minute))
	require.False(t, refresh.CanRefresh(wbAccount("cn", "", AccountTypeAPIKey)))
	a.Credentials["refresh_expires_at"] = time.Now().Add(-time.Second).Unix()
	_, err = refresh.Refresh(t.Context(), a)
	require.ErrorContains(t, err, "expired")
	require.NotContains(t, refresh.CacheKey(a), "refresh-secret")
	tokens := &TokenRefreshService{}
	tokens.SetWorkBuddyOAuthService(svc)
	require.Len(t, tokens.registrations, 1)
	require.Equal(t, PlatformWorkBuddy, tokens.registrations[0].platform)
	require.True(t, tokens.registrations[0].refresher.CanRefresh(a))
}

func TestWorkBuddyMonitorStreamingAdapter(t *testing.T) {
	original := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { monitorHTTPClient = original })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/chat/completions", r.URL.Path)
		require.Equal(t, "anonymous_12345678", r.Header.Get("X-User-Id"))
		raw, _ := io.ReadAll(r.Body)
		require.True(t, gjson.GetBytes(raw, "stream").Bool())
		require.Equal(t, "system", gjson.GetBytes(raw, "messages.0.role").String())
		w.Header().Set("Content-Type", "text/event-stream")
		io.Copy(w, bytes.NewBufferString(wbSSE))
	}))
	defer server.Close()
	text, _, status, err := callProvider(t.Context(), PlatformWorkBuddy, server.URL+"/v2", "key-12345678", "hy3", "hello", nil)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, "hello", text)
	tiers := usageQuotaTiers(&UsageInfo{WorkBuddy: &WorkBuddyUsageSnapshot{BaseTotal: 500, BaseUsed: 100, ExtraTotal: 100, ExtraUsed: 20}})
	require.Len(t, tiers, 1)
	require.Equal(t, 20.0, tiers[0].UsedPercent)
}
