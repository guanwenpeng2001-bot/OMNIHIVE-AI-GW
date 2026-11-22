package apicompat

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiMessagesStreamGolden(t *testing.T) {
	var events []string
	stream := NewGeminiMessagesStream(func(event string, data any) {
		wire, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event+" "+string(wire))
	}, func() {}, func(call map[string]any) string {
		id, _ := call["id"].(string)
		return id
	},
		func(map[string]any, string, string, map[string]bool) error { return nil },
	)
	stream.Start("msg_fixture", "fixture")
	frames := []string{
		`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_1","name":"lookup","args":"{\"q\":"}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_1","name":"lookup","args":"{\"q\":\"x\"}"}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"done"}]},"finishReason":"STOP"}]}`,
	}
	for _, frame := range frames {
		var payload map[string]any
		if err := json.Unmarshal([]byte(frame), &payload); err != nil {
			t.Fatal(err)
		}
		if err := stream.Observe(payload, func() {}); err != nil {
			t.Fatal(err)
		}
	}
	if err := stream.Finish(0, 3, 2); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`message_start {"message":{"content":[],"id":"msg_fixture","model":"fixture","role":"assistant","stop_reason":null,"stop_sequence":null,"type":"message","usage":{"input_tokens":0,"output_tokens":0}},"type":"message_start"}`,
		`content_block_start {"content_block":{"id":"call_1","input":{},"name":"lookup","type":"tool_use"},"index":0,"type":"content_block_start"}`,
		`content_block_delta {"delta":{"partial_json":"{\"q\":","type":"input_json_delta"},"index":0,"type":"content_block_delta"}`,
		`content_block_delta {"delta":{"partial_json":"\"x\"}","type":"input_json_delta"},"index":0,"type":"content_block_delta"}`,
		`content_block_stop {"index":0,"type":"content_block_stop"}`,
		`content_block_start {"content_block":{"text":"","type":"text"},"index":1,"type":"content_block_start"}`,
		`content_block_delta {"delta":{"text":"done","type":"text_delta"},"index":1,"type":"content_block_delta"}`,
		`content_block_stop {"index":1,"type":"content_block_stop"}`,
		`message_delta {"delta":{"stop_reason":"tool_use","stop_sequence":null},"type":"message_delta","usage":{"cache_read_input_tokens":2,"output_tokens":3}}`,
		`message_stop {"type":"message_stop"}`,
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events:\n%q\nwant:\n%q", events, want)
	}
}

func TestGeminiMessagesThinkingBlocks(t *testing.T) {
	parts := []any{
		map[string]any{"text": "a", "thought": true},
		map[string]any{"text": "abc", "thought": true, "thoughtSignature": "signed"},
		map[string]any{"text": "abc"},
		map[string]any{"functionCall": map[string]any{"name": "lookup"}},
		map[string]any{"text": "abcd"},
	}
	reply := func(parts []any) map[string]any {
		return map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": parts}, "finishReason": "STOP"}}}
	}
	id := func(map[string]any) string { return "call" }
	syncReply := GeminiToMessages(reply(parts[1:]), "model", "id", false, id, nil)
	blocks := syncReply["content"].([]any)
	require.Equal(t, map[string]any{"type": "thinking", "thinking": "abc", "signature": "signed"}, blocks[0])
	require.Equal(t, map[string]any{"type": "text", "text": "abc"}, blocks[1])
	history, err := convertClaudeMessagesToGeminiContents([]any{map[string]any{"role": "assistant", "content": blocks}}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, parts[1], history[0].(map[string]any)["parts"].([]any)[0])
	var got []string
	stream := NewGeminiMessagesStream(func(event string, raw any) {
		e := raw.(map[string]any)
		switch event {
		case "content_block_start":
			got = append(got, fmt.Sprintf("start %d %s", e["index"], e["content_block"].(map[string]any)["type"]))
		case "content_block_delta":
			d := e["delta"].(map[string]any)
			for _, field := range []string{"thinking", "signature", "text", "partial_json"} {
				if value, ok := d[field]; ok {
					got = append(got, fmt.Sprintf("%d %s %s", e["index"], d["type"], value))
				}
			}
		case "content_block_stop":
			got = append(got, fmt.Sprintf("stop %d", e["index"]))
		}
	}, func() {}, id, func(map[string]any, string, string, map[string]bool) error { return nil })
	for _, part := range parts {
		require.NoError(t, stream.Observe(reply([]any{part}), func() {}))
	}
	require.NoError(t, stream.Finish(0, 0, 0))
	require.Equal(t, []string{"start 0 thinking", "0 thinking_delta a", "0 thinking_delta bc", "0 signature_delta signed", "stop 0",
		"start 1 text", "1 text_delta abc", "stop 1", "start 2 tool_use", "2 input_json_delta {}", "stop 2",
		"start 3 text", "3 text_delta d", "stop 3"}, got)
}
