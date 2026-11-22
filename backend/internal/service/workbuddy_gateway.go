package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	until := time.Now().Add(time.Minute)
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
		if seconds > 86400 {
			seconds = 86400
		}
		until = time.Now().Add(time.Duration(seconds) * time.Second)
	}
	s.BlockAccountScheduling(a, until, "workbuddy_quota")
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	if s.rateLimitService != nil && s.rateLimitService.accountRepo != nil {
		_ = s.rateLimitService.accountRepo.SetTempUnschedulable(stateCtx, a.ID, until, "workbuddy_quota")
	} else if s.accountRepo != nil {
		_ = s.accountRepo.SetTempUnschedulable(stateCtx, a.ID, until, "workbuddy_quota")
	}
	failure := s.newOpenAIAccountFailoverError(a, http.StatusTooManyRequests, resp.Header, body, "WorkBuddy quota temporarily exhausted", true, false)
	return failure
}

type workBuddyStreamError struct{ payload []byte }

func (*workBuddyStreamError) Error() string { return "WorkBuddy stream returned an error" }

// Aggregate only a complete SSE response. Truncated streams and upstream error
// events must not produce successful completions or invented token counts.
func aggregateWorkBuddyChat(r io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, (32<<20)+1))
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	type choice struct {
		Message map[string]any
		Finish  any
		Tools   map[int]map[string]any
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
				c = &choice{Message: map[string]any{"role": "assistant", "content": ""}, Tools: map[int]map[string]any{}}
				choices[idx] = c
			}
			if f := entry["finish_reason"]; f != nil {
				c.Finish = f
			}
			delta, _ := entry["delta"].(map[string]any)
			for _, key := range []string{"content", "reasoning_content", "refusal"} {
				if v, ok := delta[key].(string); ok {
					old, _ := c.Message[key].(string)
					c.Message[key] = old + v
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
					dest = map[string]any{"type": "function", "function": map[string]any{"name": "", "arguments": ""}}
					c.Tools[ti] = dest
				}
				for _, k := range []string{"id", "type"} {
					if v, ok := t[k].(string); ok && v != "" {
						dest[k] = v
					}
				}
				fn, _ := t["function"].(map[string]any)
				target, ok := dest["function"].(map[string]any)
				if !ok {
					target = map[string]any{"name": "", "arguments": ""}
					dest["function"] = target
				}
				for _, k := range []string{"name", "arguments"} {
					if v, ok := fn[k].(string); ok {
						prev, _ := target[k].(string)
						target[k] = prev + v
					}
				}
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
		if len(c.Tools) > 0 {
			toolIndices := []int{}
			for i := range c.Tools {
				toolIndices = append(toolIndices, i)
			}
			sort.Ints(toolIndices)
			tools := []any{}
			for _, i := range toolIndices {
				tools = append(tools, c.Tools[i])
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
