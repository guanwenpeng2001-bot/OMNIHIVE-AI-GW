//go:build unit

package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type todayStatsCacheRepository struct {
	service.UsageLogRepository
	mutex         sync.Mutex
	revision      string
	revisionError error
	revisionCalls int
	batchCalls    int
	starts        []time.Time
	ids           []int64
	stats         usagestats.AccountStats
	batchError    error
	fallbackCalls int
	revisionHook  func()
	batchHook     func()
}

func (repo *todayStatsCacheRepository) DashboardCoverageRevision(context.Context) (string, error) {
	repo.mutex.Lock()
	repo.revisionCalls++
	revision, revisionError, hook := repo.revision, repo.revisionError, repo.revisionHook
	repo.mutex.Unlock()
	if hook != nil {
		hook()
	}
	return revision, revisionError
}

func (repo *todayStatsCacheRepository) GetAccountWindowStatsBatch(_ context.Context, accountIDs []int64, start time.Time) (map[int64]*usagestats.AccountStats, error) {
	repo.mutex.Lock()
	repo.batchCalls++
	repo.starts = append(repo.starts, start)
	repo.ids = append([]int64(nil), accountIDs...)
	stats, failure, hook := repo.stats, repo.batchError, repo.batchHook
	repo.mutex.Unlock()
	if hook != nil {
		hook()
	}
	result := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	for _, accountID := range accountIDs {
		result[accountID] = &stats
	}
	return result, failure
}

func (repo *todayStatsCacheRepository) GetAccountWindowStats(_ context.Context, _ int64, start time.Time) (*usagestats.AccountStats, error) {
	repo.mutex.Lock()
	defer repo.mutex.Unlock()
	repo.fallbackCalls++
	repo.starts = append(repo.starts, start)
	return nil, errors.New("existing fallback failure")
}

func todayStatsCacheRouter(test *testing.T, repo *todayStatsCacheRepository) *gin.Engine {
	test.Helper()
	original := accountTodayStatsBatchCache
	accountTodayStatsBatchCache = newSnapshotCache(30 * time.Second)
	test.Cleanup(func() { accountTodayStatsBatchCache = original })
	handler := &AccountHandler{accountUsageService: service.NewAccountUsageService(nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil)}
	router := gin.New()
	router.POST("/today", handler.GetBatchTodayStats)
	return router
}

