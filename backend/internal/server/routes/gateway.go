package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// GatewayActionHandlers is the adapter-name to handler binding the gateway
// ingress registers. It is exposed so the wiring invariant ("every implemented
// platform×action cell resolves to a registered handler") can be asserted
// against the authoritative action matrix instead of a copied table.
func GatewayActionHandlers(h *handler.Handlers) service.GatewayActionHandlers {
	handlers := service.GatewayActionHandlers{
		"gateway.chat":              h.Gateway.ChatCompletions,
		"openai.chat":               h.OpenAIGateway.ChatCompletions,
		"gateway.messages":          h.Gateway.Messages,
		"openai.messages":           h.OpenAIGateway.Messages,
		"gateway.responses":         h.Gateway.Responses,
		"openai.responses":          h.OpenAIGateway.Responses,
		"gateway.count_tokens":      h.Gateway.CountTokens,
		"gateway.generate_content":  h.Gateway.GeminiV1BetaModels,
		"openai.count_tokens":       h.OpenAIGateway.CountTokens,
		"cursor.count_tokens":       h.OpenAIGateway.CursorCountTokens,
		"grok.count_tokens":         h.OpenAIGateway.GrokCountTokens,
		"openai.embeddings":         h.OpenAIGateway.Embeddings,
		"openai.images":             h.OpenAIGateway.Images,
		"openai.image.variation":    h.OpenAIGateway.Images,
		"grok.images":               h.OpenAIGateway.GrokImages,
		"grok.video.generate":       h.OpenAIGateway.GrokVideoGeneration,
		"grok.video.edit":           h.OpenAIGateway.GrokVideoEdit,
		"grok.video.extend":         h.OpenAIGateway.GrokVideoExtension,
		"grok.video.status":         h.OpenAIGateway.GrokVideoStatus,
		"grok.video.content":        h.OpenAIGateway.GrokVideoContent,
		"grok.realtime":             h.OpenAIGateway.GrokRealtime,
		"openai.video.generate":     h.OpenAIGateway.OpenAIVideoGeneration,
		"openai.video.edit":         h.OpenAIGateway.OpenAIVideoEdit,
		"openai.video.extend":       h.OpenAIGateway.OpenAIVideoExtension,
		"openai.video.status":       h.OpenAIGateway.OpenAIVideoStatus,
		"openai.video.content":      h.OpenAIGateway.OpenAIVideoContent,
		"openai.audio.translate":    h.OpenAIGateway.OpenAIAudioTranslations,
		"antigravity.images":        h.Gateway.AntigravityImages,
		"bedrock.embeddings":        h.Gateway.BedrockEmbeddings,
		"bedrock.rerank":            h.Gateway.BedrockRerank,
		"bedrock.images":            h.Gateway.BedrockImages,
		"bedrock.image.variation":   h.Gateway.BedrockImages,
		"qianwen.embeddings":        h.OpenAIGateway.QianwenEmbeddings,
		"qianwen.rerank":            h.OpenAIGateway.QianwenRerank,
		"qianwen.images":            h.OpenAIGateway.QianwenImages,
		"qianwen.image.edit":        h.OpenAIGateway.QianwenImageEdit,
		"qianwen.video.generate":    h.OpenAIGateway.QianwenVideoGeneration,
		"qianwen.video.status":      h.OpenAIGateway.QianwenVideoStatus,
		"qianwen.video.content":     h.OpenAIGateway.QianwenVideoContent,
		"qianwen.video.cancel":      h.OpenAIGateway.QianwenVideoCancel,
		"qianwen.audio.tts":         h.OpenAIGateway.QianwenAudioSpeech,
		"qianwen.audio.stt":         h.OpenAIGateway.QianwenAudioTranscriptions,
		"qianwen.realtime":          h.OpenAIGateway.QianwenRealtimeCompat,
		"openai.realtime":           h.OpenAIGateway.OpenAIRealtime,
		"qianwen.native":            h.OpenAIGateway.QianwenNative,
		"qianfan.embeddings":        h.OpenAIGateway.QianfanEmbeddings,
		"qianfan.rerank":            h.OpenAIGateway.QianfanRerank,
		"qianfan.images":            h.OpenAIGateway.QianfanImages,
		"qianfan.image.edit":        h.OpenAIGateway.QianfanImageEdit,
		"qianfan.video.generate":    h.OpenAIGateway.QianfanVideoGeneration,
		"qianfan.video.status":      h.OpenAIGateway.QianfanVideoStatus,
		"qianfan.video.cancel":      h.OpenAIGateway.QianfanVideoCancel,
		"qianfan.video.content":     h.OpenAIGateway.QianfanVideoContent,
		"qianfan.audio.tts":         h.OpenAIGateway.QianfanAudioSpeech,
		"qianfan.voice":             qianfanVoiceHandler(h.OpenAIGateway.QianfanVoiceCloneCreate, h.OpenAIGateway.QianfanVoiceCloneList),
		"kimi.search":               h.OpenAIGateway.KimiSearch,
		"qianfan.search":            h.OpenAIGateway.QianfanSearch,
		"hunyuan.embeddings":        h.OpenAIGateway.HunyuanEmbeddings,
		"hunyuan.images":            h.OpenAIGateway.HunyuanImageGeneration,
		"hunyuan.image.edit":        h.OpenAIGateway.HunyuanImageEdit,
		"hunyuan.video.generate":    h.OpenAIGateway.HunyuanVideoGeneration,
		"hunyuan.video.status":      h.OpenAIGateway.HunyuanVideoStatus,
		"hunyuan.video.content":     h.OpenAIGateway.HunyuanVideoContent,
		"hunyuan.audio.tts":         h.OpenAIGateway.HunyuanAudioSpeech,
		"hunyuan.audio.stt":         h.OpenAIGateway.HunyuanAudioTranscriptions,
		"hunyuan.voice":             h.OpenAIGateway.HunyuanVoiceClone,
		"openrouter.video.generate": h.OpenAIGateway.OpenRouterVideoGeneration,
		"openrouter.video.status":   h.OpenAIGateway.OpenRouterVideoStatus,
		"openrouter.video.content":  h.OpenAIGateway.OpenRouterVideoContent,
		"openrouter.image.generate": h.OpenAIGateway.OpenRouterImages,
		"kaggle.images":             h.OpenAIGateway.KaggleImages,
		"kaggle.video.generate":     h.OpenAIGateway.KaggleVideoGeneration,
		"kaggle.video.status":       h.OpenAIGateway.KaggleVideoStatus,
		"kaggle.video.content":      h.OpenAIGateway.KaggleVideoContent,
		"openrouter.embeddings":     h.OpenAIGateway.OpenRouterEmbeddings,
		"openrouter.rerank":         h.OpenAIGateway.OpenRouterRerank,
		"openrouter.audio.tts":      h.OpenAIGateway.OpenRouterSpeech,
		"openrouter.audio.stt":      h.OpenAIGateway.OpenRouterTranscriptions,
		"mistral.embeddings":        h.OpenAIGateway.MistralEmbeddings,
		"mistral.audio.stt":         h.OpenAIGateway.MistralAudioTranscriptions,
		"mistral.audio.tts":         h.OpenAIGateway.MistralAudioSpeech,
		"mistral.voice":             h.OpenAIGateway.MistralVoices,
		"volcengine.embeddings":     h.OpenAIGateway.VolcengineEmbeddings,
		"volcengine.images":         h.OpenAIGateway.VolcengineImages,
		"volcengine.video.generate": h.OpenAIGateway.VolcengineVideoGeneration,
		"volcengine.video.edit":     h.OpenAIGateway.VolcengineVideoEdit,
		"volcengine.video.extend":   h.OpenAIGateway.VolcengineVideoExtend,
		"volcengine.video.status":   h.OpenAIGateway.VolcengineVideoStatus,
		"volcengine.video.content":  h.OpenAIGateway.VolcengineVideoContent,
		"opencode.generate_content": h.Gateway.OpenCodeGenerateContent,
		"image.async":               h.AsyncImage.Submit,
		"image.batch":               h.BatchImage.Submit,
	}
	// Build callbacks from the existing action bindings. The dispatcher copies
	// its map, so install the public Google converter before returning the final
	// registry used by route dispatch. Native Gemini/Antigravity/OpenCode retain
	// their own registered adapters.
	text := service.NewGatewayActionDispatch(handlers)
	handlers["gateway.generate_content_compat"] = handler.GeminiProtocolBridge(
		text.Handler("messages"), text.Handler("count_tokens"), text.Handler("embeddings"),
	)
	return handlers
}

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
	compositeTarget := h.OpenAIGateway.CompositeVideoJobTarget(compositeTargetPlatformMiddleware(compositeResolver))
	compositeGeminiTarget := compositeGeminiTargetPlatformMiddleware(compositeResolver)

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)
	requireGroupGoogle := middleware.RequireGroupAssignment(settingService, middleware.GoogleErrorWriter)

	// 分组级模型白名单准入：在 apiKeyAuth 之后、compositeTarget 之前，
	// 保证校验发生在合成路由改写与调度之前，且只看客户端书写的模型名。
	groupModelAllowlist := middleware.GroupModelAllowlist()
	actionRequirement := h.Gateway.ActionRequirement

	dispatch := service.NewGatewayActionDispatch(GatewayActionHandlers(h))
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
	// Standard OpenAI audio paths: the action matrix resolves each one to the
	// platform's own adapter, so the same routes serve the platforms whose cell
	// binds an OpenAI-shaped audio adapter (Hunyuan, Qianfan) and reject a platform
	// without that cell before scheduling. Speech synthesis is a JSON body and
	// keeps the tighter text limit; recognition and translation carry uploads.
	audioSpeechHandler := dispatch.Handler("audio.tts")
	audioTranscriptionsHandler := dispatch.Handler("audio.stt")
	audioTranslationsHandler := dispatch.Handler("audio.translate")
	rerankHandler := dispatch.Handler("rerank")
	guardResponsesSubpath := func(next gin.HandlerFunc) gin.HandlerFunc {
		return service.GuardResponsesSubpath(next, h.OpenAIGateway.ResponsesInputTokens)
	}
	// API网关（Claude API兼容）
	var extensionMiddleware gin.HandlerFunc
	if h != nil && h.MCP != nil {
		extensionMiddleware = h.MCP.GatewayExtensionMiddleware()
	}
	gateway := r.Group("/v1")
	gateway.Use(middleware.GatewayProtocol())
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	if extensionMiddleware != nil {
		gateway.Use(extensionMiddleware)
	}
	gateway.GET("/sub2api/billing", h.Gateway.KeyBillingInfo)
	gateway.Use(groupModelAllowlist)
	gateway.Use(compositeTarget)
	gateway.Use(actionRequirement)
	gateway.Use(requireGroupAnthropic)
	{
		// Standard resource contracts share the gateway authentication and body limits.
		gateway.POST("/files", h.Resource.UploadFile)
		gateway.GET("/files", h.Resource.ListFiles)
		gateway.GET("/files/:file_id", h.Resource.GetFile)
		gateway.GET("/files/:file_id/content", h.Resource.DownloadFileContent)
		gateway.DELETE("/files/:file_id", h.Resource.DeleteFile)
		gateway.POST("/batches", h.Resource.CreateBatch)
		gateway.GET("/batches", h.Resource.ListBatches)
		gateway.GET("/batches/:batch_id", h.Resource.GetBatch)
		gateway.POST("/batches/:batch_id/cancel", h.Resource.CancelBatch)
		gateway.POST("/moderations", h.Resource.Moderations)
		gateway.POST("/messages/batches", h.Resource.CreateMessageBatch)
		gateway.GET("/messages/batches", h.Resource.ListMessageBatches)
		gateway.GET("/messages/batches/:message_batch_id", h.Resource.GetMessageBatch)
		gateway.POST("/messages/batches/:message_batch_id/cancel", h.Resource.CancelMessageBatch)
		gateway.DELETE("/messages/batches/:message_batch_id", h.Resource.DeleteMessageBatch)
		gateway.GET("/messages/batches/:message_batch_id/results", h.Resource.MessageBatchResults)
		gateway.POST("/tools/search", textBodyLimit, dispatch.Handler("search"))
		gateway.POST("/tools/search_pro", textBodyLimit, dispatch.Handler("search"))
		gateway.POST("/videos/:request_id/cancel", dispatch.Handler("video.cancel"))
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
		gateway.POST("/audio/speech", textBodyLimit, audioSpeechHandler)
		gateway.POST("/audio/transcriptions", bodyLimit, audioTranscriptionsHandler)
		gateway.POST("/audio/translations", bodyLimit, audioTranslationsHandler)

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
	if extensionMiddleware != nil {
		gemini.Use(extensionMiddleware)
	}
	gemini.Use(groupModelAllowlist)
	gemini.Use(compositeGeminiTarget)
	gemini.Use(actionRequirement)
	gemini.Use(requireGroupGoogle)
	{
		gemini.GET("/models", h.Gateway.GeminiV1BetaListModels)
		gemini.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		// Gin treats ":" as a param marker, but Gemini uses "{model}:{action}" in the same segment.
		gemini.POST("/models/*modelAction", dispatch.Handler("generate_content"))
	}

	// OpenAI Responses API（不带v1前缀的别名）— auto-route based on group platform
	responsesHandler := dispatch.Handler("responses")
	// 根路径别名共用中间件链：白名单准入在 apiKeyAuth 之后、compositeTarget
	// 之前，避免逐条路由手工维护链导致漏挂。
	rootRoute := func(method, path string, limit gin.HandlerFunc, handler gin.HandlerFunc) {
		if extensionMiddleware != nil {
			r.Handle(method, path, middleware.GatewayProtocol(), limit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), extensionMiddleware, groupModelAllowlist, compositeTarget, actionRequirement, requireGroupAnthropic, handler)
			return
		}
		r.Handle(method, path, middleware.GatewayProtocol(), limit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), groupModelAllowlist, compositeTarget, actionRequirement, requireGroupAnthropic, handler)
	}
	rootRoute(http.MethodPost, "/files", bodyLimit, h.Resource.UploadFile)
	rootRoute(http.MethodGet, "/files", bodyLimit, h.Resource.ListFiles)
	rootRoute(http.MethodGet, "/files/:file_id", bodyLimit, h.Resource.GetFile)
	rootRoute(http.MethodGet, "/files/:file_id/content", bodyLimit, h.Resource.DownloadFileContent)
	rootRoute(http.MethodDelete, "/files/:file_id", bodyLimit, h.Resource.DeleteFile)
	rootRoute(http.MethodPost, "/batches", bodyLimit, h.Resource.CreateBatch)
	rootRoute(http.MethodGet, "/batches", bodyLimit, h.Resource.ListBatches)
	rootRoute(http.MethodGet, "/batches/:batch_id", bodyLimit, h.Resource.GetBatch)
	rootRoute(http.MethodPost, "/batches/:batch_id/cancel", bodyLimit, h.Resource.CancelBatch)
	rootRoute(http.MethodPost, "/moderations", bodyLimit, h.Resource.Moderations)
	rootRoute(http.MethodPost, "/messages/batches", bodyLimit, h.Resource.CreateMessageBatch)
	rootRoute(http.MethodGet, "/messages/batches", bodyLimit, h.Resource.ListMessageBatches)
	rootRoute(http.MethodGet, "/messages/batches/:message_batch_id", bodyLimit, h.Resource.GetMessageBatch)
	rootRoute(http.MethodPost, "/messages/batches/:message_batch_id/cancel", bodyLimit, h.Resource.CancelMessageBatch)
	rootRoute(http.MethodDelete, "/messages/batches/:message_batch_id", bodyLimit, h.Resource.DeleteMessageBatch)
	rootRoute(http.MethodGet, "/messages/batches/:message_batch_id/results", bodyLimit, h.Resource.MessageBatchResults)
	rootRoute(http.MethodPost, "/tools/search", textBodyLimit, dispatch.Handler("search"))
	rootRoute(http.MethodPost, "/tools/search_pro", textBodyLimit, dispatch.Handler("search"))
	rootRoute(http.MethodPost, "/videos/:request_id/cancel", bodyLimit, dispatch.Handler("video.cancel"))
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
	if extensionMiddleware != nil {
		codexDirect.Use(middleware.GatewayProtocol(), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), extensionMiddleware, groupModelAllowlist, compositeTarget, actionRequirement, requireGroupAnthropic)
	} else {
		codexDirect.Use(middleware.GatewayProtocol(), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), groupModelAllowlist, compositeTarget, actionRequirement, requireGroupAnthropic)
	}
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
	rootRoute(http.MethodPost, "/audio/speech", textBodyLimit, audioSpeechHandler)
	rootRoute(http.MethodPost, "/audio/transcriptions", bodyLimit, audioTranscriptionsHandler)
	rootRoute(http.MethodPost, "/audio/translations", bodyLimit, audioTranslationsHandler)

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
		antigravityV1Beta.POST("/models/*modelAction", dispatch.Handler("generate_content"))
	}

}

func dispatchCodexModelsGateway(c *gin.Context, openAIHandler, generatedHandler gin.HandlerFunc) {
	service.CodexModelsGateway(openAIHandler, generatedHandler)(c)
}
func compositeTargetPlatformMiddleware(resolver *service.CompositeRouteResolver) gin.HandlerFunc {
	return service.CompositeTargetPlatformMiddleware(resolver)
}
func compositeGeminiTargetPlatformMiddleware(resolver *service.CompositeRouteResolver) gin.HandlerFunc {
	return service.CompositeGeminiTargetPlatformMiddleware(resolver)
}
func grokCustomVoiceEndpoint(c *gin.Context) string { return service.GrokCustomVoiceEndpoint(c) }
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
