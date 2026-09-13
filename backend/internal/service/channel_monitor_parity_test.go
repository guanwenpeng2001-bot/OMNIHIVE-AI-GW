//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMonitorQianwenCommandCodeProbe(t *testing.T) {
	swapMonitorHTTPClient(t)
	for _, tc := range []struct{ provider, path, model, auth string }{
		{MonitorProviderQianwen, "/compatible-mode/v1", "qwen-flash", "Bearer test-key"},
		{MonitorProviderCommandCode, "/provider/v1", DefaultCommandCodeTestModel, ""},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			require.NoError(t, validateProvider(tc.provider))
			require.NoError(t, validateCheckMode(tc.provider, MonitorCheckModeProbe))
			require.ErrorIs(t, validateCheckMode(tc.provider, MonitorCheckModeQuota), ErrChannelMonitorInvalidCheckMode)
			require.ErrorIs(t, validateCheckMode(tc.provider, MonitorCheckModeQuotaProbe), ErrChannelMonitorInvalidCheckMode)
			require.Error(t, validateAPIMode(tc.provider, MonitorAPIModeResponses))
			require.Equal(t, tc.model, normalizeMonitorPrimaryModel(tc.provider, MonitorCheckModeProbe, ""))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, tc.path+"/chat/completions", r.URL.Path)
				require.Equal(t, tc.auth, r.Header.Get("Authorization"))
				if tc.provider == MonitorProviderCommandCode {
					require.Equal(t, "test-key", r.Header.Get("x-api-key"))
				}
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, tc.model, body["model"])
				require.Equal(t, false, body["stream"])
				require.NotEmpty(t, body["messages"])
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"2"}}]}`))
			}))
			defer server.Close()
			text, _, status, err := callProvider(context.Background(), tc.provider, server.URL+tc.path, "test-key", tc.model, "1+1", nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, status)
			require.Equal(t, "2", text)
			require.True(t, bodyMergeKeyDenyList[tc.provider]["messages"])
			require.Error(t, validateReplaceRequestBody(tc.provider, "", map[string]any{"model": tc.model}))
		})
	}
}
