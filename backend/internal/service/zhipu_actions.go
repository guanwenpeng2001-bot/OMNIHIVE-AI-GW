package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ZhipuActionEndpoint identifies a native Zhipu (bigmodel.cn open platform)
// action path. Wire shapes verified against docs.bigmodel.cn API reference
// (evidence fetched 2026-09-25, see worker report).
type ZhipuActionEndpoint string

const (
	ZhipuEndpointImagesGenerations   ZhipuActionEndpoint = "images/generations"
	ZhipuEndpointVideosGenerations   ZhipuActionEndpoint = "videos/generations"
	ZhipuEndpointAsyncResult         ZhipuActionEndpoint = "async-result"
	ZhipuEndpointVideoContent        ZhipuActionEndpoint = "video-content"
	ZhipuEndpointEmbeddings          ZhipuActionEndpoint = "embeddings"
	ZhipuEndpointRerank              ZhipuActionEndpoint = "rerank"
	ZhipuEndpointAudioSpeech         ZhipuActionEndpoint = "audio/speech"
	ZhipuEndpointAudioTranscriptions ZhipuActionEndpoint = "audio/transcriptions"
	ZhipuEndpointVoiceClone          ZhipuActionEndpoint = "voice/clone"
	ZhipuEndpointVoiceList           ZhipuActionEndpoint = "voice/list"
	ZhipuEndpointVoiceDelete         ZhipuActionEndpoint = "voice/delete"
	ZhipuEndpointWebSearch           ZhipuActionEndpoint = "web_search"
	ZhipuEndpointRealtime            ZhipuActionEndpoint = "realtime"
)

func (e ZhipuActionEndpoint) RequiresRequestBody() bool {
	switch e {
	case ZhipuEndpointAsyncResult, ZhipuEndpointVideoContent, ZhipuEndpointVoiceList:
		return false
	default:
		return true
	}
}

func (e ZhipuActionEndpoint) httpMethod() string {
	if e.RequiresRequestBody() {
		return http.MethodPost
	}
	return http.MethodGet
}

// IsVideoCreateRequest reports async video task creation (billing is deferred
// until a status/content poll observes task_status=SUCCESS with video_result).
func (e ZhipuActionEndpoint) IsVideoCreateRequest() bool {
	return e == ZhipuEndpointVideosGenerations
}

// IsVideoLookupRequest covers owner-bound task queries: status and content.
func (e ZhipuActionEndpoint) IsVideoLookupRequest() bool {
	return e == ZhipuEndpointAsyncResult || e == ZhipuEndpointVideoContent
}

// IsGenerationRequest covers client-billable generation submissions gated by
// the group image-generation permission (image + video create).
func (e ZhipuActionEndpoint) IsGenerationRequest() bool {
	return e == ZhipuEndpointImagesGenerations || e == ZhipuEndpointVideosGenerations
}

// PlanProtocol labels the request-plan protocol dimension for this endpoint.
func (e ZhipuActionEndpoint) PlanProtocol() string {
	switch e {
	case ZhipuEndpointImagesGenerations, ZhipuEndpointVideosGenerations,
		ZhipuEndpointAsyncResult, ZhipuEndpointVideoContent:
		return "media"
	case ZhipuEndpointAudioSpeech, ZhipuEndpointAudioTranscriptions,
		ZhipuEndpointVoiceClone, ZhipuEndpointVoiceList, ZhipuEndpointVoiceDelete:
		return "audio"
	case ZhipuEndpointEmbeddings:
		return "embeddings"
	case ZhipuEndpointRerank:
		return "rerank"
	case ZhipuEndpointWebSearch:
		return "search"
	case ZhipuEndpointRealtime:
		return "realtime"
	default:
		return "media"
	}
}

// zhipuCodingPlanPathSegment marks GLM Coding Plan base URLs; media, search,
// rerank, voice and realtime endpoints only exist on the open-platform domain.
const zhipuCodingPlanPathSegment = "/api/coding/paas/"

const zhipuOpenPlatformPathSegment = "/api/paas/"

// zhipuActionBaseURL resolves the account base URL for non-chat action
// families. Coding-plan credentials share the same host and API key but the
// coding domain serves chat only, so the path is mapped back to the open
// platform; plan coverage failures surface as ordinary upstream errors.
func zhipuActionBaseURL(account *Account) string {
	if account == nil {
		return ""
	}
	base := strings.TrimSpace(account.GetOpenAIFormatBaseURL())
	if strings.Contains(base, zhipuCodingPlanPathSegment) {
		base = strings.Replace(base, zhipuCodingPlanPathSegment, zhipuOpenPlatformPathSegment, 1)
	}
	return base
}

