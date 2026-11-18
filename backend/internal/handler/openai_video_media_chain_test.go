//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// This suite is the positive half of the OpenAI video cell: the published matrix
// adapter is dispatched on the real route, the local stub is called exactly
// once, the owner reads state and content from durable receipts, and the shared
// job trunk settles exactly one charge. The ingress fixture in
// openai_media_ingress_test.go keeps the negative half (an account without video
// evidence stays refused), and this file never weakens it.

// openAIVideoMediaChainAccount carries the catalogue evidence the OpenAI video
// projection publishes for a video task (service/account_model_catalog.go): the
// model is eligible on the media family.
func openAIVideoMediaChainAccount(baseURL string) service.Account {
	account := openAIMediaIngressAccount(baseURL)
	account.Extra[service.UpstreamModelCatalogExtraKey] = service.AccountModelCatalogSnapshot{
		Complete: true, Revision: 1, DiscoveredModels: []string{"sora-2", "whisper-1"},
		EligibleModels: []service.AccountCatalogModel{
			{ID: "sora-2", Endpoints: []string{service.CompositeRouteEndpointImages}, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-22T00:00:00Z"},
			{ID: "whisper-1", Endpoints: []string{service.CompositeRouteEndpointChatCompletions}, EvidenceKind: "fixture", EvidenceObservedAt: "2026-09-22T00:00:00Z"},
		},
	}
	return account
}

// newOpenAIVideoMediaChainHandler mirrors the media ingress wiring with the
// video-capable catalogue above. It is duplicated instead of parameterising the
// ingress fixture so the fail-closed contract in that file stays untouched.
func newOpenAIVideoMediaChainHandler(t *testing.T, server *httptest.Server) (*OpenAIGatewayHandler, *gatewayResourceWiringRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := newGatewayResourceWiringRepo()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	concurrency := service.NewConcurrencyService(&grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}})
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	gateway := service.ProvideOpenAIGatewayService(
		openAIImagesFailoverAccountRepo{accounts: []service.Account{openAIVideoMediaChainAccount(server.URL)}},
		nil, nil, nil, nil, nil, nil, cfg, nil, concurrency,
		service.NewBillingService(cfg, nil), nil, nil,
		gatewayResourceWiringUpstream{client: server.Client(), beforeSend: func(request *http.Request) {
			if request.Method == http.MethodPost && request.URL.Path == "/v1/videos" {
				requireGatewaySubmissionPlan(t, request, "sora-2")
			}
		}},
		nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(&unlimitedTPMReceiptStub{}, nil, nil))
	openAI := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	openAI.upstreamJobs = service.NewUpstreamJobService(repo, service.GatewayResourcePollJob(gateway), service.GatewayResourceJobCharge)
	return openAI, repo
}

// openAIVideoMediaChainAdapter resolves the adapter the published matrix binds
// for one video action. The routes table registers the same key for the real
// ingress, so a cell that loses its adapter (or a handler wired under a key the
// matrix does not name) fails this suite instead of passing through a
// hand-written dispatch key.
func openAIVideoMediaChainAdapter(t *testing.T, action string) string {
	t.Helper()
	cell := domain.PlatformAction(domain.ActionIdentity{Platform: service.PlatformOpenAI, HostedAccountType: service.AccountTypeAPIKey}, action)
	require.Equal(t, domain.ActionImplemented, cell.Status, "the video cell is published as implemented: %+v", cell)
	require.NotEmpty(t, cell.Adapter, "an implemented cell must name its adapter: %+v", cell)
	return cell.Adapter
}

