package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminDashboardDayBoundaryDST(test *testing.T) {
	for _, scenario := range []struct {
		date  string
		hours time.Duration
	}{
		{"2026-03-08", 23},
		{"2026-11-01", 25},
		{"2026-02-01", 24},
	} {
		test.Run(scenario.date, func(test *testing.T) {
			query := "?timezone=America%2FNew_York&start_date=" + scenario.date + "&end_date=" + scenario.date
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodGet, "/admin/dashboard"+query, nil)
			start, end := parseTimeRange(context)
			require.Equal(test, scenario.hours*time.Hour, end.Sub(start))
			require.Equal(test, 0, end.Hour())
			require.Equal(test, start.AddDate(0, 0, 1), end)
			repo := &adminUsageRepoCapture{}
			router := newAdminUsageRequestTypeTestRouter(repo)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/usage"+query, nil))
			require.Equal(test, http.StatusOK, recorder.Code)
			require.Equal(test, start, *repo.listFilters.StartTime)
			require.Equal(test, end, *repo.listFilters.EndTime, "dashboard and usage must share the exclusive boundary")
		})
	}
}
