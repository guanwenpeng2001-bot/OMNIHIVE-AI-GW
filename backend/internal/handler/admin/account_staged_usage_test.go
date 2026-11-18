package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stagedUsageRepo struct {
	service.AccountRepository
	account *service.Account
}

func (r *stagedUsageRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}

type stagedUsageSessions struct {
	mu                   sync.Mutex
	session              cursor.Session
	reads, writes, locks atomic.Int32
}

func (c *stagedUsageSessions) Delete(context.Context, string) error { return nil }
func (c *stagedUsageSessions) Get(context.Context, string) (cursor.Session, error) {
	c.reads.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session, nil
}
func (c *stagedUsageSessions) Set(_ context.Context, _ string, session cursor.Session) error {
	c.writes.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = session
	return nil
}
func (c *stagedUsageSessions) Lock(context.Context, string) (func(), bool, error) {
	c.locks.Add(1)
	return func() {}, true, nil
}

type stagedUsageTransport func(*http.Request) (*http.Response, error)

func (f stagedUsageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestStagedInactiveCursorUsageHandlerMakesNoOutboundOrSessionWrites(t *testing.T) {
	for _, dependency := range []string{"required", "unknown", "none", ""} {
		t.Run(dependency, func(t *testing.T) {
			var sends atomic.Int32
			original := http.DefaultTransport
			http.DefaultTransport = stagedUsageTransport(func(request *http.Request) (*http.Response, error) {
				sends.Add(1)
				body := `{}`
				if request.URL.Path == "/auth/exchange_user_api_key" {
					body = `{"accessToken":"synthetic-session","expires_in":3600}`
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = original })
			cache := &stagedUsageSessions{}
			repo := &stagedUsageRepo{account: &service.Account{ID: 42, Platform: service.PlatformCursor, Type: service.AccountTypeAPIKey, Status: "inactive", Credentials: map[string]any{"api_key": "synthetic-usage-key"}, Extra: map[string]any{service.StagedProxyDependencyExtraKey: dependency}}}
			usage := service.ProvideAccountUsageService(service.NewCursorSessionProvider(cache), repo, nil, nil, nil, nil, nil, nil, nil, service.NewUsageCache(), nil, nil, nil)
			handler := &AccountHandler{accountUsageService: usage}
			router := gin.New()
			router.GET("/api/v1/admin/accounts/:id/usage", handler.GetUsage)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/usage?force=true", nil))
			if dependency == "required" || dependency == "unknown" {
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Contains(t, recorder.Body.String(), "STAGED_PROXY_CONFIGURATION_REQUIRED")
				require.Zero(t, sends.Load(), "even ExchangeKey must not be called")
				require.Zero(t, cache.reads.Load())
				require.Zero(t, cache.writes.Load())
				require.Zero(t, cache.locks.Load())
			} else {
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.EqualValues(t, 4, sends.Load(), "existing direct exchange and three usage calls still execute")
				require.EqualValues(t, 1, cache.writes.Load())
			}
			require.NotContains(t, recorder.Body.String(), "synthetic-usage-key")
		})
	}
}