func (s *OpenAIGatewayService) buildZhipuActionURL(account *Account, endpoint ZhipuActionEndpoint, taskID string) (string, error) {
	if account == nil || account.Platform != PlatformZhipu {
		return "", fmt.Errorf("zhipu account is required")
	}
	base, err := s.validateUpstreamBaseURL(zhipuActionBaseURL(account))
	if err != nil {
		return "", err
	}
	path := string(endpoint)
	if endpoint == ZhipuEndpointAsyncResult {
		taskID = strings.Trim(strings.TrimSpace(taskID), "/")
		if taskID == "" || taskID == "." || taskID == ".." || strings.ContainsAny(taskID, "/?#\\") {
			return "", fmt.Errorf("zhipu async task id is invalid")
		}
		path += "/" + url.PathEscape(taskID)
	}
	return strings.TrimRight(base, "/") + "/" + path, nil
}

// ZhipuActionRequestInfo carries parsed inbound request facts used for
// routing, moderation, plan and billing snapshots.
type ZhipuActionRequestInfo struct {
	Model           string
	Stream          bool
	HasInputImage   bool
	DurationSeconds int
	Size            string
	InputText       string
	SearchEngine    string
}

// ModerationBody normalizes generation prompts for the content moderation
// extractor, mirroring the OpenAI images moderation shape.
func (r ZhipuActionRequestInfo) ModerationBody() []byte {
	payload := map[string]any{}
	if prompt := strings.TrimSpace(r.InputText); prompt != "" {
		payload["prompt"] = prompt
	}
	if len(payload) == 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}

// ParseZhipuActionRequest extracts request facts from JSON or multipart
// bodies. Multipart (STT, voice clone) only surfaces the model form field;
// the raw body is forwarded untouched so file parts survive.
func ParseZhipuActionRequest(endpoint ZhipuActionEndpoint, contentType string, body []byte) ZhipuActionRequestInfo {
	info := ZhipuActionRequestInfo{}
	if len(body) == 0 {
		return info
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/form-data") {
		// Form fields sit between boundary markers; a full multipart parse would
		// buffer file bytes we already hold, so read the small model field only.
		if value := extractMultipartFormField(body, contentType, "model"); value != "" {
			info.Model = value
		}
		return info
	}
	if !gjson.ValidBytes(body) {
		return info
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	info.Stream = gjson.GetBytes(body, "stream").Bool()
	info.Size = strings.TrimSpace(gjson.GetBytes(body, "size").String())
	info.SearchEngine = strings.TrimSpace(gjson.GetBytes(body, "search_engine").String())
	switch endpoint {
	case ZhipuEndpointImagesGenerations:
		info.InputText = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	case ZhipuEndpointVideosGenerations:
		info.InputText = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
		imageURL := gjson.GetBytes(body, "image_url")
		info.HasInputImage = strings.TrimSpace(imageURL.String()) != "" || (imageURL.IsArray() && len(imageURL.Array()) > 0)
		info.DurationSeconds = int(gjson.GetBytes(body, "duration").Int())
	case ZhipuEndpointAudioSpeech:
		info.InputText = strings.TrimSpace(gjson.GetBytes(body, "input").String())
	case ZhipuEndpointWebSearch:
		info.InputText = strings.TrimSpace(gjson.GetBytes(body, "search_query").String())
	}
	return info
}

// extractMultipartFormField reads a small text form field from a multipart
// body without touching file parts. Best-effort: returns "" when absent.
func extractMultipartFormField(body []byte, contentType, field string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	boundary := params["boundary"]
	if boundary == "" {
		return ""
	}
	needle := []byte(`Content-Disposition: form-data; name="` + field + `"`)
	idx := bytes.Index(body, needle)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(needle):]
	// Optional per-part headers end at the first blank line.
	headerEnd := bytes.Index(rest, []byte("\r\n\r\n"))
	if headerEnd < 0 || headerEnd > 512 {
		return ""
	}
	value := rest[headerEnd+4:]
	end := bytes.Index(value, []byte("\r\n--"+boundary))
	if end < 0 {
		return ""
	}
	value = bytes.TrimSpace(value[:end])
	if len(value) > 256 {
		return ""
	}
	return string(value)
}

