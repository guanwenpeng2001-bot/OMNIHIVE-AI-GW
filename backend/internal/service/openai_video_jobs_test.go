//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Synthetic protocol fixtures derived from the official video guide and the
// OpenAPI VideoResource schema, checked 2026-09-21; these are not recordings
// from a credentialed upstream.

func openAIVideoTestAccount() *Account {
	return &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "sk-fixture", "base_url": "https://api.openai.com/v1"}}
}

func openAIVideoPriceGroup() *Group {
	return &Group{ID: 7, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 2, VideoModelPrices: map[string]map[string]float64{"sora-2": {"720p": 0.1}, "sora-2-pro": {"1080p": 0.5}}}
}

func openAIVideoJobBody(raw string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw))}
}

func TestOpenAIVideoBillingSnapshotFreezesDocumentedTariff(t *testing.T) {
	group := openAIVideoPriceGroup()
	snapshot, err := OpenAIVideoBillingSnapshot(group, nil, &APIKey{Quota: 100}, []byte(`{"model":"sora-2","prompt":"waves","size":"1280x720","seconds":"8"}`), nil, nil)
	require.NoError(t, err)
	require.Equal(t, "sora-2", snapshot.Model)
	require.Equal(t, "720p", snapshot.Resolution)
	require.Equal(t, 0.1, snapshot.UnitPrice)
	require.Equal(t, 2.0, snapshot.RateMultiplier)
	require.True(t, snapshot.KeyQuota)

	// Documented defaults: model sora-2, size 720x1280, 720p tier.
	defaulted, err := OpenAIVideoBillingSnapshot(group, nil, nil, []byte(`{"prompt":"waves"}`), nil, nil)
	require.NoError(t, err)
	require.Equal(t, "sora-2", defaulted.Model)
	require.Equal(t, "720p", defaulted.Resolution)

	pro, err := OpenAIVideoBillingSnapshot(group, nil, nil, []byte(`{"model":"sora-2-pro-2025-10-06","size":"1792x1024"}`), nil, nil)
	require.NoError(t, err)
	require.Equal(t, "1080p", pro.Resolution)
	require.Equal(t, 0.5, pro.UnitPrice)

	for _, body := range []string{`{"model":"sora-1"}`, `{"model":"sora-2","size":"1024x1024"}`, `{"model":"sora-2-pro","size":"720x1280"}`} {
		_, err = OpenAIVideoBillingSnapshot(group, nil, nil, []byte(body), nil, nil)
		require.Error(t, err, body)
	}
	_, err = OpenAIVideoBillingSnapshot(nil, nil, nil, []byte(`{"model":"sora-2"}`), nil, nil)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
	_, err = OpenAIVideoBillingSnapshot(&Group{RateMultiplier: 1}, nil, nil, []byte(`{"model":"sora-2"}`), nil, nil)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
}

func TestOpenAIVideoBillingSnapshotInheritsOwnerCheckedSource(t *testing.T) {
	group := openAIVideoPriceGroup()
	source := &UpstreamJob{Platform: PlatformOpenAI, Family: "video", Status: "succeeded", Billing: UpstreamJobBilling{Model: "sora-2-pro", RequestedModel: "sora-2-pro", Resolution: "1080p", UnitPrice: 0.01, RateMultiplier: 1}}
	snapshot, err := OpenAIVideoBillingSnapshot(group, nil, nil, []byte(`{"video":{"id":"video_src"},"prompt":"continue","seconds":"8"}`), nil, source)
	require.NoError(t, err)
	require.Equal(t, "sora-2-pro", snapshot.Model)
	require.Equal(t, "1080p", snapshot.Resolution)
	// Current group price is re-frozen; the stale source price is never reused.
	require.Equal(t, 0.5, snapshot.UnitPrice)
	require.Equal(t, 2.0, snapshot.RateMultiplier)

	for _, broken := range []*UpstreamJob{
		{Platform: PlatformGrok, Family: "video", Status: "succeeded"},
		{Platform: PlatformOpenAI, Family: "video", Status: "running"},
		{Platform: PlatformOpenAI, Family: "batch", Status: "succeeded"},
		{Platform: PlatformOpenAI, Family: "video", Status: "succeeded", Billing: UpstreamJobBilling{Model: "unknown"}},
	} {
		_, err = OpenAIVideoBillingSnapshot(group, nil, nil, []byte(`{}`), nil, broken)
		require.ErrorIs(t, err, ErrUpstreamJobTariff)
	}
}

