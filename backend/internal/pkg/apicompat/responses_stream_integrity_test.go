package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesStreamIntegrityRecovery(t *testing.T) {
	message := func(id string, parts ...string) ResponsesOutput {
		item := ResponsesOutput{ID: id, Type: "message", Role: "assistant"}
		for _, text := range parts {
			item.Content = append(item.Content, ResponsesContentPart{Type: "output_text", Text: text})
		}
		return item
	}
	for _, terminal := range []string{"response.completed", "response.done", "response.incomplete"} {
		for _, tc := range []struct {
			name   string
			prefix []*ResponsesStreamEvent
			output []ResponsesOutput
			want   []string
		}{
			{"partial and missing outputs", []*ResponsesStreamEvent{
				{Type: "response.output_text.delta", ItemID: "a", OutputIndex: 2, Delta: "你\n"},
			}, []ResponsesOutput{message("a", "你\n好", "\n\nsecond"), message("b", "last")}, []string{"你\n", "好", "\n\nsecond", "last"}},
			{"terminal only", nil, []ResponsesOutput{message("a", "one", "two"), message("b", "three")}, []string{"one", "two", "three"}},
			{"item identity from added", []*ResponsesStreamEvent{
				{Type: "response.output_item.added", OutputIndex: 4, Item: &ResponsesOutput{Type: "message", ID: "a"}},
				{Type: "response.output_text.delta", OutputIndex: 4, Delta: "one"},
			}, []ResponsesOutput{message("a", "one tail")}, []string{"one", " tail"}},
			{"done hierarchy", []*ResponsesStreamEvent{
				{Type: "response.output_text.delta", ItemID: "a", Delta: "one"},
				{Type: "response.output_text.done", ItemID: "a", Text: "one tail"},
				{Type: "response.output_text.done", ItemID: "a", Text: "one tail"},
				{Type: "response.output_text.delta", ItemID: "a", Delta: "late"},
				{Type: "response.content_part.done", ItemID: "a", Part: &ResponsesContentPart{Type: "output_text", Text: "one tail"}},
				{Type: "response.output_item.done", Item: ptrOutput(message("a", "one tail", "second"))},
				{Type: "response.output_item.done", Item: ptrOutput(message("a", "one tail", "second"))},
			}, []ResponsesOutput{message("a", "one tail", "second")}, []string{"one", " tail", "second"}},
			{"identical text distinct items", []*ResponsesStreamEvent{
				{Type: "response.output_text.done", ItemID: "a", Text: "same"},
				{Type: "response.output_text.done", ItemID: "b", OutputIndex: 1, Text: "same"},
			}, []ResponsesOutput{message("a", "same"), message("b", "same")}, []string{"same", "same"}},
			{"anonymous delta with identified terminal", []*ResponsesStreamEvent{
				{Type: "response.output_text.delta", Delta: "one"},
			}, []ResponsesOutput{message("a", "one")}, []string{"one"}},
			{"identity introduced by done", []*ResponsesStreamEvent{
				{Type: "response.output_text.delta", Delta: "one"},
				{Type: "response.output_text.done", ItemID: "a", Text: "one tail"},
			}, []ResponsesOutput{message("a", "one tail")}, []string{"one", " tail"}},
			{"conflicting snapshot", []*ResponsesStreamEvent{
				{Type: "response.output_text.delta", ItemID: "a", Delta: "original"},
			}, []ResponsesOutput{message("a", "replacement")}, []string{"original"}},
		} {
			t.Run(terminal+"/"+tc.name, func(t *testing.T) {
				anth, chat := NewResponsesEventToAnthropicState(), NewResponsesEventToChatState()
				input := append([]*ResponsesStreamEvent{responsesCreated()}, tc.prefix...)
				end := &ResponsesStreamEvent{Type: terminal, Response: &ResponsesResponse{Status: "completed", Output: tc.output}}
				input = append(input, end, end, &ResponsesStreamEvent{Type: "response.output_text.delta", ItemID: "a", Delta: "after terminal"})
				var anthText, chatText []string
				var anthEvents []AnthropicStreamEvent
				var stops, finishes int
				for _, e := range input {
					for _, a := range ResponsesEventToAnthropicEvents(e, anth) {
						anthEvents = append(anthEvents, a)
						if a.Delta != nil && a.Delta.Type == "text_delta" {
							anthText = append(anthText, a.Delta.Text)
						}
						if a.Type == "message_stop" {
							stops++
						}
					}
					for _, c := range ResponsesEventToChatChunks(e, chat) {
						for _, choice := range c.Choices {
							if choice.Delta.Content != nil {
								chatText = append(chatText, *choice.Delta.Content)
							}
							if choice.FinishReason != nil {
								finishes++
							}
						}
					}
				}
				require.Equal(t, tc.want, anthText)
				require.Equal(t, append(append([]string{}, tc.want...), ""), chatText)
				require.Equal(t, 1, stops)
				require.Equal(t, 1, finishes)
				requireAnthropicBlockLifecycle(t, anthEvents)
				require.Empty(t, FinalizeResponsesAnthropicStream(anth))
				require.Empty(t, FinalizeResponsesChatStream(chat))
			})
		}
	}
}

