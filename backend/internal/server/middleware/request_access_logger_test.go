package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
)

type testLogSink struct {
	mu     sync.Mutex
	events []*logger.LogEvent
}

func (s *testLogSink) WriteLogEvent(event *logger.LogEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *testLogSink) list() []*logger.LogEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*logger.LogEvent, len(s.events))
	copy(out, s.events)
	return out
}

func initMiddlewareTestLogger(t *testing.T) *testLogSink {
	return initMiddlewareTestLoggerWithLevel(t, "debug")
}

func initMiddlewareTestLoggerWithLevel(t *testing.T, level string) *testLogSink {
	t.Helper()
	level = strings.TrimSpace(level)
	if level == "" {
		level = "debug"
	}
	if err := logger.Init(logger.InitOptions{
		Level:       level,
		Format:      "json",
		ServiceName: "sub2api",
		Environment: "test",
		Output: logger.OutputOptions{
			ToStdout: false,
			ToFile:   false,
		},
	}); err != nil {
		t.Fatalf("init logger: %v", err)
	}
	sink := &testLogSink{}
	logger.SetSink(sink)
	t.Cleanup(func() {
		logger.SetSink(nil)
	})
	return sink
}

func TestRequestLogger_GenerateAndPropagateRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogger())
	r.GET("/t", func(c *gin.Context) {
		reqID, ok := c.Request.Context().Value(ctxkey.RequestID).(string)
		if !ok || reqID == "" {
			t.Fatalf("request_id missing in context")
		}
		if got := c.Writer.Header().Get(requestIDHeader); got != reqID {
			t.Fatalf("response header request_id mismatch, header=%q ctx=%q", got, reqID)
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Header().Get(requestIDHeader) == "" {
		t.Fatalf("X-Request-ID should be set")
	}
}

func TestRequestLogger_KeepIncomingRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogger())
	r.GET("/t", func(c *gin.Context) {
		reqID, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
		if reqID != "rid-fixed" {
			t.Fatalf("request_id=%q, want rid-fixed", reqID)
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set(requestIDHeader, "rid-fixed")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if got := w.Header().Get(requestIDHeader); got != "rid-fixed" {
		t.Fatalf("header=%q, want rid-fixed", got)
	}
}

func TestRequestLoggerBoundsIncomingRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogger())
	r.GET("/t", func(c *gin.Context) {
		reqID, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
		if len(reqID) != 36 {
			t.Fatalf("request_id length=%d", len(reqID))
		}
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set(requestIDHeader, strings.Repeat("r", 1024))
	r.ServeHTTP(w, req)
	if got := len(w.Header().Get(requestIDHeader)); got != 36 {
		t.Fatalf("response request_id length=%d", got)
	}
}

func TestLogger_AccessLogIncludesCoreFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)

	r := gin.New()
	r.Use(Logger())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, ctxkey.AccountID, int64(101))
		ctx = context.WithValue(ctx, ctxkey.Platform, "openai")
		ctx = context.WithValue(ctx, ctxkey.Model, "gpt-5")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/api/test", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d", w.Code)
	}

	events := sink.list()
	if len(events) == 0 {
		t.Fatalf("expected at least one log event")
	}
	found := false
	for _, event := range events {
		if event == nil || event.Message != "http request completed" {
			continue
		}
		found = true
		switch v := event.Fields["status_code"].(type) {
		case int:
			if v != http.StatusCreated {
				t.Fatalf("status_code field mismatch: %v", v)
			}
		case int64:
			if v != int64(http.StatusCreated) {
				t.Fatalf("status_code field mismatch: %v", v)
			}
		default:
			t.Fatalf("status_code type mismatch: %T", v)
		}
		switch v := event.Fields["account_id"].(type) {
		case int64:
			if v != 101 {
				t.Fatalf("account_id field mismatch: %v", v)
			}
		case int:
			if v != 101 {
				t.Fatalf("account_id field mismatch: %v", v)
			}
		default:
			t.Fatalf("account_id type mismatch: %T", v)
		}
		if event.Fields["platform"] != "openai" || event.Fields["model"] != "gpt-5" {
			t.Fatalf("platform/model mismatch: %+v", event.Fields)
		}
	}
	if !found {
		t.Fatalf("access log event not found")
	}
}

func TestLogger_IngressRejectRemainsInStandardAccessLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)
	r := gin.New()
	r.Use(Logger())
	r.GET("/v1/messages", func(c *gin.Context) {
		MarkIngressRejected(c, IngressRejectInvalidAPIKey)
		c.Status(http.StatusUnauthorized)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
	events := sink.list()
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1", len(events))
	}
	if got := events[0].Fields["ingress_reject_reason"]; got != string(IngressRejectInvalidAPIKey) {
		t.Fatalf("ingress_reject_reason=%v", got)
	}
	if got, _ := events[0].Fields[logger.OpsSystemLogSkipField].(bool); !got {
		t.Fatalf("%s must be true", logger.OpsSystemLogSkipField)
	}
}

