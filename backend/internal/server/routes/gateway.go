package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterGatewayRoutes 注册 API 网关路由（Claude/OpenAI/Gemini 兼容）
func RegisterGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	compositeResolver *service.CompositeRouteResolver,
	cfg *config.Config,
) {
	// Public IDs may contain provider/model: match the encoded ID as one raw path segment.
	r.UseRawPath = true
	r.Use(middleware.GatewayUnknownRouteProtocol())
	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	textBodyLimit := middleware.RequestBodyLimit(cfg.Gateway.TextMaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()
	compositeTarget := compositeTargetPlatformMiddleware(compositeResolver)
	compositeGeminiTarget := compositeGeminiTargetPlatformMiddleware(compositeResolver)

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)
	requireGroupGoogle := middleware.RequireGroupAssignment(settingService, middleware.GoogleErrorWriter)

	// 分组级模型白名单准入：在 apiKeyAuth 之后、compositeTarget 之前，
	// 保证校验发生在合成路由改写与调度之前，且只看客户端书写的模型名。
	groupModelAllowlist := middleware.GroupModelAllowlist()
	actionRequirement := h.Gateway.ActionRequirement

	dispatch := service.NewGatewayActionDispatch(service.GatewayActionHandlers{
		"gateway.chat":         h.Gateway.ChatCompletions,
		"openai.chat":          h.OpenAIGateway.ChatCompletions,
		"gateway.messages":     h.Gateway.Messages,
		"openai.messages":      h.OpenAIGateway.Messages,
		"gateway.responses":    h.Gateway.Responses,
		"openai.responses":     h.OpenAIGateway.Responses,
		"gateway.count_tokens": h.Gateway.CountTokens,
		"openai.count_tokens":  h.OpenAIGateway.CountTokens,
		"cursor.count_tokens":  h.OpenAIGateway.CursorCountTokens,
		"grok.count_tokens":    h.OpenAIGateway.GrokCountTokens,
		"openai.embeddings":    h.OpenAIGateway.Embeddings,
		"openai.images":        h.OpenAIGateway.Images,
		"openai.image.variation": h.OpenAIGateway.Images,
		"grok.images":            h.OpenAIGateway.GrokImages,
		"grok.video.generate":    h.OpenAIGateway.GrokVideoGeneration,
		"grok.video.edit":        h.OpenAIGateway.GrokVideoEdit,
		"grok.video.extend":      h.OpenAIGateway.GrokVideoExtension,
		"grok.video.status":      h.OpenAIGateway.GrokVideoStatus,
		"grok.video.content":     h.OpenAIGateway.GrokVideoContent,
		"grok.realtime":          h.OpenAIGateway.GrokRealtime,
		"antigravity.images":     h.Gateway.AntigravityImages,
		"bedrock.embeddings":     h.Gateway.BedrockEmbeddings,
		"bedrock.rerank":         h.Gateway.BedrockRerank,
		"bedrock.images":         h.Gateway.BedrockImages,
		"bedrock.image.variation": h.Gateway.BedrockImages,
		"qianwen.embeddings":      h.OpenAIGateway.QianwenEmbeddings,
		"qianwen.rerank":          h.OpenAIGateway.QianwenRerank,
		"qianwen.images":          h.OpenAIGateway.QianwenImages,
		"qianwen.native":          h.OpenAIGateway.QianwenNative,
		"qianfan.embeddings":      h.OpenAIGateway.QianfanEmbeddings,
		"qianfan.rerank":          h.OpenAIGateway.QianfanRerank,
		"qianfan.images":          h.OpenAIGateway.QianfanImages,
		"qianfan.image.edit":      h.OpenAIGateway.QianfanImageEdit,
		"qianfan.video.generate":  h.OpenAIGateway.QianfanVideoGeneration,
		"qianfan.video.status":    h.OpenAIGateway.QianfanVideoStatus,
		"qianfan.video.cancel":    h.OpenAIGateway.QianfanVideoCancel,
		"qianfan.video.content":   h.OpenAIGateway.QianfanVideoContent,
		"qianfan.audio.tts":       h.OpenAIGateway.QianfanAudioSpeech,
		"qianfan.voice":           qianfanVoiceHandler(h.OpenAIGateway.QianfanVoiceCloneCreate, h.OpenAIGateway.QianfanVoiceCloneList),
		"qianfan.search":          h.OpenAIGateway.QianfanSearch,
		"hunyuan.embeddings":      h.OpenAIGateway.HunyuanEmbeddings,
		"hunyuan.images":          h.OpenAIGateway.HunyuanImageGeneration,
		"hunyuan.image.edit":      h.OpenAIGateway.HunyuanImageEdit,
		"hunyuan.video.generate":  h.OpenAIGateway.HunyuanVideoGeneration,
		"hunyuan.video.status":    h.OpenAIGateway.HunyuanVideoStatus,
		"hunyuan.video.content":   h.OpenAIGateway.HunyuanVideoContent,
		"hunyuan.audio.tts":       h.OpenAIGateway.HunyuanAudioSpeech,
		"hunyuan.audio.stt":       h.OpenAIGateway.HunyuanAudioTranscriptions,
		"hunyuan.voice":           h.OpenAIGateway.HunyuanVoiceClone,
		"volcengine.embeddings":     h.OpenAIGateway.VolcengineEmbeddings,
		"volcengine.images":         h.OpenAIGateway.VolcengineImages,
		"volcengine.video.generate": h.OpenAIGateway.VolcengineVideoGeneration,
		"volcengine.video.edit":     h.OpenAIGateway.VolcengineVideoEdit,
		"volcengine.video.extend":   h.OpenAIGateway.VolcengineVideoExtend,
		"volcengine.video.status":   h.OpenAIGateway.VolcengineVideoStatus,
		"volcengine.video.content":  h.OpenAIGateway.VolcengineVideoContent,
		"opencode.generate_content": h.Gateway.OpenCodeGenerateContent,
	})
	messagesHandler := dispatch.Handler("messages")
	countTokensHandler := dispatch.Handler("count_tokens")
	codexModelsHandler := service.CodexModelsGateway(h.OpenAIGateway.CodexModels, h.Gateway.CodexModels)
	modelsHandler := service.GatewayModelsHandler(codexModelsHandler, h.Gateway.Models)
	imagesHandler := dispatch.ImageHandler()
	videoGenerationHandler := dispatch.Handler("video.generate")
	videoEditHandler := dispatch.Handler("video.edit")
	videoExtensionHandler := dispatch.Handler("video.extend")
	videoStatusHandler := dispatch.Handler("video.status")
	videoContentHandler := dispatch.Handler("video.content")
	rerankHandler := dispatch.Handler("rerank")
	guardResponsesSubpath := func(next gin.HandlerFunc) gin.HandlerFunc {
		return service.GuardResponsesSubpath(next, h.OpenAIGateway.ResponsesInputTokens)
	}
	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	gateway.Use(middleware.GatewayProtocol())
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	gateway.GET("/sub2api/billing", h.Gateway.KeyBillingInfo)
	gateway.Use(groupModelAllowlist)
	gateway.Use(compositeTarget)
	gateway.Use(actionRequirement)
	gateway.Use(requireGroupAnthropic)
	{
		// /v1/messages: auto-route based on group platform
		gateway.POST("/messages", messagesHandler)
		// /v1/messages/count_tokens: OpenAI bridges upstream, Grok estimates
		// locally, and Anthropic-compatible platforms retain their existing path.
		gateway.POST("/messages/count_tokens", countTokensHandler)
		// Codex CLI / Codex app refresh their model picker from the provider's
		// /models endpoint with a client_version query and expect the ChatGPT
		// Codex manifest format; other clients keep the OpenAI-style list.
		gateway.GET("/models", modelsHandler)
		gateway.GET("/models/catalog", h.Gateway.ModelCatalog)
		gateway.GET("/models/catalog/:model", h.Gateway.ModelCatalog)
		// Single-model discovery never selects the Codex client_version manifest.
		gateway.GET("/models/:model", h.Gateway.Models)
		gateway.GET("/usage", h.Gateway.Usage)
		gateway.POST("/live", h.OpenAIGateway.Live)
		gateway.GET("/live/:call_id", h.OpenAIGateway.LiveSideband)
		// OpenAI Responses API: auto-route based on group platform
		gateway.POST("/responses", dispatch.Handler("responses"))
		gateway.POST("/responses/*subpath", guardResponsesSubpath(dispatch.Handler("responses")))
		gateway.POST("/alpha/search", textBodyLimit, h.OpenAIGateway.AlphaSearch)
		gateway.GET("/responses", func(c *gin.Context) {
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		// OpenAI Chat Completions API: auto-route based on group platform
		gateway.POST("/chat/completions", dispatch.Handler("chat"))
		gateway.POST("/embeddings", textBodyLimit, dispatch.Handler("embeddings"))
		gateway.POST("/rerank", textBodyLimit, rerankHandler)
		gateway.POST("/images/generations", imagesHandler)
		gateway.POST("/images/edits", imagesHandler)
		gateway.POST("/images/variations", imagesHandler)
		gateway.POST("/images/generations/async", h.AsyncImage.Submit)
		gateway.POST("/images/edits/async", h.AsyncImage.Submit)
		gateway.GET("/images/tasks/:task_id", h.AsyncImage.Get)
		gateway.POST("/images/batches", h.BatchImage.Submit)
		gateway.GET("/images/batches", h.BatchImage.List)
		gateway.GET("/images/batches/models", h.BatchImage.Models)
		gateway.GET("/images/batches/:id", h.BatchImage.Get)
		gateway.GET("/images/batches/:id/items", h.BatchImage.Items)
		gateway.GET("/images/batches/:id/items/:custom_id/content", h.BatchImage.ItemContent)
		gateway.GET("/images/batches/:id/download", h.BatchImage.Download)
		gateway.POST("/images/batches/:id/cancel", h.BatchImage.Cancel)
		gateway.DELETE("/images/batches/:id", h.BatchImage.DeleteRecord)
		gateway.DELETE("/images/batches/:id/outputs", h.BatchImage.DeleteOutputs)
		// OpenAI-compatible clients may create through /videos; xAI receives the
		// canonical /videos/generations route inside the Grok media forwarder.
		gateway.POST("/videos", videoGenerationHandler)
		gateway.POST("/videos/generations", videoGenerationHandler)
		gateway.POST("/videos/edits", videoEditHandler)
		gateway.POST("/videos/extensions", videoExtensionHandler)
		gateway.GET("/videos/generations/:request_id/content", videoContentHandler)
		gateway.GET("/videos/edits/:request_id/content", videoContentHandler)
		gateway.GET("/videos/extensions/:request_id/content", videoContentHandler)
		gateway.GET("/videos/generations/:request_id", videoStatusHandler)
		gateway.GET("/videos/edits/:request_id", videoStatusHandler)
		gateway.GET("/videos/extensions/:request_id", videoStatusHandler)
		gateway.GET("/videos/:request_id", videoStatusHandler)
		gateway.GET("/videos/:request_id/content", videoContentHandler)

		// xAI Voice APIs (Grok platform only): HTTP TTS/STT + Realtime WS.
		// Not part of the creation-center product surface — gateway relay only.
		voiceHandler := func(endpoint string) gin.HandlerFunc {
			return service.GatewayVoiceHandler(h.OpenAIGateway.GrokVoice, func(*gin.Context) string { return endpoint })
		}
		gateway.POST("/tts", voiceHandler("tts"))
		gateway.POST("/stt", voiceHandler("stt"))
		gateway.POST("/custom-voices", voiceHandler("custom-voices"))
		customVoicePathHandler := service.GatewayVoiceHandler(h.OpenAIGateway.GrokVoice, service.GrokCustomVoiceEndpoint)
		gateway.GET("/custom-voices", voiceHandler("custom-voices"))
		gateway.GET("/custom-voices/:voice_id/audio", customVoicePathHandler)
		gateway.GET("/custom-voices/:voice_id", customVoicePathHandler)
		gateway.PATCH("/custom-voices/:voice_id", customVoicePathHandler)
		gateway.DELETE("/custom-voices/:voice_id", customVoicePathHandler)
		gateway.GET("/realtime", dispatch.Handler("realtime"))
		gateway.POST("/web_search", service.GatewaySearchHandler(h.Gateway.WebSearch))
		gateway.POST("/x_search", service.GatewaySearchHandler(h.Gateway.XSearch))
	}

	// Gemini 原生 API 兼容层（Gemini SDK/CLI 直连）
	gemini := r.Group("/v1beta")
	gemini.Use(middleware.GatewayProtocol())
	gemini.Use(bodyLimit)
	gemini.Use(clientRequestID)
	gemini.Use(opsErrorLogger)
	gemini.Use(endpointNorm)
	gemini.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	gemini.Use(groupModelAllowlist)
	gemini.Use(compositeGeminiTarget)
	gemini.Use(actionRequirement)
	gemini.Use(requireGroupGoogle)
	{
		gemini.GET("/models", h.Gateway.GeminiV1BetaListModels)
		gemini.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		// Gin treats ":" as a param marker, but Gemini uses "{model}:{action}" in the same segment.
		gemini.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

	// OpenAI Responses API（不带v1前缀的别名）— auto-route based on group platform
	responsesHandler := dispatch.Handler("responses")
	// 根路径别名共用中间件链：白名单准入在 apiKeyAuth 之后、compositeTarget
	// 之前，避免逐条路由手工维护链导致漏挂。
	rootRoute := func(method, path string, limit gin.HandlerFunc, handler gin.HandlerFunc) {
		r.Handle(method, path, middleware.GatewayProtocol(), limit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), groupModelAllowlist, compositeTarget, actionRequirement, requireGroupAnthropic, handler)
	}
	rootRoute(http.MethodPost, "/messages", bodyLimit, messagesHandler)
	rootRoute(http.MethodPost, "/responses", bodyLimit, responsesHandler)
	// /v1/sub2api/billing 在分组/白名单准入之前注册；根别名保持同一中间件序列。
	r.GET("/sub2api/billing", middleware.GatewayProtocol(), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), h.Gateway.KeyBillingInfo)
	// Native DashScope uses the same authenticated, allowlisted composite route
	// chain. Unknown families are explicitly rejected by the native handler.
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rootRoute(method, "/api/v1/services/*nativePath", bodyLimit, h.OpenAIGateway.QianwenNative)
		rootRoute(method, "/api-ws/v1/*nativePath", bodyLimit, h.OpenAIGateway.QianwenNative)
		for _, prefix := range []string{"/api/v1/tasks", "/api/v1/apps", "/api/v1/files", "/api/v1/batches", "/api/v1/realtime", "/api/v1/indices", "/compatible-mode/v1/files", "/compatible-mode/v1/batches"} {
			rootRoute(method, prefix, bodyLimit, h.OpenAIGateway.QianwenNative)
			rootRoute(method, prefix+"/*nativePath", bodyLimit, h.OpenAIGateway.QianwenNative)
		}
	}
	rootRoute(http.MethodPost, "/responses/*subpath", bodyLimit, guardResponsesSubpath(responsesHandler))
	rootRoute(http.MethodPost, "/alpha/search", textBodyLimit, h.OpenAIGateway.AlphaSearch)
	rootRoute(http.MethodGet, "/responses", bodyLimit, func(c *gin.Context) {
		h.OpenAIGateway.ResponsesWebSocket(c)
	})
	rootRoute(http.MethodGet, "/models", bodyLimit, modelsHandler)
	rootRoute(http.MethodGet, "/models/:model", bodyLimit, h.Gateway.Models)
	rootRoute(http.MethodGet, "/models/catalog", bodyLimit, h.Gateway.ModelCatalog)
	rootRoute(http.MethodGet, "/models/catalog/:model", bodyLimit, h.Gateway.ModelCatalog)
	rootRoute(http.MethodGet, "/usage", bodyLimit, h.Gateway.Usage)
	rootRoute(http.MethodPost, "/images/batches", bodyLimit, h.BatchImage.Submit)
	rootRoute(http.MethodGet, "/images/batches", bodyLimit, h.BatchImage.List)
	rootRoute(http.MethodGet, "/images/batches/models", bodyLimit, h.BatchImage.Models)
	rootRoute(http.MethodGet, "/images/batches/:id", bodyLimit, h.BatchImage.Get)
	rootRoute(http.MethodGet, "/images/batches/:id/items", bodyLimit, h.BatchImage.Items)
	rootRoute(http.MethodGet, "/images/batches/:id/items/:custom_id/content", bodyLimit, h.BatchImage.ItemContent)
	rootRoute(http.MethodGet, "/images/batches/:id/download", bodyLimit, h.BatchImage.Download)
	rootRoute(http.MethodPost, "/images/batches/:id/cancel", bodyLimit, h.BatchImage.Cancel)
	rootRoute(http.MethodDelete, "/images/batches/:id", bodyLimit, h.BatchImage.DeleteRecord)
	rootRoute(http.MethodDelete, "/images/batches/:id/outputs", bodyLimit, h.BatchImage.DeleteOutputs)
	rootRoute(http.MethodPost, "/live", bodyLimit, h.OpenAIGateway.Live)
	rootRoute(http.MethodGet, "/live/:call_id", bodyLimit, h.OpenAIGateway.LiveSideband)
	rootRoute(http.MethodPost, "/messages/count_tokens", bodyLimit, countTokensHandler)
	codexDirect := r.Group("/backend-api/codex")
	codexDirect.Use(middleware.GatewayProtocol(), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), groupModelAllowlist, compositeTarget, actionRequirement, requireGroupAnthropic)
	{
		codexDirect.POST("/realtime/calls", h.OpenAIGateway.Live)
		codexDirect.GET("/:call_id", h.OpenAIGateway.LiveSideband)
		codexDirect.POST("/responses", responsesHandler)
		codexDirect.POST("/responses/*subpath", guardResponsesSubpath(responsesHandler))
		codexDirect.POST("/alpha/search", textBodyLimit, h.OpenAIGateway.AlphaSearch)
		codexDirect.GET("/responses", func(c *gin.Context) {
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		codexDirect.GET("/models", codexModelsHandler)
	}
	// OpenAI Chat Completions API（不带v1前缀的别名）— auto-route based on group platform
	rootRoute(http.MethodPost, "/chat/completions", bodyLimit, dispatch.Handler("chat"))
	rootRoute(http.MethodPost, "/embeddings", textBodyLimit, dispatch.Handler("embeddings"))
	rootRoute(http.MethodPost, "/rerank", textBodyLimit, rerankHandler)
	rootRoute(http.MethodPost, "/images/generations", bodyLimit, imagesHandler)
	rootRoute(http.MethodPost, "/images/edits", bodyLimit, imagesHandler)
	rootRoute(http.MethodPost, "/images/variations", bodyLimit, imagesHandler)
	rootRoute(http.MethodPost, "/images/generations/async", bodyLimit, h.AsyncImage.Submit)
	rootRoute(http.MethodPost, "/images/edits/async", bodyLimit, h.AsyncImage.Submit)
	rootRoute(http.MethodGet, "/images/tasks/:task_id", bodyLimit, h.AsyncImage.Get)
	rootRoute(http.MethodPost, "/videos", bodyLimit, videoGenerationHandler)
	rootRoute(http.MethodPost, "/videos/generations", bodyLimit, videoGenerationHandler)
	rootRoute(http.MethodPost, "/videos/edits", bodyLimit, videoEditHandler)
	rootRoute(http.MethodPost, "/videos/extensions", bodyLimit, videoExtensionHandler)
	rootRoute(http.MethodGet, "/videos/generations/:request_id/content", bodyLimit, videoContentHandler)
	rootRoute(http.MethodGet, "/videos/edits/:request_id/content", bodyLimit, videoContentHandler)
	rootRoute(http.MethodGet, "/videos/extensions/:request_id/content", bodyLimit, videoContentHandler)
	rootRoute(http.MethodGet, "/videos/generations/:request_id", bodyLimit, videoStatusHandler)
	rootRoute(http.MethodGet, "/videos/edits/:request_id", bodyLimit, videoStatusHandler)
	rootRoute(http.MethodGet, "/videos/extensions/:request_id", bodyLimit, videoStatusHandler)
	rootRoute(http.MethodGet, "/videos/:request_id", bodyLimit, videoStatusHandler)
	rootRoute(http.MethodGet, "/videos/:request_id/content", bodyLimit, videoContentHandler)

	rootVoiceHandler := func(endpoint string) gin.HandlerFunc {
		return service.GatewayVoiceHandler(h.OpenAIGateway.GrokVoice, func(*gin.Context) string { return endpoint })
	}
	rootRoute(http.MethodPost, "/tts", bodyLimit, rootVoiceHandler("tts"))
	rootRoute(http.MethodPost, "/stt", bodyLimit, rootVoiceHandler("stt"))
	rootRoute(http.MethodPost, "/custom-voices", bodyLimit, rootVoiceHandler("custom-voices"))
	rootCustomVoicePathHandler := service.GatewayVoiceHandler(h.OpenAIGateway.GrokVoice, service.GrokCustomVoiceEndpoint)
	rootRoute(http.MethodGet, "/custom-voices", bodyLimit, rootVoiceHandler("custom-voices"))
	rootRoute(http.MethodGet, "/custom-voices/:voice_id/audio", bodyLimit, rootCustomVoicePathHandler)
	rootRoute(http.MethodGet, "/custom-voices/:voice_id", bodyLimit, rootCustomVoicePathHandler)
	rootRoute(http.MethodPatch, "/custom-voices/:voice_id", bodyLimit, rootCustomVoicePathHandler)
	rootRoute(http.MethodDelete, "/custom-voices/:voice_id", bodyLimit, rootCustomVoicePathHandler)
	rootRoute(http.MethodGet, "/realtime", bodyLimit, dispatch.Handler("realtime"))
	rootRoute(http.MethodPost, "/web_search", bodyLimit, service.GatewaySearchHandler(h.Gateway.WebSearch))
	rootRoute(http.MethodPost, "/x_search", bodyLimit, service.GatewaySearchHandler(h.Gateway.XSearch))

	// Antigravity 模型列表
	r.GET("/antigravity/models", middleware.GatewayProtocol(), gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.Gateway.AntigravityModels)

	// Antigravity 专用路由（仅使用 antigravity 账户，不混合调度）
	antigravityV1 := r.Group("/antigravity/v1")
	antigravityV1.Use(middleware.GatewayProtocol())
	antigravityV1.Use(bodyLimit)
	antigravityV1.Use(clientRequestID)
	antigravityV1.Use(opsErrorLogger)
	antigravityV1.Use(endpointNorm)
	antigravityV1.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1.Use(gin.HandlerFunc(apiKeyAuth))
	antigravityV1.Use(groupModelAllowlist)
	antigravityV1.Use(actionRequirement)
	antigravityV1.Use(requireGroupAnthropic)
	{
		antigravityV1.POST("/messages", h.Gateway.Messages)
		antigravityV1.POST("/messages/count_tokens", h.Gateway.CountTokens)
		antigravityV1.GET("/models", h.Gateway.AntigravityModels)
		antigravityV1.GET("/usage", h.Gateway.Usage)
	}

	antigravityV1Beta := r.Group("/antigravity/v1beta")
	antigravityV1Beta.Use(middleware.GatewayProtocol())
	antigravityV1Beta.Use(bodyLimit)
	antigravityV1Beta.Use(clientRequestID)
	antigravityV1Beta.Use(opsErrorLogger)
	antigravityV1Beta.Use(endpointNorm)
	antigravityV1Beta.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1Beta.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	antigravityV1Beta.Use(groupModelAllowlist)
	antigravityV1Beta.Use(actionRequirement)
	antigravityV1Beta.Use(requireGroupGoogle)
	{
		antigravityV1Beta.GET("/models", h.Gateway.GeminiV1BetaListModels)
		antigravityV1Beta.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		antigravityV1Beta.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

}

func dispatchCodexModelsGateway(c *gin.Context, openAIHandler, generatedHandler gin.HandlerFunc) {
	service.CodexModelsGateway(openAIHandler, generatedHandler)(c)
}
func getGroupPlatform(c *gin.Context) string { return service.GatewayTargetPlatform(c) }
func compositeTargetPlatformMiddleware(resolver *service.CompositeRouteResolver) gin.HandlerFunc {
	return service.CompositeTargetPlatformMiddleware(resolver)
}
func compositeGeminiTargetPlatformMiddleware(resolver *service.CompositeRouteResolver) gin.HandlerFunc {
	return service.CompositeGeminiTargetPlatformMiddleware(resolver)
}
func grokCustomVoiceEndpoint(c *gin.Context) string { return service.GrokCustomVoiceEndpoint(c) }
func compositeGeminiModelFromParams(c *gin.Context) string {
	return service.CompositeGeminiModelFromParams(c)
}
func compositeRouteEndpointForPath(path string) string {
	return service.CompositeRouteEndpointForPath(path)
}

// qianfanVoiceHandler serves both voice actions on one dispatch key: voice
// enrollment is a POST and the owner-scoped voice list is a GET.
func qianfanVoiceHandler(create, list gin.HandlerFunc) gin.HandlerFunc {
	return func(requestContext *gin.Context) {
		if requestContext.Request.Method == http.MethodGet {
			list(requestContext)
			return
		}
		create(requestContext)
	}
}
