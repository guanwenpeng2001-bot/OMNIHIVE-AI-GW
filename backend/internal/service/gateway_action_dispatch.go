package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"net/http"
	"slices"
	"strings"
)

type GatewayActionHandlers map[string]gin.HandlerFunc
type GatewayActionDispatch struct{ handlers GatewayActionHandlers }

func NewGatewayActionDispatch(handlers GatewayActionHandlers) *GatewayActionDispatch {
	copied := make(GatewayActionHandlers, len(handlers))
	for adapter, handler := range handlers {
		copied[adapter] = handler
	}
	return &GatewayActionDispatch{handlers: copied}
}
func gatewayDispatchAPIKey(requestContext *gin.Context) (*APIKey, bool) {
	value, exists := requestContext.Get("api_key")
	key, valid := value.(*APIKey)
	return key, exists && valid && key != nil
}
func GatewayTargetPlatform(requestContext *gin.Context) string {
	if forced := requestContext.GetString("force_platform"); forced != "" {
		return forced
	}
	key, exists := gatewayDispatchAPIKey(requestContext)
	if !exists || key.Group == nil {
		return ""
	}
	if key.Group.Platform == PlatformComposite {
		if platform, resolved := ResolvedTargetPlatformFromContext(requestContext.Request.Context()); resolved {
			return platform
		}
	}
	return key.Group.Platform
}
func rejectGatewayAction(requestContext *gin.Context) {
	MarkOpsClientBusinessLimited(requestContext, OpsClientBusinessLimitedReasonLocalFeatureGate)
	requestContext.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "action_not_implemented", "message": "No gateway adapter is available for this platform and action"}})
}
func (d *GatewayActionDispatch) Handler(action string) gin.HandlerFunc {
	return func(requestContext *gin.Context) {
		platform := GatewayTargetPlatform(requestContext)
		// Every video action shares one target resolution: the request's own
		// resolved Composite target when the client wrote a model, and the
		// Grok pin when a model-less job lookup (or a model-less create/edit/
		// extension) left the target unresolved. Pinning through one helper keeps
		// creation, edit, extension, status and content on the same platform and
		// writes that platform into the request context so quota, account
		// selection and billing observe it too.
		if platform == PlatformComposite && isCompositeGrokVideoAction(action) {
			platform = pinUnresolvedCompositeGrokTarget(requestContext)
		}
		if platform == PlatformComposite && action == "realtime" {
			platform = pinUnresolvedCompositeGrokTarget(requestContext)
		}
		descriptor := domain.PlatformAction(domain.ActionIdentity{Platform: platform, HostedAccountType: RequiredRouteAccountType(requestContext.Request.Context())}, action)
		handler := d.handlers[descriptor.Adapter]
		if handler == nil {
			rejectGatewayAction(requestContext)
			return
		}
		handler(requestContext)
	}
}

