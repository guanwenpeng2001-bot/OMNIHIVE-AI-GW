package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// HunyuanActionEndpoint names one TokenHub (Tencent Cloud product 1823) action
// path served under the account's configured v1 base URL. Wire shapes were
// verified against the saved first-party documentation set captured on
// 2026-09-21 (TokenHub "调用指南" pages 1823/135745 image, /137202 video,
// /135791-/135792 speech recognition, /135796 speech synthesis, /133515 vector,
// /130078 protocol overview). Path segments are relative to the v1 base.
type HunyuanActionEndpoint string

const (
	// HunyuanEndpointImageGeneration is the synchronous Hy-Image-3.0 text and
	// reference-image generation endpoint (1823/135745). It has no OpenAI
	// /images/generations equivalent: the vendor path and body are native.
	HunyuanEndpointImageGeneration HunyuanActionEndpoint = "wand/hunyuan-image/v3-generation"
	// HunyuanEndpointImagePanorama is the synchronous Hy-World-2.1 360 panorama
	// image endpoint (1823/136578).
	HunyuanEndpointImagePanorama HunyuanActionEndpoint = "wand/hunyuan-image/world2-panorama"
	// HunyuanEndpointVideoGeneration submits an asynchronous Hy-Video-1.5 text or
	// image to video task (1823/137202).
	HunyuanEndpointVideoGeneration HunyuanActionEndpoint = "wand/hunyuan-video/generation"
	// HunyuanEndpointVideoScene submits the asynchronous Hy-World-2.1 3D scene task
	// (1823/136578).
	HunyuanEndpointVideoScene HunyuanActionEndpoint = "wand/hunyuan-video/world2-scene"
	// HunyuanEndpointVideoTask reads one asynchronous hy-video task by id
	// (1823/137202). The vendor documents no cancel endpoint.
	HunyuanEndpointVideoTask HunyuanActionEndpoint = "wand/hunyuan-video/tasks"
	// HunyuanEndpointVideoContent is the gateway-side content proxy for a
	// completed video task. It has no vendor path of its own: the task is re-read
	// through the owner-bound account and the settled media URL is streamed so
	// the temporary vendor URL never leaves the gateway.
	HunyuanEndpointVideoContent HunyuanActionEndpoint = "video-content"
	// HunyuanEndpointTranscribeSync transcribes an audio/video URL synchronously
	// (1823/135791).
	HunyuanEndpointTranscribeSync HunyuanActionEndpoint = "wand/asrproxy/sync_transcribe"
	// HunyuanEndpointTranscribeAsync creates an asynchronous transcription task
	// (1823/135792).
	HunyuanEndpointTranscribeAsync HunyuanActionEndpoint = "wand/asrproxy/async_create"
	// HunyuanEndpointTranscribeTask reads one asynchronous transcription task
	// (1823/135792).
	HunyuanEndpointTranscribeTask HunyuanActionEndpoint = "wand/asrproxy/tasks"
	// HunyuanEndpointSpeechSync is the synchronous TokenHub speech synthesis
	// endpoint (1823/135796, MiniMax Speech series hosted by TokenHub).
	HunyuanEndpointSpeechSync HunyuanActionEndpoint = "wand/minimax-tts/sync_tts"
	// HunyuanEndpointVoiceClone creates a reusable voice from a reference audio
	// sample (1823/135798, MiniMax 音色复刻). The vendor charges the clone fee on
	// the first synthesis that uses the returned voice_id, so only the optional
	// demo audio is priced at the gateway.
	HunyuanEndpointVoiceClone HunyuanActionEndpoint = "wand/minimax-tts/sync_clone"
	// HunyuanEndpointTextEmbeddings is the OpenAI-compatible text embedding
	// endpoint (1823/133515).
	HunyuanEndpointTextEmbeddings HunyuanActionEndpoint = "embeddings"
	// HunyuanEndpointMultimodalEmbeddings is the native fused text/image/video
	// embedding endpoint (1823/133515). It has no OpenAI-compatible equivalent.
	HunyuanEndpointMultimodalEmbeddings HunyuanActionEndpoint = "embeddings/multimodal"
	// HunyuanEndpoint3DSubmit and HunyuanEndpoint3DQuery are the HY-3D task pair
	// (1823/137181). Both are POST; the query carries {model, id}.
	HunyuanEndpoint3DSubmit HunyuanActionEndpoint = "api/3d/submit"
	HunyuanEndpoint3DQuery  HunyuanActionEndpoint = "api/3d/query"
	// HunyuanEndpointMessages and HunyuanEndpointResponses are the native
	// Anthropic and OpenAI Responses protocol paths on the same TokenHub host
	// (1823/130078, 1823/133813).
	HunyuanEndpointMessages  HunyuanActionEndpoint = "messages"
	HunyuanEndpointResponses HunyuanActionEndpoint = "responses"
)

// hunyuanAnthropicAuthEndpoints are the TokenHub paths documented to
// authenticate with x-api-key instead of Authorization: Bearer (1823/130078).
func (e HunyuanActionEndpoint) usesAnthropicAuth() bool { return e == HunyuanEndpointMessages }

// RequiresRequestBody reports whether the endpoint takes a JSON body.
func (e HunyuanActionEndpoint) RequiresRequestBody() bool {
	switch e {
	case HunyuanEndpointVideoTask, HunyuanEndpointTranscribeTask, HunyuanEndpointVideoContent:
		return false
	default:
		return true
	}
}

func (e HunyuanActionEndpoint) httpMethod() string {
	if e.RequiresRequestBody() {
		return http.MethodPost
	}
	return http.MethodGet
}

// carriesTaskID reports the endpoints whose URL embeds an owner-bound task id.
func (e HunyuanActionEndpoint) carriesTaskID() bool {
	switch e {
	case HunyuanEndpointVideoTask, HunyuanEndpointTranscribeTask:
		return true
	default:
		return false
	}
}

// IsImageGenerationRequest marks client-billable synchronous image submissions
// gated by the group image-generation permission.
func (e HunyuanActionEndpoint) IsImageGenerationRequest() bool {
	switch e {
	case HunyuanEndpointImageGeneration, HunyuanEndpointImagePanorama:
		return true
	default:
		return false
	}
}

