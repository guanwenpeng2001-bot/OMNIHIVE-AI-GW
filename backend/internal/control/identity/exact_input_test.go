package identity

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/stretchr/testify/require"
)

func TestExactGrantsRepresentationValidation(t *testing.T) {
	engine := gin.New()
	Register(engine.Group("/api/v2"), New(nil, domain.Operations{}, &config.Config{}), func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 3}) })
	for _, body := range []string{`{}`, `{"project_grants":null}`, `{"project_ids":[]}`, `{"actions":[]}`, `{"project_grants":[],"actions":null}`, `{"project_grants":[{"project_id":"02","actions":[]}]}`, `{"project_grants":[{"project_id":"3"}]}`, `{"project_grants":[{"project_id":3,"actions":[]}]}`} {
		req := httptest.NewRequest("PUT", "/api/v2/workspaces/2/members/4/grants", strings.NewReader(body))
		req.Header.Set("If-Match", `"1"`)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		require.Equal(t, 422, rec.Code, body)
	}
}