// ZhipuVideoPendingBilling is the create-time snapshot for deferred video
// billing: the async-result status body carries neither duration nor size, so
// billing evidence must come from the accepted create request.
type ZhipuVideoPendingBilling struct {
	Model                string `json:"model"`
	BillingModel         string `json:"billing_model,omitempty"`
	UpstreamModel        string `json:"upstream_model,omitempty"`
	VideoResolution      string `json:"video_resolution,omitempty"`
	VideoDurationSeconds int    `json:"video_duration_seconds,omitempty"`
	OriginalModel        string `json:"original_model,omitempty"`
	CreatedAt            string `json:"created_at,omitempty"`
}

// ZhipuVideoPendingCreatedAtNow stamps create-accept time for E2E duration.
func ZhipuVideoPendingCreatedAtNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// ZhipuVideoTaskSessionHash scopes a video task binding to owner + key so a
// task id can never be resolved across tenants.
func ZhipuVideoTaskSessionHash(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return "zhipu-video:" + DeriveSessionHashFromSeed(fmt.Sprintf("%d:%d:%s", userID, apiKeyID, taskID))
}

func zhipuVideoPendingBillingKey(taskID string, userID, apiKeyID int64) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", userID, apiKeyID, taskID)
}

func zhipuVideoPendingBillingTTL(cfg *config.Config) time.Duration {
	_ = cfg
	return 24 * time.Hour
}

func zhipuVideoBilledClaimTTL(cfg *config.Config) time.Duration {
	_ = cfg
	return 48 * time.Hour
}

// BindZhipuVideoTaskAccount records which upstream account accepted a video
// task; status/content polls must resolve through this binding only.
func (s *OpenAIGatewayService) BindZhipuVideoTaskAccount(ctx context.Context, groupID *int64, taskID string, userID, apiKeyID, accountID int64) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("zhipu video task binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(ZhipuVideoTaskSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" || accountID <= 0 {
		return fmt.Errorf("zhipu video task binding is invalid")
	}
	ttl := zhipuVideoPendingBillingTTL(s.cfg)
	if s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		if sticky := time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second; sticky > ttl {
			ttl = sticky
		}
	}
	return s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), cacheKey, accountID, ttl)
}

// ResolveZhipuVideoTaskAccount returns the bound account id for an owner task.
func (s *OpenAIGatewayService) ResolveZhipuVideoTaskAccount(ctx context.Context, groupID *int64, taskID string, userID, apiKeyID int64) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, fmt.Errorf("zhipu video task binding cache is unavailable")
	}
	cacheKey := s.openAISessionCacheKey(ZhipuVideoTaskSessionHash(taskID, userID, apiKeyID))
	if cacheKey == "" {
		return 0, fmt.Errorf("zhipu video task binding is invalid")
	}
	return s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
}

// SelectZhipuVideoTaskAccount admits only the already bound owner account;
// lookups must neither escape to another account nor refresh the binding.
func (s *OpenAIGatewayService) SelectZhipuVideoTaskAccount(
	ctx context.Context, groupID *int64, sessionHash string, accountID int64, requestedModel string,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	decision := OpenAIAccountScheduleDecision{Layer: openAIAccountScheduleLayerSessionSticky}
	if accountID <= 0 || strings.TrimSpace(sessionHash) == "" {
		return nil, decision, ErrNoAvailableAccounts
	}
	ctx = s.withOpenAIGroupPrivacyRequirement(WithOpenAIProfitControlSuppressed(ctx), groupID)
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	selection, _, err := scheduler.selectBySessionHash(ctx, OpenAIAccountScheduleRequest{
		GroupID: groupID, Platform: PlatformZhipu, SessionHash: sessionHash,
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

// StoreZhipuVideoPendingBilling persists create-time billing params so the
// deferred status poll can price a completed task.
func (s *OpenAIGatewayService) StoreZhipuVideoPendingBilling(ctx context.Context, taskID string, userID, apiKeyID int64, pending ZhipuVideoPendingBilling) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("zhipu video pending billing cache is unavailable")
	}
	key := zhipuVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("zhipu video pending billing key is invalid")
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
		pending.CreatedAt = ZhipuVideoPendingCreatedAtNow()
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return s.cache.SetGrokVideoPendingBilling(ctx, key, payload, zhipuVideoPendingBillingTTL(s.cfg))
}

// LoadZhipuVideoPendingBilling returns the create-time snapshot (nil on miss).
func (s *OpenAIGatewayService) LoadZhipuVideoPendingBilling(ctx context.Context, taskID string, userID, apiKeyID int64) (*ZhipuVideoPendingBilling, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("zhipu video pending billing cache is unavailable")
	}
	key := zhipuVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return nil, fmt.Errorf("zhipu video pending billing key is invalid")
	}
	payload, err := s.cache.GetGrokVideoPendingBilling(ctx, key)
	if err != nil || len(payload) == 0 {
		return nil, err
	}
	var pending ZhipuVideoPendingBilling
	if err := json.Unmarshal(payload, &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

// ClaimZhipuVideoBilling returns true once per completed task so concurrent
// status/content polls cannot double-bill. Fail-closed on cache errors.
func (s *OpenAIGatewayService) ClaimZhipuVideoBilling(ctx context.Context, taskID string, userID, apiKeyID int64) (bool, error) {
	if s == nil || s.cache == nil {
		return false, fmt.Errorf("zhipu video billing claim cache is unavailable")
	}
	key := zhipuVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return false, fmt.Errorf("zhipu video billing claim key is invalid")
	}
	return s.cache.ClaimGrokVideoBilled(ctx, key, zhipuVideoBilledClaimTTL(s.cfg))
}

