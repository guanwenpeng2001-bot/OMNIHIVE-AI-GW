package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
)

func cursorRelayURL(account *Account, endpoint string) string {
	base := strings.TrimRight(account.GetCredential("base_url"), "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + endpoint
}

func (gateway *CursorGatewayService) forwardRelayChat(ctx context.Context, requestContext *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	started := time.Now()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var model string
	var stream bool
	_ = json.Unmarshal(payload["model"], &model)
	_ = json.Unmarshal(payload["stream"], &stream)
	upstreamModel := account.GetMappedModel(model)
	payload["model"], _ = json.Marshal(upstreamModel)
	if stream {
		payload["stream_options"] = json.RawMessage(`{"include_usage":true}`)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cursorRelayURL(account, "/chat/completions"), bytes.NewReader(encoded))
	if err != nil {
		return nil, cursorFailover(err)
	}
	request.Header.Set("Authorization", "Bearer "+account.GetCredential("api_key"))
	request.Header.Set("Content-Type", "application/json")
	response, err := gateway.relayClient.Do(request)
	if err != nil {
		return nil, cursorFailover(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, cursorFailover(&cursor.HTTPError{Status: response.StatusCode})
	}
	result := &ForwardResult{Model: model, UpstreamModel: upstreamModel, Stream: stream, UpstreamHeaders: response.Header.Clone()}
	observe := func(data []byte) error {
		var envelope struct {
			ID    string          `json:"id"`
			Error json.RawMessage `json:"error"`
			Usage struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
				Details    struct {
					Cached int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return fmt.Errorf("cursor: malformed relay response")
		}
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			return fmt.Errorf("cursor: relay error")
		}
		if envelope.ID != "" {
			result.RequestID = envelope.ID
		}
		if envelope.Usage.Prompt > 0 || envelope.Usage.Completion > 0 {
			result.Usage = ClaudeUsage{InputTokens: max(0, envelope.Usage.Prompt-envelope.Usage.Details.Cached), OutputTokens: envelope.Usage.Completion, CacheReadInputTokens: envelope.Usage.Details.Cached}
		}
		return nil
	}
	if !stream {
		data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20+1))
		if err != nil || len(data) > 16<<20 {
			return nil, cursorFailover(fmt.Errorf("cursor: relay body read failed"))
		}
		if err := observe(data); err != nil {
			return nil, cursorFailover(err)
		}
		requestContext.Data(http.StatusOK, "application/json", data)
		result.Duration = time.Since(started)
		return result, nil
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	emitted := false
	done := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		if data == "" {
			continue
		}
		if err := observe([]byte(data)); err != nil {
			if !emitted {
				return nil, cursorFailover(err)
			}
			result.Duration = time.Since(started)
			return result, err
		}
		if !emitted {
			requestContext.Header("Content-Type", "text/event-stream")
			requestContext.Header("Cache-Control", "no-cache")
			elapsed := int(time.Since(started).Milliseconds())
			result.FirstTokenMs = &elapsed
			emitted = true
		}
		if _, err := fmt.Fprintf(requestContext.Writer, "data: %s\n\n", data); err != nil {
			result.ClientDisconnect = true
			return result, err
		}
		requestContext.Writer.Flush()
	}
	result.Duration = time.Since(started)
	if err := scanner.Err(); err != nil {
		if !emitted {
			return nil, cursorFailover(err)
		}
		return result, err
	}
	if !done {
		if !emitted {
			return nil, cursorFailover(io.ErrUnexpectedEOF)
		}
		return result, io.ErrUnexpectedEOF
	}
	if !emitted {
		requestContext.Header("Content-Type", "text/event-stream")
	}
	_, err = fmt.Fprint(requestContext.Writer, "data: [DONE]\n\n")
	requestContext.Writer.Flush()
	return result, err
}
