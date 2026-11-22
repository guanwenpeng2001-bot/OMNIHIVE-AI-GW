//go:build unit

package service

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAntigravityImageArtifactReceipt(t *testing.T) {
	const model = "gemini-2.5-flash-image"
	const text = `data:{"response":{"candidates":[{"content":{"parts":[{"text":"no image"}]}}]}}` + "\n\n"
	const image = `data:{"response":{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"YQ=="}},{"inlineData":{"mimeType":"image/png","data":"Yg=="}},{"inlineData":{"mimeType":"image/png","data":"**"}}]}}]}}` + "\n\n"
	const stop = `data:{"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2}}}` + "\n\n"
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw"}]}]}`)
	price := 0.25
	billing, resolver := newTokenCostTestEnv(t, PlatformGemini, []ChannelModelPricing{{
		Platform: PlatformGemini, Models: []string{model}, BillingMode: BillingModeImage, PerRequestPrice: &price,
	}}, nil)
	gateway := &GatewayService{billingService: billing, resolver: resolver}
	key := &APIKey{ID: 1, Group: &Group{ID: 100, Platform: PlatformGemini}}
	account := antigravityImageAccount(map[string]any{model: model})
	for _, tc := range []struct {
		name, wire string
		count      int
		partial    bool
	}{
		{"empty", "", 0, true}, {"text", text + stop, 0, false}, {"truncated", text, 0, true},
		{"partial_images", image, 2, true}, {"repeated_artifacts", image + image + stop, 2, false},
	} {
		for _, stream := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				c, _ := newAntigravityImageContext(t, "/v1beta/models/"+model+":streamGenerateContent", body)
				upstream := &httpUpstreamStub{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.wire))}}
				result, err := newAntigravityImageService(upstream).ForwardGemini(t.Context(), c, account, model, "generateContent", stream, body, false)
				if !stream && tc.partial {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.NotNil(t, result)
				require.Equal(t, tc.count, result.ImageCount)
				require.Equal(t, tc.partial, result.UsageIncomplete)
				cost := gateway.calculateRecordUsageCost(t.Context(), result, key, model, 1, 1, time.Now())
				require.Equal(t, PricingStatePriced, cost.PricingState)
				require.Equal(t, float64(tc.count)*price, cost.ActualCost)
				repo := &settlementCapture{}
				params := &postUsageBillingParams{Cost: cost, User: &User{ID: 2}, APIKey: key, Account: account, AccountRateMultiplier: 1, UsageIncomplete: result.UsageIncomplete}
				log := &UsageLog{RequestID: "image", UserID: 2, APIKeyID: key.ID, AccountID: account.ID, Model: model, ImageCount: result.ImageCount, Stream: stream, TotalCost: cost.TotalCost, ActualCost: cost.ActualCost}
				err = submitUsageSettlement(t.Context(), NewUsageSettlementService(repo, nil, nil), log, params, &billingDeps{}, false)
				if tc.partial {
					require.ErrorIs(t, err, ErrUsageSettlementNeedsReview)
					require.Equal(t, UsageSettlementNeedsReview, repo.receipt.State)
					require.Zero(t, repo.settles)
				} else {
					require.NoError(t, err)
					require.Equal(t, 1, repo.settles)
				}
			})
		}
	}
}
