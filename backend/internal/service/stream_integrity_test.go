package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStreamIntegrityAntigravitySeparatorsPreserveTextNewlines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAntigravityTestService(&config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}})
	for _, separator := range []string{"\n", "\r\n"} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		first := `{"candidates":[{"content":{"role":"model","parts":[{"text":"first\n\nsecond\r\nthird"}]}}]}`
		last := `{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}`
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + first + separator + separator + "data: " + last + separator + separator)), Header: http.Header{}}
		result, err := svc.handleGeminiStreamingResponse(c, resp, time.Now())
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, "data: "+first+"\n\ndata: "+last+"\n\n", rec.Body.String())
		require.Equal(t, "first\n\nsecond\r\nthird", gjson.Get(first, "candidates.0.content.parts.0.text").String())
	}
}

func TestStreamIntegrityCompactTerminalAndSequence(t *testing.T) {
	for _, tc := range []struct{ status, terminal string }{
		{"completed", "response.completed"}, {"failed", "response.failed"}, {"incomplete", "response.incomplete"}, {"cancelled", "response.failed"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			payload, ok := buildOpenAICompactSSEPayload([]byte(`{"id":"r","status":"` + tc.status + `","output":[{"id":"a","type":"message","content":[{"type":"output_text","text":"a\n\nb"}]}]}`))
			require.True(t, ok)
			events := parseCompactBridgeSSE(t, string(payload))
			for i, e := range events {
				seq := gjson.Get(e[1], "sequence_number")
				require.True(t, seq.Exists())
				require.Equal(t, int64(i), seq.Int())
			}
			require.Equal(t, tc.terminal, events[len(events)-1][0])
			if tc.terminal == "response.failed" {
				require.Len(t, events, 1)
			} else {
				require.Len(t, events, 2)
			}
			require.Equal(t, "a\n\nb", gjson.Get(events[len(events)-1][1], "response.output.0.content.0.text").String())
		})
	}
}

func TestStreamIntegrityCompactWritesTerminalOnce(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		c, rec := newCompactBridgeTestContext(t, true)
		if failFirst {
			writeOpenAICompactSSEFailureMessage(c, 502, "upstream_error", "broken")
		}
		require.True(t, writeOpenAICompactSSEBridge(c, 200, []byte(`{"id":"r","output":[]}`)))
		first := rec.Body.String()
		writeOpenAICompactSSEFailureMessage(c, 502, "upstream_error", "again")
		require.True(t, writeOpenAICompactSSEBridge(c, 200, []byte(`{"id":"r","output":[]}`)))
		require.Equal(t, first, rec.Body.String())
		events := parseCompactBridgeSSE(t, first)
		require.Len(t, events, 1)
		if failFirst {
			require.Equal(t, "response.failed", events[0][0])
		} else {
			require.Equal(t, "response.completed", events[0][0])
		}
	}
}

func TestStreamIntegrityCompatFailuresAndTruncation(t *testing.T) {
	for _, protocol := range []string{"chat", "anthropic"} {
		for _, terminal := range []string{"", `{"type":"response.done","response":{"status":"failed","error":{"code":"server_error","message":"broken"}}}`, `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"broken"}}}`} {
			t.Run(protocol+"/"+terminal, func(t *testing.T) {
				c, rec := newCompactBridgeTestContext(t, false)
				payload := "data: " + `{"type":"response.created","response":{"id":"r","model":"gpt-5.4"}}` + "\n\n" + "data: " + `{"type":"response.output_text.delta","item_id":"a","delta":"partial\n\ntext"}` + "\n\n"
				if terminal != "" {
					payload += "data: " + terminal + "\n\n"
				}
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}
				svc := &OpenAIGatewayService{cfg: &config.Config{}}
				account := &Account{ID: 1, Name: "test", Platform: PlatformOpenAI}
				var err error
				if protocol == "chat" {
					_, err = svc.handleChatStreamingResponse(resp, c, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", time.Now(), 0)
				} else {
					_, err = svc.handleAnthropicStreamingResponse(resp, c, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", time.Now())
				}
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover))
				body := rec.Body.String()
				require.NotContains(t, body, `"finish_reason":"stop"`)
				require.NotContains(t, body, "event: message_stop")
				require.NotContains(t, body, "data: [DONE]")
				var texts []string
				var types []string
				for _, block := range strings.Split(body, "\n\n") {
					for _, line := range strings.Split(block, "\n") {
						if !strings.HasPrefix(line, "data: ") {
							continue
						}
						data := strings.TrimPrefix(line, "data: ")
						if protocol == "chat" {
							if gjson.Get(data, "error").Exists() {
								types = append(types, "error")
							} else if gjson.Get(data, "choices.0.delta.content").Exists() {
								texts = append(texts, gjson.Get(data, "choices.0.delta.content").String())
								types = append(types, "text")
							} else {
								types = append(types, "role")
							}
						} else {
							types = append(types, gjson.Get(data, "type").String())
							if gjson.Get(data, "delta.type").String() == "text_delta" {
								texts = append(texts, gjson.Get(data, "delta.text").String())
							}
						}
					}
				}
				require.Equal(t, []string{"partial\n\ntext"}, texts)
				want := []string{"role", "text"}
				if protocol == "anthropic" {
					want = []string{"message_start", "content_block_start", "content_block_delta"}
				}
				if terminal != "" {
					want = append(want, "error")
				}
				require.Equal(t, want, types)
			})
		}
	}
}
