//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type videoJobReadRepository struct {
	service.UpstreamJobRepository
	job   *service.UpstreamJob
	reads int
}

func (r *videoJobReadRepository) Get(_ context.Context, id string, user, key int64) (*service.UpstreamJob, error) {
	r.reads++
	if id != r.job.PublicRequestID || user != r.job.UserID || key != r.job.APIKeyID {
		return nil, service.ErrUpstreamJobNotFound
	}
	return r.job, nil
}

func TestQianwenNativeVideoMissingTariffNeverAdmits(t *testing.T) {
	h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(nil, nil, nil)}
	for range 2 {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", service.QianwenNativeVideoPath, strings.NewReader(`{"model":"wan2.6-t2v","parameters":{"size":"1280*720"}}`))
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: 1, Group: &service.Group{Platform: service.PlatformQianwen, RateMultiplier: 1}})
		h.QianwenNative(c)
		require.Equal(t, 422, w.Code)
		require.Contains(t, w.Body.String(), "tariff")
	}
}

func TestQianwenNativeVideoWrongResolutionFieldNeverAdmits(t *testing.T) {
	h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(nil, nil, nil)}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", service.QianwenNativeVideoPath, strings.NewReader(`{"model":"wan2.6-t2v","parameters":{"resolution":"720P"}}`))
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: 1, Group: &service.Group{
		Platform: service.PlatformQianwen, RateMultiplier: 1,
		VideoModelPrices: map[string]map[string]float64{"wan2.6-t2v": {"720p": 0.08571428571428572}},
	}})
	h.QianwenNative(c)
	require.Equal(t, 422, w.Code)
	require.Contains(t, w.Body.String(), "parameters.size")
}

func TestQianwenNativeVideoReadOwnershipAndPublicResult(t *testing.T) {
	repo := &videoJobReadRepository{job: &service.UpstreamJob{PublicRequestID: "public-1", UpstreamTaskID: "secret-upstream", UserID: 1, APIKeyID: 2, Platform: service.PlatformQianwen, Family: "video", Status: "succeeded", ResultRef: "https://example.test/video.mp4"}}
	h := &OpenAIGatewayHandler{upstreamJobs: service.NewUpstreamJobService(repo, nil, nil)}
	for _, key := range []int64{2, 3} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/tasks/public-1", nil)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: key, UserID: 1, Group: &service.Group{}})
		h.QianwenNative(c)
		require.NotContains(t, w.Body.String(), "secret-upstream")
		if key == 2 {
			require.Equal(t, 200, w.Code)
			require.Contains(t, w.Body.String(), "video.mp4")
		} else {
			require.Equal(t, 404, w.Code)
			require.NotContains(t, w.Body.String(), "video.mp4")
		}
	}
	require.Equal(t, 2, repo.reads)
}

func TestQianwenNativeVideoUnavailableBeforeAdmission(t *testing.T) {
	// A handler without services proves no scheduler, network or billing access.
	h := &OpenAIGatewayHandler{}
	for _, method := range []string{"POST", "GET", "DELETE"} {
		for repeat := 0; repeat < 2; repeat++ {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(method, service.QianwenNativeVideoPath, strings.NewReader(`{"model":"wan2.6-t2v","input":{"prompt":"A cat"}}`))
			c.Request.Header.Set("X-DashScope-Async", "enable")
			c.Request.Header.Set("Idempotency-Key", "same-submit")
			h.QianwenNative(c)
			if method == "POST" {
				require.Equal(t, 503, w.Code)
			} else {
				require.Equal(t, 404, w.Code)
			}
			require.Contains(t, w.Body.String(), "unsupported_operation_error")
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected)
		}
	}
}

func TestQianwenNativeVideoContentOwnerOnlyDownload(t *testing.T) {
	video := []byte("mp4-bytes")
	opener := qianwenVideoContentOpener
	qianwenVideoContentOpener = func(_ context.Context, account *service.Account, rawURL string) (*http.Response, error) {
		require.Equal(t, int64(56), account.ID)
		if rawURL != "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/out.mp4" {
			return nil, service.ErrUpstreamJobNotFound
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"video/mp4"}}, ContentLength: int64(len(video)), Body: io.NopCloser(bytes.NewReader(video))}, nil
	}
	t.Cleanup(func() { qianwenVideoContentOpener = opener })
	repo := &videoJobReadRepository{job: &service.UpstreamJob{PublicRequestID: "public-1", UpstreamTaskID: "secret-upstream", UserID: 1, APIKeyID: 2, AccountID: 56, Platform: service.PlatformQianwen, Family: "video", Status: "succeeded", ResultRef: "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/out.mp4"}}
	h := newQianwenStandardTestHandler(t, nil, []service.Account{qianwenCatalogAccount(56, "wan2.6-t2v")})
	h.upstreamJobs = service.NewUpstreamJobService(repo, nil, nil)
	for _, tc := range []struct {
		key    int64
		status int
	}{
		{2, 200}, {3, 404},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/tasks/public-1/content", nil)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: tc.key, UserID: 1, Group: &service.Group{}})
		h.QianwenNative(c)
		require.Equal(t, tc.status, w.Code)
		if tc.status == 200 {
			require.Equal(t, video, w.Body.Bytes())
			require.Equal(t, "video/mp4", w.Header().Get("Content-Type"))
		} else {
			require.NotContains(t, w.Body.String(), "mp4-bytes")
			require.NotContains(t, w.Body.String(), "secret-upstream")
		}
	}
	// Non-video families and unsettled jobs never expose content.
	repo.job.Family = "3d"
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/tasks/public-1/content", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1, Group: &service.Group{}})
	h.QianwenNative(c)
	require.Equal(t, 404, w.Code)
	repo.job.Family = "video"
	repo.job.Status = "running"
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/tasks/public-1/content", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1, Group: &service.Group{}})
	h.QianwenNative(c)
	require.Equal(t, 409, w.Code)
}

