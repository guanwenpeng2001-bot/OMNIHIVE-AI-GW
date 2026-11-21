package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

const provisioningAdminKey = "test-only-admin-provisioning-key"

type provisioningUsers struct {
	service.UserRepository
	users map[int64]*service.User
}

func (r *provisioningUsers) GetByID(_ context.Context, id int64) (*service.User, error) {
	if user := r.users[id]; user != nil {
		return user, nil
	}
	return nil, service.ErrUserNotFound
}

func (r *provisioningUsers) GetFirstAdmin(context.Context) (*service.User, error) {
	return r.users[1], nil
}

func (*provisioningUsers) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

type provisioningGroups struct {
	service.GroupRepository
	group service.Group
}

func (r *provisioningGroups) GetByID(ctx context.Context, id int64) (*service.Group, error) {
	workspace, scoped := domain.WorkspaceScope(ctx)
	if !scoped || workspace != domain.DefaultWorkspaceID || id != r.group.ID {
		return nil, service.ErrGroupNotFound
	}
	return &r.group, nil
}

type provisioningSubscriptions struct {
	service.UserSubscriptionRepository
	sub *service.UserSubscription
}

func (r *provisioningSubscriptions) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	if r.sub != nil && r.sub.UserID == userID && r.sub.GroupID == groupID && r.sub.IsActive() {
		return r.sub, nil
	}
	return nil, service.ErrSubscriptionNotFound
}

type provisioningKeys struct {
	service.APIKeyRepository
	key   *service.APIKey
	user  *service.User
	group *service.Group
	calls int
}

func (r *provisioningKeys) Create(ctx context.Context, key *service.APIKey) error {
	r.calls++
	key.ID = int64(100 + r.calls)
	key.WorkspaceID = domain.ResourceWorkspaceID(ctx)
	r.key = key
	return nil
}

func (r *provisioningKeys) GetByKeyForAuth(_ context.Context, key string) (*service.APIKey, error) {
	if r.key == nil || (key != r.key.Key && key != service.APIKeyCredentialDigest(r.key.Key)) {
		return nil, service.ErrAPIKeyNotFound
	}
	copy := *r.key
	copy.User, copy.Group = r.user, r.group
	return &copy, nil
}

type provisioningAuditRepo struct {
	service.AuditLogRepository
	logs []*service.AuditLog
}

func (r *provisioningAuditRepo) BatchInsert(_ context.Context, logs []*service.AuditLog) (int64, error) {
	r.logs = append(r.logs, logs...)
	return int64(len(logs)), nil
}

type provisioningFixture struct {
	router    *gin.Engine
	users     *provisioningUsers
	groups    *provisioningGroups
	subs      *provisioningSubscriptions
	keys      *provisioningKeys
	settings  *channelMonitorRouteSettingRepoStub
	auth      *service.AuthService
	audit     *service.AuditLogService
	auditRepo *provisioningAuditRepo
}

func newProvisioningFixture(t *testing.T) *provisioningFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-only-provisioning-jwt-signing-secret", ExpireHour: 1}}
	f := &provisioningFixture{
		router: gin.New(),
		users: &provisioningUsers{users: map[int64]*service.User{
			1:  {ID: 1, Role: service.RoleAdmin, Status: service.StatusActive},
			2:  {ID: 2, Role: service.RoleAdmin, Status: service.StatusDisabled},
			42: {ID: 42, Role: service.RoleUser, Status: service.StatusActive},
		}},
		groups: &provisioningGroups{group: service.Group{ID: 7, Status: service.StatusActive}},
		subs:   &provisioningSubscriptions{},
		settings: &channelMonitorRouteSettingRepoStub{values: map[string]string{
			service.SettingKeyAdminAPIKey:        provisioningAdminKey,
			"admin_compliance_acknowledgement:1": `{"version":"` + service.AdminComplianceVersion + `"}`,
		}},
		auditRepo: &provisioningAuditRepo{},
	}
	f.keys = &provisioningKeys{user: f.users.users[42], group: &f.groups.group}
	keyService := service.NewAPIKeyService(f.keys, f.users, f.groups, f.subs, nil, nil, cfg)
	settings := service.NewSettingService(f.settings, cfg)
	f.auth = service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	f.audit = service.NewAuditLogService(f.auditRepo, nil)
	f.audit.Start()
	t.Cleanup(f.audit.Stop)
	adminAuth := middleware.NewAdminAuthMiddleware(f.auth, service.NewUserService(f.users, nil, nil, nil), settings, f.audit)
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{APIKey: admin.NewAdminAPIKeyHandler(nil, keyService)}}
	RegisterAdminRoutes(f.router.Group("/api/v1"), handlers, adminAuth, middleware.NewAuditLogMiddleware(f.audit),
		middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), settings, nil)
	// Use the actual gateway authentication and billing gates on a harmless local
	// handler; no upstream request is made during this test.
	groupSubscriptions := service.NewSubscriptionService(f.groups, f.subs, nil, nil, cfg)
	f.router.POST("/v1/messages", gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keyService, groupSubscriptions, cfg)), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return f
}

