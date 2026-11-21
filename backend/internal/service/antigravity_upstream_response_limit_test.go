package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

type trackedLimitedResponseBody struct {
	io.Reader
	bytes  int
	closed bool
}

func (b *trackedLimitedResponseBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.bytes += n
	return n, err
}
func (b *trackedLimitedResponseBody) Close() error { b.closed = true; return nil }

func TestAntigravityForwardUpstreamNonStreamReadLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const wire = `{"model":"claude-upstream","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":10,"output_tokens":4,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}`
	readFailure := errors.New("upstream reset")
	for _, tc := range []struct {
		name    string
		reader  io.Reader
		limit   int64
		wantErr error
	}{
		{"exact boundary", strings.NewReader(wire), int64(len(wire)), nil},
		{"one byte over", strings.NewReader(wire), int64(len(wire) - 1), ErrUpstreamResponseBodyTooLarge},
		{"never ending body", &endlessSSELine{}, 256, ErrUpstreamResponseBodyTooLarge},
		{"read failure", io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(readFailure)), 256, readFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedLimitedResponseBody{Reader: tc.reader}
			svc := newAntigravityTestService(&config.Config{Gateway: config.GatewayConfig{UpstreamResponseReadMaxBytes: tc.limit}})
			svc.httpUpstream = &httpUpstreamStub{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"req-limit"}}, Body: body}}
			account := &Account{ID: 1, Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://gateway.example", "api_key": "fixture"}}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			result, err := svc.ForwardUpstream(context.Background(), c, account, []byte(`{"model":"claude-alias","stream":false,"messages":[]}`))
			require.True(t, body.closed)
			require.LessOrEqual(t, int64(body.bytes), tc.limit+1)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, result, "do not construct billing results from incomplete JSON")
				require.Empty(t, observedUpstreamResponseModel(c))
				if errors.Is(tc.wantErr, ErrUpstreamResponseBodyTooLarge) {
					require.Equal(t, http.StatusBadGateway, w.Code)
					require.JSONEq(t, `{"type":"error","error":{"type":"upstream_error","message":"Upstream response too large"}}`, w.Body.String())
				} else {
					require.Empty(t, w.Body.String())
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, wire, w.Body.String())
			require.Equal(t, "application/json", w.Header().Get("Content-Type"))
			require.Equal(t, "claude-alias", result.Model)
			require.Equal(t, "claude-upstream", result.UpstreamResponseModel)
			require.Equal(t, "req-limit", result.UpstreamHeaders.Get("X-Request-Id"))
			require.False(t, result.ClientDisconnect)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 4, result.Usage.OutputTokens)
			require.Equal(t, 3, result.Usage.CacheReadInputTokens)
			require.Equal(t, 2, result.Usage.CacheCreationInputTokens)
		})
	}
}

func TestAntigravitySSEConfigFallback(t *testing.T) {
	for _, svc := range []*AntigravityGatewayService{nil, {}, {settingService: &SettingService{}}} {
		require.Nil(t, svc.antigravitySSEConfig())
		require.Equal(t, defaultUpstreamResponseReadMaxBytes, resolveUpstreamResponseReadLimit(svc.antigravitySSEConfig()))
	}
}
