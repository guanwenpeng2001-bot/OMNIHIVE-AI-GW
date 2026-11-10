package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
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