func TestOpenAIVideoSubmitParsesJobAndRejectsAmbiguity(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: openAIVideoJobBody(`{"id":"video_abc","object":"video","status":"queued","model":"sora-2","progress":0,"seconds":"8","size":"1280x720"}`)}
	service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	update, err := service.SubmitOpenAIVideo(context.Background(), openAIVideoTestAccount(), "video.generate", "application/json", []byte(`{"model":"sora-2","prompt":"waves","size":"1280x720","seconds":"8"}`), "sora-2", 0)
	require.NoError(t, err)
	require.Equal(t, "video_abc", update.TaskID)
	require.Equal(t, "submitted", update.Status)
	require.Equal(t, "queued", update.ProviderStatus)
	require.Equal(t, "/v1/videos", upstream.lastReq.URL.Path)
	require.Equal(t, "Bearer sk-fixture", upstream.lastReq.Header.Get("Authorization"))
	require.Len(t, upstream.requests, 1)

	// Extension pins the billable segment; stitched totals must not double-charge.
	upstream.resp = openAIVideoJobBody(`{"id":"video_ext","object":"video","status":"in_progress","model":"sora-2","progress":10,"seconds":"16","size":"1280x720"}`)
	update, err = service.SubmitOpenAIVideo(context.Background(), openAIVideoTestAccount(), "video.extend", "application/json", []byte(`{"video":{"id":"video_abc"},"prompt":"continue","seconds":"8"}`), "sora-2", 8)
	require.NoError(t, err)
	require.Equal(t, "running", update.Status)
	require.Equal(t, "/v1/videos/extensions", upstream.lastReq.URL.Path)

	_, err = service.SubmitOpenAIVideo(context.Background(), openAIVideoTestAccount(), "video.extend", "application/json", []byte(`{"video":{"id":"video_abc"},"prompt":"continue","seconds":"7"}`), "sora-2", 7)
	require.Error(t, err)
	_, err = service.SubmitOpenAIVideo(context.Background(), openAIVideoTestAccount(), "video.status", "application/json", []byte(`{}`), "", 0)
	require.Error(t, err)
	require.Len(t, upstream.requests, 2, "local rejections must not reach the upstream")
}

func TestOpenAIVideoPollReportsProviderTruth(t *testing.T) {
	account := openAIVideoTestAccount()
	job := &UpstreamJob{Platform: PlatformOpenAI, Family: "video", AccountID: 42, UpstreamTaskID: "video_abc"}
	upstream := &httpUpstreamRecorder{resp: openAIVideoJobBody(`{"id":"video_abc","object":"video","status":"in_progress","model":"sora-2","progress":40,"seconds":"8","size":"1280x720"}`)}
	service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: openAIVideoAccountRepository{account: account}}

	update, err := service.PollOpenAIVideo(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, "running", update.Status)
	require.Equal(t, "/v1/videos/video_abc", upstream.lastReq.URL.Path)

	upstream.resp = openAIVideoJobBody(`{"id":"video_abc","object":"video","status":"completed","model":"sora-2","progress":100,"seconds":"8","size":"1280x720"}`)
	update, err = service.PollOpenAIVideo(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, "succeeded", update.Status)
	require.Equal(t, "video_abc", update.ResultRef)
	require.Equal(t, int64(8), gjson.GetBytes(update.Usage, "seconds").Int())
	require.Equal(t, "1280x720", gjson.GetBytes(update.Usage, "size").String())

	// Completed extension preserves the admission billable segment.
	job.UsageJSON = []byte(`{"billable_seconds":8}`)
	upstream.resp = openAIVideoJobBody(`{"id":"video_abc","object":"video","status":"completed","model":"sora-2","progress":100,"seconds":"16","size":"1280x720"}`)
	update, err = service.PollOpenAIVideo(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, int64(16), gjson.GetBytes(update.Usage, "seconds").Int())
	require.Equal(t, int64(8), gjson.GetBytes(update.Usage, "billable_seconds").Int())

	upstream.resp = openAIVideoJobBody(`{"id":"video_abc","object":"video","status":"failed","error":{"code":"content_policy","message":"blocked"}}`)
	update, err = service.PollOpenAIVideo(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, "failed", update.Status)
	require.Equal(t, "content_policy", update.ErrorCode)

	upstream.resp = openAIVideoJobBody(`{"id":"video_other","object":"video","status":"in_progress","model":"sora-2","progress":1,"seconds":"8","size":"1280x720"}`)
	update, err = service.PollOpenAIVideo(context.Background(), job)
	require.NoError(t, err, "task identity is compared by the shared job service, not hidden here")
	require.Equal(t, "video_other", update.TaskID)

	brokenAccount := openAIVideoTestAccount()
	brokenAccount.Status = "disabled"
	service.accountRepo = openAIVideoAccountRepository{account: brokenAccount}
	_, err = service.PollOpenAIVideo(context.Background(), job)
	require.Error(t, err, "disabled bound account must not keep serving the job")

	job.Platform = PlatformGrok
	_, err = service.PollOpenAIVideo(context.Background(), job)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
}

