//go:build unit

package handler

import (
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestQianwenNativeAudioAuditText(t *testing.T) {
	body := qianwenNativeAuditBody([]byte(`{"model":"qwen3-tts-flash","input":{"text":"请朗读这段文字。","voice":"Cherry"}}`))
	require.Equal(t, "请朗读这段文字。", gjson.GetBytes(body, "messages.0.content").String())
}
