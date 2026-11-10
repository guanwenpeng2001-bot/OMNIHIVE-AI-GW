//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareKaggleImage(t *testing.T) {
	_, err := prepareKaggleImage([]byte(`{"model":"qwen-image-2.1","prompt":"a lighthouse"}`))
	require.NoError(t, err)
	_, err = prepareKaggleImage([]byte(`{"model":"qwen-image-2.1","prompt":"a lighthouse","response_format":"url"}`))
	require.Error(t, err)
	_, err = prepareKaggleImage([]byte(`{"model":"qwen-image-2.1"}`))
	require.Error(t, err)
	_, err = prepareKaggleImage([]byte(`{"prompt":"a lighthouse"}`))
	require.Error(t, err)
	_, err = prepareKaggleImage([]byte(`{"model":"qwen-image-2.1","prompt":"x","n":5}`))
	require.Error(t, err)
}

func TestValidateKaggleVideoRequest(t *testing.T) {
	require.NoError(t, ValidateKaggleVideoRequest([]byte(`{"model":"wan2.2-ti2v-5b","prompt":"a cat"}`)))
	require.Error(t, ValidateKaggleVideoRequest([]byte(`{"model":"wan2.2-ti2v-5b"}`)))
	require.Error(t, ValidateKaggleVideoRequest([]byte(`{"prompt":"a cat"}`)))
	require.Error(t, ValidateKaggleVideoRequest([]byte(`{"model":"wan2.2-ti2v-5b","prompt":"a cat","width":833}`)))
	require.Error(t, ValidateKaggleVideoRequest([]byte(`{"model":"wan2.2-ti2v-5b","prompt":"a cat","num_frames":48}`)))
	require.NoError(t, ValidateKaggleVideoRequest([]byte(`{"model":"wan2.2-ti2v-5b","prompt":"a cat","width":832,"height":480,"num_frames":49}`)))
}

func TestParseKaggleVideoStatuses(t *testing.T) {
	queued, err := parseKaggleVideo([]byte(`{"id":"vid_aaaaaaaaaaaaaaaaaaaa","status":"queued"}`))
	require.NoError(t, err)
	require.Equal(t, "submitted", queued.Status)
	running, err := parseKaggleVideo([]byte(`{"id":"vid_aaaaaaaaaaaaaaaaaaaa","status":"in_progress","progress":10}`))
	require.NoError(t, err)
	require.Equal(t, "running", running.Status)
	done, err := parseKaggleVideo([]byte(`  {"id":"vid_aaaaaaaaaaaaaaaaaaaa","status":"completed","seconds":2.5}  `))
	require.NoError(t, err)
	require.Equal(t, "succeeded", done.Status)
	require.Equal(t, "vid_aaaaaaaaaaaaaaaaaaaa", done.ResultRef)
	failed, err := parseKaggleVideo([]byte(`{"id":"vid_aaaaaaaaaaaaaaaaaaaa","status":"failed","error":"boom"}`))
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	_, err = parseKaggleVideo([]byte(`{"id":"vid_aaaaaaaaaaaaaaaaaaaa","status":"mystery"}`))
	require.Error(t, err)
}

func TestTrimJSONPaddingAndImageBytes(t *testing.T) {
	var picture bytes.Buffer
	require.NoError(t, png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	raw := []byte("  " + fmt.Sprintf(`{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(picture.Bytes())) + "\n")
	trimmed := trimJSONPadding(raw)
	require.True(t, trimmed[0] == '{')
}

func TestKaggleVideoAllowsUnpricedAdmission(t *testing.T) {
	require.True(t, kaggleVideoAllowsUnpricedAdmission(&UpstreamJob{Platform: PlatformKaggle, Family: "video"}))
	require.True(t, kaggleVideoAllowsUnpricedAdmission(&UpstreamJob{Platform: PlatformOpenRouter, Family: "video"}))
	require.False(t, kaggleVideoAllowsUnpricedAdmission(&UpstreamJob{Platform: PlatformOpenAI, Family: "video"}))
}

func TestKaggleMediaActionsAreSchedulerEligible(t *testing.T) {
	imageAccount := &Account{Platform: PlatformKaggle, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://example.trycloudflare.com", "model_mapping": map[string]any{"qwen-image-2.1": "qwen-image-2.1"}}}
	videoAccount := &Account{Platform: PlatformKaggle, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://example.trycloudflare.com", "model_mapping": map[string]any{"wan2.2-ti2v-5b": "wan2.2-ti2v-5b"}}}
	require.True(t, platformActionAdapterBound(imageAccount, "image.generate"))
	require.True(t, platformActionAdapterBound(videoAccount, "video.generate"))
	require.True(t, platformActionAdapterBound(imageAccount, "chat"))
	require.False(t, platformActionAdapterBound(imageAccount, "audio.tts"))
	require.Contains(t, adapterBoundCatalogFamilies(imageAccount), "images")
	require.Contains(t, adapterBoundCatalogFamilies(videoAccount), "images")

	imageCtx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "qwen-image-2.1", Protocol: "images", Route: "/v1/images/generations"}))
	require.Equal(t, CompositeRouteEndpointImages, openAICompatiblePlanCatalogEndpoint(imageCtx, imageAccount))
	require.True(t, AccountSupportsResolvedModel(imageCtx, imageAccount, "qwen-image-2.1", AccountModelBindingOptions{Endpoint: CompositeRouteEndpointImages}))
	require.True(t, AccountSupportsResolvedModel(imageCtx, imageAccount, "qwen-image-2.1", AccountModelBindingOptions{FinalModel: "qwen-image-2.1", Endpoint: CompositeRouteEndpointImages}))

	videoCtx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "wan2.2-ti2v-5b", Protocol: "media", Route: "/v1/videos"}))
	require.Equal(t, CompositeRouteEndpointImages, openAICompatiblePlanCatalogEndpoint(videoCtx, videoAccount))
	require.True(t, AccountSupportsResolvedModel(videoCtx, videoAccount, "wan2.2-ti2v-5b", AccountModelBindingOptions{Endpoint: CompositeRouteEndpointImages}))
}