// TestOpenAIVideoMediaChainReachesStubOnceThenServesStateAndSettlement drives one
// full lifecycle over the real dispatch table: submit, the shared poller's
// settlement, then owner-bound state and content reads. The stub call counts are
// the closure proof: a refused submission (the pre-fix behaviour) records zero
// calls and never reaches the assertions below. The poller call stands in for
// the shared timing wheel; no client read triggers upstream work.
func TestOpenAIVideoMediaChainReachesStubOnceThenServesStateAndSettlement(t *testing.T) {
	fixture := &openAIMediaIngressFixture{}
	server := fixture.serve(t)
	defer server.Close()
	openAI, repo := newOpenAIVideoMediaChainHandler(t, server)

	dispatch := service.NewGatewayActionDispatch(service.GatewayActionHandlers{
		openAIVideoMediaChainAdapter(t, "video.generate"): openAI.OpenAIVideoGeneration,
		openAIVideoMediaChainAdapter(t, "video.status"):   openAI.OpenAIVideoStatus,
		openAIVideoMediaChainAdapter(t, "video.content"):  openAI.OpenAIVideoContent,
	})

	// The published route for this action is POST /v1/videos.
	recorder := openAIMediaIngressRequest(t, dispatch.Handler("video.generate"), http.MethodPost, "/v1/videos", "/v1/videos",
		[]byte(`{"model":"sora-2","prompt":"waves","size":"1280x720","seconds":"8"}`), "application/json")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var submitted map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &submitted))
	publicID, _ := submitted["id"].(string)
	require.NotEmpty(t, publicID)
	require.Equal(t, "video", submitted["object"])
	require.Equal(t, "queued", submitted["status"])
	require.Equal(t, "sora-2", submitted["model"])
	require.Equal(t, 1, countOpenAIMediaCalls(fixture.calls(), "POST /v1/videos"), "an admitted submission reaches the local stub exactly once")
	require.Len(t, fixture.calls(), 1, "submission performs no other upstream work")
	t.Logf("submit: stub calls=%v", fixture.calls())

	stored := repo.snapshot(publicID)
	require.NotNil(t, stored)
	require.Equal(t, "video_upstream", stored.UpstreamTaskID)
	require.Equal(t, "submitted", stored.Status)
	require.Equal(t, "sora-2", stored.Billing.Model)
	require.Equal(t, "720p", stored.Billing.Resolution)
	require.Equal(t, 0.1, stored.Billing.UnitPrice)
	require.Nil(t, stored.ChargedUsageLogID, "an unfinished job carries no charge")

	// The shared trunk polls the observed upstream state and settles it exactly
	// once from the frozen admission tariff.
	repo.dueNow(publicID)
	polled, err := openAI.upstreamJobs.Poll(context.Background(), publicID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", polled.Status)
	require.Equal(t, 1, countOpenAIMediaCalls(fixture.calls(), "GET /v1/videos/video_upstream"), "the poll reads the upstream job exactly once")
	require.Equal(t, 1, repo.charges, "the completed job settles exactly one charge")
	require.NotNil(t, polled.ChargedUsageLogID)
	t.Logf("settle: stub calls=%v charges=%d", fixture.calls(), repo.charges)

	// The owner reads the settled receipt through GET /v1/videos/:request_id.
	recorder = openAIMediaIngressRequest(t, dispatch.Handler("video.status"), http.MethodGet, "/v1/videos/:request_id", "/v1/videos/"+publicID, nil, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var read map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &read))
	require.Equal(t, publicID, read["id"])
	require.Equal(t, "video", read["object"])
	require.Equal(t, "completed", read["status"])
	require.Equal(t, "sora-2", read["model"])
	require.Equal(t, "8", read["seconds"])
	require.Equal(t, "1280x720", read["size"])
	require.NotContains(t, recorder.Body.String(), "video_upstream")
	require.Equal(t, 1, countOpenAIMediaCalls(fixture.calls(), "GET /v1/videos/video_upstream"), "a status read never polls the upstream")
	require.Equal(t, 1, repo.charges)

	// The owner streams the settled content through the content route.
	recorder = openAIMediaIngressRequest(t, dispatch.Handler("video.content"), http.MethodGet, "/v1/videos/:request_id/content", "/v1/videos/"+publicID+"/content", nil, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "mp4-bytes", recorder.Body.String())
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, 1, countOpenAIMediaCalls(fixture.calls(), "GET /v1/videos/video_upstream/content"), "the content read reaches the stub exactly once")
	require.Equal(t, 1, countOpenAIMediaCalls(fixture.calls(), "GET /v1/videos/video_upstream"), "a content read never re-polls the job")
	require.Equal(t, 1, repo.charges, "reading content never charges a second time")
	t.Logf("reads: stub calls=%v charges=%d", fixture.calls(), repo.charges)
}
