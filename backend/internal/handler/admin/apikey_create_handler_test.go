package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type adminKeyCreateUsers struct {
	service.UserRepository
	status string
}

func (r *adminKeyCreateUsers) GetByID(_ context.Context, id int64) (*service.User, error) {
	if id != 42 {
		return nil, service.ErrUserNotFound
	}
	return &service.User{ID: id, Status: r.status, Role: service.RoleUser}, nil
}

type adminKeyCreateGroups struct{ service.GroupRepository }

func (*adminKeyCreateGroups) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if id == 7 {
		return &service.Group{ID: id, Status: service.StatusActive}, nil
	}
	if id == 8 {
		return &service.Group{ID: id, Status: "inactive"}, nil
	}
	if id == 9 {
		return &service.Group{ID: id, Status: service.StatusActive, IsExclusive: true}, nil
	}
	return nil, service.ErrGroupNotFound
}

type adminKeyCreateKeys struct {
	service.APIKeyRepository
	keys []service.APIKey
	err  error
}

func (r *adminKeyCreateKeys) Create(ctx context.Context, key *service.APIKey) error {
	if r.err != nil {
		return r.err
	}
	key.ID = int64(100 + len(r.keys))
	key.WorkspaceID = domain.ResourceWorkspaceID(ctx)
	r.keys = append(r.keys, *key)
	return nil
}

func newAdminKeyCreateRouter(users *adminKeyCreateUsers, keys *adminKeyCreateKeys, adminSvc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := service.NewAPIKeyService(keys, users, &adminKeyCreateGroups{}, nil, nil, nil, &config.Config{})
	h := NewAdminAPIKeyHandler(adminSvc, svc)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(domain.WithDefaultWorkspaceID(c.Request.Context(), 1))
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
	})
	r.POST("/api/v1/admin/users/:id/api-keys", middleware.DefaultWorkspaceScope(), h.CreateForUser)
	r.GET("/api/v1/admin/users/:id/api-keys", NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil).GetUserAPIKeys)
	return r
}

func adminKeyCreateRequest(r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Even with a repeated header this endpoint must never replay a stored secret.
	req.Header.Set("Idempotency-Key", "provision-test")
	r.ServeHTTP(rec, req)
	return rec
}