func TestOpenAIVideoJobChargeSettlesFrozenOfferOnly(t *testing.T) {
	job := &UpstreamJob{Platform: PlatformOpenAI, Family: "video", UserID: 1, APIKeyID: 2, AccountID: 42, PublicRequestID: "req-1", UpstreamTaskID: "video_abc", ResultRef: "video_abc", RequestFingerprint: "fp",
		Billing:   UpstreamJobBilling{Model: "sora-2", RequestedModel: "sora-2", Resolution: "720p", UnitPrice: 0.1, RateMultiplier: 2, AccountRateMultiplier: 1},
		UsageJSON: []byte(`{"seconds":8,"size":"1280x720","model":"sora-2"}`)}
	log, cmd, err := OpenAIVideoJobCharge(job)
	require.NoError(t, err)
	require.Equal(t, 0.8, log.TotalCost)
	require.Equal(t, 1.6, log.ActualCost)
	require.Equal(t, 2.0, log.RateMultiplier)
	require.Equal(t, 1, log.VideoCount)
	require.NotNil(t, log.VideoDurationSeconds)
	require.Equal(t, 8, *log.VideoDurationSeconds)
	require.Equal(t, "openai-video-v1", *log.PriceCardVersion)
	require.Equal(t, 1.6, cmd.BalanceCost)
	require.Equal(t, "video", cmd.MediaType)

	// Extension: only the new segment is billed, never the stitched total.
	job.UsageJSON = []byte(`{"seconds":16,"billable_seconds":8,"size":"1280x720","model":"sora-2"}`)
	log, _, err = OpenAIVideoJobCharge(job)
	require.NoError(t, err)
	require.Equal(t, 0.8, log.TotalCost)

	for _, usage := range []string{
		`{"seconds":8,"size":"1024x1024","model":"sora-2"}`,
		`{"seconds":8,"size":"1792x1024","model":"sora-2"}`,
		`{"seconds":8,"size":"1280x720","model":"sora-1"}`,
		`{"seconds":0,"size":"1280x720","model":"sora-2"}`,
		`{"seconds":8,"billable_seconds":9,"size":"1280x720","model":"sora-2"}`,
	} {
		job.UsageJSON = []byte(usage)
		_, _, err = OpenAIVideoJobCharge(job)
		require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage, usage)
	}
	job.UsageJSON = []byte(`{"seconds":8,"size":"1280x720","model":"sora-2"}`)
	job.Billing.UnitPrice = 0
	_, _, err = OpenAIVideoJobCharge(job)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
	job.ResultRef = ""
	job.Billing.UnitPrice = 0.1
	_, _, err = OpenAIVideoJobCharge(job)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
}

// Under a request plan the send boundary revalidates catalog evidence with the
// action endpoint; until registry data projects media endpoints, validation
// fails closed instead of silently forwarding.
func TestOpenAIVideoSendBoundaryFailsClosedWithoutEndpointEvidence(t *testing.T) {
	account := openAIVideoTestAccount()
	account.Extra = map[string]any{UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"sora-2"}, EligibleModels: []AccountCatalogModel{{ID: "sora-2", Endpoints: []string{"responses"}, EvidenceKind: "synthetic-contract"}}}}
	upstream := &httpUpstreamRecorder{resp: openAIVideoJobBody(`{"id":"video_abc","object":"video","status":"queued","model":"sora-2","progress":0,"seconds":"8","size":"1280x720"}`)}
	service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: openAIVideoAccountRepository{account: account}}
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "sora-2", Action: "video.generate"}))
	installUnlimitedTPMFixture(GatewayRequestPlanFromContext(ctx))
	_, err := service.SubmitOpenAIVideo(ctx, account, "video.generate", "application/json", []byte(`{"model":"sora-2","prompt":"waves","size":"1280x720","seconds":"8"}`), "sora-2", 0)
	require.ErrorIs(t, err, ErrAccountModelUnsupported)
	require.Empty(t, upstream.requests, "rejected bindings never reach the upstream")
}