func TestLogger_AccessLogUsesForwardedClientIPFromTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)

	r := gin.New()
	if err := r.SetTrustedProxies([]string{"104.23.251.120"}); err != nil {
		t.Fatalf("set trusted proxies: %v", err)
	}
	r.Use(Logger())
	r.GET("/api/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.RemoteAddr = "104.23.251.120:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}

	for _, event := range sink.list() {
		if event == nil || event.Message != "http request completed" {
			continue
		}
		if got := event.Fields["client_ip"]; got != "203.0.113.42" {
			t.Fatalf("client_ip=%q, want real forwarded ip", got)
		}
		return
	}
	t.Fatalf("access log event not found")
}

func TestLogger_HealthPathSkipped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)

	r := gin.New()
	r.Use(Logger())
	r.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if len(sink.list()) != 0 {
		t.Fatalf("health endpoint should not write access log")
	}
}

func TestLogger_AccessLogDroppedWhenLevelWarn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLoggerWithLevel(t, "warn")

	r := gin.New()
	r.Use(RequestLogger())
	r.Use(Logger())
	r.GET("/api/test", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d", w.Code)
	}

	events := sink.list()
	for _, event := range events {
		if event != nil && event.Message == "http request completed" {
			t.Fatalf("access log should not be indexed when level=warn: %+v", event)
		}
	}
}

func TestLoggers_RedactPluginUIToken(testContext *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, middlewareCase := range []struct {
		name       string
		handlers   []gin.HandlerFunc
		requestLog bool
		accessLog  bool
	}{
		{name: "request", handlers: []gin.HandlerFunc{RequestLogger()}, requestLog: true},
		{name: "access", handlers: []gin.HandlerFunc{Logger()}, accessLog: true},
		{name: "combined", handlers: []gin.HandlerFunc{RequestLogger(), Logger()}, requestLog: true, accessLog: true},
	} {
		testContext.Run(middlewareCase.name, func(testContext *testing.T) {
			for _, pathCase := range []struct {
				name     string
				target   string
				wantPath string
			}{
				{name: "asset", target: "/api/v1/plugin-ui/secret-ticket/assets/app.js", wantPath: "/api/v1/plugin-ui/***/assets/app.js"},
				{name: "query", target: "/api/v1/plugin-ui/secret-ticket/index.html?theme=dark&token=query-secret", wantPath: "/api/v1/plugin-ui/***/index.html"},
				{name: "root", target: "/api/v1/plugin-ui/secret-ticket/", wantPath: "/api/v1/plugin-ui/***/"},
				{name: "missing_resource", target: "/api/v1/plugin-ui/secret-ticket", wantPath: "/api/v1/plugin-ui/***"},
				{name: "encoded_token", target: "/api/v1/plugin-ui/secret%3Fticket/index.html", wantPath: "/api/v1/plugin-ui/***/index.html"},
				{name: "resource_unchanged", target: "/api/v1/plugin-ui/secret-ticket/access_token=asset.js", wantPath: "/api/v1/plugin-ui/***/access_token=asset.js"},
				{name: "empty_token", target: "/api/v1/plugin-ui//index.html", wantPath: "/api/v1/plugin-ui//index.html"},
				{name: "prefix_only", target: "/api/v1/plugin-ui/", wantPath: "/api/v1/plugin-ui/"},
				{name: "other_path", target: "/api/v1/accounts/access_token=visible?theme=dark", wantPath: "/api/v1/accounts/access_token=visible"},
				{name: "similar_prefix", target: "/api/v1/plugin-ui-other/visible/index.html", wantPath: "/api/v1/plugin-ui-other/visible/index.html"},
				{name: "nested_prefix", target: "/other/api/v1/plugin-ui/visible/index.html", wantPath: "/other/api/v1/plugin-ui/visible/index.html"},
			} {
				testContext.Run(pathCase.name, func(testContext *testing.T) {
					sink := initMiddlewareTestLogger(testContext)
					router := gin.New()
					router.Use(middlewareCase.handlers...)
					request := httptest.NewRequest(http.MethodGet, pathCase.target, nil)
					originalURL := *request.URL
					originalRequestURI := request.RequestURI
					router.NoRoute(func(ginContext *gin.Context) {
						if *ginContext.Request.URL != originalURL || ginContext.Request.RequestURI != originalRequestURI {
							testContext.Fatal("logging changed the request URL")
						}
						if middlewareCase.requestLog {
							logger.FromContext(ginContext.Request.Context()).Info("plugin handler log")
						}
						// Handler-side cleanup must not be needed to redact either log.
						ginContext.Request.URL.Path = "/handler-rewritten"
						ginContext.Status(http.StatusNotFound)
					})
					router.ServeHTTP(httptest.NewRecorder(), request)
					wantEvents := 0
					if middlewareCase.requestLog {
						wantEvents++
					}
					if middlewareCase.accessLog {
						wantEvents++
					}
					events := sink.list()
					if len(events) != wantEvents {
						testContext.Fatalf("events=%d, want %d", len(events), wantEvents)
					}
					for _, event := range events {
						if gotPath := event.Fields["path"]; gotPath != pathCase.wantPath {
							testContext.Errorf("%s path=%q, want %q", event.Message, gotPath, pathCase.wantPath)
						}
					}
				})
			}
		})
	}
}