// ReleaseZhipuVideoBilling clears a claim after a failed durable RecordUsage
// so a later poll can retry billing.
func (s *OpenAIGatewayService) ReleaseZhipuVideoBilling(ctx context.Context, taskID string, userID, apiKeyID int64) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("zhipu video billing claim cache is unavailable")
	}
	key := zhipuVideoPendingBillingKey(taskID, userID, apiKeyID)
	if key == "" {
		return fmt.Errorf("zhipu video billing claim key is invalid")
	}
	return s.cache.ReleaseGrokVideoBilled(ctx, key)
}

// StableZhipuVideoBillingRequestID is the durable usage/dedup key for one
// async video task (not the per-poll gateway request id).
func StableZhipuVideoBillingRequestID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	if strings.HasPrefix(taskID, "zhipu-video:") {
		return taskID
	}
	return "zhipu-video:" + taskID
}

// StableZhipuAudioBillingRequestID forces a durable money-event id for
// TTS/STT so usage dedup cannot collapse under a reused or missing client id.
func StableZhipuAudioBillingRequestID(upstreamRequestID string) string {
	upstreamRequestID = strings.TrimSpace(upstreamRequestID)
	if strings.HasPrefix(upstreamRequestID, "zhipu_audio:") {
		return upstreamRequestID
	}
	if upstreamRequestID == "" {
		upstreamRequestID = generateRequestID()
	}
	return "zhipu_audio:" + upstreamRequestID
}

// StableZhipuRealtimeBillingRequestID forces a durable id for one realtime
// session billing event.
func StableZhipuRealtimeBillingRequestID(upstreamRequestID string) string {
	upstreamRequestID = strings.TrimSpace(upstreamRequestID)
	if strings.HasPrefix(upstreamRequestID, "zhipu_realtime:") {
		return upstreamRequestID
	}
	if upstreamRequestID == "" {
		upstreamRequestID = generateRequestID()
	}
	return "zhipu_realtime:" + upstreamRequestID
}

// IsZhipuVideoTaskSuccess matches the official terminal success shape:
// task_status == "SUCCESS" and a downloadable video_result url
// (docs.bigmodel.cn 查询异步结果: PROCESSING | SUCCESS | FAIL).
func IsZhipuVideoTaskSuccess(statusBody []byte) bool {
	if len(statusBody) == 0 || !gjson.ValidBytes(statusBody) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(gjson.GetBytes(statusBody, "task_status").String()), "SUCCESS") {
		return false
	}
	return zhipuVideoResultURL(statusBody) != ""
}

// zhipuVideoResultURL extracts the first video_result[].url.
func zhipuVideoResultURL(statusBody []byte) string {
	for _, item := range gjson.GetBytes(statusBody, "video_result").Array() {
		if raw := strings.TrimSpace(item.Get("url").String()); raw != "" {
			return raw
		}
	}
	return ""
}

