package service

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/responseheaders"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func openRouterGoogleIngress(ctx context.Context) bool {
	plan := GatewayRequestPlanFromContext(ctx)
	return plan != nil && plan.Snapshot().Protocol == "gemini"
}

// Adaptive is the default. A fixed upstream choice never silently converts a
// signed native history into another protocol and discards its opaque state.
func openRouterProtocolAllowed(account *Account, protocol string) error {
	if account == nil || account.Platform != PlatformOpenRouter || account.Type != AccountTypeAPIKey {
		return fmt.Errorf("OpenRouter native protocols require an API-key account")
	}
	choice := account.GetAPIProtocol()
	if choice == "" || choice == APIProtocolAdaptive || choice == protocol {
		return nil
	}
	return fmt.Errorf("OpenRouter account is fixed to %s; %s conversion is not supported, select adaptive to match the client protocol", choice, protocol)
}

func openRouterProtocolBase(account *Account, protocol string) string {
	if account.IsAdaptiveAPIProtocol() {
		if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok {
			if base, ok := bases[protocol].(string); ok && strings.TrimSpace(base) != "" {
				return strings.TrimSpace(base)
			}
		}
	}
	return account.GetOpenAIBaseURL()
}

func openRouterNativeUsage(raw []byte, protocol string) (OpenAIUsage, bool) {
	if _, known := openRouterCounter(raw, "usage.input_tokens"); !known {
		return OpenAIUsage{}, false
	}
	if _, known := openRouterCounter(raw, "usage.output_tokens"); !known {
		return OpenAIUsage{}, false
	}
	if protocol == APIProtocolAnthropic {
		return claudeUsageToOpenAIUsage(parseClaudeUsageFromResponseBody(raw)), true
	}
	usage, known := extractOpenAIUsageFromJSONBytes(raw)
	return usage, known
}

func openRouterNativeResponseModel(raw []byte, path, original, mapped string) []byte {
	if original != mapped && gjson.GetBytes(raw, path).String() == mapped {
		if rewritten, err := sjson.SetBytes(raw, path, original); err == nil {
			return rewritten
		}
	}
	return raw
}

