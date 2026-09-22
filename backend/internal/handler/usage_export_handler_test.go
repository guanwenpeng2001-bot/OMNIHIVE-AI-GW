package handler

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userUsageExportRepo struct {
	service.UsageLogRepository

	logs    []service.UsageLog
	filters usagestats.UsageLogFilters
	calls   int
}

func (s *userUsageExportRepo) StreamUsageLogsWithFilters(
	_ context.Context,
	filters usagestats.UsageLogFilters,
	_ *usagestats.UsageExportCursor,
	_ int,
	yield func([]service.UsageLog) error,
) error {
	s.calls++
	s.filters = filters
	if len(s.logs) == 0 {
		return nil
	}
	return yield(s.logs)
}

func newUserUsageExportRouter(repo *userUsageExportRepo, userID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageService := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageService, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		c.Next()
	})
	router.GET("/usage/export", handler.Export)
	return router
}

func TestUserUsageExportScopesToTheAuthenticatedOwner(t *testing.T) {
	repo := &userUsageExportRepo{}
	router := newUserUsageExportRouter(repo, 42)

	req := httptest.NewRequest(http.MethodGet, "/usage/export?user_id=7&account_id=9&start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, repo.calls, "the browser issues exactly one export request")
	require.Equal(t, int64(42), repo.filters.UserID, "the export is scoped to the authenticated subject")
	require.Equal(t, int64(0), repo.filters.AccountID, "owner exports cannot filter by account")
	require.NotNil(t, repo.filters.EndTime)
}

func TestUserUsageExportStreamsOwnerColumnsOnly(t *testing.T) {
	userAgent := "Mozilla/5.0"
	repo := &userUsageExportRepo{logs: []service.UsageLog{{
		ID:              3,
		UserID:          42,
		APIKeyID:        5,
		RequestID:       "req-9",
		Model:           "gpt-5.1",
		InputTokens:     11,
		OutputTokens:    4,
		TotalCost:       0.25,
		ActualCost:      0.2,
		RateMultiplier:  1,
		RequestType:     service.RequestTypeStream,
		UserAgent:       &userAgent,
		CreatedAt:       time.Date(2026, 9, 22, 3, 0, 0, 0, time.UTC),
		APIKey:          &service.APIKey{ID: 5, Name: "primary"},
		BillingEvidence: nil,
	}}}
	router := newUserUsageExportRouter(repo, 42)

	req := httptest.NewRequest(http.MethodGet, "/usage/export?start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
	require.Equal(t, strings.Join(dto.UsageExportColumns(dto.UsageExportScopeSelf), ","), rec.Header().Get("X-Usage-Export-Columns"))

	records := readUserExportCSV(t, rec.Body.String())
	require.Equal(t, dto.UsageExportColumns(dto.UsageExportScopeSelf), records[0])
	require.Len(t, records, 2)
	require.Equal(t, "primary", records[1][1])
	require.Equal(t, "0.20000000", records[1][13])
	require.Equal(t, "0.25000000", records[1][14])
	require.NotContains(t, rec.Body.String(), "owner@example.com")
	require.NotContains(t, rec.Body.String(), "account_billed_cost")
}

func TestUserUsageExportRejectsUnknownFormat(t *testing.T) {
	repo := &userUsageExportRepo{}
	router := newUserUsageExportRouter(repo, 42)

	req := httptest.NewRequest(http.MethodGet, "/usage/export?format=xlsx", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, 0, repo.calls)
}

func TestUserUsageExportRequiresAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userUsageExportRepo{}
	usageService := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageService, nil, nil, nil)
	router := gin.New()
	router.GET("/usage/export", handler.Export)

	req := httptest.NewRequest(http.MethodGet, "/usage/export", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, repo.calls)
}

func readUserExportCSV(t *testing.T, content string) [][]string {
	t.Helper()
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\uFEFF")))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	require.NoError(t, err)
	return records
}
