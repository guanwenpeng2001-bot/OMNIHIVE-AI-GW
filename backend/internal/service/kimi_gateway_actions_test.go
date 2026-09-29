package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKimiSearchDispatchAndBillingFacts(t *testing.T) {
	for _, scenario := range []struct {
		path, response string
		calls          int
	}{
		{"/v1/tools/search", `{"search_results":[{"url":"https://example.com"}]}`, 1},
		{"/v1/tools/search_pro", `{"search_results":[]}`, 0},
	} {
		upstream := &kimiActionsUpstreamStub{do: func(request *http.Request) (*http.Response, error) {
			require.Equal(t, scenario.path, request.URL.Path)
			return kimiSearchHTTPResponse(http.StatusOK, scenario.response, nil), nil
		}}
		gateway := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
		called := false
		dispatch := NewGatewayActionDispatch(GatewayActionHandlers{"kimi.search": func(requestContext *gin.Context) {
			called = true
			search, usage, err := gateway.SearchKimi(requestContext.Request.Context(), kimiSearchPayGAccount(), KimiWebSearchRequest{TextQuery: "fixture", Pro: scenario.path == "/v1/tools/search_pro"})
			require.NoError(t, err)
			require.Equal(t, scenario.calls, usage.WebSearchCalls)
			requestContext.Data(http.StatusOK, "application/json", search.RawBody)
		}})
		router := gin.New()
		router.Use(func(requestContext *gin.Context) {
			requestContext.Set("api_key", &APIKey{Group: &Group{Platform: PlatformKimi}})
		})
		router.POST(scenario.path, GatewayActionRequirement, dispatch.Handler("search"))
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, scenario.path, nil))
		require.True(t, called)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.JSONEq(t, scenario.response, recorder.Body.String())
		require.Len(t, upstream.requests, 1)
	}
}

func TestKimiGatewayRejectsCodingWithoutUpstream(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{}
	gateway := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	account := kimiSearchPayGAccount()
	account.Credentials["account_mode"] = "coding"
	_, usage, err := gateway.SearchKimi(context.Background(), account, KimiWebSearchRequest{TextQuery: "fixture"})
	require.ErrorContains(t, err, "KIMI_SEARCH_UNVERIFIED_ACCOUNT_MODE")
	require.Nil(t, usage)
	require.Empty(t, upstream.requests)
}

func TestKimiGatewayAttemptBudgetAndReceiptIdentity(t *testing.T) {
	upstream := &kimiActionsUpstreamStub{do: func(request *http.Request) (*http.Response, error) {
		return kimiSearchHTTPResponse(http.StatusOK, `{"search_results":[{"url":"https://example.com"}]}`, map[string]string{"X-Msh-Track-Id": "reused-client-track"}), nil
	}}
	gateway := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	requestContext, cancel := WithAttemptBudget(context.Background(), 2, time.Minute)
	defer cancel()
	request := KimiWebSearchRequest{TextQuery: "fixture", TrackID: "reused-client-track"}
	_, firstUsage, err := gateway.SearchKimi(requestContext, kimiSearchPayGAccount(), request)
	require.NoError(t, err)
	_, secondUsage, err := gateway.SearchKimi(requestContext, kimiSearchPayGAccount(), request)
	require.NoError(t, err)
	require.NotEqual(t, firstUsage.RequestID, secondUsage.RequestID)
	require.True(t, isForcedUsageBillingRequestID(firstUsage.RequestID))
	_, usage, err := gateway.SearchKimi(requestContext, kimiSearchPayGAccount(), request)
	require.ErrorIs(t, err, ErrAttemptBudgetExhausted)
	require.Nil(t, usage)
	require.Len(t, upstream.requests, 2)
}

func TestVideoCancellationRouteRequirement(t *testing.T) {
	for _, prefix := range []string{"", "/v1"} {
		require.Equal(t, "video.cancel", GatewayActionForRoute(http.MethodPost, prefix+"/videos/job/cancel"))
		require.Equal(t, "video.status", GatewayActionForRoute(http.MethodGet, prefix+"/videos/job"))
	}
}
