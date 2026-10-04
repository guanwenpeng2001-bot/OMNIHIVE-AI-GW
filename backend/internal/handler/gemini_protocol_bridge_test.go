package handler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiProtocolBridgeConcurrentModelsAndOriginalAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key := &service.APIKey{ID: 7, Group: &service.Group{ID: 3, Platform: service.PlatformComposite, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"public-a", "public-b"}}}}
	var mu sync.Mutex
	seen := map[string]string{}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyAPIKey), key); c.Next() })
	router.Use(middleware.GroupModelAllowlist())
	router.Use(func(c *gin.Context) {
		model := service.CompositeGeminiModelFromParams(c)
		platform := service.PlatformAnthropic
		if model == "public-b" {
			platform = service.PlatformOpenAI
		}
		c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{Matched: true, PublicModel: model, TargetPlatform: platform, UpstreamModel: "private-" + model}))
		c.Set(gin.BodyBytesKey, []byte("stale body cache"))
		c.Next()
	})
	next := func(c *gin.Context) {
		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		require.NoError(t, err)
		cached, _ := c.Get(gin.BodyBytesKey)
		require.Equal(t, body, cached)
		require.Contains(t, c.Request.URL.Path, "/v1beta/models/")
		require.Equal(t, "caller-key", c.GetHeader("x-goog-api-key"))
		platform, _ := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		model := gjson.GetBytes(body, "model").String()
		mu.Lock()
		seen[model] = platform
		mu.Unlock()
		c.JSON(200, gin.H{"type": "message", "model": model, "content": []any{gin.H{"type": "text", "text": model}}, "stop_reason": "end_turn", "usage": gin.H{"input_tokens": 3, "output_tokens": 1}})
	}
	router.POST("/v1beta/models/*modelAction", GeminiProtocolBridge(next, nil, nil))
	var wg sync.WaitGroup
	for _, model := range []string{"public-a", "public-b"} {
		wg.Add(1)
		go func(model string) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/v1beta/models/"+model+":generateContent", strings.NewReader(`{"contents":[{"parts":[{"text":"hello"}]}]}`))
			req.Header.Set("x-goog-api-key", "caller-key")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Equal(t, model, gjson.Get(rec.Body.String(), "modelVersion").String())
			require.Equal(t, "private-"+model, gjson.Get(rec.Body.String(), "candidates.0.content.parts.0.text").String())
		}(model)
	}
	wg.Wait()
	require.Equal(t, map[string]string{"private-public-a": service.PlatformAnthropic, "private-public-b": service.PlatformOpenAI}, seen)
	require.Equal(t, service.PlatformComposite, key.Group.Platform)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/v1beta/models/private-public-a:generateContent", strings.NewReader(`{"contents":[{"parts":[{"text":"hello"}]}]}`)))
	require.Equal(t, 404, rec.Code)
	require.Equal(t, 2, len(seen))
}

func TestGeminiProtocolBridgeStreamsBeforeUpstreamCompletes(t *testing.T) {
	for _, alt := range []string{"sse", "json"} {
		t.Run(alt, func(t *testing.T) {
			continueStream := make(chan struct{})
			defer close(continueStream)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n")
				fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"first\"}}\n\n")
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Errorf("flush fixture response: %v", err)
					return
				}
				select {
				case <-continueStream:
				case <-r.Context().Done():
					return
				}
				fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
			}))
			defer upstream.Close()
			router := gin.New()
			router.POST("/v1beta/models/*modelAction", GeminiProtocolBridge(func(c *gin.Context) {
				request, _ := http.NewRequestWithContext(c.Request.Context(), "POST", upstream.URL, c.Request.Body)
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					c.AbortWithStatus(502)
					return
				}
				defer func() { require.NoError(t, response.Body.Close()) }()
				c.Header("Content-Type", response.Header.Get("Content-Type"))
				scanner := bufio.NewReader(response.Body)
				for {
					line, err := scanner.ReadString('\n')
					if len(line) > 0 {
						_, _ = c.Writer.WriteString(line)
						c.Writer.Flush()
					}
					if err != nil {
						return
					}
				}
			}, nil, nil))
			server := httptest.NewServer(router)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1beta/models/public:streamGenerateContent?alt="+alt, strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			defer func() { require.NoError(t, response.Body.Close()) }()
			buffer := make([]byte, 2048)
			n, err := response.Body.Read(buffer)
			require.NoError(t, err)
			require.Contains(t, string(buffer[:n]), "first")
			// Reading the first output succeeded while the upstream is still waiting.
			continueStream <- struct{}{}
			rest, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Contains(t, string(rest), "STOP")
			require.Contains(t, string(rest), "totalTokenCount")
			if alt == "json" {
				require.True(t, gjson.Valid(string(buffer[:n])+string(rest)))
				require.Equal(t, "application/json", response.Header.Get("Content-Type"))
			}
		})
	}
}

func TestGeminiProtocolBridgeCountEmbeddingAndErrorEnvelopes(t *testing.T) {
	cases := []struct {
		action, request, reply string
		status                 int
		want                   string
	}{
		{"countTokens", `{"contents":[{"parts":[{"text":"hi"}]}]}`, `{"input_tokens":4}`, 200, `"totalTokens":4`},
		{"embedContent", `{"content":{"parts":[{"text":"hi"}]}}`, `{"data":[{"index":0,"embedding":[0.25,0.5]}]}`, 200, `"values":[0.25,0.5]`},
		{"generateContent", `{"contents":[{"parts":[{"text":"hi"}]}]}`, `{"error":{"type":"invalid_request_error","message":"bad tool"}}`, 400, `"status":"INVALID_ARGUMENT"`},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			router := gin.New()
			next := func(c *gin.Context) {
				if tc.action == "countTokens" {
					c.Header("X-Token-Count-Estimated", "true")
				}
				c.Data(tc.status, "application/json", []byte(tc.reply))
			}
			router.POST("/v1beta/models/*modelAction", GeminiProtocolBridge(next, next, next))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("POST", "/v1beta/models/m:"+tc.action, strings.NewReader(tc.request)))
			require.Equal(t, tc.status, rec.Code)
			require.Contains(t, rec.Body.String(), tc.want)
			if tc.action == "countTokens" {
				require.Contains(t, rec.Body.String(), `"estimated":true`)
			}
		})
	}
}

func TestGeminiProtocolWriterHeartbeatDoesNotPreventFailover(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	w := service.NewGeminiProtocolWriter(c.Writer, "m", "streamGenerateContent", true, 0)
	w.Header().Set("Content-Type", "text/event-stream")
	before := w.Size()
	_, err := w.WriteString("event: ping\ndata: {\"type\":\"ping\"}\n\n")
	require.NoError(t, err)
	w.Flush()
	require.Equal(t, before, w.Size())
	require.False(t, w.Written())
	_, err = w.WriteString("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"committed\"}}\n\n")
	require.NoError(t, err)
	require.NotEqual(t, before, w.Size())
	require.True(t, w.Written())
	_ = w.Finish()
	require.Contains(t, rec.Body.String(), `"error"`)
	require.NotContains(t, rec.Body.String(), `"finishReason":"STOP"`)
}
