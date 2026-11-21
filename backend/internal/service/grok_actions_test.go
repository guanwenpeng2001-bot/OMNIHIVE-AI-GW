//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Documentation-derived protocol fixtures, retrieved 2026-09-21 from
// https://docs.x.ai/developers/model-capabilities/audio/text-to-speech and
// https://docs.x.ai/developers/model-capabilities/video/generation .
// These are synthetic responses, not saved real upstream wire captures.
func TestGrokActionsSpeechWireAndUsage(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse("audio-bytes")}
	upstream.response.Header.Set("Content-Type", "audio/wav")
	service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	requestContext, recorder := grokMediaContentTestContext(http.MethodPost, "/v1/audio/speech", nil)
	result, err := service.ForwardGrokVoice(context.Background(), requestContext, grokMediaContentTestAccount(), "tts", []byte(`{"model":"tts","input":"Hello 世界","voice":"eve","response_format":"wav","speed":1.2}`), "application/json")
	require.NoError(t, err)
	require.Equal(t, "https://relay.example/v1/tts", upstream.request.URL.String())
	require.Equal(t, "Bearer upstream-key", upstream.request.Header.Get("Authorization"))
	body, err := io.ReadAll(upstream.request.Body)
	require.NoError(t, err)
	require.Equal(t, "Hello 世界", gjson.GetBytes(body, "text").String())
	require.Equal(t, "eve", gjson.GetBytes(body, "voice_id").String())
	require.Equal(t, "auto", gjson.GetBytes(body, "language").String())
	require.Equal(t, "wav", gjson.GetBytes(body, "output_format.codec").String())
	require.Equal(t, int64(24000), gjson.GetBytes(body, "output_format.sample_rate").Int())
	for _, field := range []string{"model", "input", "voice", "response_format"} {
		require.False(t, gjson.GetBytes(body, field).Exists())
	}
	require.Equal(t, "audio-bytes", recorder.Body.String())
	require.Equal(t, "audio/wav", recorder.Header().Get("Content-Type"))
	require.InDelta(t, 8.0/1_000_000, result.AudioUsage.DurationOrUnits, 1e-12)
}

func TestGrokActionsSpeechRejectsLossyRequestsBeforeSend(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{}`, `{"input":5}`, `{"input":"hello","response_format":"opus"}`, `{"input":"hello","speed":2}`, `{"input":"hello","instructions":"whisper"}`, `{"input":"hello","voice":{"id":"eve"}}`, `{"input":"hello","text":"different"}`, `{"input":"hello","voice":"eve","voice_id":"ara"}`, `{"input":"hello","response_format":"wav","output_format":{}}`, `{"input":"hello","stream_format":"sse"}`} {
		t.Run(body, func(t *testing.T) {
			upstream := &grokMediaContentUpstreamStub{}
			service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			requestContext, recorder := grokMediaContentTestContext(http.MethodPost, "/v1/audio/speech", nil)
			_, err := service.ForwardGrokVoice(context.Background(), requestContext, grokMediaContentTestAccount(), "tts", []byte(body), "application/json")
			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Empty(t, upstream.requests)
		})
	}
}

func TestGrokActionsNativeVoiceWire(t *testing.T) {
	for _, endpoint := range []string{"tts", "stt", "custom-voices"} {
		t.Run(endpoint, func(t *testing.T) {
			upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"text":"hello","duration":3.45}`)}
			service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			requestContext, _ := grokMediaContentTestContext(http.MethodPost, "/v1/"+endpoint, nil)
			body := []byte(`{"text":"hello","voice_id":"eve","language":"en"}`)
			result, err := service.ForwardGrokVoice(context.Background(), requestContext, grokMediaContentTestAccount(), endpoint, body, "application/json")
			require.NoError(t, err)
			forwarded, err := io.ReadAll(upstream.request.Body)
			require.NoError(t, err)
			require.Equal(t, body, forwarded)
			require.Equal(t, "/v1/"+endpoint, upstream.request.URL.Path)
			if endpoint == "stt" {
				require.InDelta(t, 3.45/3600, result.AudioUsage.DurationOrUnits, 1e-12)
			}
		})
	}
}

