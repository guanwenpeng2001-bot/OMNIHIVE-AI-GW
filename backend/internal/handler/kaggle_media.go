package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// KaggleImages serves POST /v1/images/generations for Kaggle notebook accounts.
func (handler *OpenAIGatewayHandler) KaggleImages(request *gin.Context) {
	handler.handleAPIKeyMediaAction(request, service.PlatformKaggle, "image.generate")
}

func (handler *OpenAIGatewayHandler) KaggleVideoGeneration(request *gin.Context) {
	handler.handleVideoSubmit(request, "video.generate", service.PlatformKaggle)
}

func (handler *OpenAIGatewayHandler) KaggleVideoStatus(request *gin.Context) {
	handler.handleVideoRead(request, false, service.PlatformKaggle)
}

func (handler *OpenAIGatewayHandler) KaggleVideoContent(request *gin.Context) {
	handler.handleVideoRead(request, true, service.PlatformKaggle)
}
