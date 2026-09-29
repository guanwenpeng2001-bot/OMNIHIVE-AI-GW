package cursor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	AutoModelID       = "default"
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

// ProtocolVariantModels describes wire spellings, never model availability.
func ProtocolVariantModels() []AvailableModel {
	snapshot := defaultRunSlugTable()
	// "default" is the protocol's automatic router, not a model inventory claim.
	models := []AvailableModel{{ID: AutoModelID, RunSlug: AutoModelID}}
	for id, slugs := range snapshot {
		models = append(models, AvailableModel{ID: id, RunSlug: id, LegacySlugs: slugs})
	}
	sort.Slice(models[1:], func(left, right int) bool { return models[left+1].ID < models[right+1].ID })
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
	response, err := Do(client.HTTPClient, request, true)
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
