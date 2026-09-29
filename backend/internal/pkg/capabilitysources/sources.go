// Package capabilitysources collects unapproved, price-free observations only.
// Registry namespaces are not native deployment identities or account grants.
package capabilitysources

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const MaxBodyBytes = 32 << 20

var endpoints = map[string]string{
	"models.dev": "https://models.dev/api.json",
	"openrouter": "https://openrouter.ai/api/v1/models?output_modalities=all",
	"litellm":    "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json",
}

type Observation struct {
	ID          string           `json:"id"`
	Tier        string           `json:"tier"`
	ContentHash string           `json:"content_hash"`
	VerifiedAt  string           `json:"verified_at"`
	Records     []map[string]any `json:"records"`
}

// Endpoint returns only a fixed, public, unauthenticated registry URL.
func Endpoint(source string) (string, error) {
	endpoint, exists := endpoints[source]
	if !exists {
		return "", fmt.Errorf("unsupported capability source %q", source)
	}
	return endpoint, nil
}

func object(value any) (map[string]any, error) {
	result, valid := value.(map[string]any)
	if !valid || result == nil {
		return nil, fmt.Errorf("expected object")
	}
	return result, nil
}

func copyField(record, raw map[string]any, from, to, kind string) error {
	value, exists := raw[from]
	if !exists {
		return nil
	}
	switch kind {
	case "bool":
		if _, valid := value.(bool); !valid {
			return fmt.Errorf("%s must be boolean", from)
		}
	case "limit":
		number, valid := value.(json.Number)
		if !valid {
			return fmt.Errorf("%s must be positive int64", from)
		}
		parsed, err := number.Int64()
		if err != nil || parsed <= 0 {
			return fmt.Errorf("%s must be positive int64", from)
		}
		value = parsed
	case "array":
		items, valid := value.([]any)
		if !valid {
			return fmt.Errorf("%s must be array", from)
		}
		for _, item := range items {
			if text, valid := item.(string); !valid || strings.TrimSpace(text) == "" {
				return fmt.Errorf("invalid %s item", from)
			}
		}
	case "string":
		if text, valid := value.(string); !valid || strings.TrimSpace(text) == "" {
			return fmt.Errorf("invalid %s", from)
		}
	}
	record[to] = value
	return nil
}

func nestedFields(record, raw map[string]any, key string, fields map[string]string, kind string) error {
	value, exists := raw[key]
	if !exists {
		return nil
	}
	nested, err := object(value)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	for from, to := range fields {
		if err := copyField(record, nested, from, to, kind); err != nil {
			return err
		}
	}
	return nil
}

