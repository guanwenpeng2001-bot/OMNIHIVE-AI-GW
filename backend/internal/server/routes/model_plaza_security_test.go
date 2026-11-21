package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
)

func TestAnonymousPlaygroundRouteRetired(t *testing.T) {
	r := gin.New()
	RegisterModelPlazaRoutes(r.Group("/api/v1"), &handler.Handlers{}, func(c *gin.Context) { c.Next() }, nil, nil)
	for _, route := range r.Routes() {
		if route.Method == "POST" && route.Path == "/api/v1/public/playground/chat" {
			t.Fatal("anonymous inference route is registered")
		}
	}
}