// isCompositeGrokVideoAction reports the video-family actions whose native
// request shape is the xAI video surface. A Composite group that could not
// resolve a target for one of them still belongs to the Grok video handler.
func isCompositeGrokVideoAction(action string) bool {
	switch action {
	case "video.generate", "video.edit", "video.extend", "video.status", "video.content":
		return true
	default:
		return false
	}
}
func (d *GatewayActionDispatch) ImageHandler() gin.HandlerFunc {
	return func(requestContext *gin.Context) {
		action := "image.generate"
		switch {
		case strings.HasSuffix(requestContext.Request.URL.Path, "/edits"):
			action = "image.edit"
		case strings.HasSuffix(requestContext.Request.URL.Path, "/variations"):
			action = "image.variation"
		}
		d.Handler(action)(requestContext)
	}
}
func CodexModelsGateway(openAIHandler, generatedHandler gin.HandlerFunc) gin.HandlerFunc {
	return func(requestContext *gin.Context) {
		if GatewayTargetPlatform(requestContext) == PlatformOpenAI {
			openAIHandler(requestContext)
			return
		}
		generatedHandler(requestContext)
	}
}
func GatewayModelsHandler(codexHandler, ordinaryHandler gin.HandlerFunc) gin.HandlerFunc {
	return func(requestContext *gin.Context) {
		if requestContext.Query("client_version") != "" {
			codexHandler(requestContext)
			return
		}
		ordinaryHandler(requestContext)
	}
}
func GuardResponsesSubpath(next, inputTokens gin.HandlerFunc) gin.HandlerFunc {
	return func(requestContext *gin.Context) {
		if !IsForwardableOpenAIResponsesRequestPath(requestContext) {
			MarkOpsClientBusinessLimited(requestContext, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			requestContext.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Unsupported responses subpath"}})
			return
		}
		descriptor := domain.PlatformAction(domain.ActionIdentity{Platform: GatewayTargetPlatform(requestContext)}, "responses")
		if IsOpenAIResponsesInputTokensRequestPath(requestContext) && descriptor.Adapter == "openai.responses" {
			inputTokens(requestContext)
			return
		}
		next(requestContext)
	}
}
func GatewayVoiceHandler(handler func(*gin.Context, string), endpoint func(*gin.Context) string) gin.HandlerFunc {
	return func(requestContext *gin.Context) {
		action := "voice"
		path := endpoint(requestContext)
		if path == "tts" {
			action = "audio.tts"
		}
		if path == "stt" {
			action = "audio.stt"
		}
		// Composite Grok targets must be pinned first, then this route - which
		// wraps only the Grok voice adapter - fails closed for any other
		// platform whose action is served by its own native ingress.
		platform := pinUnresolvedCompositeGrokTarget(requestContext)
		descriptor := domain.PlatformAction(domain.ActionIdentity{Platform: platform}, action)
		if descriptor.Adapter == "" || descriptor.Adapter != "grok."+action {
			rejectGatewayAction(requestContext)
			return
		}
		handler(requestContext, path)
	}
}
func GatewaySearchHandler(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(requestContext *gin.Context) {
		platform := pinUnresolvedCompositeGrokTarget(requestContext)
		if domain.PlatformAction(domain.ActionIdentity{Platform: platform}, "search").Adapter == "" {
			rejectGatewayAction(requestContext)
			return
		}
		handler(requestContext)
	}
}

// pinUnresolvedCompositeGrokTarget resolves Composite groups for actions that
// only Grok implements and whose native requests carry no model selector
// (voice/tts/stt, standalone search, realtime without a query model). An
// explicit composite resolution always wins — GatewayTargetPlatform only
// returns PlatformComposite when nothing was resolved — so pinning never
// overrides a client-chosen target. The pin is written into the request
// context so quota, account selection and billing observe the same concrete
// platform; groups without Grok accounts still fail at account selection.
func pinUnresolvedCompositeGrokTarget(requestContext *gin.Context) string {
	platform := GatewayTargetPlatform(requestContext)
	if platform != PlatformComposite {
		return platform
	}
	requestContext.Request = requestContext.Request.WithContext(
		WithResolvedTargetPlatform(requestContext.Request.Context(), PlatformGrok))
	return PlatformGrok
}

// GatewayActionForRoute describes the inbound action; upstream bridges must not
// replace this requirement with the protocol of their transformed request.
func GatewayActionForRoute(method, path string) string {
	path = strings.TrimRight(path, "/")
	if strings.Contains(path, "/api/v1/") || strings.Contains(path, "/api-ws/v1/") || strings.Contains(path, "/compatible-mode/v1/") {
		return "native"
	}
	switch {
	case strings.Contains(path, "/images/batches"):
		return "image.batch"
	case strings.HasSuffix(path, "/async"):
		return "image.async"
	case strings.Contains(path, "/images/tasks/"):
		return ""
	case strings.Contains(path, "/images/edits"):
		return "image.edit"
	case strings.Contains(path, "/images/variations"):
		return "image.variation"
	case strings.Contains(path, "/images/generations"):
		return "image.generate"
	case strings.Contains(path, "/videos"):
		if method == http.MethodPost && strings.HasSuffix(path, "/cancel") {
			return "video.cancel"
		}
		if method == http.MethodGet {
			if strings.HasSuffix(path, "/content") {
				return "video.content"
			}
			return "video.status"
		}
		if strings.HasSuffix(path, "/edits") {
			return "video.edit"
		}
		if strings.HasSuffix(path, "/extensions") {
			return "video.extend"
		}
		return "video.generate"
	case strings.HasSuffix(path, "/chat/completions"):
		return "chat"
	case strings.Contains(path, "/messages/count_tokens"):
		return "count_tokens"
	case strings.HasSuffix(path, "/messages"):
		return "messages"
	case strings.HasSuffix(path, "/responses/compact"):
		return "responses.compact"
	case strings.Contains(path, "/responses"):
		if method == http.MethodGet {
			return "responses.websocket"
		}
		return "responses"
	case strings.HasSuffix(path, "/embeddings"):
		return "embeddings"
	case strings.HasSuffix(path, "/rerank"):
		return "rerank"
	case strings.HasSuffix(path, "/audio/speech"):
		return "audio.tts"
	case strings.HasSuffix(path, "/audio/transcriptions"):
		return "audio.stt"
	case strings.HasSuffix(path, "/audio/translations"):
		return "audio.translate"
	case strings.HasSuffix(path, "/tts"):
		return "audio.tts"
	case strings.HasSuffix(path, "/stt"):
		return "audio.stt"
	case strings.Contains(path, "/custom-voices"):
		return "voice"
	case strings.HasSuffix(path, "/tools/search"), strings.HasSuffix(path, "/tools/search_pro"):
		return "search"
	case strings.HasSuffix(path, "/web_search"), strings.HasSuffix(path, "/x_search"):
		return "search"
	case strings.HasSuffix(path, "/alpha/search"):
		return "alpha_search"
	case strings.HasSuffix(path, "/realtime"):
		return "realtime"
	case strings.HasSuffix(path, "/live"), strings.Contains(path, "/live/"), strings.Contains(path, "/realtime/calls"):
		return "live"
	case strings.Contains(path, "/v1beta/models/") && method == http.MethodPost:
		return "generate_content"
	case strings.Contains(path, "/api/v1/"), strings.Contains(path, "/api-ws/v1/"), strings.Contains(path, "/compatible-mode/v1/"):
		return "native"
	default:
		return ""
	}
}
func GatewayActionRequirement(requestContext *gin.Context) {
	gatewayActionRequirement(requestContext, nil)
}

func (s *GatewayService) ActionRequirement(requestContext *gin.Context) {
	gatewayActionRequirement(requestContext, s)
}

func gatewayActionRequirement(requestContext *gin.Context, gateway *GatewayService) {
	action := GatewayActionForRoute(requestContext.Request.Method, requestContext.Request.URL.Path)
	requestContext.Request = requestContext.Request.WithContext(context.WithValue(requestContext.Request.Context(), gatewayActionRequirementKey{}, action))
	if action == "" {
		requestContext.Next()
		return
	}
	platform := GatewayTargetPlatform(requestContext)
	if platform == "" || platform == PlatformComposite {
		if !gatewayInputAdmission(requestContext, gateway) {
			return
		}
		requestContext.Next()
		return
	}
	if domain.PlatformAction(domain.ActionIdentity{Platform: platform, HostedAccountType: RequiredRouteAccountType(requestContext.Request.Context())}, action).Adapter == "" {
		rejectGatewayAction(requestContext)
		return
	}
	if !gatewayInputAdmission(requestContext, gateway) {
		return
	}
	requestContext.Next()
}
func CompositeTargetPlatformMiddleware(resolver *CompositeRouteResolver) gin.HandlerFunc {
	if resolver == nil {
		resolver = NewCompositeRouteResolver(nil)
	}
	return func(c *gin.Context) {
		apiKey, ok := gatewayDispatchAPIKey(c)
		if !ok || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != PlatformComposite {
			c.Next()
			return
		}
		if c.Request == nil {
			c.Next()
			return
		}
		if c.Request.Method == http.MethodGet {
			// Native WS ingresses bind a public query model. Other GET routes
			// retain their existing ownership/authentication flow. This covers
			// Qianwen /api-ws/v1/realtime and the Grok voice realtime aliases
			// (/v1/realtime, /realtime).
			if strings.HasSuffix(c.Request.URL.Path, "/realtime") {
				model := c.Query("model")
				decision, err := resolver.Resolve(c.Request.Context(), apiKey.Group.ID, model, CompositeRouteEndpointAny)
				if err != nil {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "server_error", "message": "Failed to resolve composite model route"}})
					return
				}
				if decision.Matched {
					c.Request = c.Request.WithContext(WithCompositeRouteDecision(c.Request.Context(), decision))
				}
			}
			c.Next()
			return
		}

		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			status := http.StatusBadRequest
			message := "Failed to read request body"
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				status = http.StatusRequestEntityTooLarge
				message = "Request body is too large"
			}
			c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": message}})
			c.Abort()
			return
		}

		// Live 入口按 session.model 分发并改写（与白名单准入、Live handler 的
		// 解析保持一致），避免顶层 model 别名与 session 模型不一致时改错对象。
		routePath := c.FullPath()
		model := requestmodel.FromBodyForRoute(routePath, c.GetHeader("Content-Type"), body)
		if model != "" {
			decision, err := resolver.Resolve(c.Request.Context(), apiKey.Group.ID, model, CompositeRouteEndpointForPath(c.Request.URL.Path))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "server_error", "message": "Failed to resolve composite model route"}})
				c.Abort()
				return
			}
			if decision.Matched {
				c.Request = c.Request.WithContext(WithCompositeRouteDecision(c.Request.Context(), decision))
				if upstreamModel := strings.TrimSpace(decision.UpstreamModel); upstreamModel != "" && upstreamModel != model && gjson.ValidBytes(body) {
					if _, modelPath := requestmodel.JSONModelPathForRoute(routePath, body); modelPath != "" {
						if rewritten, rewriteErr := sjson.SetBytes(body, modelPath, upstreamModel); rewriteErr == nil {
							body = rewritten
						}
					}
				}
			}
		}
		requestmodel.ResetRequestBody(c.Request, body)
		c.Next()
	}
}

