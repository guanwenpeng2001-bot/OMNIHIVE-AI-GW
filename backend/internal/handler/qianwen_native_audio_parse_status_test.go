//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenNativeAudioParseErrorStatus(t *testing.T) {
	for _, scenario := range []struct {
		name, audio string
		status      int
		message     string
	}{
		{"native_audio_limit", "data:audio/wav;base64," + strings.Repeat("A", service.QianwenAudioMaxEncodedBytes), http.StatusRequestEntityTooLarge, service.ErrQianwenAudioTooLarge.Error()},
		{"malformed_audio", "data:audio/wav;base64,!!!!", http.StatusBadRequest, "invalid base64 audio data"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := `{"model":"qwen3-asr-flash","input":{"messages":[{"role":"user","content":[{"audio":"` + scenario.audio + `"}]}]}}`
			// Prove this is a native parser failure below the outer request limit.
			_, parseErr := service.ParseQianwenNativeBody([]byte(body))
			require.ErrorContains(t, parseErr, scenario.message)
			if scenario.status == http.StatusRequestEntityTooLarge {
				require.ErrorIs(t, parseErr, service.ErrQianwenAudioTooLarge)
			} else {
				require.NotErrorIs(t, parseErr, service.ErrQianwenAudioTooLarge)
			}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Gateway.MaxBodySize = 32 << 20
			cfg.Gateway.TextMaxBodySize = 32 << 20
			cfg.Server.MaxRequestBodySize = 32 << 20
			require.Less(t, int64(len(body)), cfg.Gateway.MaxBodySize)
			upstream := &compositeChatUpstream{}
			handler := newOpenAIImageChatRejectionHandler(t)
			handler.cfg = cfg
			handler.gatewayService = service.ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			recorder := httptest.NewRecorder()
			client, _ := gin.CreateTestContext(recorder)
			client.Request = httptest.NewRequest(http.MethodPost, service.QianwenNativeMultimodalPath, strings.NewReader(body))
			client.Request.Header.Set("Content-Type", "application/json")
			setImageChatTestAuth(client)
			key, _ := middleware.GetAPIKeyFromContext(client)
			key.Group = &service.Group{Platform: service.PlatformQianwen}
			handler.QianwenNative(client)
			require.Equal(t, scenario.status, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), scenario.message)
			require.Contains(t, recorder.Body.String(), "invalid_request_error")
			require.Empty(t, upstream.accountIDs)
			_, selected := client.Get(opsAccountIDKey)
			require.False(t, selected)
		})
	}
}
