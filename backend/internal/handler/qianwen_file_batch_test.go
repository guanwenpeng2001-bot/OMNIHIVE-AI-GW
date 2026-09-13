//go:build unit

package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fileBatchListRepository struct {
	*videoJobReadRepository
	listedUser, listedKey, listedGroup int64
	family                             string
}

func (r *fileBatchListRepository) ListResources(_ context.Context, user, key, group int64, family, after string, limit int) ([]*service.UpstreamJob, error) {
	r.listedUser, r.listedKey, r.listedGroup, r.family = user, key, group, family
	return []*service.UpstreamJob{r.job}, nil
}
func (r *fileBatchListRepository) FileInUse(context.Context, string) (bool, error) { return false, nil }

func TestQianwenFileBatchListUsesOwnedPublicOutput(t *testing.T) {
	g := int64(13)
	r := &fileBatchListRepository{videoJobReadRepository: &videoJobReadRepository{job: &service.UpstreamJob{UserID: 1, APIKeyID: 2, GroupID: &g, PublicRequestID: "public", UpstreamTaskID: "secret-upstream", ResultRef: "secret-output", Platform: service.PlatformQianwen, Family: "batch", Status: "succeeded"}}}
	h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(r, nil, nil)}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/compatible-mode/v1/files?purpose=batch_output&limit=1", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1, GroupID: &g, Group: &service.Group{Platform: service.PlatformQianwen}})
	h.QianwenNative(c)
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(1), r.listedUser)
	require.Equal(t, int64(2), r.listedKey)
	require.Equal(t, g, r.listedGroup)
	require.Equal(t, "batch_output", r.family)
	require.NotContains(t, w.Body.String(), "secret-")
	require.Contains(t, w.Body.String(), `"first_id":"output-public"`)
	require.Contains(t, w.Body.String(), `"has_more":false`)
}

func TestQianwenFileBatchReadOwnership(t *testing.T) {
	group := int64(13)
	repo := &videoJobReadRepository{job: &service.UpstreamJob{PublicRequestID: "public-1", UpstreamTaskID: "secret-upstream", UserID: 1, APIKeyID: 2, GroupID: &group, Platform: service.PlatformQianwen, Family: "batch", Status: "succeeded", ResultRef: "secret-output"}}
	h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(repo, nil, nil)}
	for _, tc := range []struct {
		key, group int64
		status     int
	}{{2, 13, 200}, {3, 13, 404}, {2, 14, 404}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/compatible-mode/v1/batches/public-1", nil)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: tc.key, UserID: 1, GroupID: &tc.group, Group: &service.Group{Platform: service.PlatformQianwen}})
		h.QianwenNative(c)
		require.Equal(t, tc.status, w.Code)
		require.NotContains(t, w.Body.String(), "secret-")
		if tc.status == 200 {
			require.Contains(t, w.Body.String(), "output-public-1")
			require.Contains(t, w.Body.String(), "completed")
		}
	}
}
func TestQianwenFileBatchActionsRequireAuthentication(t *testing.T) {
	h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(nil, nil, nil)}
	for _, tc := range []struct{ method, path string }{{"GET", "/compatible-mode/v1/files"}, {"GET", "/compatible-mode/v1/batches"}, {"DELETE", "/compatible-mode/v1/files/file-x"}, {"POST", "/compatible-mode/v1/batches/batch-x/cancel"}, {"POST", "/api/v1/files"}, {"POST", "/api/v1/batches"}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(tc.method, tc.path, strings.NewReader("must not read"))
		h.QianwenNative(c)
		if strings.HasPrefix(tc.path, "/compatible-mode/") {
			require.Equal(t, 401, w.Code)
			require.Contains(t, w.Body.String(), "authentication_error")
		} else {
			require.Equal(t, 404, w.Code)
			require.Contains(t, w.Body.String(), "unsupported_operation_error")
		}
		require.Contains(t, w.Body.String(), `"code"`)
	}
}
