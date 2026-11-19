package apicompat

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestGeminiMessagesStreamGolden(t *testing.T) {
	var events []string
	stream := NewGeminiMessagesStream(func(event string, data any) {
		wire, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event+" "+string(wire))
	}, func() {}, func(call map[string]any) string { return call["id"].(string) },
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