func requestTodayStats(router http.Handler, body, etag string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/today", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-None-Match", etag)
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestTodayStatsCoverageRevision(t *testing.T) {
	t.Run("normalized accounts revision and conditional response", func(test *testing.T) {
		repo := &todayStatsCacheRepository{revision: "before", stats: usagestats.AccountStats{Requests: 7, Tokens: 42, Cost: 1.25, StandardCost: 2.5, UserCost: 3.75}}
		router := todayStatsCacheRouter(test, repo)
		first := requestTodayStats(router, `{"account_ids":[2,1,2,0,-1]}`, "")
		require.Equal(test, http.StatusOK, first.Code)
		require.Equal(test, "miss", first.Header().Get("X-Snapshot-Cache"))
		require.Equal(test, []int64{1, 2}, repo.ids)
		require.JSONEq(test, `{"code":0,"message":"success","data":{"stats":{"1":{"requests":7,"tokens":42,"cost":1.25,"standard_cost":2.5,"user_cost":3.75},"2":{"requests":7,"tokens":42,"cost":1.25,"standard_cost":2.5,"user_cost":3.75}}}}`, first.Body.String())
		etag := first.Header().Get("ETag")
		require.NotEmpty(test, etag)
		hit := requestTodayStats(router, `{"account_ids":[1,2]}`, "")
		require.Equal(test, "hit", hit.Header().Get("X-Snapshot-Cache"))
		require.Equal(test, first.Body.String(), hit.Body.String())
		conditional := requestTodayStats(router, `{"account_ids":[1,2]}`, etag)
		require.Equal(test, http.StatusNotModified, conditional.Code)
		require.Empty(test, conditional.Body.String())
		require.Equal(test, "If-None-Match", conditional.Header().Get("Vary"))
		require.Equal(test, 1, repo.batchCalls)

		repo.revision = "after"
		repo.stats.Cost = 4.5
		changed := requestTodayStats(router, `{"account_ids":[1,2]}`, etag)
		require.Equal(test, http.StatusOK, changed.Code)
		require.Equal(test, "miss", changed.Header().Get("X-Snapshot-Cache"))
		require.NotEqual(test, etag, changed.Header().Get("ETag"))
		require.Contains(test, changed.Body.String(), `"cost":4.5`)
		require.Equal(test, 2, repo.batchCalls)
		require.Equal(test, 4, repo.revisionCalls)
		// A fresh load keeps the existing 200-on-miss contract even for identical data.
		repo.revision = "next"
		unchanged := requestTodayStats(router, `{"account_ids":[1,2]}`, changed.Header().Get("ETag"))
		require.Equal(test, http.StatusOK, unchanged.Code)
		require.Equal(test, changed.Header().Get("ETag"), unchanged.Header().Get("ETag"))
	})

	t.Run("revision failure cannot serve warm snapshot", func(test *testing.T) {
		repo := &todayStatsCacheRepository{revision: "before"}
		router := todayStatsCacheRouter(test, repo)
		first := requestTodayStats(router, `{"account_ids":[1]}`, "")
		repo.revisionError = errors.New("coverage unavailable")
		for attempt := 0; attempt < 2; attempt++ {
			failed := requestTodayStats(router, `{"account_ids":[1]}`, first.Header().Get("ETag"))
			require.Equal(test, http.StatusInternalServerError, failed.Code)
			require.Empty(test, failed.Header().Get("ETag"))
			require.Empty(test, failed.Header().Get("X-Snapshot-Cache"))
		}
		require.Equal(test, 1, repo.batchCalls)
		repo.revision, repo.revisionError = "after", nil
		recovered := requestTodayStats(router, `{"account_ids":[1]}`, "")
		require.Equal(test, http.StatusOK, recovered.Code)
		require.Equal(test, "miss", recovered.Header().Get("X-Snapshot-Cache"))
		require.Equal(test, 2, repo.batchCalls)
	})

	t.Run("empty accounts bypass cache and revision", func(test *testing.T) {
		repo := &todayStatsCacheRepository{revisionError: errors.New("unavailable")}
		router := todayStatsCacheRouter(test, repo)
		for _, body := range []string{`{"account_ids":[]}`, `{"account_ids":[0,-1]}`} {
			empty := requestTodayStats(router, body, `"old"`)
			require.Equal(test, http.StatusOK, empty.Code)
			require.JSONEq(test, `{"code":0,"message":"success","data":{"stats":{}}}`, empty.Body.String())
			require.Empty(test, empty.Header().Get("ETag"))
		}
		require.Zero(test, repo.revisionCalls)
		require.Zero(test, repo.batchCalls)
		require.Empty(test, accountTodayStatsBatchCache.items)
	})

	t.Run("day timezone and DST boundaries", func(test *testing.T) {
		location, err := time.LoadLocation("America/New_York")
		require.NoError(test, err)
		dayStart := time.Date(2026, 3, 8, 0, 0, 0, 0, location)
		nextDay := dayStart.AddDate(0, 0, 1)
		require.Equal(test, 23*time.Hour, nextDay.Sub(dayStart))
		before := buildAccountTodayStatsBatchCacheKey([]int64{1}, dayStart, "same")
		for _, boundary := range []time.Time{nextDay, dayStart.In(time.UTC), dayStart.In(time.FixedZone("different zone", -5*60*60))} {
			after := buildAccountTodayStatsBatchCacheKey([]int64{1}, boundary, "same")
			require.NotEqual(test, before, after)
			cache := newSnapshotCache(30 * time.Second)
			cache.Set(before, "previous day")
			_, hit, err := cache.GetOrLoad(after, func() (any, error) { return "current day", nil })
			require.NoError(test, err)
			require.False(test, hit)
		}
	})

	t.Run("query uses captured day even if timezone changes during revision read", func(test *testing.T) {
		originalLocal := time.Local
		test.Cleanup(func() { time.Local = originalLocal })
		time.Local = time.FixedZone("previous day", -12*60*60)
		capturedDay := timezone.Today()
		repo := &todayStatsCacheRepository{revision: "same", revisionHook: func() { time.Local = time.FixedZone("next day", 14*60*60) }}
		router := todayStatsCacheRouter(test, repo)
		require.Equal(test, http.StatusOK, requestTodayStats(router, `{"account_ids":[1]}`, "").Code)
		require.Equal(test, capturedDay, repo.starts[0])
		_, cached := accountTodayStatsBatchCache.Get(buildAccountTodayStatsBatchCacheKey([]int64{1}, capturedDay, "same"))
		require.True(test, cached)
		require.Equal(test, http.StatusOK, requestTodayStats(router, `{"account_ids":[1]}`, "").Code)
		require.Equal(test, 2, repo.batchCalls)
		require.Equal(test, timezone.Today(), repo.starts[1])
	})

	t.Run("old concurrent fill stays under old revision", func(test *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		defer unblock()
		repo := &todayStatsCacheRepository{revision: "before", stats: usagestats.AccountStats{Cost: 1}, batchHook: func() { close(started); <-release }}
		router := todayStatsCacheRouter(test, repo)
		oldResponse := make(chan *httptest.ResponseRecorder, 1)
		go func() { oldResponse <- requestTodayStats(router, `{"account_ids":[1]}`, "") }()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			test.Fatal("old load did not start")
		}
		repo.mutex.Lock()
		repo.revision, repo.stats.Cost, repo.batchHook = "after", 2, nil
		repo.mutex.Unlock()
		current := requestTodayStats(router, `{"account_ids":[1]}`, "")
		require.Equal(test, http.StatusOK, current.Code)
		require.Contains(test, current.Body.String(), `"cost":2`)
		unblock()
		select {
		case old := <-oldResponse:
			require.Equal(test, http.StatusOK, old.Code)
			require.Contains(test, old.Body.String(), `"cost":1`)
		case <-time.After(5 * time.Second):
			test.Fatal("old load did not finish")
		}
		hit := requestTodayStats(router, `{"account_ids":[1]}`, "")
		require.Equal(test, "hit", hit.Header().Get("X-Snapshot-Cache"))
		require.Equal(test, current.Body.String(), hit.Body.String())
		require.Equal(test, 2, repo.batchCalls)
	})

	t.Run("batch failure preserves single account fallback and zero amounts", func(test *testing.T) {
		repo := &todayStatsCacheRepository{revision: "same", batchError: errors.New("batch failed")}
		router := todayStatsCacheRouter(test, repo)
		result := requestTodayStats(router, `{"account_ids":[1]}`, "")
		require.Equal(test, http.StatusOK, result.Code)
		require.JSONEq(test, `{"code":0,"message":"success","data":{"stats":{"1":{"requests":0,"tokens":0,"cost":0,"standard_cost":0,"user_cost":0}}}}`, result.Body.String())
		require.Equal(test, 1, repo.fallbackCalls)
		require.Equal(test, repo.starts[0], repo.starts[1])
	})
}
