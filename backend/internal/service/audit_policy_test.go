//go:build unit

package service

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"net/http/httptest"
	"testing"
)

func auditPolicyContext(platform string) (*gin.Context, *Group) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	group := &Group{Platform: platform, MaxReasoningEffort: "low", MaxReasoningEffortOverLimit: ReasoningEffortOverLimitDowngrade}
	c.Set("api_key", &APIKey{Group: group})
	return c, group
}

func TestNativeReasoningPolicyCapabilities(t *testing.T) {
	c, group := auditPolicyContext(PlatformQianwen)
	for _, path := range []string{"enable_thinking", "parameters.enable_thinking"} {
		body := []byte(`{"enable_thinking":true,"parameters":{"enable_thinking":true}}`)
		updated, mode, err := prepareQianwenThinkingWithPolicy(context.Background(), c, body, body, "qwen-turbo", path)
		require.NoError(t, err)
		require.False(t, *mode)
		require.False(t, gjson.GetBytes(updated, path).Bool())
	}
	group.MaxReasoningEffortOverLimit = ReasoningEffortOverLimitDeny
	_, _, err := prepareQianwenThinkingWithPolicy(context.Background(), c, []byte(`{"enable_thinking":true}`), []byte(`{}`), "qwen-plus", "enable_thinking")
	require.True(t, IsReasoningEffortPolicyDenied(err))
	// Unknown models are not assigned a binary capability by platform alone.
	_, mode, err := prepareQianwenThinkingWithPolicy(context.Background(), c, []byte(`{"enable_thinking":true}`), []byte(`{}`), "unknown-model", "enable_thinking")
	require.NoError(t, err)
	require.True(t, *mode)
	c, group = auditPolicyContext(PlatformCommandCode)
	body := []byte(`{"model":"moonshotai/kimi-k2.7-code-highspeed","reasoning_effort":"high"}`)
	updated, err := normalizeCommandCodeReasoningWithPolicy(c, body, "moonshotai/kimi-k2.7-code-highspeed")
	require.NoError(t, err)
	require.Equal(t, "low", gjson.GetBytes(updated, "reasoning_effort").String())
	group.ReasoningEffortMappings = []ReasoningEffortMapping{{From: "high", To: "deny"}}
	_, err = normalizeCommandCodeReasoningWithPolicy(c, body, "moonshotai/kimi-k2.7-code-highspeed")
	require.True(t, IsReasoningEffortPolicyDenied(err))
	updated, err = normalizeCommandCodeReasoningWithPolicy(c, body, "unknown")
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(updated, "reasoning_effort").Exists())
	for _, platform := range []string{PlatformQianwen, PlatformCommandCode} {
		_, err := normalizeMaxReasoningEffortForPlatform(platform, "low")
		require.NoError(t, err)
		_, err = normalizeMaxReasoningEffortForPlatform(platform, "medium")
		require.Error(t, err)
	}
	_, err = normalizeMaxReasoningEffortForPlatform(PlatformGrok, "low")
	require.Error(t, err)
}

func TestExplicitFreeCardAndMissingNativePrice(t *testing.T) {
	bs := newTestBillingServiceForResolver()
	resolver := NewModelPricingResolver(nil, bs)
	group := &Group{Platform: PlatformCommandCode}
	for _, model := range []string{"inclusionai/ling-3.0-flash-sante:free", "meituan/LongCat-2.0:free", "poolside/laguna-s-2.1-free"} {
		cost, err := bs.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: model, Group: group, Resolver: resolver, Tokens: UsageTokens{InputTokens: 100, OutputTokens: 10}, RateMultiplier: 1})
		require.NoError(t, err)
		require.Zero(t, cost.TotalCost)
		require.Equal(t, "official_free:commandcode", cost.PriceSource)
		require.NotEmpty(t, cost.PriceCardVersion)
	}
	require.False(t, commandCodeOfficialFreeModel("invented:free"))
	c, _ := auditPolicyContext(PlatformQianwen)
	svc := &OpenAIGatewayService{resolver: resolver}
	mode := false
	err := svc.validateQianwenThinkingMode(context.Background(), c, "unknown-unpriced-model", &mode)
	require.ErrorContains(t, err, "model_pricing_unavailable")
}

func TestQianwenNativePolicyDenialPrecedesUpstream(t *testing.T) {
	c, group := auditPolicyContext(PlatformQianwen)
	group.MaxReasoningEffortOverLimit = ReasoningEffortOverLimitDeny
	svc := &OpenAIGatewayService{}
	_, err := svc.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeText, []byte(`{"model":"qwen-plus","input":{"prompt":"hi"},"parameters":{"enable_thinking":true}}`), false)
	require.True(t, IsReasoningEffortPolicyDenied(err))
	require.Equal(t, 403, c.Writer.Status())
}
func TestCompositeCeilingUsesSupportedNativeLevel(t *testing.T) {
	c, group := auditPolicyContext(PlatformComposite)
	group.MaxReasoningEffort = "medium"
	body := []byte(`{"model":"moonshotai/kimi-k2.7-code-highspeed","reasoning_effort":"high"}`)
	result, err := normalizeCommandCodeReasoningWithPolicy(c, body, "moonshotai/kimi-k2.7-code-highspeed")
	require.NoError(t, err)
	require.Equal(t, "low", gjson.GetBytes(result, "reasoning_effort").String())
}

func TestMissingNativePriceHTTPErrorBeforeConsumption(t *testing.T) {
	for _, platform := range []string{PlatformQianwen, PlatformCursor} {
		t.Run(platform, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("api_key", &APIKey{Group: &Group{Platform: platform}})
			var err error
			if platform == PlatformQianwen {
				svc := &OpenAIGatewayService{resolver: NewModelPricingResolver(nil, newTestBillingServiceForResolver())}
				_, err = svc.ForwardQianwenNative(context.Background(), c, qianwenTestAccount(), QianwenNativeText, []byte(`{"model":"unknown-unpriced-model","input":{"prompt":"hi"}}`), false)
			} else {
				svc := &CursorGatewayService{}
				_, err = svc.ForwardAsChatCompletions(context.Background(), c, &Account{Platform: PlatformCursor, Type: AccountTypeAPIKey}, []byte(`{"model":"echo-model","messages":[{"role":"user","content":"hi"}]}`))
			}
			require.Error(t, err)
			require.Equal(t, 400, recorder.Code)
			require.Equal(t, "model_pricing_unavailable", gjson.Get(recorder.Body.String(), "error.type").String())
			t.Logf("local HTTP recorder: platform=%s status=%d response=%s", platform, recorder.Code, recorder.Body.String())
		})
	}
}