func TestQianwenNativeAsyncDoesNotClaimOtherFamilies(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	for _, path := range []string{service.QianwenNativeMultimodalPath, "/api/v1/tasks/job"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", path, nil)
		require.False(t, h.handleQianwenNativeAsyncJobs(c))
	}
}

type qianwenVideoEgressGroups struct {
	service.ProxyGroupRepository
	group *service.ProxyGroup
}

func (repo *qianwenVideoEgressGroups) GetByID(context.Context, int64) (*service.ProxyGroup, error) {
	return repo.group, nil
}

type qianwenVideoEgressCache struct{ service.ProxyGroupCache }

func (*qianwenVideoEgressCache) GetCooldown(context.Context, int64) (*service.ProxyCooldown, error) {
	return nil, nil
}

func TestQianwenVideoContentRestoresPinnedRouteBeforeDownload(t *testing.T) {
	for _, native := range []bool{true, false} {
		name := "standard"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			groupID := int64(7)
			proxy := &service.Proxy{ID: 11, WorkspaceID: 1, Status: service.StatusActive, Protocol: "http", Host: "proxy.test", Port: 3128}
			groups := &qianwenVideoEgressGroups{group: &service.ProxyGroup{ID: groupID, WorkspaceID: 1, Status: service.StatusActive, Members: []service.ProxyGroupMember{{GroupID: groupID, WorkspaceID: 1, ProxyID: proxy.ID, Proxy: proxy}}}}
			account := qianwenCatalogAccount(56, "wan2.6-t2v")
			account.WorkspaceID, account.ProxyGroupID = 1, &groupID
			account.AttachEgress(service.NewProxyGroupRuntime(groups, nil, &qianwenVideoEgressCache{}))
			handler := newQianwenStandardTestHandler(t, nil, []service.Account{account})
			job := &service.UpstreamJob{PublicRequestID: "public", UpstreamTaskID: "private-task", UserID: 1, APIKeyID: 2, AccountID: account.ID, Platform: service.PlatformQianwen, Family: "video", Status: "succeeded", ResultRef: "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/video?Signature=private", Billing: service.UpstreamJobBilling{Egress: &service.ProxyGroupJobEgress{ProxyGroupID: groupID, ProxyID: proxy.ID, Source: service.EgressSourceGroupBinding}}}
			handler.upstreamJobs = service.NewUpstreamJobService(&videoJobReadRepository{job: job}, nil, nil)
			previous := qianwenVideoContentOpener
			t.Cleanup(func() { qianwenVideoContentOpener = previous })
			downloads := 0
			qianwenVideoContentOpener = func(_ context.Context, restored *service.Account, _ string) (*http.Response, error) {
				downloads++
				route, resolved := restored.ResolvedEgress()
				require.True(t, resolved)
				require.Equal(t, proxy.ID, route.ProxyID)
				require.Equal(t, proxy.URL(), route.URL)
				return &http.Response{Header: make(http.Header), ContentLength: 5, Body: io.NopCloser(strings.NewReader("video"))}, nil
			}
			for _, state := range []string{"pinned", "revoked-node", "revoked-group", "missing-snapshot", "missing-account"} {
				switch state {
				case "revoked-node":
					groups.group.Members = nil
				case "revoked-group":
					groups.group.Status = "disabled"
				case "missing-snapshot":
					job.Billing.Egress = nil
				case "missing-account":
					job.AccountID = 999
				}
				writer := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(writer)
				ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/tasks/public/content", nil)
				ctx.Params = gin.Params{{Key: "request_id", Value: "public"}}
				ctx.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 1, Group: &service.Group{}})
				if native {
					handler.QianwenNative(ctx)
				} else {
					handler.QianwenVideoContent(ctx)
				}
				if state == "pinned" {
					require.Equal(t, 200, writer.Code, writer.Body.String())
				} else {
					require.Equal(t, 502, writer.Code, state)
					require.Contains(t, writer.Body.String(), "resource_content_unavailable")
				}
				require.NotContains(t, writer.Body.String(), "Signature")
				require.NotContains(t, writer.Body.String(), "private-task")
			}
			require.Equal(t, 1, downloads)
		})
	}
}