// Parse preserves missing/false/empty states and rejects incomplete documents.
// No source can supply prices, eligibility, mappings, or account discovery.
func Parse(source string, body []byte, verifiedAt string) (Observation, error) {
	observation := Observation{}
	if _, err := time.Parse(time.DateOnly, verifiedAt); err != nil {
		return observation, fmt.Errorf("invalid evidence date: %w", err)
	}
	if _, err := Endpoint(source); err != nil {
		return observation, err
	}
	if len(body) > MaxBodyBytes {
		return observation, fmt.Errorf("capability response too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return observation, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return observation, fmt.Errorf("trailing source document")
	}
	if len(root) == 0 {
		return observation, fmt.Errorf("empty source")
	}
	for _, name := range []string{"has_more", "next", "next_page", "next_cursor"} {
		if value, exists := root[name]; exists && value != nil && value != false && value != "" {
			return observation, fmt.Errorf("incomplete pagination: %s", name)
		}
	}
	appendRecord := func(profile, modelID string, raw map[string]any) error {
		if strings.TrimSpace(profile) == "" || strings.TrimSpace(modelID) == "" {
			return fmt.Errorf("missing source identity")
		}
		record := map[string]any{"provider": profile, "profile": profile, "model_id": modelID}
		switch source {
		case "litellm":
			for _, field := range []string{"max_input_tokens", "max_output_tokens"} {
				if err := copyField(record, raw, field, field, "limit"); err != nil {
					return err
				}
			}
			for _, field := range []string{"supports_reasoning", "supports_vision", "supports_function_calling", "supports_prompt_caching"} {
				if err := copyField(record, raw, field, field, "bool"); err != nil {
					return err
				}
			}
			for _, field := range []string{"input_modalities", "output_modalities"} {
				if err := copyField(record, raw, field, field, "array"); err != nil {
					return err
				}
			}
			if err := copyField(record, raw, "mode", "mode", "string"); err != nil {
				return err
			}
		case "models.dev":
			for from, to := range map[string]string{"reasoning": "supports_reasoning", "tool_call": "supports_function_calling"} {
				if err := copyField(record, raw, from, to, "bool"); err != nil {
					return err
				}
			}
			if err := nestedFields(record, raw, "modalities", map[string]string{"input": "input_modalities", "output": "output_modalities"}, "array"); err != nil {
				return err
			}
			if err := nestedFields(record, raw, "limit", map[string]string{"context": "context_window", "output": "max_output_tokens"}, "limit"); err != nil {
				return err
			}
		case "openrouter":
			if err := nestedFields(record, raw, "architecture", map[string]string{"input_modalities": "input_modalities", "output_modalities": "output_modalities"}, "array"); err != nil {
				return err
			}
			if err := copyField(record, raw, "context_length", "context_window", "limit"); err != nil {
				return err
			}
			if err := copyField(record, raw, "supported_parameters", "supported_parameters", "array"); err != nil {
				return err
			}
			// Provider-union parameters do not prove native tool or reasoning support.
		}
		if _, exists := record["context_window"]; exists {
			record["context_limit_kind"] = "total"
		}
		observation.Records = append(observation.Records, record)
		return nil
	}
	switch source {
	case "openrouter":
		rows, valid := root["data"].([]any)
		if !valid {
			return observation, fmt.Errorf("missing models array")
		}
		for _, row := range rows {
			raw, err := object(row)
			if err != nil {
				return observation, err
			}
			modelID, _ := raw["id"].(string)
			if err := appendRecord("openrouter", modelID, raw); err != nil {
				return observation, err
			}
		}
	case "models.dev":
		for profile, value := range root {
			provider, err := object(value)
			if err != nil {
				return observation, err
			}
			models, err := object(provider["models"])
			if err != nil {
				return observation, err
			}
			for modelID, value := range models {
				raw, err := object(value)
				if err != nil {
					return observation, err
				}
				if err := appendRecord(profile, modelID, raw); err != nil {
					return observation, err
				}
			}
		}
	case "litellm":
		for modelID, value := range root {
			if modelID == "sample_spec" {
				continue
			}
			raw, err := object(value)
			if err != nil {
				return observation, err
			}
			profile, _ := raw["litellm_provider"].(string)
			if err := appendRecord(profile, modelID, raw); err != nil {
				return observation, err
			}
		}
	}
	if len(observation.Records) == 0 {
		return observation, fmt.Errorf("empty model collection")
	}
	sort.Slice(observation.Records, func(left, right int) bool {
		return fmt.Sprint(observation.Records[left]["profile"], "\x00", observation.Records[left]["model_id"]) < fmt.Sprint(observation.Records[right]["profile"], "\x00", observation.Records[right]["model_id"])
	})
	for index := 1; index < len(observation.Records); index++ {
		if observation.Records[index]["profile"] == observation.Records[index-1]["profile"] && observation.Records[index]["model_id"] == observation.Records[index-1]["model_id"] {
			return Observation{}, fmt.Errorf("duplicate source identity")
		}
	}
	digest := sha256.Sum256(body)
	observation.ID, observation.Tier, observation.ContentHash, observation.VerifiedAt = source, "online-registry", hex.EncodeToString(digest[:]), verifiedAt
	return observation, nil
}
