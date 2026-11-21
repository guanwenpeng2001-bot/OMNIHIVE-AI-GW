//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/policy"
	"github.com/stretchr/testify/require"
)

type testDenyNativeReader struct{ seen *policy.Layer }

func (r *testDenyNativeReader) NativeRequestAccounts(ctx context.Context, a scope.Authorized, model string, request *policy.Layer) (map[int64]bool, error) {
	r.seen = request
	return map[int64]bool{}, nil
}

func TestRequestRoutingConstraintsAcrossNativeExecutions(t *testing.T) {
	cfg := &config.Config{}
	reader := &testDenyNativeReader{}
	nativePolicyStores.Store(cfg, reader)
	t.Cleanup(func() { nativePolicyStores.Delete(cfg) })
	auth := scope.With(t.Context(), scope.Authorized{WorkspaceID: 1, ProjectID: 2, ActorID: 3})
	headers := http.Header{}
	headers.Set("X-OmniHive-Regions", "EU, US")
	headers.Set("X-OmniHive-Retention", "strict_zero")
	headers.Set("X-OmniHive-Training", "prohibited")
	parent, err := WithGatewayRoutingHeaders(auth, headers)
	require.NoError(t, err)
	gateway := &OpenAIGatewayService{cfg: cfg}
	spec := GatewayRequestSpec{Model: "model"}
	for _, kind := range []string{"http", "websocket", "async_job"} {
		t.Run(kind, func(t *testing.T) {
			ctx := parent
			switch kind {
			case "http":
				ctx, _ = gateway.CaptureRequestPlan(ctx, spec)
			case "websocket":
				ctx, _ = gateway.CaptureRequestTurn(ctx, spec)
			case "async_job":
				var cancel context.CancelFunc
				ctx, cancel = gateway.CaptureAsyncImageTaskPlan(ctx, spec, time.Minute)
				defer cancel()
			}
			selection, _, err := gateway.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "model", nil, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityChatCompletions, false, false, true, PlatformOpenAI)
			require.ErrorIs(t, err, policy.NativePolicyDenied)
			require.Nil(t, selection)
			require.NotNil(t, reader.seen)
			require.Equal(t, []string{"EU", "US"}, reader.seen.Definition.Constraints.Regions.Values)
			require.Equal(t, "strict_zero", reader.seen.Definition.Constraints.Retention)
			require.Equal(t, "prohibited", reader.seen.Definition.Constraints.Training)
			reader.seen = nil
		})
	}
}

func TestAnthropicBridgeRejectsBeforeUpstreamSend(t *testing.T) {
	for _, tc := range []struct{ tools, path string }{
		{`"tools":[{"name":"search","type":"web_search_20250305"}]`, "tools[0].type"},
		{`"tools":[{"name":"good","input_schema":{"type":"object"}},{"name":"server","type":"computer_20250124"}]`, "tools[1].type"},
		{`"tool_choice":{"type":"tool","name":"missing"}`, "tool_choice.name"},
	} {
		body := `{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}],` + tc.tools + "}"
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		upstream := &httpUpstreamRecorder{}
		gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		result, err := gateway.ForwardAsAnthropic(c.Request.Context(), c, forceChatMessagesFallbackAccount(), []byte(body), "", "")
		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, 400, rec.Code)
		require.Contains(t, rec.Body.String(), tc.path)
		require.Nil(t, upstream.lastReq)
	}
}
