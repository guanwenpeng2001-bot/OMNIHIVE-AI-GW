package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
)

func DefaultCursorModelIDs() []string {
	return NewModelRegistry().Snapshot().CandidateIDs(PlatformCursor, "")
}

func (gateway *CursorGatewayService) AvailableModels(ctx context.Context, account *Account) ([]string, error) {
	ctx = cursorAccountContext(ctx, account)
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
		defer response.Body.Close()
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
				return service.sendErrorAndEnd(requestContext, "Cursor relay model probe failed")
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
		if err != nil || json.Unmarshal(recorder.Body.Bytes(), &response) != nil || len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
			return service.sendErrorAndEnd(requestContext, "Cursor relay conversation probe failed")
		}
	} else {
		// Share the native account lock with gateway requests, including probes.
		release, err := gateway.acquireNativeSlot(ctx, account)
		if err != nil {
			return service.sendErrorAndEnd(requestContext, "Cursor probe canceled")
		}
		defer release()
		token, err := gateway.sessions.GetSessionToken(ctx, account, "")
		if err != nil {
			return service.sendErrorAndEnd(requestContext, "Cursor exchange/authentication probe failed")
		}
		credentials := gateway.credentials(ctx, account, token)
		models, err := gateway.native.AvailableModels(ctx, credentials)
		if err != nil || len(models) == 0 {
			return service.sendErrorAndEnd(requestContext, "Cursor picker probe failed")
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
		if err != nil || !textReceived {
			return service.sendErrorAndEnd(requestContext, "Cursor Run/protocol probe failed")
		}
	}
	service.sendEvent(requestContext, TestEvent{Type: "test_complete", Success: true})
	return nil
}
