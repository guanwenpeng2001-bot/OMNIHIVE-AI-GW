package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWorkBuddyOAuthRouteContract(t *testing.T) {
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{WorkBuddyOAuth: admin.NewWorkBuddyOAuthHandler(nil)}}
	registerWorkBuddyOAuthRoutes(router.Group("/api/v1/admin"), h)
	registerWorkBuddyOAuthRoutes(router.Group("/api/v1/workspaces/:workspace_id"), h)
	paths := map[string]string{}
	for _, route := range router.Routes() {
		paths[route.Path] = route.Method
	}
	for _, prefix := range []string{"/api/v1/admin", "/api/v1/workspaces/:workspace_id"} {
		for _, suffix := range []string{"start", "poll"} {
			require.Equal(t, "POST", paths[prefix+"/workbuddy/oauth/"+suffix])
		}
	}
}

type workBuddyRouteUsers struct{ *provisioningUsers }

func (*workBuddyRouteUsers) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	return nil
}

type workBuddyRouteWorkspaces struct{ service.WorkspaceRepository }

func (*workBuddyRouteWorkspaces) Role(_ context.Context, workspace, user int64, _ bool) (string, error) {
	if user == 1 || (user == 42 && workspace == 2) {
		return "admin", nil
	}
	if user == 42 && workspace == 3 {
		return "member", nil
	}
	return "", service.ErrWorkspaceNotFound
}
func (*workBuddyRouteWorkspaces) ContainsResources(_ context.Context, workspace int64, resource string, ids []int64) (bool, error) {
	return workspace == 2 && resource == "proxies" && len(ids) == 1 && ids[0] == 22, nil
}

type workBuddyOAuthRoundTripper func(*http.Request) (*http.Response, error)

func (f workBuddyOAuthRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWorkBuddyOAuthProductionAuthenticationAndRBAC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hosts := []string{"copilot.tencent.com", "workbuddy-route-fixture.invalid"}
	client, err := httpclient.GetClient(httpclient.Options{Timeout: 20 * time.Second, PrivateHosts: hosts})
	require.NoError(t, err)
	old := client.Transport
	t.Cleanup(func() { client.Transport = old })
	calls := 0
	client.Transport = workBuddyOAuthRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"code":11217,"data":{}}`
		if strings.Contains(r.URL.Path, "/auth/state") {
			body = `{"code":0,"data":{"state":"fixture-state","authUrl":"https://www.workbuddy.cn/login"}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "workbuddy-route-test-signing-secret", ExpireHour: 1}, Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: true, UpstreamHosts: hosts}}}
	users := &provisioningUsers{users: map[int64]*service.User{1: {ID: 1, Role: service.RoleAdmin, Status: service.StatusActive}, 42: {ID: 42, Role: service.RoleUser, Status: service.StatusActive}, 43: {ID: 43, Role: service.RoleUser, Status: service.StatusActive}}}
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	settings := service.NewSettingService(&channelMonitorRouteSettingRepoStub{values: map[string]string{"admin_compliance_acknowledgement:1": `{"version":"` + service.AdminComplianceVersion + `"}`}}, cfg)
	userSvc := service.NewUserService(&workBuddyRouteUsers{users}, nil, nil, nil)
	h := &handler.Handlers{Admin: &handler.AdminHandlers{WorkBuddyOAuth: admin.NewWorkBuddyOAuthHandler(service.NewWorkBuddyOAuthService(cfg, nil))}, Workspace: &handler.WorkspaceHandler{Service: service.NewWorkspaceService(&workBuddyRouteWorkspaces{})}}
	router := gin.New()
	pass := func(c *gin.Context) { c.Next() }
	RegisterAdminRoutes(router.Group("/api/v1"), h, middleware.NewAdminAuthMiddleware(auth, userSvc, settings, nil), middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass), settings, nil)
	RegisterWorkspaceRoutes(router.Group("/api/v1"), h, middleware.NewJWTAuthMiddleware(auth, userSvc, settings, nil), middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass), settings, nil)
	request := func(actor int64, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if actor != 0 {
			token, err := auth.GenerateToken(t.Context(), users.users[actor])
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		name, path, body string
		actor            int64
		status           int
		marker           string
	}{
		{"anonymous admin", "/api/v1/admin", `{}`, 0, 401, "UNAUTHORIZED"},
		{"user admin route", "/api/v1/admin", `{}`, 42, 403, "FORBIDDEN"},
		{"anonymous workspace", "/api/v1/workspaces/2", `{}`, 0, 401, "UNAUTHORIZED"},
		{"workspace member", "/api/v1/workspaces/3", `{}`, 42, 404, "WORKSPACE_NOT_FOUND"},
		{"cross workspace", "/api/v1/workspaces/4", `{}`, 42, 404, "WORKSPACE_NOT_FOUND"},
		{"nonmember", "/api/v1/workspaces/2", `{}`, 43, 404, "WORKSPACE_NOT_FOUND"},
		{"cross workspace proxy", "/api/v1/workspaces/2", `{"site":"cn","proxy_id":99}`, 42, 404, "WORKSPACE_NOT_FOUND"},
		{"admin passes middleware", "/api/v1/admin", `{}`, 1, 200, "session_id"},
		{"workspace admin passes middleware", "/api/v1/workspaces/2", `{}`, 42, 200, "session_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := request(tc.actor, tc.path+"/workbuddy/oauth/start", tc.body, nil)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tc.marker)
		})
	}
	for _, prefix := range []string{"/api/v1/admin", "/api/v1/workspaces/2"} {
		path := prefix + "/workbuddy/oauth/"
		require.Equal(t, http.StatusUnauthorized, request(0, path+"poll", `{"session_id":"unbound"}`, nil).Code)
		start := request(1, path+"start", "{}", nil)
		require.Equal(t, http.StatusOK, start.Code, start.Body.String())
		require.Equal(t, "no-store", start.Header().Get("Cache-Control"))
		cookies := start.Result().Cookies()
		require.Len(t, cookies, 1)
		id := gjson.Get(start.Body.String(), "data.session_id").String()
		require.NotEmpty(t, id)
		body := `{"session_id":"` + id + `"}`
		before := calls
		wrong := *cookies[0]
		wrong.Value = strings.Repeat("b", 64)
		require.Equal(t, http.StatusBadRequest, request(1, path+"poll", body, &wrong).Code)
		if prefix == "/api/v1/workspaces/2" {
			for _, rec := range []*httptest.ResponseRecorder{
				request(1, "/api/v1/workspaces/3/workbuddy/oauth/poll", body, cookies[0]),
				request(42, path+"poll", body, cookies[0]),
			} {
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Contains(t, rec.Body.String(), "login expired")
			}
		}
		require.Equal(t, before, calls)
		valid := request(1, path+"poll", body, cookies[0])
		require.Equal(t, http.StatusOK, valid.Code, valid.Body.String())
		require.Equal(t, "pending", gjson.Get(valid.Body.String(), "data.status").String())
		require.Equal(t, before+1, calls)
	}
}
