//go:build unit

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type reasoningReceiptRepository struct {
	interruptedWSReceiptRepository
}

func (repository *reasoningReceiptRepository) Settle(ctx context.Context, key service.UsageSettlementKey, fingerprint string) (*service.UsageSettlementResult, error) {
	result, err := repository.interruptedWSReceiptRepository.Settle(ctx, key, fingerprint)
	if result != nil {
		result.Billing = &service.UsageBillingApplyResult{}
	}
	return result, err
}

func TestGeminiReasoningEffortThroughHandlerPlanAndReceipt(t *testing.T) {
	for _, stream := range []bool{false, true} {
		mode := "buffered"
		if stream {
			mode = "streaming"
		}
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			cfg.Default.RateMultiplier = 1
			inputPrice, outputPrice := 0.25, 0.5 // Synthetic fixture prices, not catalog facts.
			group := &service.Group{ID: 3, Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1,
				ModelPricing: []service.ChannelModelPricing{{Models: []string{"gemini-2.5-flash"}, BillingMode: service.BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}}}
			account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
				Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: group.ID}}})
			accounts := []*service.Account{&account}
			handler, cleanup := newTestGatewayHandler(t, group, accounts)
			t.Cleanup(cleanup)
			handler.cfg = cfg
			upstream := &requestPlanHTTPFixture{t: t, gemini: true, stream: stream}
			receipts := &reasoningReceiptRepository{interruptedWSReceiptRepository: interruptedWSReceiptRepository{receipts: make(chan *service.UsageSettlementReceipt, 2)}}
			billing := service.NewBillingService(cfg, nil)
			snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
			handler.gatewayService = service.ProvideGatewayService(nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, cfg, snapshot, nil, billing, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
			handler.geminiCompatService = service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg)
			action := "generateContent"
			if stream {
				action = "streamGenerateContent"
			}
			client, recorder := requestPlanHTTPContext("/v1beta/models/gemini-2.5-flash:"+action,
				`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"HIGH"}}}`, group)
			client.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-flash:" + action}}
			handler.GeminiV1BetaModels(client)
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			require.Equal(t, 1, upstream.calls)
			require.NotNil(t, upstream.plan)
			require.Len(t, upstream.plan.Attempts(), 1)
			require.True(t, upstream.plan.State().Finalized)
			select {
			case receipt := <-receipts.receipts:
				require.NotNil(t, receipt.Usage.ReasoningEffort)
				require.Equal(t, "high", *receipt.Usage.ReasoningEffort)
				require.InDelta(t, 1, receipt.Usage.TotalCost, 1e-12)
				require.Equal(t, service.UsageSettlementSettled, upstream.plan.State().ReceiptState)
				require.Equal(t, receipt.Key, upstream.plan.State().ReceiptKey)
			case <-time.After(time.Second):
				t.Fatal("handler did not persist the observed usage receipt")
			}
		})
	}
}
