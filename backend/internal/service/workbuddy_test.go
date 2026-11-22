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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func wbConfig(base string) *config.Config {
	u, _ := url.Parse(base)
	return &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: true, AllowInsecureHTTP: true, AllowPrivateHosts: true, UpstreamHosts: []string{u.Hostname()}}}}
}
func wbAccount(site, base, kind string) *Account {
	return &Account{ID: 13, Name: "WorkBuddy test", Platform: PlatformWorkBuddy, Type: kind, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"site": site, "base_url": base, "access_token": "access-secret", "refresh_token": "refresh-secret", "uid": "user-13", "api_key": "ck_test-12345678"}}
}

type wbHTTP struct{ client *http.Client }

func (u wbHTTP) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(r)
}
func (u wbHTTP) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, n)
}

const wbSSE = "data: {\"id\":\"wb-1\",\"created\":1795300000,\"model\":\"hy3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hel\",\"reasoning_content\":\"think\"}}]}\n\ndata: {\"id\":\"wb-1\",\"model\":\"hy3\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":17,\"completion_tokens\":5,\"total_tokens\":22,\"prompt_tokens_details\":{\"cached_tokens\":3}}}\n\ndata: [DONE]\n\n"

func TestWorkBuddySitesHeadersExpiry(t *testing.T) {
	for _, tt := range []struct{ site, domain, base, want string }{{"", "", "", "cn"}, {"china", "", "", "cn"}, {"workbuddy", "", "", "cn"}, {"international", "", "", "global"}, {"intl", "", "", "global"}, {"workbuddy-ai", "", "", "global"}, {"workbuddy_ai", "", "", "global"}, {"", "www.workbuddy.ai", "", "global"}, {"", "", "https://www.workbuddy.ai/v2", "global"}, {"", "evilworkbuddy.ai.invalid", "", "cn"}} {
		require.Equal(t, tt.want, NormalizeWorkBuddySite(tt.site, tt.domain, tt.base))
	}
	for _, site := range []string{"cn", "global"} {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			a := wbAccount(site, "", kind)
			h := http.Header{}
			a.Credentials["enterprise_id"] = "ent-1"
			applyWorkBuddyChatHeaders(h, a, []byte(`{"messages":[{"role":"user","content":"hi"}]}`))
			require.Equal(t, "WorkBuddy/5.9.3 "+workBuddySiteFor(a).AppName+"/5.9.3 CLI/2.165.0", h.Get("User-Agent"))
			require.Equal(t, "local-agent", h.Get("X-Agent-Runtime"))
			require.Equal(t, "6.25.0", h.Get("X-Stainless-Package-Version"))
			require.Equal(t, h.Get("X-Request-ID"), h.Get("X-Conversation-Message-ID"))
			require.Len(t, h.Get("X-Conversation-ID"), 32)
			require.Len(t, h.Get("X-Conversation-Request-ID"), 32)
			if kind == AccountTypeAPIKey {
				require.Equal(t, "anonymous_12345678", h.Get("X-User-Id"))
				require.Equal(t, a.GetCredential("api_key"), h.Get("X-API-Key"))
				require.Empty(t, h.Get("X-Domain"))
				require.Empty(t, h.Get("X-Enterprise-Id"))
			} else {
				require.Equal(t, workBuddySiteFor(a).Domain, h.Get("X-Domain"))
				require.Equal(t, "ent-1", h.Get("X-Tenant-Id"))
			}
		}
	}
	a := wbAccount("cn", "", AccountTypeOAuth)
	for _, host := range []string{"team.sso.copilot.tencent.com", "team.copilot.qq.com", "team.sso.codebuddy.cn"} {
		a.Credentials["domain"] = host
		require.Equal(t, "https://"+host, workBuddyEndpoint(a))
	}
	for _, host := range []string{"tencent.sso.copilot.tencent.com", "tencent.sso.codebuddy.cn", "evil.sso.codebuddy.cn.attacker.test"} {
		a.Credentials["domain"] = host
		require.Equal(t, "https://copilot.tencent.com", workBuddyEndpoint(a))
	}
	for _, v := range []any{"2026-11-22T00:00:00Z", int64(1795305600), int64(1795305600000), float64(1795305600000), json.Number("1795305600"), "1795305600000"} {
		a.Credentials["expires_at"] = v
		require.Equal(t, int64(1795305600), a.GetCredentialAsTime("expires_at").Unix())
	}
	require.NoError(t, validateExpandedPlatformAccount(a))
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(a))
	require.True(t, domain.IsConcretePlatform(PlatformWorkBuddy))
}
func TestWorkBuddyOAuthCNGlobal(t *testing.T) {
	for _, site := range []string{"cn", "global"} {
		t.Run(site, func(t *testing.T) {
			var tokenPoll, identityPoll, refreshes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v2/plugin/auth/state":
					require.Equal(t, "POST", r.Method)
					require.Equal(t, workBuddySiteFor(wbAccount(site, "", AccountTypeOAuth)).Platform, r.URL.Query().Get("platform"))
					for _, k := range []string{"X-No-Authorization", "X-No-User-Id", "X-No-Enterprise-Id", "X-No-Department-Info"} {
						require.Equal(t, "true", r.Header.Get(k))
					}
					fmt.Fprint(w, `{"code":0,"data":{"state":"state-token","authUrl":"https://www.workbuddy.ai/login?a=1"}}`)
				case "/v2/plugin/auth/token":
					require.Equal(t, "state-token", r.URL.Query().Get("state"))
					if tokenPoll.Add(1) == 1 {
						fmt.Fprint(w, `{"code":11217}`)
						return
					}
					fmt.Fprint(w, `{"code":0,"data":{"accessToken":"issued-access","refreshToken":"issued-refresh","expiresAt":1795392000000,"refreshExpiresIn":36000}}`)
				case "/v2/plugin/login/account":
					require.Equal(t, "Bearer issued-access", r.Header.Get("Authorization"))
					if identityPoll.Add(1) == 1 {
						fmt.Fprint(w, `{"code":12151}`)
						return
					}
					fmt.Fprint(w, `{"code":0,"data":{"uid":"wb-user","nickname":"Buddy","type":"enterprise"}}`)
				case "/v2/plugin/accounts":
					fmt.Fprint(w, `{"code":0,"data":{"accounts":[{"uid":"wb-user","type":"enterprise","enterpriseId":"team-id","enterpriseName":"Team"}]}}`)
				case "/v2/plugin/auth/token/refresh":
					refreshes.Add(1)
					require.Equal(t, "issued-refresh", r.Header.Get("X-Refresh-Token"))
					require.Equal(t, "plugin", r.Header.Get("X-Auth-Refresh-Source"))
					fmt.Fprint(w, `{"code":0,"data":{"accessToken":"renewed-access","refreshToken":"rotated-refresh","expiresIn":3600}}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			svc := NewWorkBuddyOAuthService(wbConfig(server.URL), nil)
			svc.endpoints = map[string]string{site: server.URL}
			start, err := svc.Start(t.Context(), site, nil, "browser")
			require.NoError(t, err)
			require.Equal(t, 300, start.ExpiresIn)
			u, _ := url.Parse(start.AuthURL)
			require.Equal(t, workBuddyVersion, u.Query().Get("version"))
			require.Len(t, u.Query().Get("loginSessionId"), 32)
			_, err = svc.Poll(t.Context(), start.SessionID, "other")
			require.Error(t, err)
			for i := 0; i < 2; i++ {
				poll, err := svc.Poll(t.Context(), start.SessionID, "browser")
				require.NoError(t, err)
				require.Equal(t, "pending", poll.Status)
			}
			poll, err := svc.Poll(t.Context(), start.SessionID, "browser")
			require.NoError(t, err)
			require.Equal(t, "completed", poll.Status)
			require.Equal(t, "Buddy · Team", poll.AccountName)
			require.Equal(t, "team-id", poll.Credentials["enterprise_id"])
			require.Equal(t, site, poll.Credentials["site"])
			require.Equal(t, "2026-11-23T00:00:00Z", poll.Credentials["expires_at"])
			a := wbAccount(site, server.URL, AccountTypeOAuth)
			a.Credentials = poll.Credentials
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					creds, err := svc.RefreshAccountToken(context.Background(), a)
					if err != nil {
						t.Error(err)
						return
					}
					if creds["access_token"] != "renewed-access" || creds["uid"] != "wb-user" {
						t.Error("lost refresh fields")
					}
				}()
			}
			wg.Wait()
			require.EqualValues(t, 1, refreshes.Load())
			require.EqualValues(t, 2, tokenPoll.Load())
			svc.sessions[start.SessionID].expires = time.Now().Add(-time.Second)
			_, err = svc.Poll(t.Context(), start.SessionID, "browser")
			require.Error(t, err)
		})
	}
}
func TestWorkBuddyURLPolicyAndRedirect(t *testing.T) {
	svc := NewWorkBuddyOAuthService(&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"allowed.test"}}}}, nil)
	_, err := svc.Start(t.Context(), "cn", nil, "browser")
	require.ErrorContains(t, err, "URL not allowed")
	var reached atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer origin.Close()
	svc = NewWorkBuddyOAuthService(wbConfig(origin.URL), nil)
	svc.endpoints = map[string]string{"cn": origin.URL}
	_, err = svc.Start(t.Context(), "cn", nil, "browser")
	require.Error(t, err)
	require.False(t, reached.Load())
}
func TestWorkBuddyChatStreamingAndAggregation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, site := range []string{"cn", "global"} {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", site, kind, stream), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, "/v2/chat/completions", r.URL.Path)
						body, _ := io.ReadAll(r.Body)
						require.True(t, gjson.GetBytes(body, "stream").Bool())
						require.True(t, gjson.GetBytes(body, "stream_options.include_usage").Bool())
						require.Equal(t, "system", gjson.GetBytes(body, "messages.0.role").String())
						require.NotEmpty(t, r.Header.Get("X-Conversation-Request-ID"))
						require.Equal(t, "WorkBuddy", r.Header.Get("X-IDE-Type"))
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, wbSSE)
					}))
					defer server.Close()
					a := wbAccount(site, server.URL, kind)
					svc := &OpenAIGatewayService{cfg: wbConfig(server.URL), httpUpstream: wbHTTP{server.Client()}}
					body := []byte(fmt.Sprintf(`{"model":"hy3","stream":%t,"messages":[{"role":"user","content":"hello"}]}`, stream))
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
					result, err := svc.ForwardAsChatCompletions(t.Context(), c, a, body, "", "")
					require.NoError(t, err)
					require.Equal(t, 17, result.Usage.InputTokens)
					require.Equal(t, 5, result.Usage.OutputTokens)
					require.Equal(t, 3, result.Usage.CacheReadInputTokens)
					if stream {
						require.Contains(t, rec.Body.String(), "[DONE]")
					} else {
						require.Equal(t, "chat.completion", gjson.Get(rec.Body.String(), "object").String())
						require.Equal(t, "hello", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
						require.EqualValues(t, 22, gjson.Get(rec.Body.String(), "usage.total_tokens").Int())
					}
				})
			}
		}
	}
	for _, broken := range []string{strings.ReplaceAll(wbSSE, "data: [DONE]", ""), strings.ReplaceAll(wbSSE, `"usage":`, `"missing":`), "data: {broken}\n\n", `data: {"error":{"code":14018}}` + "\n\n"} {
		_, err := aggregateWorkBuddyChat(strings.NewReader(broken))
		require.Error(t, err)
	}
}

const wbConfigJSON = `{"code":0,"data":{"models":[{"id":"hy3","name":"Hy3","maxInputTokens":192000,"maxOutputTokens":64000,"supportsImages":true,"onlyReasoning":true,"reasoning":{"supportedEfforts":["low","high"],"defaultEffort":"high"}},{"id":"glm-5.3","supportsImages":true},{"id":"custom:enterprise|model","maxInputTokens":12345},{"id":"desktop-local"}],"agents":[{"name":"desktop","models":["desktop-local"]},{"name":"cli","models":["hy3","glm-5.3"]}]}}`

func TestWorkBuddyDiscoveryAndProbe(t *testing.T) {
	for _, site := range []string{"cn", "global"} {
		for _, status := range []int{200, 401, 403, 429} {
			t.Run(fmt.Sprintf("%s/%d", site, status), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "/v3/config", r.URL.Path)
					if r.Header.Get("Authorization") == "" {
						w.WriteHeader(401)
						return
					}
					require.Equal(t, "XMLHttpRequest", r.Header.Get("X-Requested-With"))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					fmt.Fprint(w, wbConfigJSON)
				}))
				defer server.Close()
				a := wbAccount(site, server.URL, AccountTypeOAuth)
				svc := &AccountTestService{cfg: wbConfig(server.URL), httpUpstream: wbHTTP{server.Client()}}
				result := svc.ProbeConnection(t.Context(), a, "")
				switch status {
				case 200:
					require.Equal(t, "passed", result.Network)
					require.Equal(t, "passed", result.Authorization)
					require.Equal(t, "passed", result.Discovery)
					require.ElementsMatch(t, []string{"hy3", "glm-5.3", "custom:enterprise|model"}, result.Models)
					ids, body, err := svc.fetchUpstreamModelList(t.Context(), a)
					require.NoError(t, err)
					require.ElementsMatch(t, result.Models, ids)
					_, metadata, err := extractUpstreamModelCatalog(body, false, PlatformWorkBuddy, site)
					require.NoError(t, err)
					require.EqualValues(t, 192000, metadata["hy3"].ContextWindow)
					require.EqualValues(t, 64000, metadata["hy3"].MaxOutputTokens)
					require.Equal(t, []string{"low", "high"}, metadata["hy3"].SupportedReasoningLevels)
					require.Equal(t, site == "cn", *metadata["glm-5.3"].SupportsVision)
				case 401, 403:
					require.Equal(t, "AUTH_REJECTED", result.AuthorizationReason)
				case 429:
					require.Equal(t, "UPSTREAM_RATE_LIMITED", result.DiscoveryReason)
				}
			})
		}
	}
	for _, site := range []string{"cn", "global"} {
		a := wbAccount(site, "", AccountTypeAPIKey)
		a.Credentials["api_key"] = "ordinary-key"
		body := workBuddyConfigFallback([]byte(`{"code":0,"data":{}}`), a)
		ids, _, err := extractWorkBuddyModelCatalog(body, site)
		require.NoError(t, err)
		require.Len(t, ids, len(workBuddyModels(site)))
		a.Credentials["api_key"] = "ck_key"
		require.Equal(t, []byte(`{"code":0,"data":{}}`), workBuddyConfigFallback([]byte(`{"code":0,"data":{}}`), a))
	}
	a := wbAccount("cn", "http://127.0.0.1:1", AccountTypeOAuth)
	svc := &AccountTestService{cfg: &config.Config{}}
	require.Equal(t, "URL_NOT_ALLOWED", svc.ProbeConnection(t.Context(), a, "").NetworkReason)
	svc.cfg = wbConfig(a.GetCredential("base_url"))
	require.Equal(t, "PROBE_FAILED", svc.ProbeConnection(t.Context(), a, "").NetworkReason)
}

type wbRepo struct {
	AccountRepository
	extras  map[string]any
	blocked int64
	until   time.Time
}

func (r *wbRepo) UpdateExtra(_ context.Context, _ int64, extra map[string]any) error {
	r.extras = extra
	return nil
}
func (r *wbRepo) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, _ *time.Time) error {
	return r.UpdateExtra(ctx, expected.ID, updates)
}
func (r *wbRepo) SetTempUnschedulable(_ context.Context, id int64, until time.Time, _ string) error {
	r.blocked = id
	r.until = until
	return nil
}
func TestWorkBuddyUsageAndPersistence(t *testing.T) {
	for _, site := range []string{"cn", "global"} {
		t.Run(site, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "Bearer access-secret", r.Header.Get("Authorization"))
				require.Equal(t, "user-13", r.Header.Get("X-User-Id"))
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "summary") {
					fmt.Fprint(w, `{"code":0,"data":{"IsPaidUser":true,"SubscriptionPackageCode":"plan","CreditGroups":[{"Kind":"plan_base","CycleTotalCapacity":"500","CycleUsedCapacity":"120"},{"Kind":"addon","CycleTotalCapacity":"80","CycleUsedCapacity":"10"}],"Packages":[{"PackageCode":"plan","CycleTotalCapacity":"500","CycleUsedCapacity":"120"}]}}`)
					return
				}
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.NotEmpty(t, body["PackageCodes"])
				fmt.Fprint(w, `{"code":0,"data":{"TotalCount":1,"Accounts":[{"PackageCode":"plan","CycleEndTime":"2030-12-01 00:00:00"}]}}`)
			}))
			defer server.Close()
			a := wbAccount(site, server.URL, AccountTypeOAuth)
			repo := &wbRepo{}
			svc := &AccountUsageService{accountRepo: repo, workBuddyOAuth: NewWorkBuddyOAuthService(wbConfig(server.URL), nil)}
			got, err := svc.getWorkBuddyUsage(t.Context(), a, true)
			require.NoError(t, err)
			require.Equal(t, site, got.WorkBuddy.Site)
			require.Equal(t, 500.0, got.WorkBuddy.BaseTotal)
			require.Equal(t, 120.0, got.WorkBuddy.BaseUsed)
			require.Equal(t, 380.0, got.WorkBuddy.BaseRemaining)
			require.Equal(t, 70.0, got.WorkBuddy.ExtraRemaining)
			require.Equal(t, 450.0, got.WorkBuddy.TotalRemaining)
			require.Equal(t, "2030-11-30T16:00:00Z", got.WorkBuddy.BaseResetAt)
			require.Equal(t, got.WorkBuddy, repo.extras[WorkBuddyUsageSnapshotExtraKey])
			require.Equal(t, 3, calls)
		})
	}
	sum := map[string]any{"Packages": []any{map[string]any{"PackageCode": "TCACA_code_008_cfWoLwvjU4", "CycleTotalCapacity": "500", "CycleUsedCapacity": "30"}}}
	details := []map[string]any{{"PackageCode": "TCACA_code_009_0XmEQc2xOf", "CycleCapacitySizePrecise": "20.5", "CycleCapacityRemainPrecise": "10.25", "ExpiredTime": "2030-12-01 00:00:00"}}
	snap := parseWorkBuddyUsage(sum, details, "cn")
	require.Equal(t, 470.0, snap.BaseRemaining)
	require.Equal(t, 10.25, snap.ExtraRemaining)
}
func TestWorkBuddyQuotaFailover(t *testing.T) {
	for _, status := range []int{200, 400, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "90")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"code":14018,"msg":"Credits exhausted"}`)
			}))
			defer server.Close()
			repo := &wbRepo{}
			svc := &OpenAIGatewayService{cfg: wbConfig(server.URL), httpUpstream: wbHTTP{server.Client()}, accountRepo: repo}
			a := wbAccount("cn", server.URL, AccountTypeOAuth)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			_, err := svc.sendCCUpstreamRequest(t.Context(), c, a, server.URL+"/v2/chat/completions", []byte(`{"messages":[{"role":"user","content":"hi"}]}`), false, workBuddyToken(a), "", "")
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, 429, failure.StatusCode)
			require.False(t, failure.RetryableOnSameAccount)
			require.Equal(t, a.ID, repo.blocked)
			require.True(t, svc.isOpenAIAccountRuntimeBlocked(a))
			next := *a
			next.ID++
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(&next))
			require.True(t, repo.until.After(time.Now().Add(80*time.Second)))
			require.False(t, c.Writer.Written())
		})
	}
	require.False(t, workBuddyQuotaError(400, []byte(`{"code":140185,"msg":"bad input"}`)))
	require.False(t, workBuddyQuotaError(400, []byte(`{"code":11153,"msg":"prompt is too long"}`)))
}
func TestWorkBuddyProtocolBridges(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, stream := range []bool{false, true} {
			for _, protocol := range []string{"messages", "responses", "gemini"} {
				t.Run(fmt.Sprintf("%s/%t/%s", kind, stream, protocol), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, "/v2/chat/completions", r.URL.Path)
						b, _ := io.ReadAll(r.Body)
						require.True(t, gjson.GetBytes(b, "stream").Bool())
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, wbSSE)
					}))
					defer server.Close()
					a := wbAccount("cn", server.URL, kind)
					svc := &OpenAIGatewayService{cfg: wbConfig(server.URL), httpUpstream: wbHTTP{server.Client()}}
					body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-5.5","max_tokens":64,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream))
					if protocol == "responses" {
						body = []byte(fmt.Sprintf(`{"model":"hy3","stream":%t,"input":"hi"}`, stream))
					}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest("POST", "/v1/"+protocol, bytes.NewReader(body))
					var result *OpenAIForwardResult
					var err error
					switch protocol {
					case "gemini":
						body, err = GeminiToMessages([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`), "hy3", stream)
						require.NoError(t, err)
						action := "generateContent"
						if stream {
							action = "streamGenerateContent"
						}
						writer := NewGeminiProtocolWriter(c.Writer, "hy3", action, stream, 0)
						c.Writer = writer
						result, err = svc.ForwardAsAnthropic(t.Context(), c, a, body, "", "")
						require.NoError(t, writer.Finish())
						require.Contains(t, rec.Body.String(), "usageMetadata")
					case "messages":
						result, err = svc.ForwardAsAnthropic(t.Context(), c, a, body, "", "")
					default:
						result, err = svc.forwardResponsesViaRawChatCompletions(t.Context(), c, a, body)
					}
					require.NoError(t, err)
					require.Equal(t, 17, result.Usage.InputTokens)
					require.Equal(t, 5, result.Usage.OutputTokens)
					if stream && (protocol == "messages" || protocol == "gemini") {
						require.Contains(t, rec.Body.String(), `"text":"hel"`)
						require.Contains(t, rec.Body.String(), `"text":"lo"`)
					} else {
						require.Contains(t, rec.Body.String(), "hello")
					}
				})
			}
		}
	}
}
func TestWorkBuddyCatalogRegistration(t *testing.T) {
	snapshot := NewModelRegistry().Snapshot()
	require.Contains(t, snapshot.CandidateIDs(PlatformWorkBuddy, "workbuddy-cn"), "hy4-preview-f")
	require.NotContains(t, snapshot.CandidateIDs(PlatformWorkBuddy, "workbuddy-cn"), "gpt-5.5")
	require.Contains(t, snapshot.CandidateIDs(PlatformWorkBuddy, "workbuddy-global"), "gpt-5.5")
	for _, action := range []string{"chat", "messages", "responses", "generate_content"} {
		require.Equal(t, domain.ActionImplemented, domain.PlatformAction(domain.ActionIdentity{Platform: PlatformWorkBuddy, HostedAccountType: AccountTypeOAuth}, action).Status, action)
	}
	for _, key := range []string{"access_token", "refresh_token", "api_key"} {
		require.True(t, IsSensitiveCredentialKey(key))
	}
}

func TestWorkBuddyTurnIDsAndToolAggregation(t *testing.T) {
	a := wbAccount("cn", "", AccountTypeOAuth)
	base := `{"messages":[{"role":"user","content":"hi"}]}`
	tool := `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","tool_calls":[{"id":"t","function":{"name":"run"}}]},{"role":"tool","content":"result"},{"role":"user","content":"tool attachment"}]}`
	next := `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"answer"},{"role":"user","content":"hi"}]}`
	first, continuation, newTurn := http.Header{}, http.Header{}, http.Header{}
	applyWorkBuddyChatHeaders(first, a, []byte(base))
	applyWorkBuddyChatHeaders(continuation, a, []byte(tool))
	applyWorkBuddyChatHeaders(newTurn, a, []byte(next))
	require.Equal(t, first.Get("X-Conversation-ID"), newTurn.Get("X-Conversation-ID"))
	require.Equal(t, first.Get("X-Conversation-Request-ID"), continuation.Get("X-Conversation-Request-ID"))
	require.NotEqual(t, first.Get("X-Conversation-Request-ID"), newTurn.Get("X-Conversation-Request-ID"))
	require.NotEqual(t, first.Get("X-Request-ID"), continuation.Get("X-Request-ID"))
	raw := "data: " + `{"id":"chat-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"run","arguments":"{\"a\":"}}]}}]}` + "\n\ndata: " + `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}` + "\n\ndata: [DONE]\n\n"
	body, err := aggregateWorkBuddyChat(strings.NewReader(raw))
	require.NoError(t, err)
	require.Equal(t, "call-1", gjson.GetBytes(body, "choices.0.message.tool_calls.0.id").String())
	require.Equal(t, `{"a":1}`, gjson.GetBytes(body, "choices.0.message.tool_calls.0.function.arguments").String())
}