// IsVideoCreateRequest marks asynchronous video submission; billing is deferred
// until a status/content read observes status=succeeded.
func (e HunyuanActionEndpoint) IsVideoCreateRequest() bool {
	return e == HunyuanEndpointVideoGeneration
}

// IsImageCreateRequest is the generation-submission alias used by the shared
// image billing and moderation flow.
func (e HunyuanActionEndpoint) IsImageCreateRequest() bool {
	return e.IsImageGenerationRequest()
}

// IsTaskCreateRequest covers every asynchronous submission that returns a task
// id but never bills on submit.
func (e HunyuanActionEndpoint) IsTaskCreateRequest() bool {
	switch e {
	case HunyuanEndpointVideoGeneration, HunyuanEndpointVideoScene,
		HunyuanEndpointTranscribeAsync, HunyuanEndpoint3DSubmit:
		return true
	default:
		return false
	}
}

// IsTaskLookupRequest covers owner-bound task reads (status plus content).
func (e HunyuanActionEndpoint) IsTaskLookupRequest() bool {
	switch e {
	case HunyuanEndpointVideoTask, HunyuanEndpointVideoContent, HunyuanEndpointTranscribeTask, HunyuanEndpoint3DQuery:
		return true
	default:
		return false
	}
}

// PlanProtocol labels the request-plan protocol dimension for this endpoint.
func (e HunyuanActionEndpoint) PlanProtocol() string {
	switch e {
	case HunyuanEndpointImageGeneration, HunyuanEndpointImagePanorama,
		HunyuanEndpointVideoGeneration, HunyuanEndpointVideoScene,
		HunyuanEndpointVideoTask, HunyuanEndpoint3DSubmit, HunyuanEndpoint3DQuery:
		return "media"
	case HunyuanEndpointTranscribeSync, HunyuanEndpointTranscribeAsync,
		HunyuanEndpointTranscribeTask, HunyuanEndpointSpeechSync,
		HunyuanEndpointVoiceClone:
		return "audio"
	case HunyuanEndpointTextEmbeddings, HunyuanEndpointMultimodalEmbeddings:
		return "embeddings"
	case HunyuanEndpointMessages:
		return "messages"
	case HunyuanEndpointResponses:
		return "responses"
	default:
		return "media"
	}
}

// buildHunyuanActionURL resolves the account base URL and appends the endpoint
// path. Task ids are validated so a caller cannot escape the documented path.
func (s *OpenAIGatewayService) buildHunyuanActionURL(account *Account, endpoint HunyuanActionEndpoint, taskID string) (string, error) {
	base, err := hunyuanActionBaseURL(account)
	if err != nil {
		return "", err
	}
	validated, err := s.validateUpstreamBaseURL(base)
	if err != nil {
		return "", err
	}
	path := string(endpoint)
	if endpoint.carriesTaskID() {
		taskID = strings.Trim(strings.TrimSpace(taskID), "/")
		if taskID == "" || taskID == "." || taskID == ".." || strings.ContainsAny(taskID, "/?#\\") {
			return "", errors.New("hunyuan task id is invalid")
		}
		path += "/" + url.PathEscape(taskID)
	}
	return strings.TrimRight(validated, "/") + "/" + path, nil
}

// HunyuanActionRequestInfo carries parsed inbound facts used for routing,
// plan snapshots, moderation and deferred billing.
type HunyuanActionRequestInfo struct {
	Endpoint        HunyuanActionEndpoint
	Model           string
	Stream          bool
	InputText       string
	HasInputImage   bool
	Size            string
	DurationSeconds int
	Resolution      string
	AspectRatio     string
	Count           int
	// TaskID is the vendor task identifier carried in the body instead of the
	// path, which is how the HY-3D query identifies its task (1823/137181).
	TaskID string
	// DemoSpeechModel is the tts_model that synthesizes the optional voice-clone
	// demo audio, so the demo characters are priced against the model that
	// actually produced them (1823/135798).
	DemoSpeechModel string
}

// ModerationBody normalizes an image generation prompt into the OpenAI images
// moderation shape so the shared extractor can read it.
func (r HunyuanActionRequestInfo) ModerationBody() []byte {
	prompt := strings.TrimSpace(r.InputText)
	if prompt == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{"prompt": prompt})
	if err != nil {
		return nil
	}
	return body
}