func CompositeGeminiTargetPlatformMiddleware(resolver *CompositeRouteResolver) gin.HandlerFunc {
	if resolver == nil {
		resolver = NewCompositeRouteResolver(nil)
	}
	return func(c *gin.Context) {
		apiKey, ok := gatewayDispatchAPIKey(c)
		if ok && apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == PlatformComposite {
			model := CompositeGeminiModelFromParams(c)
			if model != "" {
				decision, err := resolver.Resolve(c.Request.Context(), apiKey.Group.ID, model, CompositeRouteEndpointGemini)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "server_error", "message": "Failed to resolve composite model route"}})
					c.Abort()
					return
				}
				if decision.Matched {
					c.Request = c.Request.WithContext(WithCompositeRouteDecision(c.Request.Context(), decision))
				}
			}
			if _, resolved := ResolvedTargetPlatformFromContext(c.Request.Context()); !resolved {
				c.Request = c.Request.WithContext(WithResolvedTargetPlatform(c.Request.Context(), PlatformGemini))
			}
		}
		c.Next()
	}
}

// GrokCustomVoiceEndpoint derives the upstream Voice endpoint for the
// /custom-voices/:voice_id[/audio] routes.
//
// The /audio suffix must be decided from the matched route template, not from
// the raw URL path: a voice literally named "audio" makes GET
// /custom-voices/audio match /custom-voices/:voice_id, and a raw-path suffix
// check would rewrite it to custom-voices/audio/audio — turning a profile
// lookup into an audio download.
func GrokCustomVoiceEndpoint(c *gin.Context) string {
	endpoint := "custom-voices/" + c.Param("voice_id")
	if strings.HasSuffix(c.FullPath(), "/:voice_id/audio") {
		endpoint += "/audio"
	}
	return endpoint
}