func (f *provisioningFixture) request(key, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/42/api-keys", strings.NewReader(`{"name":"cumora · Alice","group_id":7}`))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("x-api-key", key)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestAdminAPIKeyCreateProductionRouteAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		actor     int64
		code      int
	}{
		{name: "unauthenticated", code: 401},
		{name: "wrong admin key", key: "invalid-key", code: 401},
		{name: "regular user JWT", actor: 42, code: 403},
		{name: "disabled admin JWT", actor: 2, code: 401},
		{name: "admin API key", key: provisioningAdminKey, code: 200},
		{name: "admin JWT", actor: 1, code: 200},
		{name: "invalid API key cannot fall back to admin JWT", key: "invalid-key", actor: 1, code: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newProvisioningFixture(t)
			var token string
			if tc.actor > 0 {
				var err error
				token, err = f.auth.GenerateToken(context.Background(), f.users.users[tc.actor])
				require.NoError(t, err)
			}
			rec := f.request(tc.key, token)
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
			if tc.code != 200 {
				require.Zero(t, f.keys.calls)
				return
			}
			require.Equal(t, 1, f.keys.calls)
			require.Equal(t, int64(42), f.keys.key.UserID)
			require.Equal(t, domain.DefaultWorkspaceID, f.keys.key.WorkspaceID)
			f.audit.Stop() // flush queued audit records before inspecting them
			require.Len(t, f.auditRepo.logs, 1)
			require.Equal(t, int64(1), *f.auditRepo.logs[0].ActorUserID)
			logged, err := json.Marshal(f.auditRepo.logs)
			require.NoError(t, err)
			require.NotContains(t, string(logged), f.keys.key.Key, "the one-time secret must not enter the audit log")
			require.NotContains(t, string(logged), provisioningAdminKey)
		})
	}
}

func TestAdminAPIKeyCreateKeepsComplianceAndGatewayGates(t *testing.T) {
	t.Run("compliance acknowledgement is still required", func(t *testing.T) {
		f := newProvisioningFixture(t)
		delete(f.settings.values, "admin_compliance_acknowledgement:1")
		rec := f.request(provisioningAdminKey, "")
		require.Equal(t, http.StatusLocked, rec.Code)
		require.Contains(t, rec.Body.String(), "ADMIN_COMPLIANCE_ACK_REQUIRED")
		require.Zero(t, f.keys.calls)
	})
	for _, subscription := range []bool{false, true} {
		t.Run(map[bool]string{false: "zero balance", true: "subscription revoked after mint"}[subscription], func(t *testing.T) {
			f := newProvisioningFixture(t)
			if subscription {
				f.groups.group.SubscriptionType = "subscription"
				f.subs.sub = &service.UserSubscription{UserID: 42, GroupID: 7, Status: service.SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}
			}
			require.Equal(t, http.StatusOK, f.request(provisioningAdminKey, "").Code)
			require.Equal(t, http.StatusUnauthorized, f.request(f.keys.key.Key, "").Code, "a minted user credential cannot authorize an admin request")
			require.Equal(t, 1, f.keys.calls)
			f.subs.sub = nil
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			req.Header.Set("Authorization", "Bearer "+f.keys.key.Key)
			rec := httptest.NewRecorder()
			f.router.ServeHTTP(rec, req)
			if subscription {
				require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), "SUBSCRIPTION_NOT_FOUND")
			} else {
				// Gateway protocol normalization reports exhausted balances as 429.
				require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), "INSUFFICIENT_BALANCE")
			}
		})
	}
}

func TestAdminAPIKeyCreateIsNotDelegatedToWorkspaceAdmins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	pass := func(c *gin.Context) { c.Next() }
	RegisterWorkspaceRoutes(r.Group("/api/v1"), &handler.Handlers{Workspace: &handler.WorkspaceHandler{}, Admin: &handler.AdminHandlers{}},
		middleware.JWTAuthMiddleware(pass), middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass), nil, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/2/users/42/api-keys", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}
