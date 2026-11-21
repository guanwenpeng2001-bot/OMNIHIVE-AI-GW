//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"fmt"
	ws "github.com/coder/websocket"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func useFixtureProxy(t *testing.T, runtime *ProxyGroupRuntime, groups *runtimeGroups, url string) {
	t.Helper()
	p := cursorTestProxy(t, url, 11)
	p.WorkspaceID = 1
	p.Status = StatusActive
	groups.group.Members = []ProxyGroupMember{{GroupID: groups.group.ID, WorkspaceID: 1, ProxyID: p.ID, Proxy: p}}
}

func TestProxyGroupCursorUsesSelectedProxy(t *testing.T) {
	var direct, proxied atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		direct.Add(1)
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture"}]}`))
	}))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied.Add(1)
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture"}]}`))
	}))
	defer proxy.Close()
	r, g, cache, a := runtimeFixture()
	useFixtureProxy(t, r, g, proxy.URL)
	a.Platform = PlatformCursor
	a.Type = AccountTypeUpstream
	a.Credentials = map[string]any{"base_url": origin.URL, "api_key": "fixture"}
	a.AttachEgress(r)
	models, err := cursorRelayTestGateway(origin).AvailableModels(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, []string{"fixture"}, models)
	require.EqualValues(t, 1, proxied.Load())
	require.Zero(t, direct.Load())
	require.Equal(t, int64(11), cache.bindings[[2]int64{7, a.ID}])
}

func TestProxyGroupWebSocketPinsPhysicalConnection(t *testing.T) {
	var direct, proxied atomic.Int32
	serve := func(counter *atomic.Int32) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			counter.Add(1)
			connection, err := ws.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer connection.CloseNow()
			for {
				kind, body, err := connection.Read(r.Context())
				if err != nil {
					return
				}
				if connection.Write(r.Context(), kind, body) != nil {
					return
				}
			}
		}
	}
	origin := httptest.NewServer(serve(&direct))
	defer origin.Close()
	proxy := httptest.NewServer(serve(&proxied))
	defer proxy.Close()
	r, g, cache, binding := runtimeFixture()
	useFixtureProxy(t, r, g, proxy.URL)
	a := openAIActionAccount()
	a.WorkspaceID = 1
	a.ProxyGroupID = binding.ProxyGroupID
	a.Credentials["base_url"] = origin.URL
	a.AttachEgress(r)
	svc := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, err := svc.OpenOpenAIRealtime(ctx, a, "gpt-realtime")
	require.NoError(t, err)
	defer connection.CloseNow()
	g.group.Status = "inactive"
	cache.bindings[[2]int64{7, a.ID}] = 12
	require.NoError(t, connection.Write(ctx, ws.MessageText, []byte("same socket")))
	_, body, err := connection.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "same socket", string(body))
	require.EqualValues(t, 1, proxied.Load())
	require.Zero(t, direct.Load())
	unresolved := *a
	unresolved.egress = nil
	_, err = svc.OpenOpenAIRealtime(ctx, &unresolved, "gpt-realtime")
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
	require.EqualValues(t, 1, proxied.Load())
	require.Zero(t, direct.Load())
}

func TestProxyGroupBatchImageProvidersUsePinnedEgress(t *testing.T) {
	useLoopbackClient(t, &batchImageProxyClient)
	for _, vendor := range []string{"gemini", "vertex"} {
		t.Run(vendor, func(t *testing.T) {
			var direct, proxied atomic.Int32
			body := `{"name":"batches/fixture","done":false}`
			jobName := "batches/fixture"
			if vendor == "vertex" {
				jobName = "projects/proj/locations/global/batchPredictionJobs/fixture"
				body = `{"name":"projects/proj/locations/global/batchPredictionJobs/fixture","state":"JOB_STATE_RUNNING"}`
			}
			serve := func(count *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					count.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(body))
				}
			}
			origin := httptest.NewServer(serve(&direct))
			defer origin.Close()
			proxy := httptest.NewServer(serve(&proxied))
			defer proxy.Close()
			r, g, cache, a := runtimeFixture()
			useFixtureProxy(t, r, g, proxy.URL)
			a.Platform = PlatformGemini
			a.Type = AccountTypeAPIKey
			a.Credentials = map[string]any{"api_key": "fixture"}
			a.AttachEgress(r)
			var provider BatchImageProvider = NewGeminiAPIBatchImageProvider(NewGeminiBatchHTTPClient(origin.URL, origin.Client()))
			if vendor == "vertex" {
				a.Type = AccountTypeServiceAccount
				a.Credentials = vertexServiceAccount().Credentials
				provider = NewVertexBatchImageProvider(VertexBatchImageProviderOptions{Enabled: true, ProjectID: "proj"}, NewVertexBatchHTTPClient(origin.URL, origin.Client()), &fakeVertexObjectStore{}, &fakeGeminiTokenCache{token: "fixture-token"})
			}
			snapshot, err := a.PinJobEgress(context.Background())
			require.NoError(t, err)
			job := &BatchImageJob{ProviderJobName: &jobName, EgressSnapshot: snapshot}
			// Simulate another operation changing/expiring the account's sticky key.
			cache.bindings[[2]int64{7, a.ID}] = 12
			_, err = provider.Get(context.Background(), job, a)
			require.NoError(t, err)
			require.EqualValues(t, 1, proxied.Load())
			require.Zero(t, direct.Load())
			g.group.Members = nil
			_, err = provider.Get(context.Background(), job, a)
			require.ErrorIs(t, err, ErrProxyGroupUnavailable)
			require.EqualValues(t, 1, proxied.Load())
			require.Zero(t, direct.Load())
		})
	}
}

