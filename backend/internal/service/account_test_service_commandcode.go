package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Test with actual inference: the public model catalog accepts even invalid keys.
func (s *AccountTestService) testCommandCodeAccountConnection(c *gin.Context, account *Account, modelID, prompt string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = DefaultCommandCodeTestModel
	}
	modelID = account.GetMappedModel(modelID)
	protocol := account.ResolveCommandCodeUpstreamProtocol(modelID)
	token := strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	if token == "" {
		return s.sendErrorAndEnd(c, "No API key available")
	}
	base := account.GetOpenAIBaseURL()
	if protocol == APIProtocolAnthropic {
		base = account.GetAnthropicProtocolBaseURL()
	}
	base, err := s.validateUpstreamBaseURL(base)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Invalid Command Code base URL: %s", err))
	}
	endpoint := buildOpenAIChatCompletionsURL(base)
	var payload any = createOpenAIChatCompletionsTestPayload(modelID, prompt)
	if protocol == APIProtocolAnthropic {
		endpoint = buildOpenAIEndpointURL(base, "/v1/messages")
		payload, err = createTestPayload(modelID)
		if err != nil {
			return s.sendErrorAndEnd(c, "Failed to create Anthropic test payload")
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to encode test payload")
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()
	s.sendEvent(c, TestEvent{Type: "test_start", Model: modelID})
	// Retry only the observed transient edge routing failure, never a missing model.
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return s.sendErrorAndEnd(c, "Failed to create inference request")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if protocol == APIProtocolAnthropic {
			req.Header.Set("anthropic-version", "2023-06-01")
			setAnthropicAPIKeyAuthHeader(req.Header, account, token, base)
		} else {
			setModelProviderChatAuthHeader(req.Header, account, token)
		}
		account.ApplyHeaderOverrides(req.Header)
		resp, err := s.doCNProviderAdaptiveRequest(req, account)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Command Code inference request failed: %s", err))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if readErr != nil {
				return s.sendErrorAndEnd(c, "Failed to read Command Code error")
			}
			if attempt == 0 && isCommandCodeTransientRouteError(resp.StatusCode, responseBody) {
				continue
			}
			if code := commandCodePlanError(resp.StatusCode, responseBody); code != "" {
				return s.sendErrorAndEnd(c, fmt.Sprintf("Command Code plan does not grant access (%d %s): %s", resp.StatusCode, code, responseBody))
			}
			if resp.StatusCode == http.StatusUnauthorized && s.accountRepo != nil {
				_ = s.accountRepo.SetError(c.Request.Context(), account.ID, "Command Code authentication failed (401)")
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("Command Code inference returned %d: %s", resp.StatusCode, responseBody))
		}
		defer func() { _ = resp.Body.Close() }()
		return s.processCommandCodeInferenceStream(c, resp.Body, protocol)
	}
	return s.sendErrorAndEnd(c, "Command Code inference failed")
}

// A public catalog, empty body, or prematurely closed stream is not proof of
// inference. Require a native completion marker after a real inference event.
func (s *AccountTestService) processCommandCodeInferenceStream(c *gin.Context, body io.Reader, protocol string) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	seenInference := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" && protocol == APIProtocolChatCompletions && seenInference {
			s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			return nil
		}
		if !gjson.Valid(data) {
			return s.sendErrorAndEnd(c, "Invalid Command Code inference stream")
		}
		event := gjson.Parse(data)
		if event.Get("error").Exists() || event.Get("type").String() == "error" {
			return s.sendErrorAndEnd(c, "Command Code inference error: "+event.Get("error.message").String())
		}
		if protocol == APIProtocolAnthropic {
			switch event.Get("type").String() {
			case "message_start", "content_block_delta":
				seenInference = true
			case "message_stop":
				if seenInference {
					s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
					return nil
				}
			}
			if text := event.Get("delta.text").String(); text != "" {
				s.sendEvent(c, TestEvent{Type: "content", Text: text})
			}
		} else {
			for _, choice := range event.Get("choices").Array() {
				if choice.Get("delta").Exists() || choice.Get("message").Exists() {
					seenInference = true
				}
				if text := choice.Get("delta.content").String(); text != "" {
					s.sendEvent(c, TestEvent{Type: "content", Text: text})
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return s.sendErrorAndEnd(c, "Command Code stream read error: "+err.Error())
	}
	return s.sendErrorAndEnd(c, "Command Code inference stream ended before completion")
}
