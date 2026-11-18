package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
)

func DefaultCursorModelIDs() []string {
	return NewModelRegistry().Snapshot().CandidateIDs(PlatformCursor, "")
}

func (gateway *CursorGatewayService) AvailableModels(ctx context.Context, account *Account) ([]string, error) {
	ctx, egressErr := cursorAccountContext(ctx, account)
	if egressErr != nil {
		return nil, egressErr
	}
	if gateway == nil {
		return nil, fmt.Errorf("cursor: service unavailable")
	}
	if account.Type == AccountTypeUpstream {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, cursorRelayURL(account, "/models"), nil)
		if err != nil {
			return nil, err
		}
		applyCursorRelayHeaders(request.Header, account)
		response, err := cursor.Do(gateway.relayClient, request, false)
		if err != nil {
			return nil, fmt.Errorf("cursor: relay models unavailable")
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusOK {
			return nil, &cursor.HTTPError{Status: response.StatusCode}
		}
		var catalog struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&catalog); err != nil {
			return nil, fmt.Errorf("cursor: invalid relay catalog")
		}
		var ids []string
		for _, model := range catalog.Data {
			if model.ID != "" {
				ids = append(ids, model.ID)
			}
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("cursor: empty relay catalog")
		}
		return ids, nil
	}
	token, err := gateway.sessions.GetSessionToken(ctx, account, "")
	if err != nil {
		return nil, err
	}
	// Explicit synchronization must report upstream failures, not fallback IDs.
	models, err := gateway.native.AvailableModels(ctx, gateway.credentials(ctx, account, token))
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("cursor: empty model catalog")
	}
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids, nil
}

func (service *AccountTestService) probeTestCursorAccountConnection(requestContext *accountProbeContext, account *Account, modelID, mode string) error {

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "" && mode != AccountTestModeDefault && mode != "text" {
		return service.sendErrorAndEnd(requestContext, "Cursor test mode unsupported: use default or text")
	}
	modelID = strings.TrimSpace(modelID)
	service.sendEvent(requestContext, TestEvent{Type: "test_start", Model: modelID})
	gateway := service.cursorGatewayService
	ctx, cancel := context.WithTimeout(requestContext.ctx, 60*time.Second)
	defer cancel()
	if gateway == nil {
		return service.sendErrorAndEnd(requestContext, "Cursor service unavailable")
	}
	if account.Type == AccountTypeUpstream {
		if modelID == "" {
			models, err := gateway.AvailableModels(ctx, account)
			if err != nil {
				return service.sendErrorAndEnd(requestContext, cursorProbeFailure(ctx, "relay model", err))
			}
			modelID = models[0]
		}
		// Exercise the same mapping, protocol conversion and headers as live traffic.
		body, _ := json.Marshal(map[string]any{"model": modelID, "messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}, "stream": false, "max_tokens": 1024})
		recorder := httptest.NewRecorder()
		probeContext, _ := gin.CreateTestContext(recorder)
		probeContext.Request = (&http.Request{}).WithContext(ctx)
		_, err := gateway.forwardRelayChat(ctx, probeContext, account, body)
		var response struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err != nil {
			return service.sendErrorAndEnd(requestContext, cursorProbeFailure(ctx, "relay conversation", err))
		}
		if json.Unmarshal(recorder.Body.Bytes(), &response) != nil {
			return service.sendErrorAndEnd(requestContext, "Cursor relay conversation probe failed: invalid response")
		}
		if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
			return service.sendErrorAndEnd(requestContext, "Cursor relay conversation probe failed: no text returned")
		}
	} else {
		// Share the native account lock with gateway requests, including probes.
		release, err := gateway.acquireNativeSlot(ctx, account)
		if err != nil {
			return service.sendErrorAndEnd(requestContext, cursorProbeFailure(ctx, "native slot", err))
		}
		defer release()
		token, err := gateway.sessions.GetSessionToken(ctx, account, "")
		if err != nil {
			return service.sendErrorAndEnd(requestContext, cursorProbeFailure(ctx, "exchange/authentication", err))
		}
		credentials := gateway.credentials(ctx, account, token)
		models, err := gateway.native.AvailableModels(ctx, credentials)
		if err != nil {
			return service.sendErrorAndEnd(requestContext, cursorProbeFailure(ctx, "picker", err))
		}
		if len(models) == 0 {
			return service.sendErrorAndEnd(requestContext, "Cursor picker probe failed: empty model catalog")
		}
		if modelID == "" {
			modelID = models[0].ID
		}
		model, err := cursor.ResolveModel(account.GetMappedModel(modelID), "", models)
		if err != nil {
			return service.sendErrorAndEnd(requestContext, "Cursor picker model resolution failed")
		}
		textReceived := false
		_, err = gateway.native.Run(ctx, credentials, []cursor.ChatMessage{{Role: "user", Content: "Reply with OK."}}, model, func(event cursor.StreamEvent) error {
			textReceived = textReceived || (event.Type == "text" && event.Text != "")
			return nil
		})
		if err != nil {
			return service.sendErrorAndEnd(requestContext, cursorProbeFailure(ctx, "Run/protocol", err))
		}
		if !textReceived {
			return service.sendErrorAndEnd(requestContext, "Cursor Run/protocol probe failed: stream completed without text")
		}
	}
	service.sendEvent(requestContext, TestEvent{Type: "test_complete", Success: true})
	return nil
}

// Only typed status and allowlisted protocol codes are safe for public probe events.
// Error messages (including Connect messages and unrecognized codes) may contain secrets.
func cursorProbeFailure(ctx context.Context, phase string, err error) string {
	prefix := "Cursor " + phase + " probe failed: "
	if errors.Is(err, context.Canceled) {
		return prefix + "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return prefix + "deadline exceeded"
	}
	var httpError *cursor.HTTPError
	if errors.As(err, &httpError) && httpError != nil {
		return fmt.Sprintf("%supstream HTTP %d", prefix, httpError.Status)
	}
	var connectError *cursor.ConnectError
	if errors.As(err, &connectError) && connectError != nil {
		switch connectError.Code {
		case "canceled", "unknown", "invalid_argument", "deadline_exceeded", "not_found",
			"already_exists", "permission_denied", "resource_exhausted", "failed_precondition",
			"aborted", "out_of_range", "unimplemented", "internal", "unavailable", "data_loss", "unauthenticated":
			return prefix + "Connect code " + connectError.Code
		}
	}
	// Some client transport paths intentionally discard the original error.
	if errors.Is(ctx.Err(), context.Canceled) {
		return prefix + "canceled"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return prefix + "deadline exceeded"
	}
	return prefix + "unclassified upstream or protocol error"
}
