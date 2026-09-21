//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func vaceFixture(t *testing.T, function string) []byte {
	t.Helper()
	body := `{"model":"wanx2.1-vace-plus","input":{"function":"video_extension","first_clip_url":"https://example.com/clip.mp4"},"parameters":{"resolution":"720P"}}`
	if function == "video_edit" {
		body = `{"model":"wanx2.1-vace-plus","input":{"function":"video_edit","prompt":"make it snow","mask_image_url":"https://example.com/mask.png"},"parameters":{"resolution":"720P"}}`
	}
	return []byte(body)
}

func vaceGroup() *Group {
	return &Group{RateMultiplier: 2, VideoModelPrices: map[string]map[string]float64{"wanx2.1-vace-plus": {"720p": 0.1}}}
}

func TestQianwenVaceAdmissionAndCharge(t *testing.T) {
	for _, function := range []string{"video_extension", "video_edit"} {
		snapshot, err := QianwenVideoBillingSnapshot(vaceGroup(), nil, &APIKey{Quota: 1}, vaceFixture(t, function), nil)
		require.NoError(t, err, function)
		require.Equal(t, "wanx2.1-vace-plus", snapshot.Model)
		require.Equal(t, "720p", snapshot.Resolution)
		require.InDelta(t, 0.1, snapshot.UnitPrice, 1e-12)
		require.InDelta(t, 2, snapshot.RateMultiplier, 1e-12)
	}
	job := &UpstreamJob{Platform: PlatformQianwen, Family: "video", UserID: 1, APIKeyID: 2, AccountID: 19, ResultRef: "https://example.com/out.mp4"}
	snapshot, err := QianwenVideoBillingSnapshot(vaceGroup(), nil, nil, vaceFixture(t, "video_extension"), nil)
	require.NoError(t, err)
	job.Billing = snapshot
	job.UsageJSON = json.RawMessage(`{"video_count":1,"video_duration":5,"SR":720}`)
	log, cmd, err := QianwenVideoJobCharge(job)
	require.NoError(t, err)
	require.InDelta(t, 0.5, log.TotalCost, 1e-10)
	require.Equal(t, QuantizeUsageBillingAmount(1.0), cmd.BalanceCost)
	require.Equal(t, 1, log.VideoCount)
	require.Equal(t, "720p", *log.VideoResolution)
	// Tier mismatches and shape violations never settle.
	job.UsageJSON = json.RawMessage(`{"video_count":1,"video_duration":5,"SR":1080}`)
	_, _, err = QianwenVideoJobCharge(job)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
	job.UsageJSON = json.RawMessage(`{"video_count":2,"video_duration":5,"SR":720}`)
	_, _, err = QianwenVideoJobCharge(job)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
}

func TestQianwenVaceAdmissionRejections(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"unknown function", `{"model":"wanx2.1-vace-plus","input":{"function":"video_repainting","video_url":"https://example.com/a.mp4"},"parameters":{"resolution":"720P"}}`},
		{"no function", `{"model":"wanx2.1-vace-plus","input":{"first_clip_url":"https://example.com/a.mp4"},"parameters":{"resolution":"720P"}}`},
		{"extension without clip", `{"model":"wanx2.1-vace-plus","input":{"function":"video_extension"},"parameters":{"resolution":"720P"}}`},
		{"edit with both masks", `{"model":"wanx2.1-vace-plus","input":{"function":"video_edit","mask_image_url":"https://example.com/a.png","mask_video_url":"https://example.com/a.mp4"},"parameters":{"resolution":"720P"}}`},
		{"edit without mask", `{"model":"wanx2.1-vace-plus","input":{"function":"video_edit","prompt":"snow"},"parameters":{"resolution":"720P"}}`},
		{"bad expand ratio", `{"model":"wanx2.1-vace-plus","input":{"function":"video_edit","mask_image_url":"https://example.com/a.png","expand_ratio":1.5},"parameters":{"resolution":"720P"}}`},
		{"undocumented tier", `{"model":"wanx2.1-vace-plus","input":{"function":"video_extension","first_clip_url":"https://example.com/a.mp4"},"parameters":{"resolution":"1080P"}}`},
		{"missing resolution", `{"model":"wanx2.1-vace-plus","input":{"function":"video_extension","first_clip_url":"https://example.com/a.mp4"}}`},
		{"unknown parameter", `{"model":"wanx2.1-vace-plus","input":{"function":"video_extension","first_clip_url":"https://example.com/a.mp4"},"parameters":{"resolution":"720P","duration":5}}`},
		{"non-https clip", `{"model":"wanx2.1-vace-plus","input":{"function":"video_extension","first_clip_url":"ftp://example.com/a.mp4"},"parameters":{"resolution":"720P"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := QianwenVideoBillingSnapshot(vaceGroup(), nil, nil, []byte(tc.body), nil)
			require.Error(t, err)
		})
	}
	// The operator group price is mandatory, like other Wan models.
	_, err := QianwenVideoBillingSnapshot(&Group{RateMultiplier: 1}, nil, nil, vaceFixture(t, "video_extension"), nil)
	require.ErrorIs(t, err, ErrUpstreamJobTariff)
	for _, origin := range []string{"http://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/x.mp4", "https://example.com/x.mp4", "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com.attacker.test/x.mp4", "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com:444/x.mp4", "https://user:pw@dashscope-result-bj.oss-cn-beijing.aliyuncs.com/x.mp4", "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/x.mp4#frag"} {
		_, err := OpenQianwenVideoContent(context.Background(), origin)
		require.ErrorContains(t, err, "origin", origin)
	}
}

func TestQianwenVaceSubmitTransport(t *testing.T) {
	a := qianwenTestAccount()
	upstream := &httpUpstreamRecorder{responses: []*http.Response{qianwenModelPage(`{"output":{"task_id":"vace-1","task_status":"PENDING"}}`, 200)}}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	u, err := s.SubmitQianwenAsync(context.Background(), a, "video", vaceFixture(t, "video_extension"))
	require.NoError(t, err)
	require.Equal(t, "submitted", u.Status)
	require.Equal(t, "/api/v1/services/aigc/video-generation/video-synthesis", upstream.lastReq.URL.Path)
	require.Equal(t, "enable", upstream.lastReq.Header.Get("X-DashScope-Async"))
	require.Equal(t, "video_extension", gjson.GetBytes(upstream.lastBody, "input.function").String())
	require.Equal(t, "https://example.com/clip.mp4", gjson.GetBytes(upstream.lastBody, "input.first_clip_url").String())
}
