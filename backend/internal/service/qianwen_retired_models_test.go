//go:build unit

package service

import (
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestQianwenRetiredModelUnavailable(t *testing.T) {
	a := qianwenTestAccount()
	a.Credentials["model_mapping"] = map[string]any{"qwen1.5-7b-chat": "qwen1.5-7b-chat", "legacy": "qwen1.5-7b-chat", "qwen-long": "qwen-long"}
	require.False(t, a.IsModelSupported("qwen1.5-7b-chat"))
	require.False(t, a.IsModelSupported("legacy"))
	require.True(t, a.IsModelSupported("qwen-long"))
	r := parseQianwenModelPrices(QianwenModelPrices{Model: "qwen1.5-7b-chat"}, 1.0/7)
	require.Nil(t, r.Pricing)
	require.Equal(t, "unavailable", r.Status)
	// Retired historical upstream entries cannot return in the refreshed catalog.
	req, _ := http.NewRequest("GET", "https://dashscope.aliyuncs.com/compatible-mode/v1/models", nil)
	ids, raw, err := fetchQianwenModelPages(req, 1<<20, func(*http.Request) (*http.Response, error) {
		return qianwenModelPage(`{"data":[{"id":"qwen1.5-7b-chat"},{"id":"qwen-long"}]}`, 200), nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"qwen-long"}, ids)
	require.NotContains(t, string(raw), "qwen1.5")
}
