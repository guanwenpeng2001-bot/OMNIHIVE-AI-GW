package admin

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
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminUsageExportRepo struct {
	service.UsageLogRepository

	logs          []service.UsageLog
	filters       usagestats.UsageLogFilters
	batches       int
	blockAfter    chan struct{}
	cancelledSeen bool
}

func (s *adminUsageExportRepo) StreamUsageLogsWithFilters(
	ctx context.Context,
	filters usagestats.UsageLogFilters,
	after *usagestats.UsageExportCursor,
	batchSize int,
	yield func([]service.UsageLog) error,
) error {
	s.filters = filters
	if len(s.logs) == 0 {
		return nil
	}
	s.batches++
	if err := yield(s.logs); err != nil {
		return err
	}
	if s.blockAfter != nil {
		select {
		case <-s.blockAfter:
		case <-ctx.Done():
			s.cancelledSeen = true
			return ctx.Err()
		}
	}
	return nil
}

func newAdminUsageExportRouter(repo *adminUsageExportRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageService := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageService, nil, nil, nil)
	router := gin.New()
	router.GET("/admin/usage/export", handler.Export)
	return router
}

func usageExportAdminLogFixture() service.UsageLog {
	userAgent := "Mozilla/5.0"
	return service.UsageLog{
		ID:              1,
		UserID:          9,
		APIKeyID:        5,
		RequestID:       "req-1",
		Model:           "gpt-5.1",
		InputTokens:     10,
		OutputTokens:    2,
		TotalCost:       0.5,
		ActualCost:      0.4,
		RateMultiplier:  1,
		RequestType:     service.RequestTypeStream,
		UserAgent:       &userAgent,
		CreatedAt:       time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC),
		User:            &service.User{ID: 9, Email: "owner@example.com"},
		APIKey:          &service.APIKey{ID: 5, Name: "primary"},
		BillingEvidence: nil,
	}
}

func TestAdminUsageExportStreamsCSVWithFrozenSnapshotRange(t *testing.T) {
	repo := &adminUsageExportRepo{logs: []service.UsageLog{usageExportAdminLogFixture()}}
	router := newAdminUsageExportRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
	require.Contains(t, rec.Header().Get("Content-Disposition"), "usage_2026-09-01_to_")

	snapshotEnd, err := time.Parse(time.RFC3339Nano, rec.Header().Get(usageExportHeaderSnapshotEnd))
	require.NoError(t, err)
	require.NotNil(t, repo.filters.EndTime)
	require.True(t, repo.filters.EndTime.Equal(snapshotEnd), "the stream filters on the frozen snapshot bound")
	require.WithinDuration(t, time.Now(), snapshotEnd, time.Minute, "the snapshot is frozen at request time")
	require.Equal(t, strings.Join(dto.UsageExportColumns(dto.UsageExportScopeAdmin), ","), rec.Header().Get(usageExportHeaderColumns))

	records := readAdminExportCSV(t, rec.Body.String())
	require.Equal(t, dto.UsageExportColumns(dto.UsageExportScopeAdmin), records[0])
	require.Len(t, records, 2)
	require.Equal(t, "owner@example.com", records[1][1])
	require.Equal(t, "0.50000000", records[1][24])
	require.Equal(t, "0.40000000", records[1][25])
}

func TestAdminUsageExportAppliesTheSameFiltersAsTheList(t *testing.T) {
	repo := &adminUsageExportRepo{}
	router := newAdminUsageExportRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?billing_mode=image&user_id=12&group_id=4&request_type=ws_v2&start_date=2026-09-01&end_date=2026-09-02&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image", repo.filters.BillingMode)
	require.Equal(t, int64(12), repo.filters.UserID)
	require.Equal(t, int64(4), repo.filters.GroupID)
	require.NotNil(t, repo.filters.RequestType)
	require.Equal(t, int16(service.RequestTypeWSV2), *repo.filters.RequestType)
}

func TestAdminUsageExportRejectsUnknownBillingMode(t *testing.T) {
	repo := &adminUsageExportRepo{}
	router := newAdminUsageExportRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?billing_mode=definitely-not-a-mode", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid billing_mode")
}

func TestAdminUsageExportEscapesSpreadsheetFormulas(t *testing.T) {
	log := usageExportAdminLogFixture()
	formula := "=cmd|' /C calc'!A0"
	log.UserAgent = &formula
	repo := &adminUsageExportRepo{logs: []service.UsageLog{log}}
	router := newAdminUsageExportRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	records := readAdminExportCSV(t, rec.Body.String())
	require.Equal(t, "'=cmd|' /C calc'!A0", records[1][31])
	require.NotContains(t, rec.Body.String(), "\n=cmd", "a formula never starts a CSV field")
}

func TestAdminUsageExportJSONReturnsTypedRows(t *testing.T) {
	repo := &adminUsageExportRepo{logs: []service.UsageLog{usageExportAdminLogFixture()}}
	router := newAdminUsageExportRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?format=json&start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.Contains(t, rec.Body.String(), `"items"`)
	require.Contains(t, rec.Body.String(), `"total_cost":0.5`)
	require.Contains(t, rec.Body.String(), `"columns"`)
}

func TestAdminUsageExportRejectsUnknownFormat(t *testing.T) {
	repo := &adminUsageExportRepo{}
	router := newAdminUsageExportRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?format=xlsx", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminUsageExportCancellationStopsTheStream(t *testing.T) {
	repo := &adminUsageExportRepo{
		logs:       []service.UsageLog{usageExportAdminLogFixture()},
		blockAfter: make(chan struct{}),
	}
	router := newAdminUsageExportRouter(repo)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	router.ServeHTTP(rec, req)

	require.True(t, repo.cancelledSeen, "a cancelled export releases the read")
	require.Contains(t, rec.Body.String(), dto.UsageExportColumns(dto.UsageExportScopeAdmin)[0])
}

func TestAdminUsageExportEmptyResultStillReturnsTheHeader(t *testing.T) {
	repo := &adminUsageExportRepo{}
	router := newAdminUsageExportRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	records := readAdminExportCSV(t, rec.Body.String())
	require.Len(t, records, 1)
}

func readAdminExportCSV(t *testing.T, content string) [][]string {
	t.Helper()
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\uFEFF")))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	require.NoError(t, err)
	return records
}
