package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// The common media ingress retains public key/group policy and receipt ownership.
func (handler *OpenAIGatewayHandler) OpenRouterImages(request *gin.Context) {
	handler.handleAPIKeyMediaAction(request, service.PlatformOpenRouter, "image.generate")
}
func (handler *OpenAIGatewayHandler) OpenRouterEmbeddings(request *gin.Context) {
	handler.handleAPIKeyMediaAction(request, service.PlatformOpenRouter, mistralActionEmbeddings)
}
func (handler *OpenAIGatewayHandler) OpenRouterRerank(request *gin.Context) {
	handler.handleAPIKeyMediaAction(request, service.PlatformOpenRouter, "rerank")
}
func (handler *OpenAIGatewayHandler) OpenRouterSpeech(request *gin.Context) {
	handler.handleAPIKeyMediaAction(request, service.PlatformOpenRouter, mistralActionSpeech)
}
func (handler *OpenAIGatewayHandler) OpenRouterTranscriptions(request *gin.Context) {
	handler.handleAPIKeyMediaAction(request, service.PlatformOpenRouter, mistralActionTranscription)
}

func (handler *OpenAIGatewayHandler) OpenRouterVideoGeneration(request *gin.Context) {
	handler.handleVideoSubmit(request, "video.generate", service.PlatformOpenRouter)
}
func (handler *OpenAIGatewayHandler) OpenRouterVideoStatus(request *gin.Context) {
	handler.handleVideoRead(request, false, service.PlatformOpenRouter)
}
func (handler *OpenAIGatewayHandler) OpenRouterVideoContent(request *gin.Context) {
	handler.handleVideoRead(request, true, service.PlatformOpenRouter)
}
