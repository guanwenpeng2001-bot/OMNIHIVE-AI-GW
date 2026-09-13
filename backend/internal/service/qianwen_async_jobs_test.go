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
)

func asyncFixture(t *testing.T, family string) (*UpstreamJob, []byte) {
	t.Helper()
	body := []byte(`{"model":"Tripo/Tripo-P1.0","input":{"prompt":"a cat"}}`)
	if family == "asr" {
		body = []byte(`{"model":"paraformer-v2","input":{"file_urls":["https://example.com/audio.wav"]},"parameters":{"channel_id":[0]}}`)
	}
	a := qianwenTestAccount()
	a.Extra = map[string]any{"qianwen_pricing_sync": map[string]any{"region": "asia-pacific-china", "usd_per_cny": 1.0 / 7, "fx_source": "test operator rate", "fx_effective_at": "2026-09-13T00:00:00Z"}}
	a.Credentials["api_base_urls"] = map[string]any{"dashscope": "https://workspace.cn-beijing.maas.aliyuncs.com/api/v1"}
	b, err := QianwenAsyncBillingSnapshot(family, &Group{RateMultiplier: 2}, a, &APIKey{Quota: 1}, body, nil)
	require.NoError(t, err)
	return &UpstreamJob{Platform: PlatformQianwen, Family: family, UserID: 1, APIKeyID: 2, AccountID: 19, Billing: b}, body
}

func asyncEvidence(family string) UpstreamJobUpdate {
	u := UpstreamJobUpdate{TaskID: "upstream-1", Status: "succeeded", ResultRef: `[{"pbr_model_url":"https://example.com/model.glb"}]`, Usage: json.RawMessage(`{"count":1,"3d_task_type":"text-to-3d","texture_quality":"standard"}`)}
	if family == "asr" {
		u.ResultRef = `[{"channel_id":0,"text":"hello"}]`
		u.Usage = json.RawMessage(`{"duration":9}`)
	}
	return u
}

func TestQianwenAsyncOfficialTariffsAndUsage(t *testing.T) {
	for _, family := range []string{"3d", "asr"} {
		j, _ := asyncFixture(t, family)
		e := asyncEvidence(family)
		j.ResultRef, j.UsageJSON = e.ResultRef, e.Usage
		log, cmd, err := QianwenAsyncJobCharge(j)
		require.NoError(t, err)
		want := 0.4
		if family == "asr" {
			want = QuantizeUsageBillingAmount(9 * 0.00008 / 7)
		}
		require.InDelta(t, want, log.TotalCost, 1e-10)
		require.Equal(t, QuantizeUsageBillingAmount(want*2), cmd.BalanceCost)
		require.Equal(t, cmd.BalanceCost, cmd.APIKeyQuotaCost)
		require.Zero(t, log.VideoCount)
		for _, bad := range []string{`{}`, `{"duration":"9"}`, `{"duration":-9}`, `{"duration":0}`, `{"count":2,"3d_task_type":"text-to-3d","texture_quality":"standard"}`} {
			j.UsageJSON = json.RawMessage(bad)
			_, _, err = QianwenAsyncJobCharge(j)
			require.Error(t, err)
		}
	}
}

func TestQianwenAsyncPricingSpecifications(t *testing.T) {
	for _, tc := range []struct {
		model, input, parameters string
		cny                      float64
	}{
		{"Tripo/Tripo-H3.1", `"prompt":"cat"`, `"texture":false,"pbr":false`, 0.7},
		{"Tripo/Tripo-H3.1", `"prompt":"cat"`, `"geometry_quality":"ultra","texture_quality":"detailed"`, 3.5},
		{"Tripo/Tripo-P1.0", `"image":"https://example.com/a.png"`, `"texture_quality":"detailed"`, 4.2},
		{"Tripo/Tripo-P1.0", `"prompt":"cat"`, `"texture":false`, 2.8}, // pbr defaults true
	} {
		b := []byte(`{"input":{` + tc.input + `},"parameters":{` + tc.parameters + `}}`)
		a, err := qianwenAsyncSpecification("3d", tc.model, b)
		require.NoError(t, err)
		require.InDelta(t, tc.cny, a.UnitCNY, 1e-10)
	}
	for _, body := range []string{`{"input":{"prompt":"cat","image":"https://example.com/a.png"}}`, `{"input":{"prompt":"cat"},"parameters":{"texture":"false"}}`, `{"input":{"prompt":"cat"},"parameters":{"quad":true}}`, `{"input":{"prompt":"cat"},"parameters":{"geometry_quality":"detailed"}}`} {
		_, err := qianwenAsyncSpecification("3d", "Tripo/Tripo-H3.1", []byte(body))
		require.Error(t, err)
	}
	_, body := asyncFixture(t, "asr")
	_, err := qianwenAsyncSpecification("asr", "paraformer-v2", []byte(strings.ReplaceAll(string(body), `[0]`, `[0,1]`)))
	require.Error(t, err)
	a := qianwenTestAccount()
	_, err = QianwenAsyncBillingSnapshot("asr", &Group{RateMultiplier: 1}, a, nil, body, nil)
	require.ErrorContains(t, err, "conversion")
}

