package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// The independent Bedrock platform shares the gateway transport, HTTP errors,
// Messages response parser and settlement result with the legacy Claude path.
func (service *GatewayService) forwardBedrockPlatform(ctx context.Context, requestContext *gin.Context, account *Account, parsed *ParsedRequest, start time.Time) (*ForwardResult, error) {
	target, valid := ResolveBedrockModelID(account, parsed.Model)
	if !valid {
		return nil, fmt.Errorf("Bedrock target is required")
	}
	if err := validateAccountForwardModel(ctx, service.accountRepo, account, target, CompositeRouteEndpointMessages); err != nil {
		return nil, err
	}
	upstreamContext, headersReceived, cancel := boundedStreamContext(ctx, streamHeaderTimeout(service.cfg, true))
	defer cancel()
	claudeInvoke := strings.Contains(target, "anthropic.")
	request, err := BuildBedrockAdapterRequest(upstreamContext, account, target, parsed.Body.Bytes(), parsed.Stream, claudeInvoke, requestContext.GetHeader("anthropic-beta"))
	if err != nil {
		return nil, err
	}
	response, err := generationDoWithTLS(service.httpUpstream, request, upstreamModelsProxyURL(account), account.ID, account.Concurrency, nil)
	headersReceived()
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, service.handleUpstreamTransportError(ctx, requestContext, account, err, OpsUpstreamErrorEvent{UpstreamURL: safeUpstreamURL(request.URL.String())})
	}
	originalBody := response.Body
	defer func() { _ = originalBody.Close() }()
	if response.Header.Get("x-request-id") == "" {
		response.Header.Set("x-request-id", response.Header.Get("x-amzn-requestid"))
	}
	if response.StatusCode >= http.StatusBadRequest {
		return service.handleBedrockUpstreamErrors(ctx, response, requestContext, account)
	}
	if parsed.OnUpstreamAccepted != nil {
		parsed.OnUpstreamAccepted()
	}
	var usage *ClaudeUsage
	var firstToken *int
	var disconnected bool
	if parsed.Stream {
		if claudeInvoke {
			result, streamErr := service.handleBedrockStreamingResponse(upstreamContext, response, requestContext, account, start, parsed.Model)
			if streamErr != nil {
				return nil, streamErr
			}
			usage, firstToken, disconnected = result.usage, result.firstTokenMs, result.clientDisconnect
		} else {
			// Existing Messages stream reader owns its idle timeout and client framing.
			reader, writer := io.Pipe()
			defer func() { _ = reader.Close() }()
			go func() {
				_, convertErr := ConvertBedrockConverseStream(upstreamContext, originalBody, writer, response.Header.Get("x-request-id"), parsed.Model)
				_ = writer.CloseWithError(convertErr)
			}()
			response.Body = reader
			result, streamErr := service.handleStreamingResponse(upstreamContext, response, requestContext, account, start, parsed.Model, target, false)
			if streamErr != nil {
				return nil, streamErr
			}
			usage, firstToken, disconnected = result.usage, result.firstTokenMs, result.clientDisconnect
		}
	} else {
		body, readErr := ReadUpstreamResponseBody(originalBody, service.cfg, requestContext, anthropicTooLargeError)
		if readErr != nil {
			return nil, readErr
		}
		if !claudeInvoke {
			body, err = ConvertBedrockConverseResponse(body, response.Header.Get("x-request-id"), parsed.Model)
			if err != nil {
				return nil, err
			}
		}
		response.Body = io.NopCloser(bytes.NewReader(body))
		usage, err = service.handleBedrockNonStreamingResponse(ctx, response, requestContext, account)
		if err != nil {
			return nil, err
		}
	}
	if usage == nil {
		return nil, fmt.Errorf("Bedrock usage is unknown")
	}
	return &ForwardResult{RequestID: response.Header.Get("x-request-id"), UpstreamHeaders: response.Header, Usage: *usage, Model: parsed.Model, UpstreamModel: target, Stream: parsed.Stream, Duration: time.Since(start), FirstTokenMs: firstToken, ClientDisconnect: disconnected}, nil
}