func CompositeGeminiModelFromParams(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if model := strings.TrimSpace(c.Param("model")); model != "" {
		return model
	}
	modelAction := strings.TrimPrefix(strings.TrimSpace(c.Param("modelAction")), "/")
	if modelAction == "" {
		return ""
	}
	if idx := strings.LastIndex(modelAction, ":"); idx >= 0 {
		return strings.TrimSpace(modelAction[:idx])
	}
	return modelAction
}

func CompositeRouteEndpointForPath(path string) string {
	switch {
	case strings.Contains(path, "/messages/count_tokens"):
		return CompositeRouteEndpointCountTokens
	case strings.Contains(path, "/messages"):
		return CompositeRouteEndpointMessages
	case strings.Contains(path, "/responses"),
		strings.Contains(path, "/alpha/search"),
		strings.Contains(path, "/realtime/calls"),
		strings.HasSuffix(strings.TrimRight(path, "/"), "/live"):
		return CompositeRouteEndpointResponses
	case strings.Contains(path, "/chat/completions"):
		return CompositeRouteEndpointChatCompletions
	case strings.Contains(path, "/embeddings"):
		return CompositeRouteEndpointEmbeddings
	case strings.Contains(path, "/images/"):
		return CompositeRouteEndpointImages
	case strings.Contains(path, "/v1beta/"):
		return CompositeRouteEndpointGemini
	default:
		return CompositeRouteEndpointAny
	}
}

