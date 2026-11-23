package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type ModelAccessHandler struct {
	service *service.ModelAccessService
	gateway *service.GatewayService
	keys    *service.APIKeyService
}

func NewModelAccessHandler(access *service.ModelAccessService, gateway *service.GatewayService, keys *service.APIKeyService) *ModelAccessHandler {
	return &ModelAccessHandler{service: access, gateway: gateway, keys: keys}
}

func (h *ModelAccessHandler) Preview(resource string, own bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identifier, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
		if err != nil || identifier <= 0 {
			response.ErrorFrom(ctx, service.ErrModelAccessNotFound)
			return
		}
		var owner int64
		if own {
			subject, ok := middleware.GetAuthSubjectFromContext(ctx)
			if !ok {
				response.ErrorFrom(ctx, service.ErrModelAccessNotFound)
				return
			}
			owner = subject.UserID
		}
		var rule service.ModelAccessRule
		if err := ctx.ShouldBindJSON(&rule); err != nil {
			response.BadRequest(ctx, "Invalid model access rule")
			return
		}
		impact, err := h.service.Preview(service.WithLegacyKeyManagement(ctx.Request.Context()), resource, identifier, owner, rule, h.gateway, h.keys)
		if err != nil {
			response.ErrorFrom(ctx, err)
			return
		}
		response.Success(ctx, impact)
	}
}
func (h *ModelAccessHandler) List(ctx *gin.Context) {
	groups, err := h.service.List(ctx.Request.Context())
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, groups)
}
func (h *ModelAccessHandler) Save(ctx *gin.Context) {
	var group service.ModelAccessGroup
	if err := ctx.ShouldBindJSON(&group); err != nil {
		response.BadRequest(ctx, "Invalid model access group")
		return
	}
	group.ID = 0
	if ctx.Param("id") != "" {
		identifier, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
		if err != nil || identifier <= 0 {
			response.ErrorFrom(ctx, service.ErrModelAccessNotFound)
			return
		}
		group.ID = identifier
	}
	saved, err := h.service.SaveGroup(ctx.Request.Context(), group)
	if err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, saved)
}
func (h *ModelAccessHandler) Delete(ctx *gin.Context) {
	identifier, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || identifier <= 0 {
		response.ErrorFrom(ctx, service.ErrModelAccessNotFound)
		return
	}
	revision, _ := strconv.ParseInt(ctx.Query("revision"), 10, 64)
	if err := h.service.DeleteGroup(ctx.Request.Context(), identifier, revision); err != nil {
		response.ErrorFrom(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"deleted": true})
}
func (h *ModelAccessHandler) Rule(resource string, own bool, write bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identifier, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
		if err != nil || identifier <= 0 {
			response.ErrorFrom(ctx, service.ErrModelAccessNotFound)
			return
		}
		var owner int64
		if own {
			subject, ok := middleware.GetAuthSubjectFromContext(ctx)
			if !ok {
				response.ErrorFrom(ctx, service.ErrModelAccessNotFound)
				return
			}
			owner = subject.UserID
		}
		requestCtx := service.WithLegacyKeyManagement(ctx.Request.Context())
		if write {
			var rule service.ModelAccessRule
			if err := ctx.ShouldBindJSON(&rule); err != nil {
				response.BadRequest(ctx, "Invalid model access rule")
				return
			}
			if err := h.service.SaveRule(requestCtx, resource, identifier, owner, rule); err != nil {
				response.ErrorFrom(ctx, err)
				return
			}
		}
		rule, err := h.service.Rule(requestCtx, resource, identifier, owner)
		if err != nil {
			response.ErrorFrom(ctx, err)
			return
		}
		response.Success(ctx, rule)
	}
}
