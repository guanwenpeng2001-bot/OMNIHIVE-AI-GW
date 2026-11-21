package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/stretchr/testify/require"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestGatewayRoutesContainNoPlatformDispatch(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "gateway.go", nil, 0)
	require.NoError(t, err)
	ast.Inspect(source, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.SwitchStmt, *ast.TypeSwitchStmt:
			t.Error("gateway route registration must delegate action policy to service")
		}
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if qualifier, ok := selector.X.(*ast.Ident); ok && (qualifier.Name == "gjson" || qualifier.Name == "sjson") {
				t.Error("routes must delegate model parsing/rewriting")
			}
		}
		return true
	})
}

func TestRegisteredPaidActionsRequirePlanAtSend(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(journal.WithStore(c.Request.Context(), journal.New(nil)))
	}, handler.InstallExecutionJournal(nil, nil), func(c *gin.Context) {
		// Simulate an adapter that skipped plan capture, immediately before transport.
		finish, err := service.BeginGenerationAttempt(c.Request.Context(), 7)
		if err == nil {
			finish(http.StatusOK, nil)
			c.AbortWithStatus(http.StatusOK)
			return
		}
		require.ErrorIs(t, err, journal.ErrDispatch)
		c.AbortWithStatus(http.StatusServiceUnavailable)
	})
	RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)}, nil, nil, nil, nil, nil, nil, &config.Config{})
	checked := map[string]bool{}
	for _, route := range router.Routes() {
		if route.Method != http.MethodPost {
			continue
		}
		action := service.GatewayActionForRoute(route.Method, route.Path)
		if action == "" {
			continue
		}
		path := strings.NewReplacer(":request_id", "job", ":batch_id", "batch", ":voice_id", "voice", "*action", "model:generateContent", "*path", "create").Replace(route.Path)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(route.Method, path, nil))
		require.Equal(t, http.StatusServiceUnavailable, w.Code, "%s %s", action, route.Path)
		checked[action] = true
	}
	for _, action := range []string{"audio.translate", "audio.stt", "audio.tts", "rerank"} {
		require.True(t, checked[action], action)
	}
}
