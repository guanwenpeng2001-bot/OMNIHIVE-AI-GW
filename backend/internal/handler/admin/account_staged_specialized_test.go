package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type stagedSpecializedAdmin struct {
	service.AdminService
	account                        *service.Account
	writes, privacy, invalidations atomic.Int32
}

func (s *stagedSpecializedAdmin) GetAccount(context.Context, int64) (*service.Account, error) {
	return s.account, nil
}
func (s *stagedSpecializedAdmin) UpdateAccount(context.Context, int64, *service.UpdateAccountInput) (*service.Account, error) {
	s.writes.Add(1)
	return s.account, nil
}
func (s *stagedSpecializedAdmin) EnsureOpenAIPrivacy(context.Context, *service.Account) string {
	s.privacy.Add(1)
	return ""
}
func (s *stagedSpecializedAdmin) EnsureAntigravityPrivacy(context.Context, *service.Account) string {
	s.privacy.Add(1)
	return ""
}
func (s *stagedSpecializedAdmin) InvalidateToken(context.Context, *service.Account) error {
	s.invalidations.Add(1)
	return nil
}

func TestStagedSpecializedRefreshHandlersDoNotUseCredentials(t *testing.T) {
	for _, dependency := range []string{"required", "unknown"} {
		for _, platform := range []string{service.PlatformGemini, service.PlatformAntigravity, service.PlatformGrok, service.PlatformOpenAI, service.PlatformAnthropic, "grok-dedicated"} {
			t.Run(dependency+"/"+platform, func(t *testing.T) {
				actualPlatform := platform
				if platform == "grok-dedicated" {
					actualPlatform = service.PlatformGrok
				}
				admin := &stagedSpecializedAdmin{account: &service.Account{ID: 47, Platform: actualPlatform, Type: service.AccountTypeOAuth, Status: "inactive", Credentials: map[string]any{"access_token": "synthetic-access", "refresh_token": "synthetic-refresh"}, Extra: map[string]any{service.StagedProxyDependencyExtraKey: dependency}}}
				var sends atomic.Int32
				previous := http.DefaultTransport
				http.DefaultTransport = stagedUsageTransport(func(*http.Request) (*http.Response, error) {
					sends.Add(1)
					return nil, errors.New("unexpected fixture transport")
				})
				t.Cleanup(func() { http.DefaultTransport = previous })
				router := gin.New()
				if platform == "grok-dedicated" {
					h := &GrokOAuthHandler{adminService: admin}
					router.POST("/api/v1/admin/accounts/:id/refresh", h.RefreshAccountToken)
				} else {
					h := &AccountHandler{adminService: admin, tokenCacheInvalidator: admin}
					router.POST("/api/v1/admin/accounts/:id/refresh", h.Refresh)
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/47/refresh", nil))
				require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "STAGED_PROXY_CONFIGURATION_REQUIRED")
				require.Zero(t, sends.Load())
				require.Zero(t, admin.writes.Load())
				require.Zero(t, admin.privacy.Load())
				require.Zero(t, admin.invalidations.Load())
				require.NotContains(t, recorder.Body.String(), "synthetic-refresh")
			})
		}
	}
}

type stagedSpecializedCache struct{ calls atomic.Int32 }

func (s *stagedSpecializedCache) GetAccessToken(context.Context, string) (string, error) {
	s.calls.Add(1)
	return "synthetic-cached-token", nil
}
func (s *stagedSpecializedCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	s.calls.Add(1)
	return nil
}
func (s *stagedSpecializedCache) DeleteAccessToken(context.Context, string) error {
	s.calls.Add(1)
	return nil
}
func (s *stagedSpecializedCache) AcquireRefreshLock(context.Context, string, string, time.Duration) (bool, error) {
	s.calls.Add(1)
	return true, nil
}
func (s *stagedSpecializedCache) ReleaseRefreshLock(context.Context, string, string) error {
	s.calls.Add(1)
	return nil
}

type stagedSpecializedRepo struct {
	service.AccountRepository
	account *service.Account
	writes  atomic.Int32
}

func (s *stagedSpecializedRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return s.account, nil
}
func (s *stagedSpecializedRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	s.writes.Add(1)
	return nil
}

func TestStagedOpenAIQuotaHandlersRejectBeforeTokenCacheOrClient(t *testing.T) {
	for _, dependency := range []string{"required", "unknown"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(dependency+method, func(t *testing.T) {
				cache := &stagedSpecializedCache{}
				repo := &stagedSpecializedRepo{account: &service.Account{ID: 47, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: "inactive", Credentials: map[string]any{"access_token": "synthetic-access", "chatgpt_account_id": "synthetic-tenant"}, Extra: map[string]any{service.StagedProxyDependencyExtraKey: dependency}}}
				var clients atomic.Int32
				quota := service.NewOpenAIQuotaService(repo, nil, service.NewOpenAITokenProvider(repo, cache, nil), func(string) (*req.Client, error) {
					clients.Add(1)
					return nil, errors.New("fixture forbids client creation")
				})
				h := &OpenAIOAuthHandler{quotaService: quota}
				router := gin.New()
				router.GET("/api/v1/admin/openai/accounts/:id/quota", h.QueryQuota)
				router.POST("/api/v1/admin/openai/accounts/:id/quota", h.RefreshQuota)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(method, "/api/v1/admin/openai/accounts/47/quota", nil))
				require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "STAGED_PROXY_CONFIGURATION_REQUIRED")
				require.Zero(t, clients.Load())
				require.Zero(t, cache.calls.Load())
				require.Zero(t, repo.writes.Load())
			})
		}
	}
}
