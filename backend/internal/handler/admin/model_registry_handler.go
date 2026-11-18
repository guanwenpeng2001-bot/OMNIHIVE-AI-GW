package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// ReloadModelRegistry runs under the existing administrator authentication group.
// The request supplies a local package location, never a pin or remote URL.
func (h *AccountHandler) ReloadModelRegistry(c *gin.Context) {
	var request struct {
		Directory string `json:"directory" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "A local approved package directory is required")
		return
	}
	snapshot, err := service.NewModelRegistry().ReloadApprovedLocalPackage(request.Directory)
	if err != nil {
		response.BadRequest(c, "Model registry package rejected; previous generation retained")
		return
	}
	response.Success(c, gin.H{"generation": snapshot.Generation(), "manifest_sha256": snapshot.Version()})
}