func (gateway *OpenAIGatewayService) forwardOpenRouterNative(ctx context.Context, request *gin.Context, account *Account, body []byte, protocol, defaultMappedModel string) (*OpenAIForwardResult, error) {
	started := time.Now()
	nativeErrorWriter := writeOpenAIResponsesFallbackError
	endpoint := "/v1/responses"
	if protocol == APIProtocolAnthropic {
		nativeErrorWriter = writeAnthropicError
		endpoint = "/v1/messages"
	}
	writeError := func(current *gin.Context, status int, kind, message string) {
		nativeErrorWriter(current, status, kind, message)
		MarkResponseCommitted(current)
	}
	if err := openRouterProtocolAllowed(account, protocol); err != nil {
		writeError(request, 400, "unsupported_protocol_conversion", err.Error())
		return nil, err
	}
	modelField := gjson.GetBytes(body, "model")
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() || modelField.Type != gjson.String || strings.TrimSpace(modelField.String()) == "" {
		writeError(request, 400, "invalid_request_error", "model and a JSON object are required")
		return nil, fmt.Errorf("invalid OpenRouter native request")
	}
	original := strings.TrimSpace(modelField.String())
	mapped := resolveOpenAIForwardModel(account, original, defaultMappedModel)
	outbound := ReplaceModelInBody(body, mapped)
	base, err := gateway.validateUpstreamBaseURL(openRouterProtocolBase(account, protocol))
	if err != nil {
		return nil, err
	}
	token, _, err := gateway.getRequestCredential(ctx, request, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("OpenRouter API key missing")
	}
	network, headersReceived, closeNetwork := boundedStreamContext(WithHTTPUpstreamRedirectsDisabled(ctx), streamHeaderTimeout(gateway.cfg, true))
	defer closeNetwork()
	upstream, err := http.NewRequestWithContext(network, http.MethodPost, account.buildOpenAIEndpointURL(base, endpoint), bytes.NewReader(outbound))
	if err != nil {
		return nil, err
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept", "application/json")
	stream := gjson.GetBytes(body, "stream").Bool()
	if stream {
		upstream.Header.Set("Accept", "text/event-stream")
	}
	if protocol == APIProtocolAnthropic {
		if version := request.GetHeader("anthropic-version"); version != "" {
			upstream.Header.Set("anthropic-version", version)
		}
	}
	account.ApplyHeaderOverrides(upstream.Header)
	upstream.Header.Del("x-api-key")
	upstream.Header.Set("Authorization", "Bearer "+token)
	SetActualOpenAIUpstreamEndpoint(request, endpoint)
	SetOpsUpstreamModel(request, mapped)
	observer := beginUpstreamResponseModelObservation(request)
	proxy, err := account.EgressProxyURL(ctx)
	if err != nil {
		return nil, err
	}
	response, err := gateway.doOpenAIUpstream(upstream, proxy, account)
	headersReceived()
	if err != nil {
		return nil, gateway.handleOpenAIUpstreamTransportError(ctx, request, account, err, false)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Debug("provider response cleanup failed")
		}
	}()
	if response.StatusCode >= 400 {
		raw, message := gateway.readOpenAIUpstreamError(response)
		if failover := gateway.openRouterFailover(ctx, account, response, raw, message); failover != nil {
			return nil, failover
		}
		return gateway.handleCompatErrorResponse(response, request, account, writeError, mapped)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeError(request, 502, "upstream_error", "Unexpected OpenRouter status")
		return nil, fmt.Errorf("unexpected OpenRouter native status")
	}
	result := &OpenAIForwardResult{RequestID: response.Header.Get("x-request-id"), UpstreamHeaders: response.Header, Model: original, BillingModel: mapped, UpstreamModel: mapped, UpstreamEndpoint: endpoint, Stream: stream, ReasoningEffort: extractOpenAIReasoningEffortFromBody(body, mapped, original)}
	if protocol == APIProtocolAnthropic {
		if effort := gjson.GetBytes(body, "output_config.effort").String(); effort != "" {
			result.ReasoningEffort = optionalTrimmedStringPtr(effort)
		}
	}
	finish := func(usage OpenAIUsage) *OpenAIForwardResult {
		result.UpstreamResponseModel = observedUpstreamResponseModel(request)
		result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(request)
		result.UpstreamResponseServiceTier = observedUpstreamResponseServiceTier(request)
		result.ServiceTier = resolvedOpenAIUpstreamServiceTier(request, extractOpenAIServiceTierFromBody(body))
		result.Usage = usage
		result.Duration = time.Since(started)
		return result
	}
	if !stream {
		raw, err := ReadUpstreamResponseBody(response.Body, gateway.cfg, request, func(current *gin.Context) { writeError(current, 502, "upstream_error", "Upstream response too large") })
		if err != nil {
			return nil, err
		}
		if !gjson.ValidBytes(raw) || !gjson.ParseBytes(raw).IsObject() {
			writeError(request, 502, "upstream_error", "Invalid native OpenRouter JSON response")
			return nil, fmt.Errorf("invalid native OpenRouter JSON")
		}
		status := gjson.GetBytes(raw, "status").String()
		failed := status == "failed" || status == "cancelled" || status == "canceled"
		complete := gjson.GetBytes(raw, "object").String() == "response" && (status == "completed" || status == "incomplete" || failed)
		if protocol == APIProtocolAnthropic {
			complete = gjson.GetBytes(raw, "type").String() == "message" && gjson.GetBytes(raw, "id").String() != "" && gjson.GetBytes(raw, "content").IsArray() && gjson.GetBytes(raw, "stop_reason").Type == gjson.String
			failed = false
		}
		if !complete {
			writeError(request, 502, "upstream_error", "Native response has no terminal state")
			return nil, fmt.Errorf("nonterminal native response")
		}
		usage, known := openRouterNativeUsage(raw, protocol)
		if !known && !failed && status != "incomplete" {
			writeError(request, 502, "upstream_error", "Native response usage is unknown")
			return nil, fmt.Errorf("native usage unknown")
		}
		if protocol == APIProtocolAnthropic {
			observer.ObserveAnthropic(raw)
		} else {
			observer.ObserveOpenAI(raw, "")
		}
		raw = openRouterNativeResponseModel(raw, "model", original, mapped)
		if gateway.responseHeaderFilter != nil {
			responseheaders.WriteFilteredHeaders(request.Writer.Header(), response.Header, gateway.responseHeaderFilter)
		}
		request.Data(response.StatusCode, "application/json", raw)
		MarkResponseCommitted(request)
		if !known {
			return nil, fmt.Errorf("native terminal response has unknown usage")
		}
		if failed {
			return finish(usage), fmt.Errorf("OpenRouter native response failed")
		}
		return finish(usage), nil
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		writeError(request, 502, "upstream_error", "Expected native OpenRouter SSE response")
		return nil, fmt.Errorf("native stream content type mismatch")
	}
	maxSize := defaultMaxLineSize
	if gateway.cfg != nil && gateway.cfg.Gateway.MaxLineSize > 0 {
		maxSize = gateway.cfg.Gateway.MaxLineSize
	}
	scanner := newUpstreamFrameReader(response.Body, maxSize, ctx, gateway.anthropicNativeStreamInterval())
	defer scanner.Close()
	writeHeaders := gateway.newStreamHeaderWriter(request, response.Header)
	usage := OpenAIUsage{}
	anthropicUsage := &ClaudeUsage{}
	inputKnown, outputKnown := false, false
	startedMessage := false
	streamFailure := func(err error) (*OpenAIForwardResult, error) {
		if inputKnown && outputKnown {
			return finish(usage), newOpenAIUpstreamStreamReadError(err)
		}
		return nil, newOpenAIUpstreamStreamReadError(err)
	}
	for scanner.Scan() {
		raw := []byte(scanner.frame.Data)
		event := scanner.frame.EventType
		if !gjson.ValidBytes(raw) || !gjson.ParseBytes(raw).IsObject() {
			return streamFailure(fmt.Errorf("invalid native SSE JSON"))
		}
		payloadType := gjson.GetBytes(raw, "type").String()
		if event == "" {
			event = payloadType
		}
		if payloadType != "" && event != payloadType {
			return streamFailure(fmt.Errorf("native event type mismatch"))
		}
		if event == "" {
			return streamFailure(fmt.Errorf("native stream event has no type"))
		}
		if protocol == APIProtocolAnthropic {
			observer.ObserveAnthropic(raw)
		} else {
			observer.ObserveOpenAI(raw, event)
		}
		terminal, failed := false, false
		if protocol == APIProtocolAnthropic {
			switch event {
			case "message_start":
				startedMessage = true
				node := gjson.GetBytes(raw, "message")
				if count, ok := openRouterCounter([]byte(node.Raw), "usage.input_tokens"); ok {
					anthropicUsage.InputTokens = count
					inputKnown = true
				}
				raw = openRouterNativeResponseModel(raw, "message.model", original, mapped)
			case "message_delta":
				if count, ok := openRouterCounter(raw, "usage.output_tokens"); ok {
					anthropicUsage.OutputTokens = count
					outputKnown = true
				}
			case "message_stop":
				terminal = true
				if !startedMessage {
					return streamFailure(fmt.Errorf("message_stop without message_start"))
				}
			case "error":
				terminal = true
				failed = true
			}
			parseSSEUsagePassthrough(string(raw), anthropicUsage)
			usage = claudeUsageToOpenAIUsage(anthropicUsage)
		} else {
			terminal = openAIStreamEventTypeIsTerminal(event)
			failed = event == "response.failed" || event == "error" || event == "response.cancelled" || event == "response.canceled"
			if terminal && event != "error" {
				native := gjson.GetBytes(raw, "response")
				if !failed && (native.Get("object").String() != "response" || (native.Get("status").String() != "completed" && native.Get("status").String() != "incomplete")) {
					return streamFailure(fmt.Errorf("invalid native response terminal"))
				}
				parsed, known := openRouterNativeUsage([]byte(native.Raw), protocol)
				if known {
					usage = parsed
					inputKnown, outputKnown = true, true
				}
			}
			raw = openRouterNativeResponseModel(raw, "response.model", original, mapped)
		}
		known := inputKnown && outputKnown
		if terminal && !known && !failed && event != "response.incomplete" {
			return nil, newOpenAIUpstreamStreamReadError(fmt.Errorf("native terminal usage is unknown"))
		}
		writeHeaders()
		if event != "" {
			if _, err := request.Writer.WriteString("event: " + event + "\n"); err != nil {
				scanner.Disconnected()
				result.ClientDisconnect = true
				if known {
					return finish(usage), err
				}
				return nil, err
			}
		}
		if _, err := request.Writer.WriteString("data: " + strings.ReplaceAll(string(raw), "\n", "\ndata: ") + "\n\n"); err != nil {
			scanner.Disconnected()
			result.ClientDisconnect = true
			if known {
				return finish(usage), err
			}
			return nil, err
		}
		request.Writer.Flush()
		semantic := terminal || strings.HasPrefix(event, "content_block_") || strings.HasPrefix(event, "response.output_") || strings.HasPrefix(event, "response.content_part")
		if semantic {
			scanner.Semantic()
		}
		if semantic && result.FirstTokenMs == nil {
			elapsed := int(time.Since(started).Milliseconds())
			result.FirstTokenMs = &elapsed
		}
		if terminal {
			MarkResponseCommitted(request)
			if !known {
				return nil, fmt.Errorf("native failed/incomplete terminal has unknown usage")
			}
			if failed {
				return finish(usage), fmt.Errorf("OpenRouter native response failed")
			}
			return finish(usage), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return streamFailure(err)
	}
	return streamFailure(ErrOpenAIUpstreamStreamTruncated)
}
