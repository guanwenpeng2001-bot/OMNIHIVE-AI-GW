package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/gatewayerror"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Native JSON streams are copied as received while observing each element for
// billing. Only Code Assist's mandatory SSE wrapper needs conversion to an array.
func (s *GeminiMessagesCompatService) handleNativeJSONStreamingResponse(ctx *gin.Context, response *http.Response, started time.Time, oauth, upstreamSSE bool, account *Account, requestID string) (*geminiNativeStreamResult, error) {
	responseheaders.WriteFilteredHeaders(ctx.Writer.Header(), response.Header, s.responseHeaderFilter)
	ctx.Header("Content-Type", "application/json")
	ctx.Writer.Header().Del("Content-Length")
	ctx.Status(response.StatusCode)
	result := &geminiNativeStreamResult{usage: &ClaudeUsage{}}
	observer := upstreamResponseModelObserverFromContext(ctx)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(ctx)
	}
	observe := func(body []byte) {
		if usage := extractGeminiUsage(body); usage != nil {
			result.usage = usage
		}
		observer.ObserveGemini(body)
		observeGeminiImageOutputs(ctx, body)
		if signal, ok := detectGeminiResponseSignal(body); ok {
			s.markGeminiResponseSignal(ctx, account, signal, true, requestID)
		}
		if result.firstTokenMs == nil {
			elapsed := int(time.Since(started).Milliseconds())
			result.firstTokenMs = &elapsed
		}
	}
	if upstreamSSE {
		reader := newUpstreamFrameReader(response.Body, defaultMaxLineSize, ctx.Request.Context(), 300*time.Second)
		defer reader.Close()
		if _, err := io.WriteString(ctx.Writer, "["); err != nil {
			return nil, err
		}
		separator := ""
		for reader.Scan() {
			payload := strings.TrimSpace(reader.frame.Data)
			if payload == "" || payload == "[DONE]" {
				continue
			}
			body := []byte(payload)
			if oauth {
				unwrapped, err := unwrapGeminiResponse(body)
				if err != nil {
					return nil, err
				}
				body = unwrapped
			}
			if !json.Valid(body) {
				return nil, errors.New("invalid upstream JSON stream")
			}
			observe(body)
			body, _ = sanitizeGoogleErrorPayload(body)
			if _, err := io.WriteString(ctx.Writer, separator+string(body)); err != nil {
				return nil, err
			}
			separator = ","
			ctx.Writer.Flush()
		}
		if err := reader.Err(); err != nil {
			return nil, err
		}
		_, err := io.WriteString(ctx.Writer, "]")
		return result, err
	}
	var pending bytes.Buffer
	decoder := json.NewDecoder(io.TeeReader(response.Body, &pending))
	var consumed int64
	flushParsed := func() error {
		length := decoder.InputOffset() - consumed
		_, err := ctx.Writer.Write(pending.Next(int(length)))
		consumed = decoder.InputOffset()
		return err
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('[') {
		return nil, errors.New("upstream JSON stream must be an array")
	}
	if err := flushParsed(); err != nil {
		return nil, err
	}
	for decoder.More() {
		var body json.RawMessage
		if err := decoder.Decode(&body); err != nil {
			return nil, err
		}
		observe(body)
		if sanitized, changed := sanitizeGoogleErrorPayload(body); changed {
			parsed := pending.Next(int(decoder.InputOffset() - consumed))
			prefix := parsed[:len(parsed)-len(body)]
			if _, err := ctx.Writer.Write(prefix); err != nil {
				return nil, err
			}
			if _, err := ctx.Writer.Write(sanitized); err != nil {
				return nil, err
			}
			consumed = decoder.InputOffset()
		} else if err := flushParsed(); err != nil {
			return nil, err
		}
		ctx.Writer.Flush()
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if err := flushParsed(); err != nil {
		return nil, err
	}
	return result, nil
}

func sanitizeGoogleErrorPayload(body []byte) ([]byte, bool) {
	if !gjson.GetBytes(body, "error").Exists() {
		return body, false
	}
	status := int(gjson.GetBytes(body, "error.code").Int())
	if status < 400 || status > 599 {
		status = http.StatusBadGateway
	}
	message := gjson.GetBytes(body, "error.message").String()
	if message == "" {
		message = "Upstream request failed"
	}
	_, envelope := gatewayerror.Envelope("google", status, "upstream_error", message)
	sanitized, err := json.Marshal(envelope)
	if err != nil {
		return body, false
	}
	return sanitized, true
}

// A provider may ignore alt=sse and return JSON. Convert only that representation
// mismatch; normal native SSE continues through the existing streaming reader.
func (s *GeminiMessagesCompatService) handleNativeJSONAsSSE(ctx *gin.Context, response *http.Response, started time.Time, oauth bool, account *Account, requestID string) (*geminiNativeStreamResult, error) {
	responseheaders.WriteFilteredHeaders(ctx.Writer.Header(), response.Header, s.responseHeaderFilter)
	ctx.Header("Content-Type", "text/event-stream")
	ctx.Writer.Header().Del("Content-Length")
	ctx.Status(response.StatusCode)
	result := &geminiNativeStreamResult{usage: &ClaudeUsage{}}
	observer := upstreamResponseModelObserverFromContext(ctx)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(ctx)
	}
	var best geminiResponseSignal
	allEmpty := true
	finish := func() {
		if best.Kind > 0 {
			s.markGeminiResponseSignal(ctx, account, best, true, requestID)
		} else if allEmpty {
			s.markGeminiEmptyResponse(ctx, account, true, requestID)
		}
	}
	reader := bufio.NewReader(response.Body)
	var leading byte
	for {
		prefix, err := reader.Peek(1)
		if err != nil {
			if errors.Is(err, io.EOF) {
				finish()
				return result, nil
			}
			return nil, err
		}
		leading = prefix[0]
		if !strings.ContainsRune(" \r\n\t", rune(leading)) {
			break
		}
		_, _ = reader.Discard(1)
	}
	decoder := json.NewDecoder(reader)
	emit := func() error {
		var body json.RawMessage
		if err := decoder.Decode(&body); err != nil {
			return err
		}
		if oauth {
			unwrapped, err := unwrapGeminiResponse(body)
			if err != nil {
				return err
			}
			body = unwrapped
		}
		observer.ObserveGemini(body)
		observeGeminiImageOutputs(ctx, body)
		if usage := extractGeminiUsage(body); usage != nil {
			result.usage = usage
		}
		if signal, ok := detectGeminiResponseSignal(body); ok && signal.Kind > best.Kind {
			best = signal
		}
		if !isGeminiEmptyResponseBody(body) {
			allEmpty = false
		}
		if result.firstTokenMs == nil {
			elapsed := int(time.Since(started).Milliseconds())
			result.firstTokenMs = &elapsed
		}
		body, _ = sanitizeGoogleErrorPayload(body)
		if _, err := fmt.Fprintf(ctx.Writer, "data: %s\n\n", body); err != nil {
			return err
		}
		ctx.Writer.Flush()
		return nil
	}
	switch leading {
	case '[':
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		for decoder.More() {
			if err := emit(); err != nil {
				return nil, err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
	case '{', 'n':
		if err := emit(); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("invalid upstream JSON representation")
	}
	finish()
	return result, nil
}
