package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestUserUsageExport_HTTP11ReadFailsAfterFirstBatch(t *testing.T) {
	testUserUsageExportTransport(t, false)
}
func TestUserUsageExport_HTTP2StreamResetAfterFirstBatch(t *testing.T) {
	testUserUsageExportTransport(t, true)
}
func testUserUsageExportTransport(t *testing.T, httpTwo bool) {
	for _, fail := range []bool{false, true} {
		name := "complete"
		if fail {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			releaseFailure := make(chan struct{})
			repo := &userUsageExportRepo{logs: []service.UsageLog{{ID: 3, UserID: 42, Model: "gpt-5.1"}}, blockAfter: releaseFailure}
			if fail {
				repo.streamError = errors.New("read failed: secret must not reach client")
				repo.failAfterFirstBatch = true
			}
			router := newUserUsageExportRouter(repo, 42)
			router.GET("/healthy", func(context *gin.Context) { context.String(http.StatusOK, "healthy") })
			server := httptest.NewUnstartedServer(router)
			server.EnableHTTP2 = httpTwo
			if httpTwo {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			client := server.Client()
			client.Timeout = 5 * time.Second
			result, err := client.Get(server.URL + "/api/v1/usage/export?timezone=UTC")
			close(releaseFailure) // Headers arrive only after the handler flushed its first batch.
			require.NoError(t, err)
			defer result.Body.Close()
			require.Equal(t, http.StatusOK, result.StatusCode)
			expectedProtocol := 1
			if httpTwo {
				expectedProtocol = 2
			}
			require.Equal(t, expectedProtocol, result.ProtoMajor)
			body, readErr := io.ReadAll(result.Body)
			if fail {
				require.Error(t, readErr)
			} else {
				require.NoError(t, readErr)
				require.Len(t, readUserExportCSV(t, string(body)), 2)
			}
			require.NotContains(t, string(body), "secret")
			require.Equal(t, int64(42), repo.filters.UserID)
			var reused bool
			healthyRequest, err := http.NewRequest(http.MethodGet, server.URL+"/healthy", nil)
			require.NoError(t, err)
			healthyRequest = healthyRequest.WithContext(httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}))
			healthy, err := client.Do(healthyRequest)
			require.NoError(t, err)
			defer healthy.Body.Close()
			healthyBody, err := io.ReadAll(healthy.Body)
			require.NoError(t, err)
			require.Equal(t, "healthy", string(healthyBody))
			require.Equal(t, expectedProtocol, healthy.ProtoMajor)
			if httpTwo {
				require.True(t, reused, "the healthy request must reuse the same HTTP/2 connection")
			}
		})
	}
}

type exportFaultResponseWriter struct {
	gin.ResponseWriter
	failWrite int
	calls     int
	partial   int
	commit    bool
}

func (writer *exportFaultResponseWriter) Write(data []byte) (int, error) {
	writer.calls++
	if writer.calls != writer.failWrite {
		return writer.ResponseWriter.Write(data)
	}
	if writer.commit {
		writer.ResponseWriter.WriteHeaderNow()
	}
	if writer.partial > 0 {
		written, _ := writer.ResponseWriter.Write(data[:writer.partial])
		return written, io.ErrClosedPipe
	}
	return 0, io.ErrClosedPipe
}
func TestUserUsageExport_WriterFailuresUseCommittedBoundary(t *testing.T) {
	cases := []struct {
		name      string
		failWrite int
		partial   int
		commit    bool
		rows      bool
		abort     bool
	}{
		{name: "empty_uncommitted", failWrite: 1},
		{name: "empty_partial_BOM", failWrite: 1, partial: 1, abort: true},
		{name: "empty_committed_without_bytes", failWrite: 1, commit: true, abort: true},
		{name: "empty_Close_header_failure", failWrite: 2, partial: 2, abort: true},
		{name: "batch_partial_BOM", failWrite: 1, partial: 1, rows: true, abort: true},
		{name: "batch_write_failure", failWrite: 2, rows: true, abort: true},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			repo := &userUsageExportRepo{}
			if scenario.rows {
				repo.logs = []service.UsageLog{{ID: 3, UserID: 42}}
			}
			injectFailure := func(context *gin.Context) {
				context.Writer = &exportFaultResponseWriter{ResponseWriter: context.Writer, failWrite: scenario.failWrite, partial: scenario.partial, commit: scenario.commit}
				context.Next()
			}
			router := newUserUsageExportRouter(repo, 42, injectFailure)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/usage/export?timezone=UTC", nil)
			run := func() { router.ServeHTTP(recorder, request) }
			if scenario.abort {
				require.PanicsWithValue(t, http.ErrAbortHandler, run)
				require.NotContains(t, recorder.Body.String(), "INTERNAL_ERROR")
				require.NotContains(t, recorder.Body.String(), `"code"`)
			} else {
				require.NotPanics(t, run)
				require.Equal(t, http.StatusInternalServerError, recorder.Code)
				require.Contains(t, recorder.Body.String(), "INTERNAL_ERROR")
				require.Empty(t, recorder.Header().Get("Content-Disposition"))
			}
		})
	}
}

func TestUsageExportRecovery_ResourceHTTP2AbortReachesTransport(t *testing.T) {
	releaseFailure := make(chan struct{})
	router := gin.New()
	router.Use(middleware.Recovery())
	resourceHandler := &GatewayResourceHandler{}
	router.GET("/resource", func(context *gin.Context) {
		context.String(http.StatusOK, "partial")
		context.Writer.Flush()
		<-releaseFailure
		resourceHandler.abortResourceStream(context)
	})
	server := httptest.NewUnstartedServer(router)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	result, err := client.Get(server.URL + "/resource")
	close(releaseFailure)
	require.NoError(t, err)
	defer result.Body.Close()
	require.Equal(t, 2, result.ProtoMajor)
	_, err = io.ReadAll(result.Body)
	require.Error(t, err)
}

func TestUserUsageExport_AbortLogsOnlyScopeRequestAndErrorType(t *testing.T) {
	core, observed := observer.New(zap.ErrorLevel)
	repo := &userUsageExportRepo{logs: []service.UsageLog{{ID: 3, UserID: 42}}, streamError: errors.New("password=secret SQL raw-row"), failAfterFirstBatch: true}
	capture := func(context *gin.Context) {
		context.Request = context.Request.WithContext(logger.IntoContext(context.Request.Context(), zap.New(core).With(zap.String("request_id", "export-request"))))
		context.Next()
	}
	router := newUserUsageExportRouter(repo, 42, capture)
	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/usage/export", nil))
	})
	entries := observed.All()
	require.Len(t, entries, 1)
	require.Contains(t, entries[0].Message, "scope=self")
	require.Contains(t, entries[0].Message, "error_type=")
	require.NotContains(t, entries[0].Message, "secret")
	require.NotContains(t, entries[0].Message, "SQL")
	require.Equal(t, "export-request", entries[0].ContextMap()["request_id"])
}
