package handler

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/usagestats"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type userUsageExportRepo struct {
	service.UsageLogRepository

	logs    []service.UsageLog
	filters usagestats.UsageLogFilters
	calls   int

	// streamError 让替身模拟服务端读取失败（例如 USAGE_HISTORY_INCOMPLETE 守卫）。
	streamError         error
	failAfterFirstBatch bool
	blockAfter          chan struct{}
}

func (s *userUsageExportRepo) StreamUsageLogsWithFilters(
	ctx context.Context,
	filters usagestats.UsageLogFilters,
	_ *usagestats.UsageExportCursor,
	_ int,
	yield func([]service.UsageLog) error,
) error {
	s.calls++
	s.filters = filters
	if s.streamError != nil && !s.failAfterFirstBatch {
		return s.streamError
	}
	if len(s.logs) == 0 {
		return nil
	}
	if err := yield(s.logs); err != nil {
		return err
	}
	if s.blockAfter != nil {
		select {
		case <-s.blockAfter:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.streamError
}

func newUserUsageExportRouter(repo *userUsageExportRepo, userID int64, extraMiddleware ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageService := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageService, nil, nil, nil)
	router := gin.New()
	router.Use(middleware.Recovery(), middleware.RequestLogger(),
		middleware.SessionBindingContext(&config.Config{}), middleware.Logger(), middleware.CORS(config.CORSConfig{}),
		middleware.SecurityHeaders(config.CSPConfig{}, nil), middleware.ServerTiming(true))
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		c.Next()
	})
	router.Use(extraMiddleware...)
	router.GET("/usage/export", handler.Export)
	router.GET("/api/v1/usage/export", handler.Export)
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
	responseModel := "declared-model"
	repo := &userUsageExportRepo{logs: []service.UsageLog{{
		ID:                    3,
		UserID:                42,
		APIKeyID:              5,
		RequestID:             "req-9",
		Model:                 "gpt-5.1",
		UpstreamResponseModel: &responseModel,
		InputTokens:           11,
		OutputTokens:          4,
		TotalCost:             0.25,
		ActualCost:            0.2,
		RateMultiplier:        1,
		RequestType:           service.RequestTypeStream,
		UserAgent:             &userAgent,
		CreatedAt:             time.Date(2026, 9, 22, 3, 0, 0, 0, time.UTC),
		APIKey:                &service.APIKey{ID: 5, Name: "primary"},
		BillingEvidence:       nil,
	}}}
	router := newUserUsageExportRouter(repo, 42)

	req := httptest.NewRequest(http.MethodGet, "/usage/export?user_id=7&start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
	require.Equal(t, strings.Join(dto.UsageExportColumns(dto.UsageExportScopeSelf), ","), rec.Header().Get("X-Usage-Export-Columns"))

	records := readUserExportCSV(t, rec.Body.String())
	require.Equal(t, dto.UsageExportColumns(dto.UsageExportScopeSelf), records[0])
	require.Len(t, records, 2)
	require.Equal(t, "primary", records[1][1])
	require.Equal(t, int64(42), repo.filters.UserID)
	require.Equal(t, "declared-model", records[1][3])
	require.Equal(t, "0.20000000", records[1][14])
	require.Equal(t, "0.25000000", records[1][15])
	require.NotContains(t, rec.Body.String(), "owner@example.com")
	require.NotContains(t, rec.Body.String(), "account_billed_cost")
}

// 读取失败必须回到统一的 JSON 错误信封：不能留着 CSV 下载头，让客户端把 JSON 正文当成文件（409 里的精确原因靠它传达）。
func TestUserUsageExportAnswersReadFailuresWithTheJSONEnvelope(t *testing.T) {
	repo := &userUsageExportRepo{streamError: infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable").
		WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})}
	router := newUserUsageExportRouter(repo, 42)

	req := httptest.NewRequest(http.MethodGet, "/usage/export?format=csv&start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json", "失败响应不能声明 CSV 正文")
	require.Empty(t, rec.Header().Get("Content-Disposition"), "失败响应不是附件下载")
	require.Empty(t, rec.Header().Get("X-Usage-Export-Columns"), "列顺序头描述的是没有发出去的文件")
	require.Empty(t, rec.Header().Get("X-Usage-Export-Snapshot-End"))

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, float64(http.StatusConflict), envelope["code"])
	require.Equal(t, "USAGE_HISTORY_INCOMPLETE", envelope["reason"])
	require.Equal(t, map[string]any{"raw_retained_from": "2026-03-05T00:00:00Z"}, envelope["metadata"])
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
	router.Use(middleware.Recovery(), middleware.RequestLogger(),
		middleware.SessionBindingContext(&config.Config{}), middleware.Logger(), middleware.CORS(config.CORSConfig{}),
		middleware.SecurityHeaders(config.CSPConfig{}, nil), middleware.ServerTiming(true))
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
