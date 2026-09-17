package apicompat

import "strings"

// ResponsesTerminalSentKey guards synthetic terminal writes on a request context.
const ResponsesTerminalSentKey = "responses_terminal_sent"

// responsesTextTracker reconciles snapshots with text actually sent downstream.
// Item IDs survive providers that compact/reorder the terminal output array.
// Without IDs, terminal indices cannot safely be matched to streamed indices.
type responsesTextTracker struct {
	ids          map[int]string
	text         map[responsesTextKey]*strings.Builder
	done         map[responsesTextKey]bool
	lastSequence int
	hasSequence  bool
}

type responsesTextKey struct {
	id              string
	output, content int
}

func (t *responsesTextTracker) accept(e *ResponsesStreamEvent) bool {
	if e.sequencePresent || e.SequenceNumber != 0 {
		if t.hasSequence && e.SequenceNumber <= t.lastSequence {
			return false
		}
		t.lastSequence, t.hasSequence = e.SequenceNumber, true
	}
	if t.ids == nil {
		t.ids = make(map[int]string)
		t.text = make(map[responsesTextKey]*strings.Builder)
		t.done = make(map[responsesTextKey]bool)
	}
	id := e.ItemID
	if e.Item != nil && e.Item.ID != "" {
		id = e.Item.ID
	}
	if id != "" {
		t.ids[e.OutputIndex] = id
		// Some providers introduce item_id only in the done event.
		for key, text := range t.text {
			if key.id == "" && key.output == e.OutputIndex {
				next := responsesTextKey{id: id, content: key.content}
				t.text[next], t.done[next] = text, t.done[key]
				delete(t.text, key)
				delete(t.done, key)
			}
		}
	}
	return true
}

func (t *responsesTextTracker) key(output, content int, id string) responsesTextKey {
	if id == "" {
		id = t.ids[output]
	}
	if id != "" {
		return responsesTextKey{id: id, content: content}
	}
	return responsesTextKey{output: output, content: content}
}

func (t *responsesTextTracker) delta(e *ResponsesStreamEvent) string {
	key := t.key(e.OutputIndex, e.ContentIndex, e.ItemID)
	if t.done[key] || e.Delta == "" {
		return ""
	}
	if t.text[key] == nil {
		t.text[key] = &strings.Builder{}
	}
	t.text[key].WriteString(e.Delta)
	return e.Delta
}

func (t *responsesTextTracker) recover(output, content int, id, text string) string {
	key := t.key(output, content, id)
	builder, known := t.text[key]
	current := ""
	if known {
		current = builder.String()
	}
	if !known && key.id == "" && len(t.text) > 0 {
		return ""
	}
	if !strings.HasPrefix(text, current) {
		return ""
	}
	tail := text[len(current):]
	if builder == nil {
		builder = &strings.Builder{}
		t.text[key] = builder
	}
	builder.WriteString(tail)
	t.done[key] = true
	return tail
}

// terminalText visits only assistant text. Take the conservative no-ID decision
// once, before recovering any part, so terminal-only multi-part output is whole.
func (t *responsesTextTracker) terminalText(r *ResponsesResponse, emit func(string)) {
	if r == nil {
		return
	}
	hadText := len(t.text) > 0
	anonymous := false
	knownIDs := make(map[string]bool)
	for key := range t.text {
		if key.id == "" {
			anonymous = true
		} else {
			knownIDs[key.id] = true
		}
	}
	for i, item := range r.Output {
		if item.Type != "message" || (hadText && item.ID == "") || (anonymous && !knownIDs[item.ID]) {
			continue
		}
		for j, part := range item.Content {
			if part.Type != "output_text" {
				continue
			}
			if !hadText && item.ID == "" {
				key := t.key(i, j, "")
				builder := &strings.Builder{}
				builder.WriteString(part.Text)
				t.text[key], t.done[key] = builder, true
				emit(part.Text)
			} else {
				emit(t.recover(i, j, item.ID, part.Text))
			}
		}
	}
}

// ResponsesStreamEventFailed also recognizes failures carried by WS response.done.
func ResponsesStreamEventFailed(e *ResponsesStreamEvent) bool {
	return e.Type == "error" || e.Type == "response.cancelled" || e.Type == "response.canceled" ||
		(e.Response != nil && (e.Response.Status == "failed" || e.Response.Error != nil)) ||
		e.Type == "response.failed"
}