// Group media/audio/search price fields are the pricing facts every Grok
// action class resolves through; the frozen request plan snapshots them at
// admission time. Nil group fields must stay nil so missing prices never
// resolve to free.
func TestGrokActionsGroupPriceConfigsResolve(t *testing.T) {
	ttsPrice, sttPrice, realtimePrice := 4.2, 0.9, 0.06
	image1K, image2K, video480, video720 := 0.02, 0.04, 0.21, 0.35
	searchPer1k, searchPerCall := 5.0, 0.01
	group := &Group{
		ImagePrice1K:                 &image1K,
		ImagePrice2K:                 &image2K,
		VideoPrice480P:               &video480,
		VideoPrice720P:               &video720,
		VideoModelPrices:             map[string]map[string]float64{"grok-imagine-video": {"720p": 0.5}},
		SearchPricePer1k:             &searchPer1k,
		WebSearchPricePerCall:        &searchPerCall,
		AudioRealtimePricePerMin:     &realtimePrice,
		AudioTTSPricePerMillionChars: &ttsPrice,
		AudioSTTPricePerHour:         &sttPrice,
	}
	apiKey := &APIKey{Group: group}

	audio := groupAudioPriceConfigFromAPIKey(apiKey)
	require.NotNil(t, audio)
	require.Equal(t, &ttsPrice, audio.TTSPerMChars)
	require.Equal(t, &sttPrice, audio.STTPerHour)
	require.Equal(t, &realtimePrice, audio.RealtimePerMin)

	video := videoPriceConfigFromAPIKey(apiKey)
	require.NotNil(t, video)
	require.Equal(t, &video480, video.Price480P)
	require.Equal(t, &video720, video.Price720P)
	modelPrice := group.GetVideoPriceForModel("grok-imagine-video", "720p")
	require.NotNil(t, modelPrice)
	require.Equal(t, 0.5, *modelPrice, "per-model override beats the flat column")
	flatFallback := group.GetVideoPriceForModel("grok-imagine-video", "480p")
	require.NotNil(t, flatFallback)
	require.Equal(t, video480, *flatFallback, "resolution missing from the override map falls back to the flat column")
	require.Equal(t, &image1K, group.GetImagePrice("1K"))
	require.Equal(t, &image2K, group.GetImagePrice("2K"))
	require.Equal(t, &searchPer1k, group.GetSearchPricePer1k())
	require.Equal(t, &searchPerCall, webSearchPricePerCallFromAPIKey(apiKey))

	blank := &APIKey{Group: &Group{}}
	require.Nil(t, blank.Group.GetImagePrice("1K"))
	require.Nil(t, blank.Group.GetVideoPriceForModel("grok-imagine-video", "720p"))
	require.Nil(t, blank.Group.GetSearchPricePer1k())
	blankAudio := groupAudioPriceConfigFromAPIKey(blank)
	require.NotNil(t, blankAudio)
	require.Nil(t, blankAudio.TTSPerMChars)
	require.Nil(t, blankAudio.STTPerHour)
	require.Nil(t, blankAudio.RealtimePerMin)
	require.Nil(t, groupAudioPriceConfigFromAPIKey(nil))
}