type egressUsageFetcher struct{ urls []string }

func (f *egressUsageFetcher) FetchUsage(_ context.Context, _, url string) (*ClaudeUsageResponse, error) {
	f.urls = append(f.urls, url)
	return &ClaudeUsageResponse{}, nil
}
func (f *egressUsageFetcher) FetchUsageWithOptions(ctx context.Context, options *ClaudeUsageFetchOptions) (*ClaudeUsageResponse, error) {
	return f.FetchUsage(ctx, options.AccessToken, options.ProxyURL)
}

func TestProxyGroupQuotaAndUsageBeforeOutbound(t *testing.T) {
	r, _, _, a := runtimeFixture()
	a.Platform = PlatformAnthropic
	a.Credentials = map[string]any{"access_token": "fixture"}
	fetcher := &egressUsageFetcher{}
	svc := &AccountUsageService{usageFetcher: fetcher}
	_, err := svc.fetchOAuthUsageRaw(context.Background(), a)
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
	require.Empty(t, fetcher.urls)
	a.AttachEgress(r)
	want, err := a.EgressProxyURL(context.Background())
	require.NoError(t, err)
	_, err = svc.fetchOAuthUsageRaw(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, []string{want}, fetcher.urls)
	for _, get := range []func(context.Context, *Account) (string, error){(&CNProviderQuotaService{}).resolveProxyURL, (&CNProviderBalanceService{}).resolveProxyURL, (&GrokQuotaService{}).resolveProxyURL, (&AntigravityQuotaFetcher{}).GetProxyURL} {
		url, err := get(context.Background(), a)
		require.NoError(t, err)
		require.Equal(t, want, url)
		unresolved := *a
		unresolved.egress = nil
		_, err = get(context.Background(), &unresolved)
		require.ErrorIs(t, err, ErrProxyGroupUnavailable)
	}
}

func TestProxyGroupCursorAuthorizationPollUsesOneServerRoute(t *testing.T) {
	var direct, proxied atomic.Int32
	access := "header." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(time.Hour).Unix()))) + ".signature"
	serve := func(count *atomic.Int32) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			count.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"accessToken":%q,"refreshToken":"fixture-refresh"}`, access)
		}
	}
	origin := httptest.NewServer(serve(&direct))
	defer origin.Close()
	proxy := httptest.NewServer(serve(&proxied))
	defer proxy.Close()
	r, g, cache, _ := runtimeFixture()
	useFixtureProxy(t, r, g, proxy.URL)
	target, err := r.ResolveForNewSession(context.Background(), 7, "server-session-seed")
	require.NoError(t, err)
	svc := NewCursorAuthService()
	svc.poll = func(ctx context.Context, session cursor.PKCE) (*cursor.OAuthTokens, error) {
		return cursor.PollPKCE(ctx, origin.Client(), origin.URL, session)
	}
	state, err := svc.StartDraft("browser", func(_ context.Context, credentials map[string]any) (*AccountCredentialDraft, error) {
		require.Equal(t, "fixture-refresh", credentials["refresh_token"])
		return &AccountCredentialDraft{Reference: "fixture-draft"}, nil
	}, target.URL)
	require.NoError(t, err)
	finished, err := svc.PollBound(context.Background(), state.ID, "browser", false)
	require.NoError(t, err)
	require.Equal(t, "ready", finished.Status)
	require.EqualValues(t, 1, proxied.Load())
	require.Zero(t, direct.Load())
	require.Empty(t, cache.bindings)
}
