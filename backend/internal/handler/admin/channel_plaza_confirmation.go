package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func plazaAdministrator(c *gin.Context) (int64, bool) {
	subject, exists := middleware.GetAuthSubjectFromContext(c)
	role, hasRole := middleware.GetUserRoleFromContext(c)
	if !exists || !hasRole || role != service.RoleAdmin || subject.UserID <= 0 {
		response.Error(c, 403, "Administrator identity required")
		return 0, false
	}
	return subject.UserID, true
}

func (handler *ChannelHandler) plazaCandidates(c *gin.Context) {
	if _, ok := plazaAdministrator(c); !ok {
		return
	}
	channelID, channelErr := strconv.ParseInt(c.Param("id"), 10, 64)
	groupID, groupErr := strconv.ParseInt(c.Query("plaza_group_id"), 10, 64)
	if channelErr != nil || groupErr != nil || channelID <= 0 || groupID <= 0 {
		response.ErrorFrom(c, service.ErrPlazaConfirmationInvalid)
		return
	}
	view, err := handler.channelService.PlazaCandidates(c.Request.Context(), channelID, groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

func (handler *ChannelHandler) confirmPlazaModels(c *gin.Context) {
	actorID, ok := plazaAdministrator(c)
	if !ok {
		return
	}
	channelID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var input service.PlazaConfirmationInput
	if err != nil || channelID <= 0 || c.ShouldBindJSON(&input) != nil || input.GroupID <= 0 || input.Revision == "" || input.Models == nil {
		response.ErrorFrom(c, service.ErrPlazaConfirmationInvalid)
		return
	}
	if err := handler.channelService.ConfirmPlazaModels(c.Request.Context(), channelID, input, actorID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}