func TestGrokActionsMediaWireBudgetAndCancellation(t *testing.T) {
	for _, fixture := range []struct {
		endpoint GrokMediaEndpoint
		path     string
		response string
	}{
		{GrokMediaEndpointImagesGenerations, "images/generations", `{"data":[{"url":"https://example.com/image.png"}]}`},
		{GrokMediaEndpointImagesEdits, "images/edits", `{"data":[{"url":"https://example.com/image.png"}]}`},
		{GrokMediaEndpointVideosGenerations, "videos/generations", `{"request_id":"task-fixture"}`},
		{GrokMediaEndpointVideosEdits, "videos/edits", `{"request_id":"task-fixture"}`},
		{GrokMediaEndpointVideosExtensions, "videos/extensions", `{"request_id":"task-fixture"}`},
	} {
		t.Run(fixture.path, func(t *testing.T) {
			upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(fixture.response)}
			service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			requestContext, _ := grokMediaContentTestContext(http.MethodPost, "/v1/"+fixture.path, nil)
			ctx, cancel := WithAttemptBudget(context.Background(), 1, time.Minute)
			defer cancel()
			body := []byte(`{"model":"grok-imagine-video","prompt":"A moving cloud","image":{"url":"https://example.com/input.png"},"video":{"url":"https://example.com/input.mp4"},"duration":5,"resolution":"720p"}`)
			if fixture.endpoint == GrokMediaEndpointImagesGenerations || fixture.endpoint == GrokMediaEndpointImagesEdits {
				body = []byte(`{"model":"grok-imagine-video","prompt":"A moving cloud","image":{"url":"https://example.com/input.png"},"resolution":"1k"}`)
			}
			result, err := service.ForwardGrokMedia(ctx, requestContext, grokMediaContentTestAccount(), fixture.endpoint, "", body, "application/json")
			require.NoError(t, err)
			require.Equal(t, "/v1/"+fixture.path, upstream.request.URL.Path)
			require.Equal(t, http.MethodPost, upstream.request.Method)
			require.Equal(t, "Bearer upstream-key", upstream.request.Header.Get("Authorization"))
			forwarded, err := io.ReadAll(upstream.request.Body)
			require.NoError(t, err)
			require.Equal(t, "grok-imagine-video", gjson.GetBytes(forwarded, "model").String(), "upstream body must carry the requested model")
			if fixture.endpoint == GrokMediaEndpointImagesGenerations || fixture.endpoint == GrokMediaEndpointImagesEdits {
				require.Equal(t, 1, result.ImageCount)
			} else {
				require.Equal(t, "task-fixture", result.ResponseID)
				require.Zero(t, result.VideoCount, "submission must not be billed as completed video")
			}
			_, err = service.ForwardGrokMedia(ctx, requestContext, grokMediaContentTestAccount(), fixture.endpoint, "", body, "application/json")
			require.Error(t, err)
			require.Len(t, upstream.requests, 1, "same logical request cannot submit twice")
			cancelled, stop := context.WithCancel(context.Background())
			stop()
			cancelled, release := WithAttemptBudget(cancelled, 1, time.Minute)
			defer release()
			_, err = service.ForwardGrokMedia(cancelled, requestContext, grokMediaContentTestAccount(), fixture.endpoint, "", body, "application/json")
			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
		})
	}
}

// Standalone search (/v1/web_search, /v1/x_search) reaches the account's
// native Responses upstream with the search tool body; evidence:
// https://docs.x.ai/developers/tools/web-search retrieved 2026-09-21.
// Documentation fixture, not a saved live capture.
func TestGrokActionsSearchWireAndRejects(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"output":[]}`)}
	service := &GatewayService{httpUpstream: upstream}
	body := []byte(`{"model":"grok-4.6","input":"latest xAI news","tools":[{"type":"web_search"}],"store":false}`)
	resp, err := service.DoGrokNativeResponsesJSON(context.Background(), grokMediaContentTestAccount(), body)
	require.NoError(t, err)
	require.JSONEq(t, `{"output":[]}`, string(resp))
	require.Equal(t, "https://relay.example/v1/responses", upstream.request.URL.String())
	require.Equal(t, http.MethodPost, upstream.request.Method)
	require.Equal(t, "Bearer upstream-key", upstream.request.Header.Get("Authorization"))
	forwarded, err := io.ReadAll(upstream.request.Body)
	require.NoError(t, err)
	require.Equal(t, "grok-4.6", gjson.GetBytes(forwarded, "model").String())
	require.Equal(t, "web_search", gjson.GetBytes(forwarded, "tools.0.type").String())

	nonGrok := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "upstream-key"}}
	_, err = service.DoGrokNativeResponsesJSON(context.Background(), nonGrok, body)
	require.Error(t, err, "search must not leak a non-Grok credential to the xAI upstream")
	require.Len(t, upstream.requests, 1, "rejected before any upstream call")

	rateLimited := &grokMediaContentUpstreamStub{response: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"slow down"}}`)),
	}}
	failing := &GatewayService{httpUpstream: rateLimited}
	_, err = failing.DoGrokNativeResponsesJSON(context.Background(), grokMediaContentTestAccount(), body)
	require.Error(t, err, "upstream failures must surface instead of a fake empty result")
}
