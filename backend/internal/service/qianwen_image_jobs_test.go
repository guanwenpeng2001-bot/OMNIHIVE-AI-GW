//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type qianwenImageAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *qianwenImageAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func imageAsyncFixture(t *testing.T, model string) (*UpstreamJob, []byte) {
	t.Helper()
	body := []byte(`{"model":"` + model + `","input":{"prompt":"a cat"},"parameters":{"size":"1280*720","n":1}}`)
	a := qianwenTestAccount()
	a.Extra = map[string]any{"qianwen_pricing_sync": map[string]any{"region": "asia-pacific-china", "usd_per_cny": 1.0 / 7, "fx_source": "test operator rate", "fx_effective_at": "2026-09-13T00:00:00Z"}}
	b, err := QianwenAsyncBillingSnapshot("image", &Group{RateMultiplier: 2}, a, &APIKey{Quota: 1}, body, nil)
	require.NoError(t, err)
	return &UpstreamJob{Platform: PlatformQianwen, Family: "image", UserID: 1, APIKeyID: 2, AccountID: 19, Billing: b}, body
}

func TestQianwenImageAsyncOfficialTariffs(t *testing.T) {
	for _, tc := range []struct {
		model string
		cny   float64
		size  string
	}{
		{"wan2.5-t2i-preview", 0.20, "1280*1280"}, {"wan2.2-t2i-plus", 0.20, "1280*720"}, {"wan2.2-t2i-flash", 0.14, "1280*720"},
		{"wanx2.1-t2i-plus", 0.20, "1280*720"}, {"wanx2.1-t2i-turbo", 0.14, "1280*720"}, {"wanx2.0-t2i-turbo", 0.04, "1024*1024"},
	} {
		a, err := qianwenImageAsyncSpecification(tc.model, gjson.Parse(`{"input":{"prompt":"cat"},"parameters":{"size":"`+tc.size+`","n":2}}`))
		require.NoError(t, err, tc.model)
		require.InDelta(t, tc.cny, a.UnitCNY, 1e-10, tc.model)
		require.Equal(t, "text-to-image", a.TaskType)
	}
}