// ParseHunyuanActionRequest reads the inbound request facts. The body is always
// forwarded untouched; this only observes small scalar fields.
func ParseHunyuanActionRequest(endpoint HunyuanActionEndpoint, contentType string, body []byte) HunyuanActionRequestInfo {
	info := HunyuanActionRequestInfo{Endpoint: endpoint}
	if len(body) == 0 {
		return info
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/form-data") {
		// Multipart image/video edit uploads carry the model as a text part; a
		// full parse would buffer file bytes the caller still forwards.
		if value := extractMultipartFormField(body, contentType, "model"); value != "" {
			info.Model = value
		}
		info.HasInputImage = true
		info.InputText = extractMultipartFormField(body, contentType, "prompt")
		return info
	}
	if !gjson.ValidBytes(body) {
		return info
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	info.Stream = gjson.GetBytes(body, "stream").Bool()
	info.Size = strings.TrimSpace(gjson.GetBytes(body, "size").String())
	info.Resolution = strings.TrimSpace(gjson.GetBytes(body, "resolution").String())
	info.AspectRatio = strings.TrimSpace(gjson.GetBytes(body, "aspect_ratio").String())
	switch endpoint {
	case HunyuanEndpointImageGeneration, HunyuanEndpointImagePanorama:
		info.InputText = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
		info.Count = int(gjson.GetBytes(body, "n").Int())
		if info.Count == 0 {
			info.Count = 1
		}
		images := gjson.GetBytes(body, "images")
		info.HasInputImage = (images.IsArray() && len(images.Array()) > 0) || strings.TrimSpace(gjson.GetBytes(body, "image").String()) != ""
	case HunyuanEndpointVideoGeneration, HunyuanEndpointVideoScene:
		info.InputText = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
		info.HasInputImage = strings.TrimSpace(gjson.GetBytes(body, "image_url").String()) != "" ||
			strings.TrimSpace(gjson.GetBytes(body, "image").String()) != "" ||
			(gjson.GetBytes(body, "image_url").IsArray() && len(gjson.GetBytes(body, "image_url").Array()) > 0)
		info.DurationSeconds = int(gjson.GetBytes(body, "duration").Int())
		info.Count = int(gjson.GetBytes(body, "n").Int())
	case HunyuanEndpointTranscribeSync, HunyuanEndpointTranscribeAsync:
		info.InputText = ""
	case HunyuanEndpointSpeechSync:
		info.InputText = strings.TrimSpace(gjson.GetBytes(body, "text").String())
	case HunyuanEndpointVoiceClone:
		// text is the optional demo line synthesized with the cloned voice.
		info.InputText = strings.TrimSpace(gjson.GetBytes(body, "text").String())
		info.DemoSpeechModel = strings.TrimSpace(gjson.GetBytes(body, "tts_model").String())
	case HunyuanEndpoint3DQuery:
		// The HY-3D query is a POST whose body carries {model, id}; the id is the
		// owner-bound task key, so it is read here instead of from the path.
		info.TaskID = strings.TrimSpace(gjson.GetBytes(body, "id").String())
	}
	return info
}

// hunyuanVideoTaskStatus / hunyuanTranscribeTaskStatus are the documented
// terminal and in-flight states (1823/137202, 1823/135792).
const (
	hunyuanTaskStatusSucceeded = "succeeded"
	hunyuanTaskStatusCompleted = "completed"
	hunyuanTaskStatusFailed    = "failed"
)

// IsHunyuanVideoTaskSuccess matches the documented terminal success shape:
// status=succeeded with a downloadable videos[].url (1823/137202).
func IsHunyuanVideoTaskSuccess(statusBody []byte) bool {
	if len(statusBody) == 0 || !gjson.ValidBytes(statusBody) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(gjson.GetBytes(statusBody, "status").String()), hunyuanTaskStatusSucceeded) {
		return false
	}
	return hunyuanVideoResultURL(statusBody) != ""
}

// IsHunyuanVideoTaskTerminal reports whether polling may stop.
func IsHunyuanVideoTaskTerminal(statusBody []byte) bool {
	status := strings.ToLower(strings.TrimSpace(gjson.GetBytes(statusBody, "status").String()))
	return status == hunyuanTaskStatusSucceeded || status == hunyuanTaskStatusFailed
}

// IsHunyuanTranscribeTaskSuccess matches status=completed for the asynchronous
// speech recognition task (1823/135792).
func IsHunyuanTranscribeTaskSuccess(statusBody []byte) bool {
	if len(statusBody) == 0 || !gjson.ValidBytes(statusBody) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(gjson.GetBytes(statusBody, "status").String()), hunyuanTaskStatusCompleted)
}

func hunyuanVideoResultURL(statusBody []byte) string {
	for _, item := range gjson.GetBytes(statusBody, "videos").Array() {
		if raw := strings.TrimSpace(item.Get("url").String()); raw != "" {
			return raw
		}
	}
	return ""
}

// hunyuanTaskIDFromResponse reads the submit-time task identifier. Video and
// speech recognition use "task_id"/"id"; the 3D pair uses "id".
func hunyuanTaskIDFromResponse(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	for _, path := range []string{"task_id", "id"} {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

// hunyuanTaskSubmitAccepted reports whether an asynchronous submission produced
// a task id. Submissions answer 200 with a {"status":"failed"} envelope when the
// platform rejected the job, so a 2xx alone is not acceptance (1823/137202).
func hunyuanTaskSubmitAccepted(body []byte) bool {
	if hunyuanTaskIDFromResponse(body) == "" {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "status").String()))
	return status != hunyuanTaskStatusFailed
}

// HunyuanTaskPendingBilling snapshots submit-time billing evidence for an
// asynchronous task. The documented task status body carries neither model nor
// (for video) duration and resolution, so those must be captured when the task
// is accepted. Video tiers stay empty for speech recognition tasks.
type HunyuanTaskPendingBilling struct {
	Model                string `json:"model"`
	BillingModel         string `json:"billing_model,omitempty"`
	UpstreamModel        string `json:"upstream_model,omitempty"`
	OriginalModel        string `json:"original_model,omitempty"`
	VideoResolution      string `json:"video_resolution,omitempty"`
	VideoDurationSeconds int    `json:"video_duration_seconds,omitempty"`
	CreatedAt            string `json:"created_at,omitempty"`
}

// HunyuanTaskPendingCreatedAtNow stamps the submit-accept time for E2E duration.
func HunyuanTaskPendingCreatedAtNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// HunyuanTaskSessionHash scopes a durable task binding to owner and key so a
// task id can never resolve across tenants.
func HunyuanTaskSessionHash(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return "hunyuan-task:" + DeriveSessionHashFromSeed(fmt.Sprintf("%d:%d:%s", userID, apiKeyID, taskID))
}

func hunyuanTaskPendingBillingKey(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", userID, apiKeyID, taskID)
}

func hunyuanTaskPendingBillingTTL() time.Duration { return 24 * time.Hour }

func hunyuanTaskBilledClaimTTL() time.Duration { return 48 * time.Hour }

// BindHunyuanTaskAccount records which upstream account accepted a task. Status
// and content reads must resolve through this binding only.
func (s *OpenAIGatewayService) BindHunyuanTaskAccount(ctx context.Context, groupID *int64, taskID string, userID, apiKeyID, accountID int64) error {
	if s == nil || s.cache == nil {
		return errors.New("hunyuan task binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(HunyuanTaskSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" || accountID <= 0 {
		return errors.New("hunyuan task binding is invalid")
	}
	ttl := hunyuanTaskPendingBillingTTL()
	if s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		if sticky := time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second; sticky > ttl {
			ttl = sticky
		}
	}
	return s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), cacheKey, accountID, ttl)
}

