package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newBillingTimelineRequest(t *testing.T, query string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/usage/billing-timeline?"+query, nil)
	return c, recorder
}

func TestParseBillingTimelineWindow_InterpretsDatesInTheRequestedTimezone(t *testing.T) {
	c, _ := newBillingTimelineRequest(t, "start_date=2026-09-01&end_date=2026-09-05&timezone=Asia/Shanghai")

	startAt, endAt, ok := parseBillingTimelineWindow(c)
	require.True(t, ok)
	require.NotNil(t, startAt)
	require.NotNil(t, endAt)

	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, time.September, 1, 0, 0, 0, 0, shanghai).UTC(), startAt.UTC())
	// end_date is inclusive for the caller and exclusive for the query.
	require.Equal(t, time.Date(2026, time.September, 6, 0, 0, 0, 0, shanghai).UTC(), endAt.UTC())
}

func TestParseBillingTimelineWindow_LeavesBoundsOpenWhenUnset(t *testing.T) {
	c, _ := newBillingTimelineRequest(t, "")

	startAt, endAt, ok := parseBillingTimelineWindow(c)
	require.True(t, ok)
	require.Nil(t, startAt)
	require.Nil(t, endAt)
}

func TestGetMyBillingTimeline_RejectsInvalidDates(t *testing.T) {
	c, recorder := newBillingTimelineRequest(t, "start_date=not-a-date")
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})

	(&UsageHandler{}).GetMyBillingTimeline(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Contains(t, recorder.Body.String(), "Invalid start_date")
	_ = body
}

func TestGetMyBillingTimeline_RequiresAuthentication(t *testing.T) {
	c, recorder := newBillingTimelineRequest(t, "")

	(&UsageHandler{}).GetMyBillingTimeline(c)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
