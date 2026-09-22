package service

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const volcengineRuntimeBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

// volcengineChatRequest is the private seam for the shared CC pipeline. The
// caller still owns model eligibility, pricing, HTTP sending, SSE and settlement.
// It must supply the already resolved account target, not a catalog canonical ID.
type volcengineChatRequest struct {
	TargetURL string
	APIKey    string
	Body      []byte
	Stream    bool
}

// prepareVolcengineChatRequest implements only the frozen Ark runtime contract:
// Bearer API key, /api/v3/chat/completions, and opaque account target IDs.
// It neither discovers models nor converts foundation IDs, ep IDs or aliases.
// No platform constant is registered here; registration belongs to integration.
func (service *OpenAIGatewayService) prepareVolcengineChatRequest(account *Account, body []byte, upstreamModel string) (*volcengineChatRequest, error) {
	if account == nil || account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("volcengine runtime requires an API key account")
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" || strings.ContainsAny(apiKey, "\r\n") {
		return nil, fmt.Errorf("volcengine runtime requires a valid inference api_key")
	}
	// Deliberately never fall back to access_key, secret_key or access_token.
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, fmt.Errorf("volcengine chat body must be a JSON object")
	}
	model := gjson.GetBytes(body, "model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return nil, fmt.Errorf("volcengine chat model is required")
	}
	if strings.TrimSpace(upstreamModel) == "" || upstreamModel != strings.TrimSpace(upstreamModel) {
		return nil, fmt.Errorf("volcengine requires an exact resolved account target")
	}
	stream := gjson.GetBytes(body, "stream")
	if stream.Exists() && stream.Type != gjson.True && stream.Type != gjson.False {
		return nil, fmt.Errorf("volcengine chat stream must be a boolean")
	}
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		baseURL = volcengineRuntimeBaseURL
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, fmt.Errorf("volcengine runtime base URL must not contain credentials, query or fragment")
	}
	// Accept the documented base or an explicit full chat endpoint. In
	// particular, do not append /v1 to a host or send a runtime key to /models
	// or a management Action URL. Custom relays use the same runtime path.
	switch strings.TrimRight(parsedURL.Path, "/") {
	case "/api/v3", "/api/v3/chat/completions":
	default:
		return nil, fmt.Errorf("volcengine runtime base URL must use /api/v3 or /api/v3/chat/completions")
	}
	validatedURL, err := service.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("volcengine runtime base URL is invalid or disallowed")
	}
	upstreamBody := append([]byte(nil), body...)
	if model.String() != upstreamModel {
		upstreamBody, err = sjson.SetBytes(upstreamBody, "model", upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("set volcengine account target: %w", err)
		}
	}
	if stream.Bool() {
		upstreamBody, err = ensureOpenAIChatStreamUsage(upstreamBody)
		if err != nil {
			return nil, fmt.Errorf("request volcengine stream usage: %w", err)
		}
	}
	return &volcengineChatRequest{
		TargetURL: buildOpenAIChatCompletionsURL(validatedURL),
		APIKey:    apiKey,
		Body:      upstreamBody,
		Stream:    stream.Bool(),
	}, nil
}

// volcengineModelDiscoveryUnsupported uses the existing catalog failure state,
// so integration can retain historical evidence and offer explicit declarations.
// The frozen contract does not confirm a runtime /models or management listing.
//
//nolint:unused // referenced only from //go:build unit tests, so the default lint pass cannot see its callers.
func volcengineModelDiscoveryUnsupported() error {
	return newUpstreamModelSyncUnsupportedError("Volcengine automatic model discovery is not supported: runtime model listing and management credentials/contracts are unconfirmed; use an account-scoped availability declaration", nil)
}