// ResolveHunyuanTaskAccount returns the bound account id for an owner task.
func (s *OpenAIGatewayService) ResolveHunyuanTaskAccount(ctx context.Context, groupID *int64, taskID string, userID, apiKeyID int64) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, errors.New("hunyuan task binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(HunyuanTaskSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" {
		return 0, errors.New("hunyuan task binding is invalid")
	}
	return s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
}

// SelectHunyuanTaskAccount admits only the already bound owner account; reads
// must neither escape to another account nor refresh the binding.
func (s *OpenAIGatewayService) SelectHunyuanTaskAccount(
	ctx context.Context, groupID *int64, sessionHash string, accountID int64, requestedModel string,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	decision := OpenAIAccountScheduleDecision{Layer: openAIAccountScheduleLayerSessionSticky}
	if accountID <= 0 || strings.TrimSpace(sessionHash) == "" {
		return nil, decision, ErrNoAvailableAccounts
	}
	ctx = s.withOpenAIGroupPrivacyRequirement(WithOpenAIProfitControlSuppressed(ctx), groupID)
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	selection, _, err := scheduler.selectBySessionHash(ctx, OpenAIAccountScheduleRequest{
		GroupID: groupID, Platform: PlatformHunyuan, SessionHash: sessionHash,
		StickyAccountID: accountID, PreserveStickyBinding: true, DisableStickyEscape: true,
		RequestedModel: requestedModel, RequiredTransport: OpenAIUpstreamTransportHTTPSSE,
		RequirePrivacySet: s.openAIGroupRequiresPrivacySet(ctx, groupID),
	})
	if err != nil {
		return nil, decision, err
	}
	if selection == nil || selection.Account == nil {
		return nil, decision, ErrNoAvailableAccounts
	}
	decision.StickySessionHit = true
	decision.SelectedAccountID = selection.Account.ID
	decision.SelectedAccountType = selection.Account.Type
	return selection, decision, nil
}

// StoreHunyuanTaskPendingBilling persists submit-time billing evidence so a
// later status or content read can price the completed task.
func (s *OpenAIGatewayService) StoreHunyuanTaskPendingBilling(ctx context.Context, taskID string, userID, apiKeyID int64, pending HunyuanTaskPendingBilling) error {
	if s == nil || s.cache == nil {
		return errors.New("hunyuan pending billing cache is unavailable")
	}
	key := hunyuanTaskPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return errors.New("hunyuan pending billing key is invalid")
	}
	pending.Model = strings.TrimSpace(pending.Model)
	pending.BillingModel = strings.TrimSpace(pending.BillingModel)
	pending.UpstreamModel = strings.TrimSpace(pending.UpstreamModel)
	pending.OriginalModel = strings.TrimSpace(pending.OriginalModel)
	if pending.VideoResolution != "" {
		pending.VideoResolution = NormalizeVideoBillingResolutionOrDefault(pending.VideoResolution)
	}
	if pending.VideoDurationSeconds > 0 {
		pending.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(pending.VideoDurationSeconds)
	}
	if strings.TrimSpace(pending.CreatedAt) == "" {
		pending.CreatedAt = HunyuanTaskPendingCreatedAtNow()
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return s.cache.SetGrokVideoPendingBilling(ctx, key, payload, hunyuanTaskPendingBillingTTL())
}

// LoadHunyuanTaskPendingBilling returns the submit-time snapshot (nil on miss).
func (s *OpenAIGatewayService) LoadHunyuanTaskPendingBilling(ctx context.Context, taskID string, userID, apiKeyID int64) (*HunyuanTaskPendingBilling, error) {
	if s == nil || s.cache == nil {
		return nil, errors.New("hunyuan pending billing cache is unavailable")
	}
	key := hunyuanTaskPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return nil, errors.New("hunyuan pending billing key is invalid")
	}
	payload, err := s.cache.GetGrokVideoPendingBilling(ctx, key)
	if err != nil || len(payload) == 0 {
		return nil, err
	}
	var pending HunyuanTaskPendingBilling
	if err := json.Unmarshal(payload, &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

// ClaimHunyuanTaskBilling returns true once per completed task so concurrent
// status and content reads cannot double-bill. Fail-closed on cache errors.
func (s *OpenAIGatewayService) ClaimHunyuanTaskBilling(ctx context.Context, taskID string, userID, apiKeyID int64) (bool, error) {
	if s == nil || s.cache == nil {
		return false, errors.New("hunyuan billing claim cache is unavailable")
	}
	key := hunyuanTaskPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return false, errors.New("hunyuan billing claim key is invalid")
	}
	return s.cache.ClaimGrokVideoBilled(ctx, key, hunyuanTaskBilledClaimTTL())
}

// ReleaseHunyuanTaskBilling clears a claim after a failed durable RecordUsage
// so a later read can retry billing.
func (s *OpenAIGatewayService) ReleaseHunyuanTaskBilling(ctx context.Context, taskID string, userID, apiKeyID int64) error {
	if s == nil || s.cache == nil {
		return errors.New("hunyuan billing claim cache is unavailable")
	}
	key := hunyuanTaskPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return errors.New("hunyuan billing claim key is invalid")
	}
	return s.cache.ReleaseGrokVideoBilled(ctx, key)
}

// StableHunyuanVideoBillingRequestID is the durable usage key for one
// asynchronous task, independent of the per-poll gateway request id.
func StableHunyuanVideoBillingRequestID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	if strings.HasPrefix(taskID, "hunyuan-video:") {
		return taskID
	}
	return "hunyuan-video:" + taskID
}

// StableHunyuanAudioBillingRequestID forces a durable money-event id for
// speech synthesis and recognition so usage dedup cannot collapse under a
// reused or missing client request id.
func StableHunyuanAudioBillingRequestID(upstreamRequestID string) string {
	upstreamRequestID = strings.TrimSpace(upstreamRequestID)
	if strings.HasPrefix(upstreamRequestID, "hunyuan_audio:") {
		return upstreamRequestID
	}
	if upstreamRequestID == "" {
		upstreamRequestID = generateRequestID()
	}
	return "hunyuan_audio:" + upstreamRequestID
}