// TestOpenAIVideoSendBoundaryResolvesMediaCatalogEvidence is the admitted half
// of the same boundary: an account whose catalogue records the media family for
// the video model resolves generate, edit and extension, and each action reaches
// the upstream exactly once. The identical fixture without that evidence stays
// refused above, so the boundary still fails closed on missing evidence.
func TestOpenAIVideoSendBoundaryResolvesMediaCatalogEvidence(t *testing.T) {
	for _, test := range []struct {
		action          string
		body            string
		billableSeconds int
	}{
		{action: "video.generate", body: `{"model":"sora-2","prompt":"waves","size":"1280x720","seconds":"8"}`},
		{action: "video.edit", body: `{"video":{"id":"video_parent"},"prompt":"continue"}`},
		{action: "video.extend", body: `{"video":{"id":"video_parent"},"prompt":"continue","seconds":"8"}`, billableSeconds: 8},
	} {
		t.Run(test.action, func(t *testing.T) {
			account := openAIVideoTestAccount()
			account.Extra = map[string]any{UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"sora-2"}, EligibleModels: []AccountCatalogModel{{ID: "sora-2", Endpoints: []string{CompositeRouteEndpointImages}, EvidenceKind: "synthetic-contract"}}}}
			upstream := &httpUpstreamRecorder{resp: openAIVideoJobBody(`{"id":"video_abc","object":"video","status":"queued","model":"sora-2","progress":0,"seconds":"8","size":"1280x720"}`)}
			service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: openAIVideoAccountRepository{account: account}}
			ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "sora-2", Action: "video.generate"}))
			installUnlimitedTPMFixture(GatewayRequestPlanFromContext(ctx))
			update, err := service.SubmitOpenAIVideo(ctx, account, test.action, "application/json", []byte(test.body), "sora-2", test.billableSeconds)
			require.NoError(t, err)
			require.Equal(t, "video_abc", update.TaskID)
			require.Len(t, upstream.requests, 1, "an admitted submission reaches the upstream exactly once")
		})
	}
}

// The shared job service owns the exclusive claim, poll scheduling and
// settlement; this test proves the OpenAI video adapter plugs into that trunk
// with idempotent submission and a single charge.
func TestOpenAIVideoJobServiceIdempotentSubmitAndSingleSettlement(t *testing.T) {
	ctx := context.Background()
	group := openAIVideoPriceGroup()
	snapshot, err := OpenAIVideoBillingSnapshot(group, nil, &APIKey{ID: 2}, []byte(`{"model":"sora-2","prompt":"waves","size":"1280x720","seconds":"8"}`), nil, nil)
	require.NoError(t, err)
	repository := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	template := &UpstreamJob{Platform: PlatformOpenAI, Family: "video", UserID: 1, APIKeyID: 2, AccountID: 42, GroupID: &group.ID, Billing: snapshot}
	body := []byte(`{"model":"sora-2","prompt":"waves","size":"1280x720","seconds":"8"}`)

	account := openAIVideoTestAccount()
	var submissions atomic.Int32
	upstream := &httpUpstreamRecorder{resp: openAIVideoJobBody(`{"id":"video_abc","object":"video","status":"queued","model":"sora-2","progress":0,"seconds":"8","size":"1280x720"}`)}
	service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: openAIVideoAccountRepository{account: account}}
	submit := func(context.Context) (UpstreamJobUpdate, error) {
		submissions.Add(1)
		// The job trunk injects its own plan; catalog endpoint projection for the
		// media actions is model-registry-data scope, so the send-boundary binding
		// is exercised separately in TestOpenAIVideoSubmitParsesJobAndRejectsAmbiguity.
		return service.SubmitOpenAIVideo(context.Background(), account, "video.generate", "application/json", body, "sora-2", 0)
	}
	poll := func(ctx context.Context, j *UpstreamJob) (UpstreamJobUpdate, error) {
		return service.PollOpenAIVideo(ctx, j)
	}
	jobs := NewUpstreamJobService(repository, poll, OpenAIVideoJobCharge)
	first, err := jobs.Submit(ctx, template, body, submit)
	require.NoError(t, err)
	second, err := jobs.Submit(ctx, template, body, submit)
	require.NoError(t, err)
	require.Equal(t, first.PublicRequestID, second.PublicRequestID)
	require.Equal(t, int32(1), submissions.Load(), "an equivalent retry must reuse the durable claim, not resubmit")
	require.Equal(t, "video_abc", first.UpstreamTaskID)

	// Client retry mid-flight still resolves the same job instead of a new task.
	again, err := jobs.Submit(ctx, template, body, submit)
	require.NoError(t, err)
	require.Equal(t, first.PublicRequestID, again.PublicRequestID)
	require.Equal(t, int32(1), submissions.Load())

	service.httpUpstream = &httpUpstreamRecorder{resp: openAIVideoJobBody(`{"id":"video_abc","object":"video","status":"completed","model":"sora-2","progress":100,"seconds":"8","size":"1280x720"}`)}
	// Move the service clock past the submission backoff so the poll is due.
	jobs.now = func() time.Time { return time.Now().Add(time.Minute) }
	polled, err := jobs.Poll(ctx, first.PublicRequestID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", polled.Status)
	require.NotNil(t, polled.ChargedUsageLogID)
	require.Equal(t, 1, repository.charges)
	settled, err := jobs.Poll(ctx, first.PublicRequestID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", settled.Status)
	require.Equal(t, 1, repository.charges, "a settled job is never charged twice")

	// Owner mismatch on the same public ID is not a cross-tenant read.
	_, err = repository.Get(ctx, first.PublicRequestID, 9, 9)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
}
