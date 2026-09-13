package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const qianwenNativeMaxFrame = 8 << 20

// observeQianwenNative preserves cumulative usage (also for incremental text).
// Missing usage in a later frame never erases tokens already observed.
func observeQianwenNative(body []byte, result *OpenAIForwardResult) error {
	if !gjson.ValidBytes(body) {
		return fmt.Errorf("invalid DashScope response JSON")
	}
	if id := gjson.GetBytes(body, "request_id").String(); id != "" {
		result.RequestID = id
	}
	if code := gjson.GetBytes(body, "code").String(); code != "" {
		return fmt.Errorf("DashScope response error: %s", code)
	}
	u := gjson.GetBytes(body, "usage")
	result.Usage.InputTokens = max(result.Usage.InputTokens, int(u.Get("input_tokens").Int()))
	result.Usage.OutputTokens = max(result.Usage.OutputTokens, int(u.Get("output_tokens").Int()))
	result.Usage.CacheReadInputTokens = max(result.Usage.CacheReadInputTokens, int(u.Get("prompt_tokens_details.cached_tokens").Int()))
	result.Usage.ImageInputTokens = max(result.Usage.ImageInputTokens, int(u.Get("image_tokens").Int()))
	return nil
}

// ForwardQianwenNative is a wire adapter under the shared Chat lifecycle.
// Request cancellation closes upstream IO; partial usage survives stream errors.
func (s *OpenAIGatewayService) ForwardQianwenNative(ctx context.Context, c *gin.Context, account *Account, operation QianwenNativeOperation, body []byte, stream bool) (*OpenAIForwardResult, error) {
	started := time.Now()
	clientCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	model, err := ParseQianwenNativeBody(body)
	if err != nil {
		return nil, err
	}
	body, thinking, err := prepareQianwenThinkingWithPolicy(ctx, c, body, body, account.GetMappedModel(model), "parameters.enable_thinking")
	if err == nil {
		err = s.validateQianwenThinkingMode(ctx, c, account.GetMappedModel(model), thinking)
	}
	if err != nil {
		status, kind := nativePolicyErrorResponse(err)
		writeChatCompletionsError(c, status, kind, err.Error())
		return nil, err
	}
	req, err := s.BuildQianwenNativeRequest(ctx, account, operation, body, stream)
	if err != nil {
		return nil, err
	}
	endpoint := req.URL.Path
	SetActualOpenAIUpstreamEndpoint(c, endpoint)
	SetOpsUpstreamModel(c, account.GetMappedModel(model))
	resp, err := s.doOpenAIUpstream(req, upstreamModelsProxyURL(account), account)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { cancel(); _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return s.handleChatCompletionsErrorResponse(resp, c, account, model)
	}
	result := &OpenAIForwardResult{Model: model, BillingModel: account.GetMappedModel(model), UpstreamModel: account.GetMappedModel(model), UpstreamEndpoint: endpoint, Stream: stream, UpstreamHeaders: resp.Header.Clone(), RequestID: resp.Header.Get("X-Request-Id")}
	result.ThinkingEnabled = thinking
	defer func() { result.Duration = time.Since(started); result.ClientDisconnect = clientCtx.Err() != nil }()
	if !stream || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		data, err := io.ReadAll(io.LimitReader(resp.Body, qianwenNativeMaxFrame+1))
		if err != nil {
			return nil, err
		}
		if len(data) > qianwenNativeMaxFrame {
			return nil, fmt.Errorf("DashScope response exceeds size limit")
		}
		if err := observeQianwenNative(data, result); err != nil {
			return nil, err
		}
		c.Data(resp.StatusCode, "application/json", data)
		return result, nil
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), qianwenNativeMaxFrame)
	pump := newAnthropicNativeLinePump(scanner, s.anthropicNativeStreamInterval())
	defer pump.stop()
	var frame, data strings.Builder
	terminal := false
	flush := func() error {
		if data.Len() > 0 {
			payload := strings.TrimSuffix(data.String(), "\n")
			if err := observeQianwenNative([]byte(payload), result); err != nil {
				return err
			}
			finish := gjson.Get(payload, "output.finish_reason").String()
			if finish != "" && finish != "null" {
				terminal = true
			}
			gjson.Get(payload, "output.choices").ForEach(func(_, choice gjson.Result) bool {
				finish := choice.Get("finish_reason").String()
				if finish != "" && finish != "null" {
					terminal = true
				}
				return true
			})
			if result.FirstTokenMs == nil {
				ms := int(time.Since(started).Milliseconds())
				result.FirstTokenMs = &ms
			}
		}
		if frame.Len() > 0 {
			if _, err := io.WriteString(c.Writer, frame.String()+"\n"); err != nil {
				return err
			}
			c.Writer.Flush()
		}
		frame.Reset()
		data.Reset()
		return nil
	}
	for {
		line, err := pump.next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return result, err
			}
			if err := flush(); err != nil {
				return result, err
			}
			if !terminal {
				return result, fmt.Errorf("DashScope stream ended without a terminal event")
			}
			return result, nil
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if line == "" {
			if err := flush(); err != nil {
				return result, err
			}
			if terminal {
				return result, nil
			}
			continue
		}
		if frame.Len()+len(line) > qianwenNativeMaxFrame {
			return result, fmt.Errorf("DashScope SSE frame exceeds size limit")
		}
		frame.WriteString(line)
		frame.WriteByte('\n')
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			data.WriteByte('\n')
		}
	}
}