// zhipuVideoResolutionFromSize maps a create-request size ("1920x1080") onto
// the shared billing resolution tiers; 4K-class sizes cap at the top tier
// rather than defaulting to the bottom one.
func zhipuVideoResolutionFromSize(size string) string {
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

// zhipuVideoDurationOrDefault applies the documented cogvideox default of 5s
// when the create request omitted duration.
func zhipuVideoDurationOrDefault(durationSeconds int) int {
	if durationSeconds <= 0 {
		return 5
	}
	return durationSeconds
}

// BuildZhipuVideoPendingBilling snapshots create-time billing evidence for a
// freshly accepted video task. The async-result status body carries neither
// duration nor size, so both must be captured here.
func BuildZhipuVideoPendingBilling(requestInfo ZhipuActionRequestInfo, billingModel, upstreamModel, originalModel, createdAt string) ZhipuVideoPendingBilling {
	pending := ZhipuVideoPendingBilling{
		Model:                strings.TrimSpace(requestInfo.Model),
		BillingModel:         strings.TrimSpace(billingModel),
		UpstreamModel:        strings.TrimSpace(upstreamModel),
		OriginalModel:        strings.TrimSpace(originalModel),
		VideoResolution:      zhipuVideoResolutionFromSize(requestInfo.Size),
		VideoDurationSeconds: NormalizeVideoBillingDurationSecondsOrDefault(zhipuVideoDurationOrDefault(requestInfo.DurationSeconds)),
		CreatedAt:            strings.TrimSpace(createdAt),
	}
	if pending.VideoResolution != "" {
		pending.VideoResolution = NormalizeVideoBillingResolutionOrDefault(pending.VideoResolution)
	}
	if pending.CreatedAt == "" {
		pending.CreatedAt = ZhipuVideoPendingCreatedAtNow()
	}
	return pending
}

// MergeZhipuVideoPendingBilling overlays create-time snapshot fields onto a
// completed-task billing record. Duration defaults to the documented 5s when
// the create request omitted it.
func MergeZhipuVideoPendingBilling(result *OpenAIForwardResult, pending *ZhipuVideoPendingBilling, taskID string) *OpenAIForwardResult {
	if result == nil || pending == nil {
		return result
	}
	merged := *result
	merged.Model = firstNonEmpty(pending.BillingModel, pending.Model, merged.Model)
	merged.BillingModel = firstNonEmpty(pending.BillingModel, pending.Model, merged.BillingModel, merged.Model)
	merged.UpstreamModel = firstNonEmpty(pending.UpstreamModel, merged.UpstreamModel)
	if strings.TrimSpace(pending.VideoResolution) != "" {
		merged.VideoResolution = pending.VideoResolution
	}
	if pending.VideoDurationSeconds > 0 {
		merged.VideoDurationSeconds = pending.VideoDurationSeconds
	}
	merged.VideoResolution = NormalizeVideoBillingResolutionOrDefault(merged.VideoResolution)
	merged.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(zhipuVideoDurationOrDefault(merged.VideoDurationSeconds))
	if e2e := zhipuVideoE2EDuration(pending.CreatedAt, time.Now()); e2e > 0 {
		merged.Duration = e2e
	}
	merged.RequestID = StableZhipuVideoBillingRequestID(firstNonEmpty(merged.ResponseID, taskID))
	return &merged
}

func zhipuVideoE2EDuration(createdAt string, discoveredAt time.Time) time.Duration {
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

// estimateZhipuAudioUsage derives billing units. TTS: million characters of
// input text. STT: hours — the official response carries no duration, so the
// body-size/elapsed floors apply (official limit is 30s per file).
func estimateZhipuAudioUsage(endpoint ZhipuActionEndpoint, reqBody []byte, respBody []byte, elapsed time.Duration) *AudioUsage {
	switch endpoint {
	case ZhipuEndpointAudioSpeech:
		chars := 0
		if gjson.ValidBytes(reqBody) {
			chars = len([]rune(strings.TrimSpace(gjson.GetBytes(reqBody, "input").String())))
		}
		if chars <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "tts", DurationOrUnits: float64(chars) / 1_000_000.0}
	case ZhipuEndpointAudioTranscriptions:
		secs := 0.0
		if gjson.ValidBytes(respBody) {
			for _, path := range []string{"duration", "duration_seconds", "audio_duration"} {
				if v := gjson.GetBytes(respBody, path); v.Exists() && v.Type == gjson.Number && v.Float() > 0 {
					secs = v.Float()
					break
				}
			}
		}
		sizeFloor := 0.0
		if len(reqBody) > 0 {
			sizeFloor = float64(len(reqBody)) / 16000.0
		}
		if secs <= 0 {
			secs = elapsed.Seconds()
		}
		if sizeFloor > secs {
			secs = sizeFloor
		}
		if secs <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "stt", DurationOrUnits: secs / 3600.0}
	default:
		return nil
	}
}

