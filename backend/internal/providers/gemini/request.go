package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/geminicli"
)

const RequestIDHeader = "x-request-id"

type Action struct {
	Model  string
	Name   string
	Stream bool
}

func (a Action) CodeAssist(ctx context.Context, baseURL, project, token string, body []byte) (*http.Request, error) {
	fullURL := strings.TrimRight(baseURL, "/") + "/v1internal:" + a.Name
	if a.Stream {
		fullURL += "?alt=sse"
	}
	var inner any
	if err := json.Unmarshal(body, &inner); err != nil {
		return nil, fmt.Errorf("failed to parse gemini request: %w", err)
	}
	wrapped, err := json.Marshal(map[string]any{"model": a.Model, "project": project, "request": inner})
	if err != nil {
		return nil, err
	}
	req, err := Generate(ctx, fullURL, "", token, wrapped)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", geminicli.GeminiCLIUserAgent)
	return req, nil
}

func ValidateAPIKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("gemini api_key not configured")
	}
	return nil
}

func Generate(ctx context.Context, fullURL, apiKey, token string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("x-goog-api-key", apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}
