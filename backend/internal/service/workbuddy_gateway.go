package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func prepareWorkBuddyChat(body []byte) ([]byte, error) {
	var b map[string]any
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, err
	}
	messages, ok := b["messages"].([]any)
	if !ok || len(messages) == 0 {
		return nil, errors.New("WorkBuddy requires messages")
	}
	for _, raw := range messages {
		if m, ok := raw.(map[string]any); ok {
			if m["role"] == "developer" {
				m["role"] = "system"
			}
		}
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" {
		messages = append([]any{map[string]any{"role": "system", "content": "You are a helpful assistant."}}, messages...)
	} else if raw, _ := json.Marshal(first["content"]); strings.TrimSpace(workBuddyContentSignature(raw)) == "" {
		first["content"] = "You are a helpful assistant."
	}
	b["messages"] = messages
	b["stream"] = true
	options, _ := b["stream_options"].(map[string]any)
	if options == nil {
		options = map[string]any{}
	}
	options["include_usage"] = true
	b["stream_options"] = options
	if v, ok := b["max_completion_tokens"]; ok {
		if _, exists := b["max_tokens"]; !exists {
			b["max_tokens"] = v
		}
		delete(b, "max_completion_tokens")
	}
	return json.Marshal(b)
}
func workBuddyQuotaError(status int, body []byte) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	for _, path := range []string{"code", "error.code", "data.code"} {
		if gjson.GetBytes(body, path).String() == "14018" {
			return true
		}
	}
	message := strings.ToLower(firstNonEmpty(gjson.GetBytes(body, "msg").String(), gjson.GetBytes(body, "message").String(), gjson.GetBytes(body, "error.message").String()))
	return strings.Contains(message, "credits exhausted") || strings.Contains(message, "insufficient credits") || strings.Contains(message, "frequency limit") || strings.Contains(message, "额度已用尽")
}
func (s *OpenAIGatewayService) workBuddyFailover(ctx context.Context, a *Account, resp *http.Response, body []byte) *UpstreamFailoverError {
	if !workBuddyQuotaError(resp.StatusCode, body) {
		return nil
	}
	until := workBuddyCooldownUntil(resp.Header, time.Now())
	s.BlockAccountScheduling(a, until, "workbuddy_quota")
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	var persistErr error
	if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
		persistErr = s.rateLimitService.accountRepo.SetTempUnschedulable(stateCtx, a.ID, until, "workbuddy_quota")
	} else if s.accountRepo != nil {
		persistErr = s.accountRepo.SetTempUnschedulable(stateCtx, a.ID, until, "workbuddy_quota")
	}
	if persistErr != nil {
		slog.Warn("WorkBuddy quota cooldown persistence failed", "account_id", a.ID, "until", until)
	}
	failure := s.newOpenAIAccountFailoverError(a, http.StatusTooManyRequests, resp.Header, body, "WorkBuddy quota temporarily exhausted", true, false)
	return failure
}

func workBuddyCooldownUntil(header http.Header, now time.Time) time.Time {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return now.Add(time.Duration(min(seconds, 86400)) * time.Second)
	}
	if reset, err := http.ParseTime(value); err == nil && reset.After(now) {
		return now.Add(min(reset.Sub(now), 24*time.Hour))
	}
	return now.Add(time.Minute)
}

type workBuddyStreamError struct{ payload []byte }

func (*workBuddyStreamError) Error() string { return "WorkBuddy stream returned an error" }