func ptrOutput(output ResponsesOutput) *ResponsesOutput { return &output }

func TestResponsesStreamIntegritySequenceReplay(t *testing.T) {
	for _, sequenced := range []bool{false, true} {
		anth, chat := NewResponsesEventToAnthropicState(), NewResponsesEventToChatState()
		var aText, cText string
		for _, seq := range []int{0, 0, 1, 1} {
			wire := map[string]any{"type": "response.output_text.delta", "item_id": "a", "delta": "ha"}
			if sequenced {
				wire["sequence_number"] = seq
			}
			data, err := json.Marshal(wire)
			require.NoError(t, err)
			var e ResponsesStreamEvent
			require.NoError(t, json.Unmarshal(data, &e))
			for _, a := range ResponsesEventToAnthropicEvents(&e, anth) {
				if a.Delta != nil {
					aText += a.Delta.Text
				}
			}
			for _, c := range ResponsesEventToChatChunks(&e, chat) {
				for _, choice := range c.Choices {
					if choice.Delta.Content != nil {
						cText += *choice.Delta.Content
					}
				}
			}
		}
		want := "hahahaha"
		if sequenced {
			want = "haha"
		}
		require.Equal(t, want, aText)
		require.Equal(t, want, cText)
	}
}

func TestResponsesStreamIntegrityFailureNeverFinishesSuccessfully(t *testing.T) {
	for _, typ := range []string{"response.failed", "response.done", "error", "response.cancelled"} {
		anth, chat := NewResponsesEventToAnthropicState(), NewResponsesEventToChatState()
		e := &ResponsesStreamEvent{Type: typ, Response: &ResponsesResponse{Status: "failed", Error: &ResponsesError{Message: "broken"}}}
		a := ResponsesEventToAnthropicEvents(e, anth)
		require.Len(t, a, 1)
		require.Equal(t, "error", a[0].Type)
		require.Empty(t, ResponsesEventToChatChunks(e, chat))
		require.Empty(t, ResponsesEventToAnthropicEvents(e, anth))
		require.Empty(t, FinalizeResponsesAnthropicStream(anth))
		require.Empty(t, FinalizeResponsesChatStream(chat))
	}
}

func TestResponsesStreamIntegrityZeroSequenceOnAllEvents(t *testing.T) {
	for _, typ := range []string{"response.created", "response.output_text.delta", "response.completed", "response.failed", "response.done"} {
		data, err := json.Marshal(ResponsesStreamEvent{Type: typ})
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data, &fields))
		require.JSONEq(t, "0", string(fields["sequence_number"]))
	}
}