// hunyuanVideoResolutionFromSize maps a documented Hy-Image/video size onto the
// shared billing resolution tiers; 4K-class sizes cap at the top tier instead of
// defaulting to the bottom one.
func hunyuanVideoResolutionFromSize(size string) string {
	size = strings.ToLower(strings.TrimSpace(size))
	if size == "" {
		return ""
	}
	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil || width <= 0 || height <= 0 {
		return ""
	}
	shortSide := width
	if height < shortSide {
		shortSide = height
	}
	switch {
	case shortSide <= 480:
		return VideoBillingResolution480P
	case shortSide <= 720:
		return VideoBillingResolution720P
	default:
		return VideoBillingResolution1080P
	}
}

// BuildHunyuanTaskPendingBilling snapshots submit-time billing evidence. Video
// tasks need resolution and duration because the documented status body carries
// neither; the request may carry explicit values, so request evidence wins and
// the documented 5s/720p default fills the gap. Speech recognition tasks only
// need the model, so those video tiers stay empty.
func BuildHunyuanTaskPendingBilling(requestInfo HunyuanActionRequestInfo, billingModel, upstreamModel, originalModel, createdAt string) HunyuanTaskPendingBilling {
	pending := HunyuanTaskPendingBilling{
		Model:         strings.TrimSpace(requestInfo.Model),
		BillingModel:  strings.TrimSpace(billingModel),
		UpstreamModel: strings.TrimSpace(upstreamModel),
		OriginalModel: strings.TrimSpace(originalModel),
		CreatedAt:     strings.TrimSpace(createdAt),
	}
	if requestInfo.Endpoint == HunyuanEndpointVideoGeneration || requestInfo.Endpoint == HunyuanEndpointVideoScene {
		resolution := strings.TrimSpace(requestInfo.Resolution)
		if resolution == "" {
			resolution = hunyuanVideoResolutionFromSize(requestInfo.Size)
		}
		if resolution == "" {
			// The documented hy-video-v1.5 output is fixed 720p, so an omitted
			// request value must not fall back to the runtime floor of 480p.
			resolution = VideoBillingResolution720P
		}
		duration := requestInfo.DurationSeconds
		if duration <= 0 {
			duration = hunyuanVideoDefaultDurationSeconds
		}
		pending.VideoResolution = NormalizeVideoBillingResolutionOrDefault(resolution)
		pending.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(duration)
	}
	if pending.CreatedAt == "" {
		pending.CreatedAt = HunyuanTaskPendingCreatedAtNow()
	}
	return pending
}

// hunyuanVideoDefaultDurationSeconds is the documented fixed hy-video-v1.5
// output length (1823/137202).
const hunyuanVideoDefaultDurationSeconds = 5

// MergeHunyuanTaskPendingBilling overlays submit-time evidence onto a completed
// task result and pins the durable billing request id. Video tiers are only
// normalized when the snapshot actually carried them, so a speech recognition
// task never inherits video resolution or duration defaults.
func MergeHunyuanTaskPendingBilling(result *OpenAIForwardResult, pending *HunyuanTaskPendingBilling, taskID string) *OpenAIForwardResult {
	if result == nil {
		return result
	}
	merged := *result
	if pending != nil {
		merged.Model = firstNonEmpty(pending.BillingModel, pending.Model, merged.Model)
		merged.BillingModel = firstNonEmpty(pending.BillingModel, pending.Model, merged.BillingModel, merged.Model)
		merged.UpstreamModel = firstNonEmpty(pending.UpstreamModel, merged.UpstreamModel)
		if pending.VideoResolution != "" {
			merged.VideoResolution = NormalizeVideoBillingResolutionOrDefault(pending.VideoResolution)
		}
		if pending.VideoDurationSeconds > 0 {
			merged.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(pending.VideoDurationSeconds)
		}
		if e2e := hunyuanTaskE2EDuration(pending.CreatedAt, time.Now()); e2e > 0 {
			merged.Duration = e2e
		}
	}
	if merged.VideoCount > 0 {
		merged.RequestID = StableHunyuanVideoBillingRequestID(firstNonEmpty(merged.ResponseID, taskID))
	}
	return &merged
}

func hunyuanTaskE2EDuration(createdAt string, discoveredAt time.Time) time.Duration {
	createdAt = strings.TrimSpace(createdAt)
	if createdAt == "" {
		return 0
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		if created, err = time.Parse(time.RFC3339, createdAt); err != nil {
			return 0
		}
	}
	if discoveredAt.IsZero() {
		discoveredAt = time.Now()
	}
	if d := discoveredAt.Sub(created); d > 0 {
		return d
	}
	return 0
}

// hunyuanResponseTokenTotal reads the documented usage counters. TokenHub mixes
// three spellings: tokenhub_usage.total_tokens (image/video), usage.total_token
// (speech) and the OpenAI-style usage.total_tokens (embeddings).
//
//nolint:unused // referenced only from //go:build unit tests, so the default lint pass cannot see its callers.
func hunyuanResponseTokenTotal(body []byte) int {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return 0
	}
	for _, path := range []string{"tokenhub_usage.total_tokens", "usage.total_token", "usage.total_tokens", "usage.prompt_tokens"} {
		if value := gjson.GetBytes(body, path); value.Exists() && value.Int() > 0 {
			return int(value.Int())
		}
	}
	return 0
}

// hunyuanActionUsageFromResponse builds token usage for endpoints billed inside
// ForwardHunyuanAction. Image and video tasks are priced through ImageCount,
// VideoCount and the pending-billing snapshot instead.
func hunyuanActionUsageFromResponse(endpoint HunyuanActionEndpoint, body []byte) OpenAIUsage {
	switch endpoint {
	case HunyuanEndpointTextEmbeddings, HunyuanEndpointMultimodalEmbeddings:
		return extractOpenAIEmbeddingsUsage(body)
	default:
		return OpenAIUsage{}
	}
}

// hunyuanVoiceCloneOutcome reads the documented 音色复刻 envelope: voice_id,
// input_sensitive and the optional demo-audio counters (1823/135798).
// input_sensitive marks a content rejection of the reference audio, which is a
// client-input failure rather than a transient upstream fault.
func hunyuanVoiceCloneOutcome(body []byte) (voiceID string, inputSensitive bool) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return "", false
	}
	return strings.TrimSpace(gjson.GetBytes(body, "voice_id").String()), gjson.GetBytes(body, "input_sensitive").Bool()
}

