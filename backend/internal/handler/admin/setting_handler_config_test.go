//go:build unit

package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSettingsGetAndPutUseTheSameProjection(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySiteName:       "Example Gateway",
		service.SettingKeyOpenAITTFTMode: service.OpenAITTFTModeVisible,
	})
	getRecorder := httptest.NewRecorder()
	getContext, _ := gin.CreateTestContext(getRecorder)
	getContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(getContext)
	require.Equal(t, http.StatusOK, getRecorder.Code)

	putRecorder := doUpdateSettings(t, h, map[string]any{"site_name": "Example Gateway"}, nil)
	require.Equal(t, http.StatusOK, putRecorder.Code)
	var getResponse, putResponse struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(getRecorder.Body.Bytes(), &getResponse))
	require.NoError(t, json.Unmarshal(putRecorder.Body.Bytes(), &putResponse))
	require.Equal(t, getResponse.Data, putResponse.Data)
	require.Equal(t, service.OpenAITTFTModeVisible, getResponse.Data["openai_ttft_mode"])
	require.Contains(t, getResponse.Data, "web_search_emulation_enabled")
	require.NotContains(t, getResponse.Data, "turnstile_secret_key")
}

func TestGetEffectiveConfigRedactsSecretsAndPaginates(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyTurnstileSecretKey: "never-return-this"})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/config/effective?limit=200", nil)
	h.GetEffectiveConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data dto.EffectiveConfigResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.NotContains(t, recorder.Body.String(), "never-return-this")
	require.Greater(t, len(envelope.Data.Items), 1)
	for index, item := range envelope.Data.Items {
		if item.Sensitive {
			require.Nil(t, item.Value)
			require.NotNil(t, item.Configured)
		}
		if index > 0 {
			require.Less(t, envelope.Data.Items[index-1].Key, item.Key)
		}
	}
}

func TestResetConfigOverridesRejectsUnknownKeyWithoutDeleting(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyLinuxDoConnectClientID: "keep"})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/config/reset", bytes.NewBufferString(`{"keys":["unknown"]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.ResetConfigOverrides(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "keep", repo.values[service.SettingKeyLinuxDoConnectClientID])
}
