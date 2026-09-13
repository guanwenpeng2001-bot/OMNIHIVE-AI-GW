//go:build unit

package service

import (
	"context"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCursorEchoOperatorPriceAllowsForwardAndBilling(t *testing.T) {
	for _, requested := range []string{"echo-model", "diagnostic-alias"} {
		t.Run(requested, func(t *testing.T) {
			gateway, account, _ := cursorTestGateway()
			account.Credentials["model_mapping"] = map[string]any{requested: "echo-model"}
			gateway.catalog[cursorSessionKey(account)] = cursorCatalogEntry{models: []cursor.AvailableModel{{ID: "echo-model", RunSlug: "echo-model"}}, expires: time.Now().Add(time.Hour)}
			called := false
			gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, model string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
				called = true
				require.Equal(t, "echo-model", model)
				require.NoError(t, emit(cursor.StreamEvent{Type: "text", Text: "hello"}))
				return cursor.TokenUsage{InputTokens: 1000, OutputTokens: 100}, nil
			}}
			in, out := .5/1e6, 2.5/1e6
			group := &Group{Platform: PlatformComposite, ModelPricing: []ChannelModelPricing{{Platform: PlatformCursor, Models: []string{requested}, BillingMode: BillingModeToken, InputPrice: &in, OutputPrice: &out, NativePricingSource: "operator_peer_tier"}}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("api_key", &APIKey{Group: group})
			_, err := gateway.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"`+requested+`","messages":[{"role":"user","content":"hello"}]}`))
			require.NoError(t, err)
			require.True(t, called)
			bs := newTestBillingServiceForResolver()
			cost, err := bs.CalculateCostUnified(CostInput{Ctx: WithResolvedTargetPlatform(context.Background(), PlatformCursor), Model: requested, Group: group, Resolver: NewModelPricingResolver(nil, bs), Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 100}, RateMultiplier: 1})
			require.NoError(t, err)
			require.InDelta(t, .00075, cost.TotalCost, 1e-12)
			require.Contains(t, cost.PriceSource, "operator_peer_tier")
		})
	}
}

func TestCursorEchoRequiresUsablePriceForBillingName(t *testing.T) {
	in, out := .5/1e6, 2.5/1e6
	for _, tc := range []struct {
		name, platform, model string
		input                 *float64
	}{
		{"wrong provider", PlatformQianwen, "echo-model", &in},
		{"only upstream alias priced", PlatformCursor, "upstream-echo", &in},
		{"missing input", PlatformCursor, "echo-model", nil},
		{"nonfinite", PlatformCursor, "echo-model", func() *float64 { v := math.Inf(1); return &v }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformComposite, ModelPricing: []ChannelModelPricing{{Platform: tc.platform, Models: []string{tc.model}, InputPrice: tc.input, OutputPrice: &out}}}})
			require.False(t, cursorHasConfiguredEchoPrice(c, "echo-model"))
		})
	}
}
