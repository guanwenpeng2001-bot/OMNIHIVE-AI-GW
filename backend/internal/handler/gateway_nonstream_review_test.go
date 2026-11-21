//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNonstreamAcceptedFailureReview(t *testing.T) {
	for _, tc := range []struct{ protocol, path, target string }{
		{domain.APIProtocolAnthropic, "/v1/messages", "/messages"},
		{domain.APIProtocolChatCompletions, "/v1/messages", "/chat/completions"},
		{domain.APIProtocolChatCompletions, "/v1/responses", "/chat/completions"},
	} {
		t.Run(tc.protocol+tc.path, func(t *testing.T) {
			restore := domain.SetCompatibleProvidersForTest([]domain.CompatibleProvider{compatibleReviewProvider(tc.protocol, "bearer")})
			defer restore()
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !strings.HasSuffix(r.URL.Path, tc.target) {
					t.Errorf("unexpected upstream path %s", r.URL.Path)
				}
				if tc.protocol == domain.APIProtocolChatCompletions {
					_, _ = w.Write([]byte(`{"choices":`))
					return
				}
				w.Header().Set("Content-Length", "100")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
			}))
			defer upstream.Close()
			handler, group, receipts, _ := compatibleReviewHandler(t, upstream.URL+"/v1")
			client, rec := requestPlanHTTPContext(tc.path, `{"model":"fixture-model","input":"hi","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`, group)
			if tc.path == "/v1/messages" {
				handler.Messages(client)
			} else {
				handler.Responses(client)
			}
			require.Equal(t, 502, rec.Code, rec.Body.String())
			require.EqualValues(t, 1, calls.Load())
			requireReviewOnly(t, receipts)
		})
	}
}

func requireReviewOnly(t *testing.T, receipts *hunyuanSTTReceiptRepository) {
	t.Helper()
	require.Len(t, receipts.receipts, 1)
	for _, receipt := range receipts.receipts {
		require.Equal(t, service.UsageSettlementNeedsReview, receipt.State)
		require.Equal(t, "unresolved", receipt.ResolutionState)
	}
	require.Zero(t, receipts.debit)
	require.Zero(t, receipts.settles)
}

func nativeMessagesReviewHandler(t *testing.T, base string) (*GatewayHandler, *service.Group, *hunyuanSTTReceiptRepository) {
	t.Helper()
	cfg := compatibleReviewConfig()
	accounts := &heartbeatAccountRepository{platform: service.PlatformAnthropic}
	for _, id := range []int64{7, 8} {
		account := compatibleReviewAccount(base)
		account.Platform = service.PlatformAnthropic
		account.ID = id
		accounts.accounts = append(accounts.accounts, account)
	}
	group := &service.Group{ID: 3, Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil)
	t.Cleanup(billing.Stop)
	receipts := newHunyuanSTTReceiptRepository()
	gateway := service.ProvideGatewayService(accounts, &heartbeatGroupRepository{group: group}, &cursorRouteUsageRepo{}, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), service.NewRateLimitService(accounts, nil, cfg, nil, nil), billing, nil, repository.NewHTTPUpstream(cfg), &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(receipts, nil, nil))
	handler := NewGatewayHandler(gateway, nil, nil, nil, nil, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, nil, nil, nil, nil, nil, cfg, nil)
	return handler, group, receipts
}

func TestNativeMessagesAcceptedEOFReview(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected upstream path %s", r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer upstream.Close()
	handler, group, receipts := nativeMessagesReviewHandler(t, upstream.URL)
	client, rec := requestPlanHTTPContext("/v1/messages", `{"model":"fixture-model","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`, group)
	handler.Messages(client)
	require.Equal(t, 502, rec.Code, rec.Body.String())
	requireReviewOnly(t, receipts)
}
