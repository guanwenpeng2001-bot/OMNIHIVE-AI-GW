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

func TestAdminDashboardDayBoundaryDST(t *testing.T) {
	for _, scenario := range []struct {
		date  string
		hours time.Duration
	}{
		{"2026-03-08", 23},
		{"2026-11-01", 25},
		{"2026-02-01", 24},
	} {
		t.Run(scenario.date, func(t *testing.T) {
			query := "?timezone=America%2FNew_York&start_date=" + scenario.date + "&end_date=" + scenario.date
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodGet, "/admin/dashboard"+query, nil)
			start, end := parseTimeRange(context)
			require.Equal(t, scenario.hours*time.Hour, end.Sub(start))
			require.Equal(t, 0, end.Hour())
			require.Equal(t, start.AddDate(0, 0, 1), end)
			repo := &adminUsageRepoCapture{}
			router := newAdminUsageRequestTypeTestRouter(repo)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/usage"+query, nil))
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, start, *repo.listFilters.StartTime)
			require.Equal(t, end, *repo.listFilters.EndTime, "dashboard and usage must share the exclusive boundary")
		})
	}
}
