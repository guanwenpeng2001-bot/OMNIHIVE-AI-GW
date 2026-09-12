package cursor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	modelsEndpoint    = "/aiserver.v1.AiService/AvailableModels"
	modelsExcludeMax  = 3
	modelsParameters  = 5
	modelsReactPicker = 11
	modelListName     = 1
	modelListDetails  = 2
	modelName         = 1
	modelServerName   = 18
	modelHidden       = 35
	modelLegacySlug   = 36
	modelAlias        = 37
)

type AvailableModel struct {
	ID, RunSlug          string
	LegacySlugs, Aliases []string
}

// FallbackModels combines the calibrated picker IDs with known run variants.
// New IDs without captured variants retain their explicit picker ID.
func FallbackModels() []AvailableModel {
	ids := strings.Fields("default grok-4.6 composer-2.5 claude-opus-5 claude-opus-4-8 gpt-5.6-sol gpt-5.5 claude-fable-5-1 claude-fable-5 grok-4.5 gemini-3.8-flash gemini-3.7-flash muse-spark-1.3 gpt-5.6-terra claude-sonnet-5 claude-sonnet-4-6 gpt-5.3-codex claude-opus-4-7 gpt-5.4 claude-opus-4-6 claude-opus-4-5 gpt-5.2 gpt-5.6-luna gemini-3.6-flash gemini-3.1-pro gpt-5.4-mini gpt-5.4-nano claude-haiku-4-5 claude-sonnet-4-5 gpt-5.1 gemini-3-flash gemini-3.5-flash claude-sonnet-4 gpt-5-mini gemini-2.5-flash kimi-k3 kimi-k2.7-code glm-5.2")
	snapshot := defaultRunSlugTable()
	models := make([]AvailableModel, 0, len(ids))
	for _, id := range ids {
		models = append(models, AvailableModel{ID: id, RunSlug: id, LegacySlugs: snapshot[id]})
	}
	return models
}

func ResolveModel(requested, effort string, catalog []AvailableModel) (string, error) {
	return ResolveModelVariant(requested, RunOptions{Effort: effort}, catalog)
}

func (client *Client) AvailableModels(ctx context.Context, credentials Credentials) ([]AvailableModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var payload message
	payload.number(modelsExcludeMax, 1)
	payload.number(modelsParameters, 1)
	payload.number(modelsReactPicker, 1)
	baseURL := client.BaseURL
	if baseURL == "" {
		baseURL = APIBaseURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+modelsEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("cursor: invalid picker endpoint")
	}
	request.Header = headers(credentials, "")
	request.Header.Set("Content-Type", "application/proto")
	response, err := client.HTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cursor: picker transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &HTTPError{Status: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxFrameSize+1))
	if err != nil || len(data) > maxFrameSize {
		return nil, fmt.Errorf("cursor: invalid picker response")
	}
	decoded, err := fields(data)
	if err != nil {
		return nil, err
	}
	var models, names []AvailableModel
	for _, item := range decoded {
		if item.number == modelListName {
			names = append(names, AvailableModel{ID: string(item.data), RunSlug: string(item.data)})
		}
		if item.number != modelListDetails {
			continue
		}
		details, err := fields(item.data)
		if err != nil {
			return nil, err
		}
		if findField(details, modelHidden).integer != 0 {
			continue
		}
		model := AvailableModel{ID: string(findField(details, modelName).data), RunSlug: string(findField(details, modelServerName).data)}
		for _, detail := range details {
			if detail.number == modelLegacySlug {
				model.LegacySlugs = append(model.LegacySlugs, string(detail.data))
			}
			if detail.number == modelAlias {
				model.Aliases = append(model.Aliases, string(detail.data))
			}
		}
		if model.ID != "" {
			models = append(models, model)
		}
	}
	if len(models) == 0 {
		models = names
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("cursor: empty picker")
	}
	return models, nil
}
