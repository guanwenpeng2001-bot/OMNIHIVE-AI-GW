//go:build unit

package handler

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiPlanTransportFixture struct {
	requestPlanHTTPFixture
	failure    error
	accountIDs []int64
}

func (f *geminiPlanTransportFixture) Do(request *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	f.accountIDs = append(f.accountIDs, accountID)
	if len(f.accountIDs) == 1 {
		f.plan = service.GatewayRequestPlanFromContext(request.Context())
		require.NotNil(f.t, f.plan)
		return nil, f.failure
	}
	return f.requestPlanHTTPFixture.Do(request, proxy, accountID, concurrency)
}

func TestGeminiTransportFailurePreservesPlanAndReceiptBoundary(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		failure  error
		switches int
		succeeds bool
	}{
		{"unsent switches account", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, 1, true},
		{"unknown outcome cannot replay", errors.New("unexpected EOF"), 1, false},
		{"canceled cannot replay", context.Canceled, 1, false},
		{"exhausted budget cannot replay", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, 0, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.MaxAccountSwitches = scenario.switches
			group := &service.Group{ID: 3, Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true}
			accounts := make([]*service.Account, 0, 2)
			for _, accountID := range []int64{7, 8} {
				account := preparedGatewayFixtureAccount(service.Account{ID: accountID, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture"}, AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: 3}}})
				account.Extra[service.UpstreamModelCatalogExtraKey] = service.AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"gemini-2.5-flash"}, EligibleModels: []service.AccountCatalogModel{{ID: "gemini-2.5-flash", Endpoints: []string{"generate_content"}, EvidenceKind: "fixture"}}}
				accounts = append(accounts, &account)
			}
			handler, cleanup := newTestGatewayHandler(t, group, accounts)
			defer cleanup()
			handler.cfg = cfg
			handler.maxAccountSwitches = scenario.switches
			handler.maxAccountSwitchesGemini = 1
			usage := &cursorRouteUsageRepo{}
			receipts := newHunyuanSTTReceiptRepository()
			upstream := &geminiPlanTransportFixture{requestPlanHTTPFixture: requestPlanHTTPFixture{t: t, gemini: true}, failure: scenario.failure}
			snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
			handler.gatewayService = service.ProvideGatewayService(nil, &fakeGroupRepo{group: group}, usage, nil, nil, nil, nil, nil, cfg, snapshot, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
			handler.geminiCompatService = service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg)
			request, response := requestPlanHTTPContext("/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, group)
			request.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-flash:generateContent"}}
			budgetContext, cancelBudget := service.WithAttemptBudget(request.Request.Context(), scenario.switches+1, time.Minute)
			defer cancelBudget()
			request.Request = request.Request.WithContext(budgetContext)
			handler.GeminiV1BetaModels(request)
			require.NotNil(t, upstream.plan, response.Body.String())
			require.True(t, upstream.plan.State().Finalized)
			if scenario.succeeds {
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.Len(t, upstream.accountIDs, 2)
				require.NotEqual(t, upstream.accountIDs[0], upstream.accountIDs[1])
				require.Len(t, upstream.plan.Attempts(), 2)
				require.Equal(t, "not_sent", upstream.plan.Attempts()[0].Outcome)
				require.Len(t, receipts.receipts, 1)
				// The fixture has no approved price: retain usage for review instead of inventing a quote.
				require.Equal(t, service.UsageSettlementNeedsReview, upstream.plan.State().ReceiptState)
				for _, receipt := range receipts.receipts {
					require.Equal(t, 2, receipt.Usage.InputTokens)
					require.Equal(t, 1, receipt.Usage.OutputTokens)
				}
			} else {
				require.Len(t, upstream.accountIDs, 1, response.Body.String())
				require.Len(t, upstream.plan.Attempts(), 1)
				require.Empty(t, receipts.receipts)
			}
		})
	}
}
