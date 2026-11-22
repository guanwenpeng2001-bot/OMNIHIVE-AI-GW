package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWorkBuddyRetryAfterDateAndBounds(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		value    string
		duration time.Duration
	}{
		{now.Add(20 * time.Minute).Format(http.TimeFormat), 20 * time.Minute}, {"120", 2 * time.Minute},
		{now.Add(48 * time.Hour).Format(http.TimeFormat), 24 * time.Hour}, {"100000000000", 24 * time.Hour}, {"invalid", time.Minute},
	} {
		require.Equal(t, now.Add(tc.duration), workBuddyCooldownUntil(http.Header{"Retry-After": {tc.value}}, now))
	}
}

func TestWorkBuddyAggregateFragmentedToolArguments(t *testing.T) {
	frames := []string{
		`{"choices":[{"index":1,"delta":{"content":"one","refusal":"no"}},{"index":0,"delta":{"reasoning_content":"think","tool_calls":[{"index":0,"id":"call","function":{"name":"get","arguments":"["}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"reasoning_content":"ing","tool_calls":[{"index":0,"function":{"name":"Item","arguments":"]"}}]},"finish_reason":"tool_calls"},{"index":1,"delta":{"content":" two","refusal":" thanks"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":5}}`,
		"[DONE]",
	}
	raw, err := aggregateWorkBuddyChat(strings.NewReader("data:" + strings.Join(frames, "\n\ndata:") + "\n\n"))
	require.NoError(t, err)
	messages := gjson.GetBytes(raw, "choices.#.message").Array()
	require.Len(t, messages, 2)
	require.Equal(t, "thinking", messages[0].Get("reasoning_content").Str)
	require.Equal(t, "getItem", messages[0].Get("tool_calls.0.function.name").Str)
	require.Equal(t, "[]", messages[0].Get("tool_calls.0.function.arguments").Str)
	require.Equal(t, "one two", messages[1].Get("content").Str)
	require.Equal(t, "no thanks", messages[1].Get("refusal").Str)
}

func TestWorkBuddyHeaderTimeoutEndsBeforeBody(t *testing.T) {
	for _, stream := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, _, finish := boundedStreamContext(parent, time.Second)
			defer finish()
			r, w := io.Pipe()
			go func() {
				time.Sleep(2 * time.Second)
				if ctx.Err() != nil {
					_ = w.CloseWithError(ctx.Err())
					return
				}
				_, _ = io.WriteString(w, wbSSE)
				_ = w.Close()
			}()
			resp := &http.Response{StatusCode: 200, Body: r, Header: http.Header{"Content-Type": {"text/event-stream"}}}
			out, err := (&OpenAIGatewayService{}).normalizeWorkBuddyResponse(ctx, wbAccount("cn", "", AccountTypeOAuth), resp, stream)
			require.NoError(t, err)
			_, err = io.ReadAll(out.Body)
			require.NoError(t, err)
			require.NoError(t, out.Body.Close())
			require.NoError(t, ctx.Err())
			cancel()
			require.ErrorIs(t, ctx.Err(), context.Canceled)
		})
	}
}

func TestWorkBuddySlowRawAndMessages(t *testing.T) {
	for _, route := range []string{"chat/completions", "messages"} {
		t.Run(route, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const base = "http://127.0.0.1"
				client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					r, w := io.Pipe()
					go func() {
						time.Sleep(2 * time.Second)
						if err := req.Context().Err(); err != nil {
							_ = w.CloseWithError(err)
							return
						}
						_, _ = io.WriteString(w, wbSSE)
						_ = w.Close()
					}()
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: r}, nil
				})}
				cfg := wbConfig(base)
				cfg.Gateway.OpenAIResponseHeaderTimeout = 1
				gateway := &OpenAIGatewayService{cfg: cfg, httpUpstream: wbHTTP{client}}
				account := wbAccount("cn", base, AccountTypeAPIKey)
				body := []byte(`{"model":"hy3","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest("POST", "/v1/"+route, nil)
				forward := gateway.ForwardAsChatCompletions
				if route == "messages" {
					forward = gateway.ForwardAsAnthropic
				}
				result, err := forward(t.Context(), c, account, body, "", "")
				require.NoError(t, err)
				require.Equal(t, 5, result.Usage.OutputTokens)
				require.Contains(t, rec.Body.String(), "hello")
			})
		})
	}
}