// zhipuActionUsageFromResponse builds token usage for endpoints billed inside
// ForwardZhipuAction (embeddings, rerank). Video lookups are priced by the
// handler through MergeZhipuVideoPendingBilling instead.
func zhipuActionUsageFromResponse(endpoint ZhipuActionEndpoint, requestInfo ZhipuActionRequestInfo, responseBody []byte) OpenAIUsage {
	_ = requestInfo
	if endpoint == ZhipuEndpointEmbeddings || endpoint == ZhipuEndpointRerank {
		return extractOpenAIEmbeddingsUsage(responseBody)
	}
	return OpenAIUsage{}
}

// ForwardZhipuAction forwards one Zhipu open-platform action with the shared
// OpenAI-trunk account, error and billing policies. Responses are passed
// through: JSON for most actions, audio bytes for TTS, SSE for streamed STT.
func (s *OpenAIGatewayService) ForwardZhipuAction(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint ZhipuActionEndpoint,
	taskID string,
	body []byte,
	contentType string,
	rawQuery string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil || account.Platform != PlatformZhipu {
		return nil, fmt.Errorf("zhipu account is required")
	}
	if endpoint == ZhipuEndpointVideoContent {
		return s.forwardZhipuVideoContent(ctx, c, account, taskID, startTime)
	}
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	targetURL, err := s.buildZhipuActionURL(account, endpoint, taskID)
	if err != nil {
		return nil, err
	}
	if !endpoint.RequiresRequestBody() && strings.TrimSpace(rawQuery) != "" {
		targetURL += "?" + strings.TrimPrefix(rawQuery, "?")
	}

	requestInfo := ParseZhipuActionRequest(endpoint, contentType, body)
	upstreamModel := requestInfo.Model
	if endpoint.RequiresRequestBody() && gjson.ValidBytes(body) {
		if mappedModel := strings.TrimSpace(account.GetMappedModel(requestInfo.Model)); mappedModel != "" {
			upstreamModel = mappedModel
		}
		if upstreamModel != requestInfo.Model {
			if body, err = sjson.SetBytes(body, "model", upstreamModel); err != nil {
				return nil, fmt.Errorf("rewrite zhipu action account mapped model: %w", err)
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
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Accept", "application/json")
	if endpoint == ZhipuEndpointAudioSpeech {
		upstreamReq.Header.Set("Accept", "application/json, audio/*")
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

	if endpoint == ZhipuEndpointImagesGenerations && countOpenAIResponseImageOutputsFromJSONBytes(respBody) <= 0 {
		setOpsUpstreamError(c, http.StatusBadGateway, "zhipu upstream returned no image output", truncateString(string(respBody), 512))
		return nil, &UpstreamFailoverError{
			StatusCode:      http.StatusBadGateway,
			ResponseBody:    respBody,
			ResponseHeaders: resp.Header.Clone(),
		}
	}
	if endpoint == ZhipuEndpointAsyncResult {
		respBody = rewriteZhipuVideoContentURLs(respBody, taskID, zhipuVideoContentProxyURL(c, taskID))
	}
	writeGrokMediaResponse(c, resp, respBody, s.responseHeaderFilter)

	result := &OpenAIForwardResult{
		RequestID:       requestIDHeader,
		UpstreamHeaders: resp.Header,
		Usage:           zhipuActionUsageFromResponse(endpoint, requestInfo, respBody),
		Model:           requestInfo.Model,
		BillingModel:    requestInfo.Model,
		UpstreamModel:   upstreamModel,
		ResponseHeaders: resp.Header.Clone(),
		Duration:        time.Since(startTime),
	}
	switch endpoint {
	case ZhipuEndpointImagesGenerations:
		result.ImageCount = countOpenAIResponseImageOutputsFromJSONBytes(respBody)
	case ZhipuEndpointVideosGenerations:
		result.ResponseID = strings.TrimSpace(gjson.GetBytes(respBody, "id").String())
		if result.ResponseID == "" {
			result.ResponseID = strings.TrimSpace(gjson.GetBytes(respBody, "request_id").String())
		}
	case ZhipuEndpointAudioSpeech, ZhipuEndpointAudioTranscriptions:
		result.AudioUsage = estimateZhipuAudioUsage(endpoint, body, respBody, time.Since(startTime))
		if result.Model == "" {
			result.Model = string(endpoint)
		}
	case ZhipuEndpointWebSearch:
		if strings.TrimSpace(gjson.GetBytes(respBody, "id").String()) != "" || len(gjson.GetBytes(respBody, "search_result").Array()) > 0 {
			result.WebSearchCalls = 1
		}
		if result.Model == "" {
			result.Model = firstNonEmpty(requestInfo.SearchEngine, "web_search")
			result.BillingModel = result.Model
		}
	case ZhipuEndpointAsyncResult:
		result.ResponseID = strings.TrimSpace(taskID)
		if statusModel := strings.TrimSpace(gjson.GetBytes(respBody, "model").String()); statusModel != "" {
			result.Model = statusModel
			result.BillingModel = statusModel
		}
		if IsZhipuVideoTaskSuccess(respBody) {
			result.VideoCount = 1
		}
	}
	return result, nil
}

// forwardZhipuVideoContent proxies the CDN download of a completed task. The
// task status is re-queried through the owner-bound account so the CDN URL
// never leaves the gateway; unready tasks see their status body instead.
func (s *OpenAIGatewayService) forwardZhipuVideoContent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	taskID string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	statusURL, err := s.buildZhipuActionURL(account, ZhipuEndpointAsyncResult, taskID)
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
	statusReq.Header.Set("Authorization", "Bearer "+token)
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
			return nil, fmt.Errorf("zhipu async-result redirect is not allowed")
		}
		return s.handleErrorResponse(ctx, statusResp, c, account, nil, "")
	}
	statusBody, err := ReadUpstreamResponseBody(statusResp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	videoURL := zhipuVideoResultURL(statusBody)
	if !IsZhipuVideoTaskSuccess(statusBody) || videoURL == "" {
		// Task not ready or failed: surface the official status body as-is.
		writeGrokMediaResponse(c, statusResp, statusBody, s.responseHeaderFilter)
		return &OpenAIForwardResult{ResponseID: strings.TrimSpace(taskID), Duration: time.Since(startTime)}, nil
	}
	parsed, err := url.Parse(videoURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		setOpsUpstreamError(c, http.StatusBadGateway, "zhipu upstream returned an invalid video url", truncateString(videoURL, 256))
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: statusBody}
	}

	contentReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodGet, videoURL, nil)
	if err != nil {
		return nil, err
	}
	// CDN download: no upstream credential leaks to the media host; client
	// Range/Accept preferences pass through for resumable downloads.
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
		RequestID:  StableZhipuVideoBillingRequestID(taskID),
	}, nil
}

