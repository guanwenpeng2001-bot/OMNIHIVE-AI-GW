package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
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

	// streamError 让替身模拟服务端读取失败（例如 USAGE_HISTORY_INCOMPLETE 守卫）。
	// failAfterFirstBatch=true 时失败发生在第一批行已经写进响应之后，用于验证
	// 「流已经开始」时不得把 JSON 错误追回到 CSV 正文里。
	streamError         error
	failAfterFirstBatch bool
}

func (s *adminUsageExportRepo) StreamUsageLogsWithFilters(
	ctx context.Context,
	filters usagestats.UsageLogFilters,
	after *usagestats.UsageExportCursor,
	batchSize int,
	yield func([]service.UsageLog) error,
) error {
	s.filters = filters
	if s.streamError != nil && !s.failAfterFirstBatch {
		return s.streamError
	}
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
	return s.streamError
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

func usageExportHistoryIncompleteError() error {
	return infraerrors.Conflict("USAGE_HISTORY_INCOMPLETE", "Usage history is incomplete: raw history or a verified archive is unavailable").
		WithMetadata(map[string]string{"raw_retained_from": "2026-03-05T00:00:00Z"})
}

// 读取失败必须回到统一的 JSON 错误信封：不能留着 CSV 下载头，让客户端把 JSON 正文当成文件。
func TestAdminUsageExportAnswersReadFailuresWithTheJSONEnvelope(t *testing.T) {
	repo := &adminUsageExportRepo{streamError: usageExportHistoryIncompleteError()}
	router := newAdminUsageExportRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?format=csv&start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json", "失败响应不能声明 CSV 正文")
	require.Empty(t, rec.Header().Get("Content-Disposition"), "失败响应不是附件下载")
	require.Empty(t, rec.Header().Get(usageExportHeaderColumns), "列顺序头描述的是没有发出去的文件")
	require.Empty(t, rec.Header().Get(usageExportHeaderSnapshotEnd))

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, float64(http.StatusConflict), envelope["code"])
	require.Equal(t, "USAGE_HISTORY_INCOMPLETE", envelope["reason"])
	require.Equal(t, map[string]any{"raw_retained_from": "2026-03-05T00:00:00Z"}, envelope["metadata"])
}

// 响应体已经开始写时不能再发 JSON 错误：流按中断处理，已发出的部分保持合法 CSV。
func TestAdminUsageExportAbortsAfterTheStreamStartedInsteadOfAppendingJSON(t *testing.T) {
	repo := &adminUsageExportRepo{
		logs:                []service.UsageLog{usageExportAdminLogFixture()},
		streamError:         usageExportHistoryIncompleteError(),
		failAfterFirstBatch: true,
	}
	router := newAdminUsageExportRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/export?format=csv&start_date=2026-09-01&end_date=2026-09-22&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "第一批行写出后响应状态已经提交")
	require.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
	require.NotContains(t, rec.Body.String(), "USAGE_HISTORY_INCOMPLETE", "JSON 错误不得追回 CSV 正文")
	require.NotContains(t, rec.Body.String(), `"reason"`)

	records := readAdminExportCSV(t, rec.Body.String())
	require.Equal(t, dto.UsageExportColumns(dto.UsageExportScopeAdmin), records[0], "中断前发出的部分仍是合法 CSV")
	require.Len(t, records, 2)
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