// Aggregate only a complete SSE response. Truncated streams and upstream error
// events must not produce successful completions or invented token counts.
func aggregateWorkBuddyChat(r io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, (32<<20)+1))
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	type toolCall struct {
		ID        string
		Type      string
		Name      strings.Builder
		Arguments strings.Builder
	}
	type choice struct {
		Text    map[string]*strings.Builder
		Message map[string]any
		Finish  any
		Tools   map[int]*toolCall
	}
	choices := map[int]*choice{}
	out := map[string]any{"object": "chat.completion"}
	done := false
	usage := false
	size := 0
	for scanner.Scan() {
		line := scanner.Text()
		size += len(line) + 1
		if size > 32<<20 {
			return nil, errors.New("WorkBuddy response exceeds size limit")
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			done = true
			break
		}
		if payload == "" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return nil, fmt.Errorf("invalid WorkBuddy SSE chunk: %w", err)
		}
		if chunk["error"] != nil || gjson.Get(payload, "code").Int() != 0 || workBuddyQuotaError(200, []byte(payload)) {
			return nil, &workBuddyStreamError{payload: []byte(payload)}
		}
		for _, key := range []string{"id", "model", "created", "system_fingerprint", "service_tier"} {
			if v, ok := chunk[key]; ok {
				out[key] = v
			}
		}
		if u, ok := chunk["usage"].(map[string]any); ok {
			if _, p := u["prompt_tokens"]; p {
				if _, c := u["completion_tokens"]; c {
					out["usage"] = u
					usage = true
				}
			}
		}
		entries, _ := chunk["choices"].([]any)
		for _, raw := range entries {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			n, _ := entry["index"].(float64)
			idx := int(n)
			c := choices[idx]
			if c == nil {
				c = &choice{Text: map[string]*strings.Builder{}, Message: map[string]any{"role": "assistant", "content": ""}, Tools: map[int]*toolCall{}}
				choices[idx] = c
			}
			if f := entry["finish_reason"]; f != nil {
				c.Finish = f
			}
			delta, _ := entry["delta"].(map[string]any)
			for _, key := range []string{"content", "reasoning_content", "refusal"} {
				if v, ok := delta[key].(string); ok {
					if c.Text[key] == nil {
						c.Text[key] = &strings.Builder{}
					}
					c.Text[key].WriteString(v)
				}
			}
			tools, _ := delta["tool_calls"].([]any)
			for _, raw := range tools {
				t, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				tn, _ := t["index"].(float64)
				ti := int(tn)
				dest := c.Tools[ti]
				if dest == nil {
					dest = &toolCall{Type: "function"}
					c.Tools[ti] = dest
				}
				if id, _ := t["id"].(string); id != "" {
					dest.ID = id
				}
				if kind, _ := t["type"].(string); kind != "" {
					dest.Type = kind
				}
				fn, _ := t["function"].(map[string]any)
				name, _ := fn["name"].(string)
				args, _ := fn["arguments"].(string)
				dest.Name.WriteString(name)
				dest.Arguments.WriteString(args)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !done || len(choices) == 0 || !usage {
		return nil, errors.New("WorkBuddy stream ended without complete usage and terminal event")
	}
	indices := []int{}
	for i := range choices {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	result := []any{}
	for _, idx := range indices {
		c := choices[idx]
		if c.Finish == nil {
			return nil, errors.New("WorkBuddy stream ended without finish reason")
		}
		for key, value := range c.Text {
			c.Message[key] = value.String()
		}
		if len(c.Tools) > 0 {
			toolIndices := []int{}
			for i := range c.Tools {
				toolIndices = append(toolIndices, i)
			}
			sort.Ints(toolIndices)
			tools := []any{}
			for _, i := range toolIndices {
				call := c.Tools[i]
				tool := map[string]any{"type": call.Type, "function": map[string]string{"name": call.Name.String(), "arguments": call.Arguments.String()}}
				if call.ID != "" {
					tool["id"] = call.ID
				}
				tools = append(tools, tool)
			}
			c.Message["tool_calls"] = tools
			if c.Message["content"] == "" {
				c.Message["content"] = nil
			}
		}
		result = append(result, map[string]any{"index": idx, "message": c.Message, "finish_reason": c.Finish})
	}
	out["choices"] = result
	return json.Marshal(out)
}

// Inspect the first event before committing a downstream response, allowing a
// quota error carried under HTTP 200 to move to the next account in the pool.
func (s *OpenAIGatewayService) normalizeWorkBuddyResponse(ctx context.Context, a *Account, resp *http.Response, stream bool) (*http.Response, error) {
	// HTTP headers precede both first-frame inspection and synchronous aggregation.
	if received, ok := ctx.Value(responseHeaderTimerKey{}).(func()); ok {
		received()
	}
	if resp.StatusCode >= 400 || !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		body, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
		_ = resp.Body.Close()
		if err != nil || len(body) > 8<<20 {
			return nil, errors.New("WorkBuddy response could not be read")
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if failure := s.workBuddyFailover(ctx, a, resp, body); failure != nil {
			return nil, failure
		}
		if resp.StatusCode < 400 {
			resp.StatusCode = http.StatusBadGateway
		}
		return resp, nil
	}
	original := resp.Body
	reader := bufio.NewReaderSize(original, 4<<20)
	var prefix bytes.Buffer
	for prefix.Len() < 4<<20 {
		fragment, err := reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			_ = original.Close()
			return nil, errors.New("WorkBuddy SSE event too large")
		}
		line := string(fragment)
		prefix.Write(fragment)
		if strings.HasPrefix(line, "data:") {
			data := []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			if failure := s.workBuddyFailover(ctx, a, resp, data); failure != nil {
				_ = original.Close()
				return nil, failure
			}
			if gjson.GetBytes(data, "error").Exists() || gjson.GetBytes(data, "code").Int() != 0 {
				_ = original.Close()
				return nil, errors.New("WorkBuddy stream returned an error")
			}
			break
		}
		if err != nil {
			_ = original.Close()
			return nil, errors.New("WorkBuddy stream ended before a response")
		}
	}
	if prefix.Len() >= 4<<20 {
		_ = original.Close()
		return nil, errors.New("WorkBuddy SSE preamble too large")
	}
	resp.Body = &workBuddyReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), reader), Closer: original}
	if stream {
		return resp, nil
	}
	body, err := aggregateWorkBuddyChat(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		var upstream *workBuddyStreamError
		if errors.As(err, &upstream) {
			if failure := s.workBuddyFailover(ctx, a, resp, upstream.payload); failure != nil {
				return nil, failure
			}
		}
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Del("Content-Length")
	return resp, nil
}

type workBuddyReadCloser struct {
	io.Reader
	io.Closer
}
