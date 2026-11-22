package service

import (
	"net/http"
	"strings"
	"testing"
	"time"

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