func (service *AccountTestService) fetchBedrockModels(ctx context.Context, account *Account) ([]string, []byte, error) {
	if account.Type != AccountTypeBedrock {
		return nil, nil, newUpstreamModelSyncUnsupportedError("Bedrock requires a Bedrock credential account", nil)
	}
	if account.GetCredential("aws_access_key_id") == "" || account.GetCredential("aws_secret_access_key") == "" {
		return nil, nil, newUpstreamModelSyncUnsupportedError("Bedrock runtime API keys do not establish catalog permission; use exact target declarations", nil)
	}
	if service.httpUpstream == nil {
		return nil, nil, newUpstreamModelSyncConfigError("Upstream HTTP client is not configured", nil)
	}
	fetch := func(action, target, cursor string) ([]byte, error) {
		request, err := BuildBedrockCatalogRequest(ctx, account, action, target, cursor)
		if err != nil {
			return nil, err
		}
		if _, err = service.validateUpstreamBaseURL(request.URL.Scheme + "://" + request.URL.Host); err != nil {
			return nil, err
		}
		response, err := service.doUpstreamModelsRequest(request, upstreamModelsProxyURL(account), account)
		if err != nil {
			return nil, err
		}
		defer func() { _ = response.Body.Close() }()
		body, err := readUpstreamResponseBodyLimited(response.Body, resolveModelsListReadLimit(service.cfg))
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			return nil, newUpstreamModelDiscoveryHTTPError(response.StatusCode, body)
		}
		return body, nil
	}
	body, err := fetch("foundation-models", "", "")
	if err != nil {
		return nil, nil, err
	}
	catalog, err := ParseBedrockFoundationCatalog(bedrockRuntimeRegion(account), body)
	if err != nil {
		return nil, nil, err
	}
	entries := make([]map[string]any, 0, len(catalog.Models))
	models := make([]string, 0, len(catalog.Models))
	for _, model := range catalog.Models {
		availabilityBody, fetchErr := fetch("availability", model.ModelID, "")
		if fetchErr != nil {
			return nil, nil, fetchErr
		}
		availability, parseErr := ParseBedrockModelAvailability(availabilityBody)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		if availability.ModelID != model.ModelID {
			return nil, nil, fmt.Errorf("Bedrock availability identity mismatch")
		}
		methods := []string{}
		if availability.AuthorizationStatus == "AUTHORIZED" && availability.AgreementAvailability.Status == "AVAILABLE" && availability.EntitlementAvailability == "AVAILABLE" && availability.RegionAvailability == "AVAILABLE" && slices.Contains(model.InferenceTypesSupported, "ON_DEMAND") && slices.Contains(model.OutputModalities, "TEXT") {
			methods = []string{CompositeRouteEndpointMessages}
		}
		entries = append(entries, map[string]any{"id": model.ModelID, "display_name": model.ModelName, "supported_methods": methods, "input_modalities": lowerBedrockModalities(model.InputModalities), "output_modalities": lowerBedrockModalities(model.OutputModalities)})
		models = append(models, model.ModelID)
	}
	profiles, err := CollectBedrockProfiles(ctx, func(_ context.Context, cursor string) ([]byte, error) { return fetch("inference-profiles", "", cursor) })
	if err != nil {
		return nil, nil, err
	}
	for _, profile := range profiles {
		// A profile being listed does not establish Invoke authorization.
		for _, target := range []string{profile.ID, profile.ARN} {
			if target == "" {
				continue
			}
			entries = append(entries, map[string]any{"id": target, "display_name": profile.Name, "supported_methods": []string{}})
			models = append(models, target)
		}
	}
	encoded, err := json.Marshal(map[string]any{"data": entries})
	if err != nil {
		return nil, nil, err
	}
	return dedupeAndSortModelIDs(models), encoded, nil
}

func lowerBedrockModalities(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.ToLower(value)
	}
	return result
}