// rewriteZhipuVideoContentURLs rewrites video_result[].url in a status body
// to the gateway content proxy so downloads inherit the owner check.
func rewriteZhipuVideoContentURLs(body []byte, taskID, proxyURL string) []byte {
	if len(body) == 0 || !gjson.ValidBytes(body) || strings.TrimSpace(proxyURL) == "" {
		return body
	}
	items := gjson.GetBytes(body, "video_result").Array()
	if len(items) == 0 {
		return body
	}
	out := body
	for index := range items {
		rewritten, err := sjson.SetBytes(out, fmt.Sprintf("video_result.%d.url", index), proxyURL)
		if err == nil {
			out = rewritten
		}
	}
	return out
}

// zhipuVideoContentProxyURL builds the client-facing gateway content URL.
func zhipuVideoContentProxyURL(c *gin.Context, taskID string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil || strings.TrimSpace(taskID) == "" {
		return ""
	}
	pathPrefix := ""
	if strings.HasPrefix(c.Request.URL.Path, "/v1/") {
		pathPrefix = "/v1"
	}
	return pathPrefix + "/videos/" + url.PathEscape(strings.Trim(taskID, "/")) + "/content"
}

// --- Realtime (wss /paas/v4/realtime) ---

// DefaultZhipuRealtimeDialTimeout bounds the pre-accept upstream handshake.
const DefaultZhipuRealtimeDialTimeout = 12 * time.Second

// ZhipuRealtimeUpstream is one dialed upstream realtime connection.
type ZhipuRealtimeUpstream struct {
	conn      openAIWSClientConn
	accountID int64
}

// ZhipuRealtimeDialError preserves an HTTP status returned before upgrade.
type ZhipuRealtimeDialError struct {
	StatusCode int
	Err        error
}

func (e *ZhipuRealtimeDialError) Error() string { return e.Err.Error() }
func (e *ZhipuRealtimeDialError) Unwrap() error { return e.Err }

func (u *ZhipuRealtimeUpstream) Close() error {
	if u == nil || u.conn == nil {
		return nil
	}
	return u.conn.Close()
}

