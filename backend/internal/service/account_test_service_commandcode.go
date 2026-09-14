package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Test with actual inference: the public model catalog accepts even invalid keys.
func (s *AccountTestService) testCommandCodeAccountConnection(c *accountProbeContext, account *Account, modelID, prompt string) error {
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

	s.sendEvent(c, TestEvent{Type: "test_start", Model: modelID})
	// Retry only the observed transient edge routing failure, never a missing model.
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(c.ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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
			if attempt == 0 && isCommandCodeTransientUpstreamError(resp.StatusCode, responseBody) {
				continue
			}
			if code := commandCodePlanError(resp.StatusCode, responseBody); code != "" {
				return s.sendErrorAndEnd(c, fmt.Sprintf("Command Code plan does not grant access (%d %s): %s", resp.StatusCode, code, responseBody))
			}
			if isCommandCodeInvalidCredential(resp.StatusCode, responseBody) && s.accountRepo != nil {
				_ = s.accountRepo.SetError(c.ctx, account.ID, fmt.Sprintf("Command Code authentication failed (%d)", resp.StatusCode))
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("Command Code inference returned %d: %s", resp.StatusCode, responseBody))
		}
		defer func() { _ = resp.Body.Close() }()
		return s.processCommandCodeInferenceStream(c, resp.Body, protocol)
	}
	return s.sendErrorAndEnd(c, "Command Code inference failed")
}

// Use the same terminal-state checks as every other provider of this protocol.
func (s *AccountTestService) processCommandCodeInferenceStream(c *accountProbeContext, body io.Reader, protocol string) error {
	if protocol == APIProtocolAnthropic {
		return s.processClaudeStream(c, body)
	}
	return s.processOpenAIChatCompletionsStream(c, body)
}