func TestQianwenImageAsyncSpecificationRejections(t *testing.T) {
	for _, tc := range []struct{ name, model, body string }{
		{"unknown model", "wanx9.9-t2i", `{"input":{"prompt":"cat"}}`},
		{"z-image unverified endpoint", "z-image-turbo", `{"input":{"prompt":"cat"}}`},
		{"wan2.6 new protocol", "wan2.6-t2i", `{"input":{"prompt":"cat"}}`},
		{"empty prompt", "wanx2.1-t2i-turbo", `{"input":{"prompt":" "}}`},
		{"prompt too long", "wanx2.1-t2i-turbo", `{"input":{"prompt":"` + strings.Repeat("喵", 501) + `"}}`},
		{"negative too long", "wanx2.1-t2i-turbo", `{"input":{"prompt":"cat","negative_prompt":"` + strings.Repeat("x", 501) + `"}}`},
		{"bad size format", "wanx2.1-t2i-turbo", `{"input":{"prompt":"cat"},"parameters":{"size":"1280x720"}}`},
		{"size out of bounds", "wanx2.1-t2i-turbo", `{"input":{"prompt":"cat"},"parameters":{"size":"256*256"}}`},
		{"wan2.5 area below band", "wan2.5-t2i-preview", `{"input":{"prompt":"cat"},"parameters":{"size":"1024*1024"}}`},
		{"extreme ratio", "wanx2.1-t2i-turbo", `{"input":{"prompt":"cat"},"parameters":{"size":"1440*5120"}}`},
		{"n out of range", "wanx2.1-t2i-turbo", `{"input":{"prompt":"cat"},"parameters":{"n":5}}`},
		{"fractional n", "wanx2.1-t2i-turbo", `{"input":{"prompt":"cat"},"parameters":{"n":1.5}}`},
		{"bad seed", "wanx2.1-t2i-turbo", `{"input":{"prompt":"cat"},"parameters":{"seed":-1}}`},
		{"unknown parameter", "wanx2.1-t2i-turbo", `{"input":{"prompt":"cat"},"parameters":{"strength":0.5}}`},
		{"unknown input", "wanx2.1-t2i-turbo", `{"input":{"prompt":"cat","image":"https://example.com/a.png"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := qianwenImageAsyncSpecification(tc.model, gjson.Parse(tc.body))
			require.Error(t, err)
		})
	}
}

func TestQianwenImageAsyncSubmitTransportAndBillingSnapshot(t *testing.T) {
	j, _ := imageAsyncFixture(t, "wanx2.1-t2i-turbo")
	require.Equal(t, "wanx2.1-t2i-turbo", j.Billing.Model)
	require.InDelta(t, 0.14/7, j.Billing.UnitPrice, 1e-12)
	a := qianwenTestAccount()
	a.Credentials["model_mapping"] = map[string]any{"alias-image": "wanx2.1-t2i-turbo"}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{qianwenModelPage(`{"output":{"task_id":"upstream-image-1","task_status":"PENDING"}}`, 200)}}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	u, err := s.SubmitQianwenAsync(context.Background(), a, "image", []byte(`{"model":"alias-image","input":{"prompt":"cat"},"parameters":{"size":"1280*720","n":1}}`))
	require.NoError(t, err)
	require.Equal(t, "submitted", u.Status)
	require.Equal(t, "upstream-image-1", u.TaskID)
	require.Equal(t, "enable", upstream.lastReq.Header.Get("X-DashScope-Async"))
	require.Empty(t, upstream.lastReq.Header.Get("X-DashScope-SSE"))
	require.Equal(t, "/api/v1/services/aigc/text2image/image-synthesis", upstream.lastReq.URL.Path)
	require.Equal(t, "wanx2.1-t2i-turbo", gjson.GetBytes(upstream.lastBody, "model").String())
	// Polling uses the shared task query with the account's own credential.
	pollAccount := qianwenTestAccount()
	pollAccount.Status = StatusActive
	s.accountRepo = &qianwenImageAccountRepo{account: pollAccount}
	upstream.responses = []*http.Response{qianwenModelPage(`{"output":{"task_id":"upstream-image-1","task_status":"SUCCEEDED","results":[{"url":"https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/1.png"}]},"usage":{"image_count":1}}`, 200)}
	j.UpstreamTaskID = "upstream-image-1"
	got, err := s.PollQianwenAsyncJob(context.Background(), j)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	require.Contains(t, got.ResultRef, "1.png")
	require.Equal(t, "/api/v1/tasks/upstream-image-1", upstream.lastReq.URL.Path)
	require.Empty(t, upstream.lastReq.Header.Get("X-DashScope-Async"))
}

func TestQianwenImageAsyncChargeOfficialUsage(t *testing.T) {
	j, _ := imageAsyncFixture(t, "wanx2.1-t2i-turbo")
	j.ResultRef = `[{"url":"https://example.com/1.png"},{"url":"https://example.com/2.png"}]`
	j.UsageJSON = json.RawMessage(`{"image_count":2}`)
	log, cmd, err := QianwenAsyncJobCharge(j)
	require.NoError(t, err)
	// 2 images × 0.14 CNY × (1/7 USD/CNY), group multiplier 2 on actual.
	require.InDelta(t, 2*0.14/7, log.TotalCost, 1e-10)
	require.Equal(t, QuantizeUsageBillingAmount(log.TotalCost*2), cmd.BalanceCost)
	require.Equal(t, cmd.BalanceCost, cmd.APIKeyQuotaCost)
	require.Equal(t, "image", *log.BillingMode)
	require.Equal(t, "async-families-2026-09-22", *log.PriceCardVersion)
	require.Contains(t, *log.BillingTier, "qianwen_image_v1:units=2")
	for _, bad := range []struct{ usage, results string }{
		{`{}`, `[{"url":"https://example.com/1.png"}]`},
		{`{"image_count":0}`, `[]`},
		{`{"image_count":1.5}`, `[{"url":"https://example.com/1.png"}]`},
		{`{"image_count":2}`, `[{"url":"https://example.com/1.png"}]`},
		{`{"image_count":1}`, `[{"url":"notaurl"}]`},
		{`{"image_count":5}`, `[{"url":"https://example.com/1.png"},{"url":"https://example.com/2.png"},{"url":"https://example.com/3.png"},{"url":"https://example.com/4.png"},{"url":"https://example.com/5.png"}]`},
	} {
		j.UsageJSON = json.RawMessage(bad.usage)
		j.ResultRef = bad.results
		_, _, err := QianwenAsyncJobCharge(j)
		require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage, bad.usage)
	}
	// A tariff mismatch (snapshot tampered) never settles.
	j.ResultRef = `[{"url":"https://example.com/1.png"}]`
	j.UsageJSON = json.RawMessage(`{"image_count":1}`)
	j.Billing.UnitPrice = 0.01
	_, _, err = QianwenAsyncJobCharge(j)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
}

func TestQianwenImageAsyncConcurrentClaimRestartAndSettlement(t *testing.T) {
	j, body := imageAsyncFixture(t, "wan2.2-t2i-flash")
	repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
	var submissions atomic.Int32
	poll := func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error) {
		return UpstreamJobUpdate{TaskID: "upstream-1", Status: "succeeded", ResultRef: `[{"url":"https://example.com/1.png"}]`, Usage: json.RawMessage(`{"image_count":1}`)}, nil
	}
	svc := NewUpstreamJobService(repo, poll, QianwenAsyncJobCharge)
	submit := func(context.Context) (UpstreamJobUpdate, error) {
		submissions.Add(1)
		return UpstreamJobUpdate{TaskID: "upstream-1", Status: "submitted"}, nil
	}
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := svc.Submit(context.Background(), j, body, submit)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- got.PublicRequestID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id == "" {
			id = got
		}
		require.Equal(t, id, got, "equivalent submissions must share one durable job")
	}
	require.EqualValues(t, 1, submissions.Load())
	// Cross-tenant reads are denied by the owner filter.
	_, err := svc.Get(context.Background(), id, 1, 999)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	// Restart on durable state, then settle exactly once.
	svc = NewUpstreamJobService(repo, poll, QianwenAsyncJobCharge)
	svc.now = func() time.Time { return time.Now().Add(time.Minute) }
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Poll(context.Background(), id)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := svc.Get(context.Background(), id, 1, 2)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	require.NotNil(t, got.ChargedUsageLogID)
	require.Equal(t, 1, repo.charges)
}

func TestQianwenImageAsyncMissingFXNeverAdmits(t *testing.T) {
	a := qianwenTestAccount()
	body := []byte(`{"model":"wanx2.1-t2i-turbo","input":{"prompt":"cat"}}`)
	_, err := QianwenAsyncBillingSnapshot("image", &Group{RateMultiplier: 1}, a, nil, body, nil)
	require.ErrorContains(t, err, "conversion")
}
