//go:build integration

package restore_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	control "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/restore"
	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/restore"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/ops/restore/testfixture"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestRestoreStartupWithUnreachableIsolatedTarget(t *testing.T) {
	journal, journalCfg := testfixture.Database(t, "UPSTREAM_JOB_TEST_DSN", "job_test")
	_, ent, _ := testutil.NewCapabilityPostgres(t)

	// Accept and close connections so the isolated target returns EOF.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	isolated := url.URL{
		Scheme: "postgresql", User: url.User("fixture"), Host: listener.Addr().String(),
		Path: "/unused", RawQuery: "sslmode=disable",
	}
	t.Setenv("OMNIHIVE_RESTORE_JOURNAL_DSN", journalCfg.Database.DSN())
	t.Setenv("OMNIHIVE_RESTORE_ISOLATED_DSN", isolated.String())
	db, err := repository.OpenApplicationDB(os.Getenv("UPSTREAM_JOB_TEST_DSN"), false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfg := &config.Config{}
	backup := service.NewBackupService(repository.NewSettingRepository(ent), cfg, nil, nil, nil)
	backup.SetLeaderLock(nil, db)
	runtime, err := legacy.New(db, cfg, backup, nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	require.Nil(t, runtime.Journal, "restore worker must stay disabled")
	status, err := backup.GetScheduleStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, service.RestoreRuntimeStatus{Status: "unavailable", Reason: "ISOLATED_TARGET_UNAVAILABLE"}, status.Restore)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/ordinary", func(c *gin.Context) { c.Status(http.StatusOK) })
	control.Register(router.Group("/api/v2"), control.New(runtime), func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	})
	handler := runtime.Maintenance(router)
	call := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader("{}")))
		return rec
	}
	require.Equal(t, http.StatusOK, call(http.MethodGet, "/ordinary").Code)
	for _, req := range []struct{ method, path string }{
		{http.MethodPost, "/api/v2/admin/restores/previews"},
		{http.MethodPost, "/api/v2/admin/restores"},
		{http.MethodGet, "/api/v2/admin/restores/fixture"},
	} {
		rec := call(req.method, req.path)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "RESTORE_UNAVAILABLE")
	}
	_, err = backup.StartAuthorizedRestore(t.Context(), 42, "fixture", "fixture", "")
	require.Equal(t, "RESTORE_UNAVAILABLE", errors.Reason(err))

	// Degraded restore initialization still honors an existing production fence.
	tx, err := journal.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), "SELECT pg_advisory_xact_lock(298,3)")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, call(http.MethodGet, "/health").Code)
	rec := call(http.MethodGet, "/ordinary")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "RESTORE_MAINTENANCE")
}