func TestQianwenAsyncConcurrentClaimRestartAndSettlement(t *testing.T) {
	for _, family := range []string{"3d", "asr"} {
		t.Run(family, func(t *testing.T) {
			j, body := asyncFixture(t, family)
			repo := &fileBatchMemoryRepo{jobs: map[string]*UpstreamJob{}, claims: map[string]string{}}
			var submissions atomic.Int32
			poll := func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error) { return asyncEvidence(family), nil }
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
				require.Equal(t, id, got)
			}
			require.EqualValues(t, 1, submissions.Load())
			_, err := svc.Get(context.Background(), id, 1, 999)
			require.ErrorIs(t, err, ErrUpstreamJobNotFound)
			// Restart the service on durable state, then inject a settlement rollback.
			svc = NewUpstreamJobService(repo, poll, QianwenAsyncJobCharge)
			svc.now = func() time.Time { return time.Now().Add(time.Minute) }
			repo.failCharge = true
			_, err = svc.Poll(context.Background(), id)
			require.Error(t, err)
			require.Zero(t, repo.charges)
			repo.failCharge = false
			for range 20 {
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
			_, err = svc.Settle(context.Background(), id, asyncEvidence(family))
			require.NoError(t, err)
			require.Equal(t, 1, repo.charges)
		})
	}
}

func TestQianwenAsyncSubmitTransport(t *testing.T) {
	for _, family := range []string{"3d", "asr"} {
		_, body := asyncFixture(t, family)
		a := qianwenTestAccount()
		upstream := &httpUpstreamRecorder{responses: []*http.Response{qianwenModelPage(`{"output":{"task_id":"upstream-1","task_status":"PENDING"}}`, 200)}}
		s := &OpenAIGatewayService{httpUpstream: upstream}
		u, err := s.SubmitQianwenAsync(context.Background(), a, family, body)
		require.NoError(t, err)
		require.Equal(t, "submitted", u.Status)
		require.Equal(t, "enable", upstream.lastReq.Header.Get("X-DashScope-Async"))
		path := QianwenNative3DPath
		if family == "asr" {
			path = QianwenNativeASRPath
		}
		require.Equal(t, path, upstream.lastReq.URL.Path)
	}
	for _, origin := range []string{"http://127.0.0.1/x", "https://example.com/x", "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com.attacker.test/x", "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com:444/x"} {
		_, err := fetchQianwenTranscript(context.Background(), origin)
		require.ErrorContains(t, err, "origin")
	}
}

func TestQianwenASRCompletionDownloadsTextAndRejectsPartialSuccess(t *testing.T) {
	u := UpstreamJobUpdate{TaskID: "task", Status: "succeeded", ResultRef: `[{"subtask_status":"SUCCEEDED","transcription_url":"https://result.test/a"}]`, Usage: json.RawMessage(`{"duration":9}`)}
	calls := 0
	fetch := func(_ context.Context, url string) ([]byte, error) {
		calls++
		require.Equal(t, "https://result.test/a", url)
		return []byte(`{"file_url":"private-source","transcripts":[{"channel_id":0,"text":"hello world","sentences":[]}]}`), nil
	}
	got, err := completeQianwenASR(context.Background(), u, fetch)
	require.NoError(t, err)
	require.JSONEq(t, `[{"channel_id":0,"text":"hello world"}]`, got.ResultRef)
	require.Equal(t, 1, calls)
	require.JSONEq(t, `{"duration":9}`, string(got.Usage))
	for _, bad := range []string{`[]`, `[{"subtask_status":"FAILED"}]`, `[{"subtask_status":"SUCCEEDED"},{"subtask_status":"FAILED"}]`} {
		u.ResultRef = bad
		got, err = completeQianwenASR(context.Background(), u, fetch)
		require.NoError(t, err)
		require.Equal(t, "failed", got.Status)
		require.Equal(t, 1, calls)
	}
}

func TestQianwen3DUntexturedOutputUsesBaseModelEvidence(t *testing.T) {
	j, _ := asyncFixture(t, "3d")
	j.Billing.Async.Texture = "none"
	j.Billing.Async.UnitCNY = 2.1
	j.Billing.UnitPrice = 2.1 * j.Billing.Async.USDPerCNY
	j.ResultRef = `[{"base_model_url":"https://example.com/base.glb"}]`
	j.UsageJSON = json.RawMessage(`{"count":1,"3d_task_type":"text-to-3d","texture_quality":"standard"}`)
	log, _, err := QianwenAsyncJobCharge(j)
	require.NoError(t, err)
	require.InDelta(t, 0.3, log.TotalCost, 1e-10)
	j.ResultRef = `[{"pbr_model_url":"https://example.com/textured.glb"}]`
	_, _, err = QianwenAsyncJobCharge(j)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
}
