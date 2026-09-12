package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// This fetcher records the actual Anthropic request host if the forbidden
// provider is reached, without transmitting credentials or opening a socket.
type cursorUsageOutboundRecorder struct{ hosts []string }

func (f *cursorUsageOutboundRecorder) FetchUsage(ctx context.Context, token, proxy string) (*ClaudeUsageResponse, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	f.hosts = append(f.hosts, req.URL.Hostname())
	return nil, fmt.Errorf("unexpected upstream usage request")
}
func (f *cursorUsageOutboundRecorder) FetchUsageWithOptions(ctx context.Context, opts *ClaudeUsageFetchOptions) (*ClaudeUsageResponse, error) {
	return f.FetchUsage(ctx, opts.AccessToken, opts.ProxyURL)
}

func TestCursorUsageUnsupportedWithoutAnthropicRequests(t *testing.T) {
	account := Account{ID: 701, Platform: PlatformCursor, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "cursor-test-secret"}}
	outbound := &cursorUsageOutboundRecorder{}
	svc := &AccountUsageService{accountRepo: &stubOpenAIAccountRepo{accounts: []Account{account}}, usageFetcher: outbound, cache: NewUsageCache()}
	assertUnsupported := func(info *UsageInfo) {
		t.Helper()
		require.NotNil(t, info)
		require.Equal(t, "unsupported", info.ErrorCode)
		require.Contains(t, info.Error, "Cursor")
		require.Nil(t, info.FiveHour)
		require.Nil(t, info.SevenDay)
		require.Nil(t, info.ThirtyDay)
	}
	for _, force := range []bool{false, true} {
		info, err := svc.GetUsage(context.Background(), account.ID, force)
		require.NoError(t, err)
		assertUnsupported(info)
		infos, errs, err := svc.GetUsageBatch(context.Background(), []int64{account.ID}, force)
		require.NoError(t, err)
		require.Empty(t, errs)
		assertUnsupported(infos[account.ID])
	}
	_, err := svc.fetchOAuthUsageRaw(context.Background(), &account)
	require.ErrorContains(t, err, "cursor accounts cannot use Anthropic usage")
	require.NotContains(t, outbound.hosts, "api.anthropic.com")
	require.Empty(t, outbound.hosts, "Cursor usage must not make any upstream request")
}

func TestCursorCountTokensAnthropicDefense(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			account := &Account{ID: 702, Platform: PlatformCursor, Type: accountType, Credentials: map[string]any{"access_token": "cursor-test-secret", "api_key": "cursor-test-secret"}}
			upstream := &anthropicHTTPUpstreamRecorder{}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			token, kind, err := svc.GetAccessToken(context.Background(), account)
			require.Error(t, err)
			require.Empty(t, token)
			require.Empty(t, kind)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
			body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
			parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-6"}
			require.Error(t, svc.ForwardCountTokens(context.Background(), c, account, parsed))
			require.GreaterOrEqual(t, rec.Code, 400)
			require.NotContains(t, rec.Body.String(), "cursor-test-secret")
			req, _, err := svc.buildCountTokensRequest(context.Background(), c, account, body, "cursor-test-secret", "oauth", parsed.Model, false)
			require.Error(t, err)
			require.Nil(t, req)
			req, err = svc.buildCountTokensRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "cursor-test-secret")
			require.Error(t, err)
			require.Nil(t, req)
			req, _, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "cursor-test-secret", "oauth", parsed.Model, false, false)
			require.Error(t, err)
			require.Nil(t, req)
			if upstream.lastReq != nil {
				require.NotEqual(t, "api.anthropic.com", upstream.lastReq.URL.Hostname())
			}
			require.Nil(t, upstream.lastReq, "Cursor account must never reach the Anthropic transport")
		})
	}
}