// estimateHunyuanAudioUsage derives billing units from the documented fields:
// speech synthesis reports extra_info.usage_characters (million characters),
// speech recognition reports output.duration_ms (hours).
func estimateHunyuanAudioUsage(endpoint HunyuanActionEndpoint, reqBody, respBody []byte, elapsed time.Duration) *AudioUsage {
	switch endpoint {
	case HunyuanEndpointSpeechSync:
		if gjson.ValidBytes(respBody) {
			if chars := gjson.GetBytes(respBody, "extra_info.usage_characters").Int(); chars > 0 {
				return &AudioUsage{Mode: "tts", DurationOrUnits: float64(chars) / 1_000_000.0}
			}
		}
		if !gjson.ValidBytes(reqBody) {
			return nil
		}
		chars := len([]rune(strings.TrimSpace(gjson.GetBytes(reqBody, "text").String())))
		if chars <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "tts", DurationOrUnits: float64(chars) / 1_000_000.0}
	case HunyuanEndpointVoiceClone:
		// Creating the voice itself carries no gateway unit: the vendor charges
		// the clone fee on the first synthesis that uses the returned voice_id,
		// which the gateway cannot attribute to this request. Only the optional
		// demo audio (request carried text) is billable, and it is synthesized by
		// tts_model, so it uses the same TTS character unit.
		if voiceID, _ := hunyuanVoiceCloneOutcome(respBody); voiceID == "" {
			return nil
		}
		if gjson.ValidBytes(respBody) {
			if chars := gjson.GetBytes(respBody, "extra_info.usage_characters").Int(); chars > 0 {
				return &AudioUsage{Mode: "tts", DurationOrUnits: float64(chars) / 1_000_000.0}
			}
		}
		if !gjson.ValidBytes(reqBody) {
			return nil
		}
		demoChars := len([]rune(strings.TrimSpace(gjson.GetBytes(reqBody, "text").String())))
		if demoChars <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "tts", DurationOrUnits: float64(demoChars) / 1_000_000.0}
	case HunyuanEndpointTranscribeSync, HunyuanEndpointTranscribeTask:
		if !gjson.ValidBytes(respBody) || (endpoint == HunyuanEndpointTranscribeTask && !IsHunyuanTranscribeTaskSuccess(respBody)) {
			return nil
		}
		duration := gjson.GetBytes(respBody, "output.duration_ms")
		milliseconds := duration.Float()
		if duration.Type != gjson.Number || milliseconds <= 0 || math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) {
			return nil
		}
		hours := milliseconds / 1000 / 3600
		if hours <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "stt", DurationOrUnits: hours}
	default:
		return nil
	}
}

// HunyuanTranscribeTaskBillingRequestID pins one durable usage id per speech
// recognition task so repeated polling cannot double-bill.
func HunyuanTranscribeTaskBillingRequestID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	if strings.HasPrefix(taskID, "hunyuan-transcribe:") {
		return taskID
	}
	return "hunyuan-transcribe:" + taskID
}

// hunyuanImageOutputCount counts generated images for the endpoint's documented
// envelope. Hy-Image-3.0 answers with data[].url (1823/135745) while the
// Hy-World-2.1 panorama answers with data[].super_resolution_image_url plus a
// preview cover_image_url (1823/136578), so the OpenAI counter alone cannot
// judge the panorama family.
func hunyuanImageOutputCount(endpoint HunyuanActionEndpoint, body []byte) int {
	if endpoint == HunyuanEndpointImagePanorama {
		if len(body) == 0 || !gjson.ValidBytes(body) {
			return 0
		}
		count := 0
		for _, item := range gjson.GetBytes(body, "data").Array() {
			if strings.TrimSpace(item.Get("super_resolution_image_url").String()) != "" ||
				strings.TrimSpace(item.Get("cover_image_url").String()) != "" {
				count++
			}
		}
		return count
	}
	return countOpenAIResponseImageOutputsFromJSONBytes(body)
}

