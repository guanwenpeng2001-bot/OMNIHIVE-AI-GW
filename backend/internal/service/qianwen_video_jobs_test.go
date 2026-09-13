//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestQianwenVideoTransportSubmitPollAndRejection(t *testing.T) {
	account := qianwenTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"video-alias": "wan2.6-t2v"}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		qianwenModelPage(`{"output":{"task_id":"task-1","task_status":"PENDING"}}`, 200),
		qianwenModelPage(`{"output":{"task_id":"task-1","task_status":"SUCCEEDED","video_url":"https://example.test/v.mp4"},"usage":{"duration":5,"video_count":1,"SR":720}}`, 200),
		qianwenModelPage(`{"code":"InvalidParameter","message":"provider details"}`, 400),
	}}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	update, err := s.SubmitQianwenVideo(context.Background(), account, []byte(`{"model":"video-alias","input":{"prompt":"cat"}}`))
	require.NoError(t, err)
	require.Equal(t, "submitted", update.Status)
	require.Equal(t, "task-1", update.TaskID)
	require.Equal(t, "enable", upstream.lastReq.Header.Get("X-DashScope-Async"))
	require.Equal(t, http.MethodPost, upstream.lastReq.Method)
	require.Equal(t, QianwenNativeVideoPath, upstream.lastReq.URL.Path)
	require.Contains(t, string(upstream.lastBody), `"model":"wan2.6-t2v"`)
	update, err = s.qianwenVideoRequest(context.Background(), account, http.MethodGet, "/tasks/task-1", nil)
	require.NoError(t, err)
	require.Equal(t, "succeeded", update.Status)
	require.JSONEq(t, `{"duration":5,"video_count":1,"SR":720}`, string(update.Usage))
	require.Empty(t, upstream.lastReq.Header.Get("X-DashScope-Async"))
	require.Equal(t, "/api/v1/tasks/task-1", upstream.lastReq.URL.Path)
	update, err = s.SubmitQianwenVideo(context.Background(), account, []byte(`{"model":"wan2.6-t2v"}`))
	require.NoError(t, err)
	require.Equal(t, "failed", update.Status)
	require.Equal(t, "upstream_rejected", update.ErrorCode)
	require.NotContains(t, update.ErrorMessage, "provider details")
}

func TestQianwenVideoOfficialUsageAndTariff(t *testing.T) {
	j := &UpstreamJob{Platform: PlatformQianwen, Family: "video", ResultRef: "https://example.com/video.mp4", Billing: UpstreamJobBilling{Model: "wan2.6-r2v", Resolution: "720p", UnitPrice: 0.1, RateMultiplier: 2, AccountRateMultiplier: 1}}
	j.UsageJSON = json.RawMessage(`{"video_count":1,"duration":10.5,"output_video_duration":5,"input_video_duration":5.5,"SR":720,"size":"1280*720"}`)
	log, cmd, err := QianwenVideoJobCharge(j)
	require.NoError(t, err)
	require.Equal(t, 1.05, log.TotalCost)
	require.Equal(t, 2.1, cmd.BalanceCost)
	require.Nil(t, log.VideoDurationSeconds)
	for _, raw := range []string{`{}`, `{"video_count":2,"duration":5,"SR":720}`, `{"video_count":1,"duration":5,"SR":1080}`, `{"video_count":1,"duration":"5","SR":720}`, `{"video_count":1,"output_video_duration":5,"SR":720}`, `{"video_count":1,"duration":0,"SR":720}`} {
		j.UsageJSON = json.RawMessage(raw)
		_, _, err = QianwenVideoJobCharge(j)
		require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
		require.JSONEq(t, raw, string(j.UsageJSON))
	}
	j.Billing.UnitPrice = 0
	_, _, err = QianwenVideoJobCharge(j)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
}

func TestQianwenVideoPricingNeverUsesGrokDefaults(t *testing.T) {
	defaultPrice := 0.5
	group := &Group{RateMultiplier: 1, VideoPrice720P: &defaultPrice}
	body := []byte(`{"model":"wan2.6-t2v","parameters":{"resolution":"720P"}}`)
	_, err := QianwenVideoBillingSnapshot(group, nil, nil, body, nil)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
	group.VideoModelPrices = map[string]map[string]float64{"wan2.6-t2v": {"720p": 0.1}}
	b, err := QianwenVideoBillingSnapshot(group, nil, nil, body, nil)
	require.NoError(t, err)
	require.Equal(t, 0.1, b.UnitPrice)
	_, err = QianwenVideoBillingSnapshot(group, nil, nil, []byte(`{"model":"wan2.6-t2v"}`), nil)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
	group.VideoModelPrices["wan2.6-t2v"]["720p"] = 0
	_, err = QianwenVideoBillingSnapshot(group, nil, nil, body, nil)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
}
