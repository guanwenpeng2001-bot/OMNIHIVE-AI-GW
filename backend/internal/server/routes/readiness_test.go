package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestReadinessDependencyFailureAndDrainPreserveLiveness(t *testing.T) {
	var calls atomic.Int32
	var draining atomic.Bool
	var dependency atomic.Bool
	dependency.Store(true)
	ready := NewReadinessHandler(func(context.Context) bool { calls.Add(1); return dependency.Load() }, func() bool { return !draining.Load() })
	probe := func() int {
		w := httptest.NewRecorder()
		ready(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
		return w.Code
	}
	require.Equal(t, http.StatusOK, probe())
	require.Equal(t, http.StatusOK, probe())
	require.EqualValues(t, 1, calls.Load())
	draining.Store(true)
	require.Equal(t, http.StatusServiceUnavailable, probe())
	draining.Store(false)
	dependency.Store(false)
	time.Sleep(2100 * time.Millisecond)
	require.Equal(t, http.StatusServiceUnavailable, probe())
	r := gin.New()
	RegisterCommonRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestReadinessBoundsStalledDriverAndDoesNotLeakError(t *testing.T) {
	var calls atomic.Int32
	blocked := make(chan struct{})
	defer close(blocked)
	h := NewReadinessHandler(func(context.Context) bool { calls.Add(1); <-blocked; return false }, func() bool { return true })
	for i := 0; i < 2; i++ {
		start := time.Now()
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
		require.Equal(t, http.StatusServiceUnavailable, w.Code)
		require.Less(t, time.Since(start), 1500*time.Millisecond)
		require.Equal(t, "not ready\n", w.Body.String())
	}
	require.EqualValues(t, 1, calls.Load())
}