// ForwardHunyuanAction forwards one TokenHub native action through the shared
// OpenAI-trunk account, error, proxy and concurrency policies. Responses are
// passed through: JSON for most actions, the upstream media bytes for content
// downloads, SSE when the vendor endpoint streams.
func (s *OpenAIGatewayService) ForwardHunyuanAction(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint HunyuanActionEndpoint,
	taskID string,
	body []byte,
	contentType string,
	rawQuery string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil || account.Platform != PlatformHunyuan {
		return nil, errors.New("hunyuan account is required")
	}
	if endpoint == HunyuanEndpointVideoContent {
		return s.ForwardHunyuanVideoContent(ctx, c, account, taskID, startTime)
	}
	_, apiKey, err := hunyuanAccountEndpoint(account)
	if err != nil {
		return nil, err
	}
	targetURL, err := s.buildHunyuanActionURL(account, endpoint, taskID)
	if err != nil {
		return nil, err
	}
	if !endpoint.RequiresRequestBody() && strings.TrimSpace(rawQuery) != "" {
		targetURL += "?" + strings.TrimPrefix(rawQuery, "?")
	}

	requestInfo := ParseHunyuanActionRequest(endpoint, contentType, body)
	upstreamModel := requestInfo.Model
	if endpoint.RequiresRequestBody() && gjson.ValidBytes(body) {
		if mappedModel := strings.TrimSpace(account.GetMappedModel(requestInfo.Model)); mappedModel != "" {
			upstreamModel = mappedModel
		}
		if upstreamModel != requestInfo.Model {
			if body, err = sjson.SetBytes(body, "model", upstreamModel); err != nil {
				return nil, fmt.Errorf("rewrite hunyuan action mapped model: %w", err)
			}
		}
	}

	var bodyReader io.Reader
	if endpoint.RequiresRequestBody() {
		bodyReader = bytes.NewReader(body)
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, endpoint.httpMethod(), targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	if endpoint.usesAnthropicAuth() {
		upstreamReq.Header.Set("x-api-key", apiKey)
	} else {
		upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	upstreamReq.Header.Set("Accept", "application/json")
	if requestInfo.Stream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	}
	if endpoint.RequiresRequestBody() {
		contentType = strings.TrimSpace(contentType)
		if contentType == "" {
			contentType = "application/json"
		}
		upstreamReq.Header.Set("Content-Type", contentType)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := generationDo(s.httpUpstream, upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	requestIDHeader := strings.TrimSpace(resp.Header.Get("x-request-id"))
	if resp.StatusCode >= 400 {
		return s.handleErrorResponse(ctx, resp, c, account, body, requestInfo.Model)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if endpoint.IsImageGenerationRequest() && hunyuanImageOutputCount(endpoint, respBody) <= 0 {
		setOpsUpstreamError(c, http.StatusBadGateway, "hunyuan upstream returned no image output", truncateString(string(respBody), 512))
		return nil, &UpstreamFailoverError{
			StatusCode:      http.StatusBadGateway,
			ResponseBody:    respBody,
			ResponseHeaders: resp.Header.Clone(),
		}
	}
	if endpoint.IsTaskCreateRequest() && !hunyuanTaskSubmitAccepted(respBody) {
		// The vendor answers 200 with {"status":"failed"} when the platform
		// rejected the job; without a task id the request must not be billed as
		// accepted and failover may retry it.
		setOpsUpstreamError(c, http.StatusBadGateway, "hunyuan upstream rejected the task submission", truncateString(string(respBody), 512))
		return nil, &UpstreamFailoverError{
			StatusCode:      http.StatusBadGateway,
			ResponseBody:    respBody,
			ResponseHeaders: resp.Header.Clone(),
		}
	}
	if endpoint == HunyuanEndpointVoiceClone {
		// 音色复刻 answers 200 with input_sensitive=true when the reference audio
		// was rejected by the vendor's content policy; that is a client-input
		// failure, so the vendor body is passed through instead of failing over.
		// Any other success envelope without a voice_id is unusable and fails over.
		voiceID, inputSensitive := hunyuanVoiceCloneOutcome(respBody)
		if voiceID == "" && !inputSensitive {
			setOpsUpstreamError(c, http.StatusBadGateway, "hunyuan upstream returned no cloned voice id", truncateString(string(respBody), 512))
			return nil, &UpstreamFailoverError{
				StatusCode:      http.StatusBadGateway,
				ResponseBody:    respBody,
				ResponseHeaders: resp.Header.Clone(),
			}
		}
	}
	if endpoint == HunyuanEndpointVideoTask {
		respBody = rewriteHunyuanVideoContentURLs(respBody, taskID, hunyuanVideoContentProxyURL(c, taskID))
	}

	writeGrokMediaResponse(c, resp, respBody, s.responseHeaderFilter)

	result := &OpenAIForwardResult{
		RequestID:       requestIDHeader,
		UpstreamHeaders: resp.Header,
		Usage:           hunyuanActionUsageFromResponse(endpoint, respBody),
		Model:           requestInfo.Model,
		BillingModel:    requestInfo.Model,
		UpstreamModel:   upstreamModel,
		ResponseHeaders: resp.Header.Clone(),
		Duration:        time.Since(startTime),
	}
	switch endpoint {
	case HunyuanEndpointImageGeneration, HunyuanEndpointImagePanorama:
		result.ImageCount = hunyuanImageOutputCount(endpoint, respBody)
		result.ImageOutputSize = strings.TrimSpace(gjson.GetBytes(respBody, "data.0.size").String())
		if result.ImageOutputSize == "" {
			result.ImageOutputSize = requestInfo.Size
		}
	case HunyuanEndpointVideoGeneration, HunyuanEndpointVideoScene:
		result.ResponseID = hunyuanTaskIDFromResponse(respBody)
	case HunyuanEndpointTranscribeAsync:
		result.ResponseID = hunyuanTaskIDFromResponse(respBody)
	case HunyuanEndpointTranscribeSync:
		result.AudioUsage = estimateHunyuanAudioUsage(endpoint, body, respBody, time.Since(startTime))
		result.AudioUsageUnresolved = result.AudioUsage == nil
	case HunyuanEndpointTranscribeTask:
		result.ResponseID = strings.TrimSpace(taskID)
		result.AudioUsage = estimateHunyuanAudioUsage(endpoint, body, respBody, time.Since(startTime))
		result.AudioUsageUnresolved = IsHunyuanTranscribeTaskSuccess(respBody) && result.AudioUsage == nil
	case HunyuanEndpointSpeechSync:
		result.AudioUsage = estimateHunyuanAudioUsage(endpoint, body, respBody, time.Since(startTime))
		if result.Model == "" {
			result.Model = string(endpoint)
			result.BillingModel = result.Model
		}
	case HunyuanEndpointVoiceClone:
		result.AudioUsage = estimateHunyuanAudioUsage(endpoint, body, respBody, time.Since(startTime))
		if result.AudioUsage != nil && requestInfo.DemoSpeechModel != "" {
			// The demo audio is synthesized by tts_model, so those characters are
			// priced against the model that produced them.
			result.Model = requestInfo.DemoSpeechModel
			result.BillingModel = requestInfo.DemoSpeechModel
		}
		if voiceID, _ := hunyuanVoiceCloneOutcome(respBody); voiceID != "" {
			result.ResponseID = voiceID
		}
	case HunyuanEndpointVideoTask:
		result.ResponseID = strings.TrimSpace(taskID)
		if statusModel := strings.TrimSpace(gjson.GetBytes(respBody, "model").String()); statusModel != "" {
			result.Model = statusModel
			result.BillingModel = statusModel
		}
		if IsHunyuanVideoTaskSuccess(respBody) {
			result.VideoCount = 1
		}
	case HunyuanEndpoint3DSubmit, HunyuanEndpoint3DQuery:
		result.ResponseID = hunyuanTaskIDFromResponse(respBody)
	}
	return result, nil
}

// ForwardHunyuanVideoContent proxies the CDN download of a completed video
// task. The task is re-read through the owner-bound account so the temporary
// vendor URL never leaves the gateway; unready tasks see the status body.
func (s *OpenAIGatewayService) ForwardHunyuanVideoContent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	taskID string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	_, apiKey, err := hunyuanAccountEndpoint(account)
	if err != nil {
		return nil, err
	}
	statusURL, err := s.buildHunyuanActionURL(account, HunyuanEndpointVideoTask, taskID)
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	statusReq, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(upstreamCtx), http.MethodGet, statusURL, nil)
	if err != nil {
		return nil, err
	}
	statusReq.Header.Set("Authorization", "Bearer "+apiKey)
	statusReq.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(statusReq.Header)

	upstreamStart := time.Now()
	statusResp, err := generationDo(s.httpUpstream, statusReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = statusResp.Body.Close() }()
	if statusResp.StatusCode >= 300 {
		if statusResp.StatusCode < 400 {
			return nil, errors.New("hunyuan video task redirect is not allowed")
		}
		return s.handleErrorResponse(ctx, statusResp, c, account, nil, "")
	}
	statusBody, err := ReadUpstreamResponseBody(statusResp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	videoURL := hunyuanVideoResultURL(statusBody)
	if !IsHunyuanVideoTaskSuccess(statusBody) || videoURL == "" {
		// Task not ready or failed: surface the official status body as-is.
		writeGrokMediaResponse(c, statusResp, statusBody, s.responseHeaderFilter)
		return &OpenAIForwardResult{ResponseID: strings.TrimSpace(taskID), Duration: time.Since(startTime)}, nil
	}
	parsed, err := url.Parse(videoURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		setOpsUpstreamError(c, http.StatusBadGateway, "hunyuan upstream returned an invalid video url", truncateString(videoURL, 256))
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: statusBody}
	}

	contentReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodGet, videoURL, nil)
	if err != nil {
		return nil, err
	}
	// CDN download: no upstream credential leaks to the media host; client
	// Range and Accept preferences pass through for resumable downloads.
	if rangeHeader := strings.TrimSpace(c.GetHeader("Range")); rangeHeader != "" {
		contentReq.Header.Set("Range", rangeHeader)
	}
	contentReq.Header.Set("Accept", "*/*")
	contentResp, err := generationDo(s.httpUpstream, contentReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = contentResp.Body.Close() }()
	if contentResp.StatusCode >= 400 {
		return s.handleErrorResponse(ctx, contentResp, c, account, nil, "")
	}
	if err := writeGrokMediaContentResponse(c, contentResp); err != nil {
		return nil, err
	}
	return &OpenAIForwardResult{
		ResponseID: strings.TrimSpace(taskID),
		Duration:   time.Since(startTime),
		VideoCount: 1,
		RequestID:  StableHunyuanVideoBillingRequestID(taskID),
	}, nil
}

// rewriteHunyuanVideoContentURLs rewrites videos[].url in a task status body to
// the gateway content proxy so downloads inherit the owner check.
func rewriteHunyuanVideoContentURLs(body []byte, taskID, proxyURL string) []byte {
	if len(body) == 0 || !gjson.ValidBytes(body) || strings.TrimSpace(proxyURL) == "" {
		return body
	}
	items := gjson.GetBytes(body, "videos").Array()
	if len(items) == 0 {
		return body
	}
	out := body
	for index := range items {
		rewritten, err := sjson.SetBytes(out, fmt.Sprintf("videos.%d.url", index), proxyURL)
		if err == nil {
			out = rewritten
		}
	}
	return out
}

// hunyuanVideoContentProxyURL builds the client-facing gateway content URL.
func hunyuanVideoContentProxyURL(c *gin.Context, taskID string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil || strings.TrimSpace(taskID) == "" {
		return ""
	}
	pathPrefix := ""
	if strings.HasPrefix(c.Request.URL.Path, "/v1/") {
		pathPrefix = "/v1"
	}
	return pathPrefix + "/videos/" + url.PathEscape(strings.Trim(taskID, "/")) + "/content"
}

// --- TokenHub error classification ---

// hunyuanBusinessCodeScope maps the documented TokenHub business codes
// (1823/137202 appendix, rechecked 2026-09-21) onto the gateway's coarse
// upstream scopes. It never proves model absence or permanent revocation: the
// result only decides retry and failover behaviour.
func hunyuanBusinessCodeScope(businessCode int) (scope string, known bool) {
	switch businessCode {
	case 1103:
		return "account_permission", true
	case 1100, 1101, 1102:
		return "account_quota", true
	case 1000, 1001, 1002, 1003, 1004:
		return "account_authentication", true
	case 1300, 1301, 1304:
		return "content_policy", true
	case 1302, 1303:
		return "rate_limit", true
	case 5000, 5001, 5002:
		return "upstream_unavailable", true
	case 1200, 1201, 1202, 1203:
		return "request", true
	default:
		return "request", false
	}
}

// HunyuanActionErrorScope classifies one native action failure. It reads the
// documented business code when present and otherwise falls back to the HTTP
// status; an unrecognized code stays request-scoped instead of inventing a
// stronger claim.
func HunyuanActionErrorScope(statusCode int, body []byte) string {
	businessCode := 0
	if len(body) > 0 && gjson.ValidBytes(body) {
		for _, path := range []string{"error.code", "base_resp.status_code"} {
			value := gjson.GetBytes(body, path)
			if !value.Exists() {
				continue
			}
			// The video envelope carries error.code as a string ("1300") while
			// base_resp.status_code is numeric, so both spellings are accepted.
			if value.Type == gjson.String {
				if parsed, convErr := strconv.Atoi(strings.TrimSpace(value.String())); convErr == nil {
					businessCode = parsed
				}
			} else {
				businessCode = int(value.Int())
			}
			if businessCode != 0 {
				break
			}
		}
	}
	if businessCode != 0 {
		if scope, known := hunyuanBusinessCodeScope(businessCode); known {
			return scope
		}
	}
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "account_authentication"
	case http.StatusTooManyRequests:
		return "rate_limit"
	case http.StatusUnprocessableEntity:
		return "content_policy"
	default:
		return "request"
	}
}
