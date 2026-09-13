//go:build unit

package admin

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

type auditErrorRuleRepo struct {
	service.ErrorPassthroughRepository
	created *model.ErrorPassthroughRule
}

func (r *auditErrorRuleRepo) List(context.Context) ([]*model.ErrorPassthroughRule, error) {
	return nil, nil
}
func (r *auditErrorRuleRepo) Create(_ context.Context, rule *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	r.created = rule
	return rule, nil
}
func TestErrorPassthroughCreateDefaultsToPrivateBody(t *testing.T) {
	repo := &auditErrorRuleRepo{}
	h := NewErrorPassthroughHandler(service.NewErrorPassthroughService(repo, nil))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"test","error_codes":[502]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.Create(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotNil(t, repo.created)
	require.False(t, repo.created.PassthroughBody)
	require.Equal(t, "Upstream request failed", *repo.created.CustomMessage)
}