// gatewayActionRequirementKey carries the inbound action through protocol
// bridges. It does not contain credentials, grants, pricing or retry budgets.
type gatewayActionRequirementKey struct{}

func gatewayRequiredAction(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if action, ok := ctx.Value(gatewayActionRequirementKey{}).(string); ok {
		return action
	}
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
		spec := plan.Snapshot()
		for _, action := range domain.GatewayActions() {
			if spec.Action == action {
				return action
			}
		}
		method := http.MethodPost
		if spec.Protocol == "realtime" {
			method = http.MethodGet
		}
		return GatewayActionForRoute(method, spec.Route)
	}
	return ""
}
func accountImplementsGatewayAction(ctx context.Context, account *Account) bool {
	action := gatewayRequiredAction(ctx)
	if action == "" {
		return true
	}
	identity := domain.ActionIdentity{Platform: account.Platform, HostedAccountType: account.Type, Profile: resolveCapabilityProfile(account), Protocol: account.GetCredential("api_protocol")}
	return domain.PlatformAction(identity, action).Status == domain.ActionImplemented
}

// GatewayInputRequirement records only explicit request features. Missing
// capability facts are never converted to false or a zero limit.
type GatewayInputRequirement struct {
	Image, Audio, Video, PDF, Tools  bool
	MaxOutputTokens, Count, Duration float64
}
type gatewayInputRequirementKey struct{}

