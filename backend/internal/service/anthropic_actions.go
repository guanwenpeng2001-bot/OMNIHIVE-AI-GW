package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Protocol evidence checked 2026-09-21:
// https://platform.claude.com/docs/en/api/messages/batches/create
// https://platform.claude.com/docs/en/build-with-claude/batch-processing
// This adapter is consumed by the resource-action ingress. Admission, durable
// submission claims, frozen prices and settlement remain owned by the shared
// request/job services. In particular, POST must not be retried after an
// ambiguous transport failure: custom_id is NOT an upstream idempotency key.
var anthropicBatchIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ValidateAnthropicMessageBatch preserves native per-request model and content
// blocks (including vision, tools and prompt caching) instead of flattening a
// batch into a single Messages request. Model authorization applies to EVERY
// requests[].params.model at admission, before this wire adapter is invoked.
func ValidateAnthropicMessageBatch(body []byte) error {
	var batch struct {
		Requests []struct {
			CustomID string `json:"custom_id"`
			Params   struct {
				Model     string            `json:"model"`
				MaxTokens *int              `json:"max_tokens"`
				Messages  []json.RawMessage `json:"messages"`
				Stream    bool              `json:"stream"`
				Speed     json.RawMessage   `json:"speed"`
			} `json:"params"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(body, &batch); err != nil {
		return fmt.Errorf("invalid Anthropic message batch: %w", err)
	}
	if len(batch.Requests) == 0 || len(batch.Requests) > 100000 {
		return errors.New("Anthropic message batch requires 1 to 100000 requests")
	}
	seen := make(map[string]bool, len(batch.Requests))
	for _, request := range batch.Requests {
		if !anthropicBatchIdentifier.MatchString(request.CustomID) || seen[request.CustomID] {
			return errors.New("Anthropic batch custom_id must be unique and contain 1 to 64 alphanumeric, underscore or hyphen characters")
		}
		seen[request.CustomID] = true
		if strings.TrimSpace(request.Params.Model) == "" || request.Params.MaxTokens == nil || *request.Params.MaxTokens <= 0 || len(request.Params.Messages) == 0 {
			return errors.New("Anthropic batch params require model, positive max_tokens and messages")
		}
		if request.Params.Stream || len(request.Params.Speed) != 0 {
			return errors.New("Anthropic message batches do not support streaming or speed")
		}
	}
	return nil
}

// BuildAnthropicMessageBatchRequest creates a native request. For create, job
// is the shared, owner-bound admission record; for all other operations it must
// be fetched from the shared owner-checked repository, never client-supplied.
// Listing upstream batches is deliberately absent: it would expose resources
// belonging to other gateway tenants sharing the same provider credential.
func (s *GatewayService) BuildAnthropicMessageBatchRequest(ctx context.Context, account *Account, job *UpstreamJob, userID, keyID, groupID int64, operation string, body []byte) (*http.Request, error) {
	if account == nil || job == nil || userID <= 0 || keyID <= 0 || groupID <= 0 ||
		job.UserID != userID || job.APIKeyID != keyID || job.GroupID == nil || *job.GroupID != groupID ||
		job.AccountID != account.ID || job.Platform != PlatformAnthropic || job.Family != "batch" {
		return nil, ErrUpstreamJobNotFound
	}
	// API-key evidence does not establish OAuth/setup-token/hosted permission.
	if account.Platform != PlatformAnthropic || account.Type != AccountTypeAPIKey {
		return nil, errors.New("Anthropic message batch credentials are not verified for this account type")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	method, suffix := http.MethodGet, ""
	switch operation {
	case "create":
		if job.UpstreamTaskID != "" {
			return nil, errors.New("Anthropic batch already has an upstream binding")
		}
		if err := ValidateAnthropicMessageBatch(body); err != nil {
			return nil, err
		}
		method = http.MethodPost
	case "retrieve", "cancel", "results", "delete":
		if !strings.HasPrefix(job.UpstreamTaskID, "msgbatch_") || !anthropicBatchIdentifier.MatchString(job.UpstreamTaskID) {
			return nil, errors.New("invalid Anthropic batch upstream binding")
		}
		suffix = "/" + job.UpstreamTaskID
		body = nil
		if operation == "cancel" {
			method, suffix = http.MethodPost, suffix+"/cancel"
		}
		if operation == "results" {
			suffix += "/results"
		}
		if operation == "delete" {
			method = http.MethodDelete
		}
	default:
		return nil, errors.New("unknown Anthropic batch operation")
	}
	baseURL := account.GetBaseURL()
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	// Results are reconstructed from the pinned account and task, not an
	// arbitrary results_url from a response or client (credential/SSRF boundary).
	targetURL := strings.TrimSuffix(strings.TrimRight(validatedURL, "/"), "/v1") + "/v1/messages/batches" + suffix
	request, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	setAnthropicAPIKeyAuthHeader(request.Header, account, token, baseURL)
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("Content-Type", "application/json")
	account.ApplyHeaderOverrides(request.Header)
	return request, nil
}

// AnthropicMessageBatchState retains partial-success counts. "ended" only
// means processing stopped; results must still be downloaded and each message
// settled using its own model and usage, including after cancellation.
type AnthropicMessageBatchState struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	ProcessingStatus string `json:"processing_status"`
	RequestCounts    struct {
		Processing int `json:"processing"`
		Succeeded  int `json:"succeeded"`
		Errored    int `json:"errored"`
		Canceled   int `json:"canceled"`
		Expired    int `json:"expired"`
	} `json:"request_counts"`
}

func ParseAnthropicMessageBatchState(body []byte, expectedID string) (*AnthropicMessageBatchState, error) {
	var state AnthropicMessageBatchState
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, err
	}
	if state.Type != "message_batch" || !strings.HasPrefix(state.ID, "msgbatch_") || !anthropicBatchIdentifier.MatchString(state.ID) || (expectedID != "" && state.ID != expectedID) {
		return nil, errors.New("invalid Anthropic batch response identity")
	}
	switch state.ProcessingStatus {
	case "in_progress", "canceling", "ended":
	default:
		return nil, errors.New("unknown Anthropic batch processing status")
	}
	counts := state.RequestCounts
	if counts.Processing < 0 || counts.Succeeded < 0 || counts.Errored < 0 || counts.Canceled < 0 || counts.Expired < 0 || (state.ProcessingStatus == "ended" && counts.Processing != 0) {
		return nil, errors.New("invalid Anthropic batch request counts")
	}
	return &state, nil
}

// AnthropicMessageBatchResult is one JSONL result, keyed by custom_id rather
// than position. Keep the native message/error bytes for resource responses and
// server-tool accounting; token usage alone is not a complete invoice.
type AnthropicMessageBatchResult struct {
	CustomID string
	Status   string
	Model    string
	Usage    *ClaudeUsage
	Message  json.RawMessage
	Error    json.RawMessage
}

func ParseAnthropicMessageBatchResult(line []byte) (*AnthropicMessageBatchResult, error) {
	var envelope struct {
		CustomID string `json:"custom_id"`
		Result   struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
			Error   json.RawMessage `json:"error"`
		} `json:"result"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil, err
	}
	if !anthropicBatchIdentifier.MatchString(envelope.CustomID) {
		return nil, errors.New("invalid Anthropic batch result custom_id")
	}
	result := &AnthropicMessageBatchResult{CustomID: envelope.CustomID, Status: envelope.Result.Type, Message: envelope.Result.Message, Error: envelope.Result.Error}
	switch result.Status {
	case "succeeded":
		var message struct {
			Type  string `json:"type"`
			Model string `json:"model"`
			Usage *struct {
				Input          *int `json:"input_tokens"`
				Output         *int `json:"output_tokens"`
				Creation       int  `json:"cache_creation_input_tokens"`
				Read           int  `json:"cache_read_input_tokens"`
				CreationDetail struct {
					Short int `json:"ephemeral_5m_input_tokens"`
					Long  int `json:"ephemeral_1h_input_tokens"`
				} `json:"cache_creation"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(result.Message, &message); err != nil {
			return nil, err
		}
		if message.Type != "message" || message.Model == "" || message.Usage == nil || message.Usage.Input == nil || message.Usage.Output == nil {
			return nil, errors.New("Anthropic batch success requires model and explicit token usage")
		}
		usage := message.Usage
		if *usage.Input < 0 || *usage.Output < 0 || usage.Creation < 0 || usage.Read < 0 || usage.CreationDetail.Short < 0 || usage.CreationDetail.Long < 0 {
			return nil, errors.New("negative Anthropic batch token usage")
		}
		result.Model, result.Usage = message.Model, parseClaudeUsageFromResponseBody(result.Message)
	case "errored":
		if len(result.Error) == 0 || string(result.Error) == "null" {
			return nil, errors.New("Anthropic batch error result requires error details")
		}
	case "canceled", "expired":
	default:
		return nil, errors.New("unknown Anthropic batch result type")
	}
	return result, nil
}

// AnthropicMessageBatchTokenPricing derives batch token rates from the caller's
// frozen standard model tariff. No model aliases or live catalogue lookup occurs
// here. Server-tool fees are separate and must not receive this token discount.
func AnthropicMessageBatchTokenPricing(standard ModelPricing) ModelPricing {
	standard.InputPricePerToken *= 0.5
	standard.OutputPricePerToken *= 0.5
	standard.CacheCreationPricePerToken *= 0.5
	standard.CacheReadPricePerToken *= 0.5
	standard.CacheCreation5mPrice *= 0.5
	standard.CacheCreation1hPrice *= 0.5
	standard.InputPricePerTokenPriority = 0
	standard.OutputPricePerTokenPriority = 0
	standard.CacheCreationPricePerTokenPriority = 0
	standard.CacheReadPricePerTokenPriority = 0
	standard.FastMultiplier, standard.FlexMultiplier = nil, nil
	return standard
}