// OpenZhipuRealtime dials wss://{base}/realtime with the account bearer token.
// The session model is chosen by the client's session.update event, so the
// dial carries no model query (docs.bigmodel.cn AsyncAPI realtime).
func (s *OpenAIGatewayService) OpenZhipuRealtime(ctx context.Context, account *Account, token string) (*ZhipuRealtimeUpstream, error) {
	if s == nil || account == nil || account.Platform != PlatformZhipu {
		return nil, fmt.Errorf("zhipu realtime account is required")
	}
	base, err := s.buildZhipuActionURL(account, ZhipuEndpointRealtime, "")
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	u.Scheme = "wss"
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	account.ApplyHeaderOverrides(headers)
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	conn, status, _, err := s.getOpenAIWSPassthroughDialer().Dial(ctx, u.String(), headers, proxyURL)
	if err != nil {
		return nil, &ZhipuRealtimeDialError{StatusCode: status, Err: err}
	}
	return &ZhipuRealtimeUpstream{conn: conn, accountID: account.ID}, nil
}

// ProxyZhipuRealtimeConn relays GLM-Realtime JSON events both ways. Audio is
// base64 inside JSON events, so preserving the JSON bytes is sufficient; each
// client response.create opens a logical billing turn like the Grok relay.
func (s *OpenAIGatewayService) ProxyZhipuRealtimeConn(ctx context.Context, c *gin.Context, client *coderws.Conn, upstream *ZhipuRealtimeUpstream) (bool, error) {
	if s == nil || client == nil || upstream == nil || upstream.conn == nil {
		return false, fmt.Errorf("realtime connection is required")
	}
	conn := upstream.conn

	ctx, cancel := AttemptExecutionContext(ctx)
	defer cancel()
	errCh := make(chan error, 2)
	var audioObserved atomic.Bool

	go func() {
		for {
			msg, readErr := conn.ReadMessage(ctx)
			if readErr != nil {
				errCh <- readErr
				return
			}
			if zhipuRealtimeEventHasAudio(msg) {
				audioObserved.Store(true)
			}
			if writeErr := client.Write(ctx, coderws.MessageText, msg); writeErr != nil {
				errCh <- writeErr
				return
			}
		}
	}()

	go func() {
		logicalTurn := 0
		var turnPlan *GatewayRequestPlan
		defer func() {
			if turnPlan != nil {
				turnPlan.Finalize(false)
			}
		}()
		for {
			kind, msg, readErr := client.Read(ctx)
			if readErr != nil {
				errCh <- readErr
				return
			}
			if kind != coderws.MessageText && kind != coderws.MessageBinary {
				continue
			}
			if zhipuRealtimeEventHasAudio(msg) {
				audioObserved.Store(true)
			}
			var raw json.RawMessage
			if unmarshalErr := json.Unmarshal(msg, &raw); unmarshalErr != nil {
				errCh <- fmt.Errorf("invalid realtime event: %w", unmarshalErr)
				return
			}
			turnContext := ctx
			if gjson.GetBytes(raw, "type").String() == "response.create" {
				logicalTurn++
				BeginAttemptStreamTurn(ctx, logicalTurn)
				if turnPlan != nil {
					turnPlan.Finalize(false)
				}
				if parent := GatewayRequestPlanFromContext(ctx); parent != nil {
					spec := parent.Snapshot()
					spec.ExecutionID = fmt.Sprintf("%s:turn:%d", spec.ExecutionID, logicalTurn)
					turnContext, turnPlan = s.CaptureRequestTurn(ctx, spec)
				}
			}
			if writeErr := generationWS(turnContext, upstream.accountID, raw, func(sendContext context.Context) error { return conn.WriteJSON(sendContext, raw) }); writeErr != nil {
				errCh <- writeErr
				return
			}
		}
	}()

	err := <-errCh
	return audioObserved.Load(), err
}

// zhipuRealtimeEventHasAudio reports whether an event carries audio payload
// (base64 in audio/delta/data fields), excluding transcript-only events.
func zhipuRealtimeEventHasAudio(msg []byte) bool {
	if !gjson.ValidBytes(msg) {
		return false
	}
	eventType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(msg, "type").String()))
	if !strings.Contains(eventType, "audio") || strings.Contains(eventType, "transcript") {
		return false
	}
	for _, path := range []string{"audio", "delta", "data"} {
		value := gjson.GetBytes(msg, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return true
		}
	}
	return false
}