func parseGatewayInputRequirement(body []byte) GatewayInputRequirement {
	root := gjson.ParseBytes(body)
	requirement := GatewayInputRequirement{Tools: len(root.Get("functions").Array()) > 0, Count: root.Get("n").Float(), Duration: root.Get("duration").Float()}
	for _, tool := range root.Get("tools").Array() {
		if tool.Get("type").String() == "function" || tool.Get("name").Exists() || len(tool.Get("functionDeclarations").Array()) > 0 {
			requirement.Tools = true
		}
	}
	for _, field := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens", "generationConfig.maxOutputTokens"} {
		if value := root.Get(field).Float(); value > requirement.MaxOutputTokens {
			requirement.MaxOutputTokens = value
		}
	}
	var visit func(gjson.Result)
	visit = func(value gjson.Result) {
		if value.IsArray() {
			value.ForEach(func(_, child gjson.Result) bool { visit(child); return true })
			return
		}
		if !value.IsObject() {
			return
		}
		switch value.Get("type").String() {
		case "image", "image_url", "input_image":
			requirement.Image = true
		case "audio", "input_audio":
			requirement.Audio = true
		case "video", "video_url", "input_video":
			requirement.Video = true
		case "document":
			if value.Get("source.media_type").String() == "application/pdf" {
				requirement.PDF = true
			}
		}
		for _, field := range []string{"inlineData.mimeType", "fileData.mimeType", "source.media_type"} {
			mediaType := value.Get(field).String()
			if strings.HasPrefix(mediaType, "image/") {
				requirement.Image = true
			}
			if strings.HasPrefix(mediaType, "audio/") {
				requirement.Audio = true
			}
			if strings.HasPrefix(mediaType, "video/") {
				requirement.Video = true
			}
			if mediaType == "application/pdf" {
				requirement.PDF = true
			}
		}
		for _, field := range []string{"content", "parts"} {
			visit(value.Get(field))
		}
	}
	for _, field := range []string{"messages", "input", "contents"} {
		visit(root.Get(field))
	}
	return requirement
}
func (r GatewayInputRequirement) empty() bool { return r == (GatewayInputRequirement{}) }
func (r GatewayInputRequirement) SupportedBy(metadata UpstreamModelMetadata) bool {
	for _, feature := range []struct {
		required bool
		known    *bool
	}{{r.Image, metadata.SupportsVision}, {r.PDF, metadata.SupportsPDF}, {r.Tools, metadata.SupportsFunctionCalling}} {
		if feature.required && feature.known != nil && !*feature.known {
			return false
		}
	}
	if metadata.InputModalities != nil && metadata.InputModalitiesStatus != "partial" {
		for _, modality := range []struct {
			required bool
			name     string
		}{{r.Image, "image"}, {r.Audio, "audio"}, {r.Video, "video"}, {r.PDF, "pdf"}} {
			if modality.required && !slices.Contains(metadata.InputModalities, modality.name) {
				return false
			}
		}
	}
	if metadata.MaxOutputTokens > 0 && r.MaxOutputTokens > float64(metadata.MaxOutputTokens) {
		return false
	}
	for _, limit := range []struct {
		field     string
		requested float64
	}{{"max_duration_seconds", r.Duration}, {"max_images", r.Count}} {
		var maximum float64
		if json.Unmarshal(metadata.TaskLimits[limit.field], &maximum) == nil && maximum > 0 && limit.requested > maximum {
			return false
		}
	}
	return true
}
func gatewayInputAdmission(requestContext *gin.Context, gateway *GatewayService) bool {
	if requestContext.Request.Method != http.MethodPost || !strings.Contains(requestContext.GetHeader("Content-Type"), "application/json") {
		return true
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(requestContext.Request)
	if err != nil {
		status := http.StatusBadRequest
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			status = http.StatusRequestEntityTooLarge
		}
		requestContext.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "Failed to read request body"}})
		return false
	}
	requestmodel.ResetRequestBody(requestContext.Request, body)
	if key, ok := gatewayDispatchAPIKey(requestContext); ok && key.Group != nil {
		requestContext.Request = requestContext.Request.WithContext(context.WithValue(requestContext.Request.Context(), accountMessagesDispatchGroupKey{}, key.Group))
	}
	if gateway == nil {
		return true
	}
	if _, authoritative := gateway.accountRepo.(ModelCatalogAccountRepository); !authoritative {
		return true
	}
	resolver := &VisibleModelResolver{gateway: gateway}
	frozen, err := resolver.freezeMode(requestContext.Request.Context(), false)
	if err != nil {
		requestContext.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "server_error", "message": "Model requirements temporarily unavailable"}})
		return false
	}
	requestContext.Request = requestContext.Request.WithContext(frozen)
	return true
}
