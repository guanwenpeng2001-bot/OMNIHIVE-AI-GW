package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
)

func DefaultCursorModelIDs() []string {
	models := cursor.FallbackModels()
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func (gateway *CursorGatewayService) AvailableModels(ctx context.Context, account *Account) ([]string, error) {
	if gateway == nil {
		return nil, fmt.Errorf("cursor: service unavailable")
	}
	if account.Type == AccountTypeUpstream {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, cursorRelayURL(account, "/models"), nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+account.GetCredential("api_key"))
		response, err := gateway.relayClient.Do(request)
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
	models := gateway.picker(ctx, account, gateway.credentials(ctx, account, token))
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids, nil
}

func (service *AccountTestService) testCursorAccountConnection(requestContext *gin.Context, account *Account) error {
	requestContext.Header("Content-Type", "text/event-stream")
	requestContext.Header("Cache-Control", "no-cache")
	service.sendEvent(requestContext, TestEvent{Type: "test_start", Model: "Cursor model catalog"})
	gateway := service.cursorGatewayService
	ctx := requestContext.Request.Context()
	if gateway == nil {
		return service.sendErrorAndEnd(requestContext, "Cursor service unavailable")
	}
	if account.Type == AccountTypeUpstream {
		if _, err := gateway.AvailableModels(ctx, account); err != nil {
			return service.sendErrorAndEnd(requestContext, "Cursor relay model probe failed")
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
		model, err := cursor.ResolveModel(models[0].ID, "", models)
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
