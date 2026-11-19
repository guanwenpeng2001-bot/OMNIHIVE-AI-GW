package media

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifiedMediaInput(t *testing.T) {
	_, ok := Lookup("audio", "qwen3-tts-flash-realtime")
	require.False(t, ok)
	_, ok = Lookup("video", "unverified")
	require.False(t, ok)
	require.NoError(t, Validate("audio", strings.Repeat("声", 600), "wav", "", 0, 0))
	require.Error(t, Validate("audio", strings.Repeat("声", 601), "wav", "", 0, 0))
	require.Error(t, Validate("audio", "a", "mp3", "", 0, 0))
	require.Error(t, Validate("audio", "a", "wav", "", 5, 0))
	require.NoError(t, Validate("video", "a", "mp4", "1280x720", 5, 0))
	require.Error(t, Validate("video", "a", "mp4", "1280x720", 6, 0))
	require.Error(t, Validate("video", "a", "mp4", "1920x1080", 5, 0))
	require.Error(t, Validate("video", "a", "mp4", "1280x720", 5, 2))
}