func TestAdminAPIKeyCreateCumoraContractAndSecretVisibility(t *testing.T) {
	keys := &adminKeyCreateKeys{}
	adminSvc := newStubAdminService()
	r := newAdminKeyCreateRouter(&adminKeyCreateUsers{status: service.StatusActive}, keys, adminSvc)
	var previous string
	for i := 0; i < 2; i++ {
		rec := adminKeyCreateRequest(r, "/api/v1/admin/users/42/api-keys", `{"name":"cumora · Alice","group_id":7}`)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		var envelope struct {
			Code    int                        `json:"code"`
			Message string                     `json:"message"`
			Data    map[string]json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
		require.Zero(t, envelope.Code)
		require.Equal(t, "success", envelope.Message)
		require.Len(t, envelope.Data, 2, "only the ID and one-time secret belong in the response")
		var id int64
		var secret string
		require.NoError(t, json.Unmarshal(envelope.Data["id"], &id))
		require.NoError(t, json.Unmarshal(envelope.Data["key"], &secret))
		require.Equal(t, keys.keys[i].ID, id)
		require.Equal(t, keys.keys[i].Key, secret)
		require.Regexp(t, `^sk-[0-9a-f]{64}$`, secret)
		require.NotEqual(t, previous, secret)
		previous = secret
		require.Equal(t, int64(42), keys.keys[i].UserID, "the admin is not the owner")
		require.Equal(t, domain.DefaultWorkspaceID, keys.keys[i].WorkspaceID)
		require.Equal(t, int64(7), *keys.keys[i].GroupID)
		require.Equal(t, "cumora · Alice", keys.keys[i].Name)
	}
	adminSvc.apiKeys = keys.keys
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/42/api-keys", nil))
	// Exercise the existing list handler even when its service mistakenly carries
	// a plaintext field: the standard DTO must remain masked.
	require.Equal(t, http.StatusOK, rec.Code)
	for _, key := range keys.keys {
		require.NotContains(t, rec.Body.String(), key.Key)
	}
	require.Contains(t, rec.Body.String(), `"items":[{`)
}

func TestAdminAPIKeyCreateRejectsInvalidInputAndUnavailableTargets(t *testing.T) {
	for _, tc := range []struct {
		name, id, body, status, reason string
		code                           int
	}{
		{name: "bad id", id: "abc", body: `{"name":"test","group_id":7}`, code: 400},
		{name: "zero id", id: "0", body: `{"name":"test","group_id":7}`, code: 400},
		{name: "negative id", id: "-1", body: `{"name":"test","group_id":7}`, code: 400},
		{name: "overflow id", id: "9223372036854775808", body: `{"name":"test","group_id":7}`, code: 400},
		{name: "bad json", body: `{`, code: 400},
		{name: "trailing json", body: `{"name":"test","group_id":7}{}`, code: 400},
		{name: "trailing garbage", body: `{"name":"test","group_id":7}x`, code: 400},
		{name: "empty name", body: `{"name":"  ","group_id":7}`, code: 400},
		{name: "missing name", body: `{"group_id":7}`, code: 400},
		{name: "wrong group type", body: `{"name":"test","group_id":"7"}`, code: 400},
		{name: "missing group", body: `{"name":"test"}`, code: 400, reason: "API_KEY_GROUP_REQUIRED"},
		{name: "unmapped Cumora tier", body: `{"name":"test","group_id":null}`, code: 400, reason: "API_KEY_GROUP_REQUIRED"},
		{name: "zero group", body: `{"name":"test","group_id":0}`, code: 400, reason: "API_KEY_GROUP_REQUIRED"},
		{name: "negative group", body: `{"name":"test","group_id":-1}`, code: 400, reason: "API_KEY_GROUP_REQUIRED"},
		{name: "missing target", id: "999", body: `{"name":"test","group_id":7}`, code: 404, reason: "USER_NOT_FOUND"},
		{name: "disabled target", status: service.StatusDisabled, body: `{"name":"test","group_id":7}`, code: 403, reason: "USER_NOT_ACTIVE"},
		{name: "missing group", body: `{"name":"test","group_id":999}`, code: 404, reason: "GROUP_NOT_FOUND"},
		{name: "inactive group", body: `{"name":"test","group_id":8}`, code: 400, reason: "GROUP_NOT_ACTIVE"},
		{name: "ungranted group", body: `{"name":"test","group_id":9}`, code: 403, reason: "GROUP_NOT_ALLOWED"},
		{name: "workspace override", body: `{"name":"test","group_id":7,"workspace_id":2}`, code: 400},
		{name: "owner override", body: `{"name":"test","group_id":7,"user_id":1}`, code: 400},
		{name: "custom secret", body: `{"name":"test","group_id":7,"custom_key":"do-not-echo-this-secret"}`, code: 400},
		{name: "quota override", body: `{"name":"test","group_id":7,"quota":999}`, code: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.id == "" {
				tc.id = "42"
			}
			if tc.status == "" {
				tc.status = service.StatusActive
			}
			keys := &adminKeyCreateKeys{}
			r := newAdminKeyCreateRouter(&adminKeyCreateUsers{status: tc.status}, keys, nil)
			rec := adminKeyCreateRequest(r, "/api/v1/admin/users/"+tc.id+"/api-keys", tc.body)
			require.Equal(t, tc.code, rec.Code)
			require.Empty(t, keys.keys, "invalid requests must not mint credentials")
			require.NotContains(t, rec.Body.String(), "do-not-echo-this-secret")
			require.NotContains(t, rec.Body.String(), `"key":`)
			var envelope struct {
				Code   int
				Reason string
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
			require.Equal(t, tc.code, envelope.Code)
			if tc.reason != "" {
				require.Equal(t, tc.reason, envelope.Reason)
			}
		})
	}
}

func TestAdminAPIKeyCreateStorageFailureDoesNotReturnSecret(t *testing.T) {
	keys := &adminKeyCreateKeys{err: errors.New("storage unavailable")}
	r := newAdminKeyCreateRouter(&adminKeyCreateUsers{status: service.StatusActive}, keys, nil)
	rec := adminKeyCreateRequest(r, "/api/v1/admin/users/42/api-keys", `{"name":"test","group_id":7}`)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), `"key":`)
	require.Empty(t, keys.keys)
}
