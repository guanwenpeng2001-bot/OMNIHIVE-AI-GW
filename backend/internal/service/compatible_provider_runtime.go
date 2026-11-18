package service

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/tidwall/gjson"
)

// A request binding never changes persisted identity, budgets, proxy or credentials.
type compatibleRequestBinding struct {
	Endpoint    domain.CompatibleEndpoint
	Base        string
	Action      string
	observation compatibleUsageObservation
}

// IsCompatibleProvider identifies table-owned providers without changing their platform identity.
func IsCompatibleProvider(platform string) bool {
	_, ok := domain.LookupCompatibleProvider(platform)
	return ok
}
func (a *Account) compatibleMode() (domain.CompatibleProvider, domain.CompatibleMode, error) {
	if a == nil {
		return domain.CompatibleProvider{}, domain.CompatibleMode{}, fmt.Errorf("missing account")
	}
	p, ok := domain.LookupCompatibleProvider(a.Platform)
	if !ok || a.Type != AccountTypeAPIKey {
		return p, domain.CompatibleMode{}, fmt.Errorf("compatible provider requires API key account")
	}
	for _, field := range []string{"account_mode", "api_protocol"} {
		if raw, exists := a.Credentials[field]; exists && raw != nil {
			if _, valid := raw.(string); !valid {
				return p, domain.CompatibleMode{}, fmt.Errorf("compatible provider %s must be a string", field)
			}
		}
	}
	m, ok := p.ResolveMode(strings.TrimSpace(a.GetCredential("account_mode")))
	if !ok {
		return p, m, fmt.Errorf("unsupported compatible provider account mode")
	}
	return p, m, nil
}

// Bases are request prefixes before /chat/completions, /messages or /responses.
// Protocol overrides are complete prefixes. For account-source rows only base_url
// is a resource root; append the declared Path exactly once (also accept that full prefix).
func (a *Account) compatibleBase(endpoint domain.CompatibleEndpoint) (string, error) {
	if isCopilotNous(a) {
		config, err := compatibleSpecialAuthConfig(a)
		if err != nil {
			return "", err
		}
		endpoints, err := ResolveCopilotNousEndpoints(config)
		return endpoints.ChatBaseURL, err
	}
	p, _, err := a.compatibleMode()
	if err != nil {
		return "", err
	}
	if bases, ok := a.Credentials["api_base_urls"].(map[string]any); ok {
		if base, ok := bases[endpoint.Protocol].(string); ok && strings.TrimSpace(base) != "" {
			return strings.TrimRight(strings.TrimSpace(base), "/"), nil
		}
	}
	base := strings.TrimRight(strings.TrimSpace(a.GetCredential("base_url")), "/")
	if p.BaseSource == "account" {
		if base == "" {
			return "", fmt.Errorf("compatible provider requires resource base_url")
		}
		if !strings.HasSuffix(base, endpoint.Path) {
			base += endpoint.Path
		}
		return base, nil
	}
	if base != "" {
		return base, nil
	}
	if endpoint.BaseURL == "" {
		return "", fmt.Errorf("compatible endpoint has no base URL")
	}
	return strings.TrimRight(endpoint.BaseURL, "/"), nil
}
func (a *Account) compatibleProtocolBase(protocol string) string {
	_, mode, err := a.compatibleMode()
	if err != nil {
		return ""
	}
	endpoint, ok := mode.LookupEndpoint(protocol)
	if !ok {
		return ""
	}
	base, _ := a.compatibleBase(endpoint)
	return base
}
func (a *Account) compatibleEndpoint(action, model string) (domain.CompatibleEndpoint, error) {
	_, mode, err := a.compatibleMode()
	if err != nil {
		return domain.CompatibleEndpoint{}, err
	}
	protocol := strings.TrimSpace(a.GetCredential("api_protocol"))
	if protocol == "" || protocol == APIProtocolAdaptive {
		if raw, ok := a.Credentials["protocol_rules"]; ok {
			rules, err := parseModelProtocolRules(raw)
			if err != nil {
				return domain.CompatibleEndpoint{}, err
			}
			for _, rule := range rules {
				if modelProtocolPatternMatches(rule.Pattern, model) {
					protocol = rule.Protocol
					break
				}
			}
		}
	}
	endpoint, _, ok := mode.ResolveAction(action, protocol)
	if !ok {
		return endpoint, fmt.Errorf("compatible provider mode does not support %s with protocol %s", action, protocol)
	}
	return endpoint, nil
}
func bindCompatibleRequest(ctx context.Context, account *Account, action, model string) (*Account, error) {
	if !IsCompatibleProvider(account.Platform) || account.compatibleBinding != nil {
		return account, nil
	}
	if gatewayRequiredAction(ctx) == "generate_content" || openRouterGoogleIngress(ctx) {
		action = "generate_content"
	}
	endpoint, err := account.compatibleEndpoint(action, model)
	if err != nil {
		return nil, err
	}
	base, err := account.compatibleBase(endpoint)
	if err != nil {
		return nil, err
	}
	if isCopilotNous(account) {
		if err := validateCompatibleSpecialSecret(account); err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(account.GetCredential("api_key")) == "" {
		return nil, fmt.Errorf("compatible provider requires api_key")
	}
	bound := *account
	bound.compatibleBinding = &compatibleRequestBinding{Endpoint: endpoint, Base: base, Action: action}
	return &bound, nil
}
func setCompatibleAuth(header http.Header, auth, key string) {
	header.Del("Authorization")
	header.Del("X-Api-Key")
	header.Del("Api-Key")
	header.Del("X-Goog-Api-Key")
	switch auth {
	case "bearer":
		header.Set("Authorization", "Bearer "+key)
	case "x_api_key":
		header.Set("X-Api-Key", key)
	case "api_key_header":
		header.Set("api-key", key)
	}
}

// At the common send boundary, reject accidental cross-protocol sends. URL was
// already resolved and validated by the builder; this function never redirects it.

func compatibleMediaPathAction(path string) string {
	path = strings.TrimRight(path, "/")
	switch {
	case strings.HasSuffix(path, "/images/generations"):
		return "image.generate"
	case strings.HasSuffix(path, "/content") && strings.Contains(path, "/videos/"):
		return "video.content"
	case strings.HasSuffix(path, "/videos"):
		return "video.generate"
	case strings.Contains(path, "/videos/"):
		return "video.status"
	default:
		return ""
	}
}

func compatibleMediaRelativePathAllowed(rel, action string) bool {
	switch action {
	case "image.generate":
		return rel == "/images/generations"
	case "video.generate":
		return rel == "/videos"
	case "video.status":
		id := strings.TrimPrefix(rel, "/videos/")
		return id != rel && !strings.Contains(id, "/") && openAIResourceID(id)
	case "video.content":
		if !strings.HasSuffix(rel, "/content") {
			return false
		}
		id := strings.TrimSuffix(strings.TrimPrefix(rel, "/videos/"), "/content")
		return id != "" && !strings.Contains(id, "/") && openAIResourceID(id)
	default:
		return false
	}
}

func applyCompatibleMediaRequestAuth(account *Account, request *http.Request) error {
	if account == nil || request == nil || request.URL == nil {
		return fmt.Errorf("unsupported compatible upstream operation")
	}
	action := compatibleMediaPathAction(request.URL.Path)
	if action == "" || !platformActionAdapterBound(account, action) {
		return fmt.Errorf("unsupported compatible upstream operation")
	}
	_, mode, err := account.compatibleMode()
	if err != nil {
		return err
	}
	protocol := mode.DefaultProtocol
	if declared := strings.TrimSpace(account.GetCredential("api_protocol")); declared != "" && declared != APIProtocolAdaptive {
		protocol = declared
	}
	endpoint, ok := mode.LookupEndpoint(protocol)
	if !ok {
		return fmt.Errorf("unsupported compatible upstream operation")
	}
	base, err := account.compatibleBase(endpoint)
	if err != nil {
		return err
	}
	parsedBase, err := url.Parse(base)
	if err != nil {
		return err
	}
	basePath := strings.TrimRight(parsedBase.EscapedPath(), "/")
	path := request.URL.EscapedPath()
	if !strings.HasPrefix(path, basePath+"/") {
		return fmt.Errorf("compatible upstream URL does not match the account endpoint")
	}
	rel := strings.TrimPrefix(path, basePath)
	if !strings.EqualFold(request.URL.Scheme, parsedBase.Scheme) || !strings.EqualFold(request.URL.Host, parsedBase.Host) || request.URL.RawQuery != "" || !compatibleMediaRelativePathAllowed(rel, action) {
		return fmt.Errorf("compatible upstream URL does not match the account endpoint")
	}
	setCompatibleAuth(request.Header, endpoint.Auth, account.GetCredential("api_key"))
	return nil
}

func applyCompatibleRequestAuth(account *Account, request *http.Request) error {
	if isCopilotNous(account) {
		return applyCompatibleSpecialAuth(account, request)
	}
	if !IsCompatibleProvider(account.Platform) {
		return nil
	}
	_, mode, err := account.compatibleMode()
	if err != nil {
		return err
	}
	protocol := ""
	for p, suffix := range map[string]string{APIProtocolChatCompletions: "/chat/completions", APIProtocolAnthropic: "/messages", APIProtocolResponses: "/responses"} {
		if strings.HasSuffix(request.URL.Path, suffix) {
			protocol = p
		}
	}
	if protocol == "" {
		return applyCompatibleMediaRequestAuth(account, request)
	}
	endpoint, ok := mode.LookupEndpoint(protocol)
	if !ok {
		return fmt.Errorf("unsupported compatible upstream operation")
	}
	if account.compatibleBinding != nil && account.compatibleBinding.Endpoint.Protocol != protocol {
		return fmt.Errorf("compatible protocol binding mismatch")
	}
	base, err := account.compatibleBase(endpoint)
	if err != nil {
		return err
	}
	suffix := map[string]string{APIProtocolChatCompletions: "/chat/completions", APIProtocolAnthropic: "/messages", APIProtocolResponses: "/responses"}[protocol]
	expected, err := url.Parse(strings.TrimRight(base, "/") + suffix)
	if err != nil || !strings.EqualFold(request.URL.Scheme, expected.Scheme) || !strings.EqualFold(request.URL.Host, expected.Host) || request.URL.EscapedPath() != expected.EscapedPath() || request.URL.RawQuery != expected.RawQuery {
		return fmt.Errorf("compatible upstream URL does not match the account endpoint")
	}
	setCompatibleAuth(request.Header, endpoint.Auth, account.GetCredential("api_key"))
	return nil
}
func buildCompatibleModelsRequest(ctx context.Context, account *Account, validate func(string) (string, error)) (*http.Request, error) {
	if isCopilotNous(account) {
		if _, _, err := account.compatibleMode(); err != nil {
			return nil, newUpstreamModelSyncConfigError("Invalid model discovery configuration", err)
		}
		config, err := compatibleSpecialAuthConfig(account)
		if err != nil {
			return nil, newUpstreamModelSyncConfigError("Invalid model discovery configuration", err)
		}
		endpoints, err := ResolveCopilotNousEndpoints(config)
		if err != nil {
			return nil, newUpstreamModelSyncConfigError("Invalid model discovery configuration", err)
		}
		for _, destination := range []string{endpoints.ChatBaseURL, endpoints.ModelsURL} {
			if _, err := validate(destination); err != nil {
				return nil, newUpstreamModelSyncConfigError("Invalid model discovery configuration", err)
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoints.ModelsURL, nil)
		if err != nil {
			return nil, newUpstreamModelSyncConfigError("Invalid model discovery configuration", err)
		}
		request.Header.Set("Accept", "application/json")
		account.ApplyHeaderOverrides(request.Header)
		if err := applyCompatibleSpecialAuth(account, request); err != nil {
			return nil, newUpstreamModelSyncConfigError("Invalid model discovery configuration", err)
		}
		return request, nil
	}
	_, mode, err := account.compatibleMode()
	if err != nil {
		return nil, newUpstreamModelSyncConfigError(err.Error(), err)
	}
	if mode.Discovery != "openai_models" {
		return nil, newUpstreamModelSyncDiscoveryUnconfirmedError("Model discovery has not been confirmed for this account mode; declare models manually", nil)
	}
	endpoint, ok := mode.LookupEndpoint(mode.DiscoveryProtocol)
	if !ok {
		return nil, newUpstreamModelSyncConfigError("Discovery endpoint is not configured; declare models manually", nil)
	}
	if strings.TrimSpace(account.GetCredential("base_url")) == "" {
		if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok && len(bases) > 0 {
			selected, _ := bases[endpoint.Protocol].(string)
			if strings.TrimSpace(selected) == "" {
				return nil, newUpstreamModelSyncConfigError("Custom endpoint has no matching discovery override; declare models manually", nil)
			}
		}
	}
	base, err := account.compatibleBase(endpoint)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid model discovery configuration", err)
	}
	base, err = validate(base)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid model discovery configuration", err)
	}
	key := strings.TrimSpace(account.GetCredential("api_key"))
	if key == "" {
		return nil, newUpstreamModelSyncConfigError("Missing API key", nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid model discovery configuration", err)
	}
	account.ApplyHeaderOverrides(req.Header)
	setCompatibleAuth(req.Header, endpoint.Auth, key)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func validateCompatibleAccount(account *Account) error {
	provider, mode, err := account.compatibleMode()
	if err != nil {
		return err
	}
	if err := provider.ValidateBillingPlan(account.Credentials); err != nil {
		return err
	}
	if isCopilotNous(account) {
		config, err := compatibleSpecialAuthConfig(account)
		if err != nil {
			return err
		}
		if _, err := ResolveCopilotNousEndpoints(config); err != nil {
			return err
		}
		_, referenced := CredentialReference(account.Credentials)
		_, supplied := account.Credentials["api_key"]
		if (!referenced || supplied) && !compatibleSpecialKeyOptional(account) && strings.TrimSpace(account.GetCredential("api_key")) == "" {
			return fmt.Errorf("api_key is required")
		}
	} else if strings.TrimSpace(account.GetCredential("api_key")) == "" {
		return fmt.Errorf("api_key is required")
	}
	protocol := strings.TrimSpace(account.GetCredential("api_protocol"))
	if protocol != "" && protocol != APIProtocolAdaptive {
		if _, ok := mode.LookupEndpoint(protocol); !ok {
			return fmt.Errorf("protocol is unavailable in selected mode")
		}
	}
	if raw, ok := account.Credentials["protocol_rules"]; ok {
		rules, err := parseModelProtocolRules(raw)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			if _, ok := mode.LookupEndpoint(rule.Protocol); !ok {
				return fmt.Errorf("protocol rule is unavailable in selected mode")
			}
		}
	}
	for _, endpoint := range mode.Endpoints {
		if _, err := account.compatibleBase(endpoint); err != nil {
			return err
		}
	}
	return nil
}

func (s *AccountTestService) testCompatibleAccountConnection(c *accountProbeContext, account *Account, model, prompt string) error {
	if strings.TrimSpace(model) == "" {
		return s.sendErrorAndEnd(c, "Select a declared model to test this account")
	}
	_, mode, err := account.compatibleMode()
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	action := map[string]string{APIProtocolChatCompletions: "chat", APIProtocolAnthropic: "messages", APIProtocolResponses: "responses"}[mode.DefaultProtocol]
	if p := account.GetAPIProtocol(); p != "" && p != APIProtocolAdaptive {
		action = map[string]string{APIProtocolChatCompletions: "chat", APIProtocolAnthropic: "messages", APIProtocolResponses: "responses"}[p]
	}
	bound, err := bindCompatibleRequest(c.ctx, account, action, account.GetMappedModel(model))
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	if isCopilotNous(bound) {
		base, err := s.validateUpstreamBaseURL(bound.GetOpenAIBaseURL())
		if err != nil {
			return s.sendErrorAndEnd(c, err.Error())
		}
		return s.testOpenAIChatCompletionsConnection(c, bound, account.GetMappedModel(model), prompt, base, bound.GetCredential("api_key"))
	}
	switch bound.GetAPIProtocol() {
	case APIProtocolAnthropic:
		return s.testCNProviderAnthropicConnection(c, bound, account.GetMappedModel(model))
	case APIProtocolResponses:
		return s.testOpenCodeGoResponsesConnection(c, bound, account.GetMappedModel(model))
	default:
		return s.testCNProviderChatCompletionsConnection(c, bound, model, prompt)
	}
}

// Observe optional counters without changing successful output or terminal events.
// Presence is separate from scalar zero and follows the result into settlement.
type TokenUsageObservation struct {
	InputTokensKnown  bool `json:"input_tokens_known"`
	OutputTokensKnown bool `json:"output_tokens_known"`
}

func (o *TokenUsageObservation) known() bool {
	return o == nil || (o.InputTokensKnown && o.OutputTokensKnown)
}

type compatibleUsageObservation struct{ inputKnown, outputKnown atomic.Bool }

func attachCompatibleUsage(account *Account, result *OpenAIForwardResult, forwardErr error) {
	if result == nil || account.compatibleBinding == nil {
		return
	}
	result.compatibleIncomplete = forwardErr != nil
	evidence := &account.compatibleBinding.observation
	result.TokenUsageObservation = &TokenUsageObservation{InputTokensKnown: evidence.inputKnown.Load(), OutputTokensKnown: evidence.outputKnown.Load()}
}

type compatibleUsageBody struct {
	source                io.ReadCloser
	scanner               *bufio.Scanner
	pending               []byte
	protocol              string
	limit                 int64
	initialized, terminal bool
	observation           *compatibleUsageObservation
}

func (b *compatibleUsageBody) Close() error { return b.source.Close() }
func (b *compatibleUsageBody) observe(raw []byte) {
	prefix := "usage."
	if gjson.GetBytes(raw, "type").String() == "message_start" {
		prefix = "message.usage."
	}
	if gjson.GetBytes(raw, "response").IsObject() {
		prefix = "response.usage."
	}
	input, output := "input_tokens", "output_tokens"
	if b.protocol == APIProtocolChatCompletions {
		input, output = "prompt_tokens", "completion_tokens"
	}
	if _, ok := openRouterCounter(raw, prefix+input); ok {
		b.observation.inputKnown.Store(true)
	}
	if _, ok := openRouterCounter(raw, prefix+output); ok && gjson.GetBytes(raw, "type").String() != "message_start" {
		b.observation.outputKnown.Store(true)
	}
}
func (b *compatibleUsageBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(b.pending) == 0 {
		if b.scanner == nil {
			if b.initialized {
				return 0, io.EOF
			}
			b.initialized = true
			raw, err := readUpstreamResponseBodyLimited(b.source, b.limit)
			if err != nil {
				return 0, err
			}
			b.observe(raw)

			if !gjson.ValidBytes(raw) || !gjson.ParseBytes(raw).IsObject() {
				return 0, fmt.Errorf("invalid compatible upstream JSON")
			}

			b.pending = raw
		} else {
			if !b.scanner.Scan() {
				if err := b.scanner.Err(); err != nil {
					return 0, err
				}
				if !b.terminal {
					return 0, newOpenAIUpstreamStreamReadError(fmt.Errorf("compatible upstream missing terminal event"))
				}
				return 0, io.EOF
			}
			line := b.scanner.Bytes()

			if bytes.HasPrefix(line, []byte("data:")) {
				raw := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
				b.observe(raw)
				kind := gjson.GetBytes(raw, "type").String()
				terminal := bytes.Equal(raw, []byte("[DONE]")) || kind == "message_stop" || openAIStreamEventTypeIsTerminal(kind)
				if b.protocol == APIProtocolChatCompletions {
					for _, choice := range gjson.GetBytes(raw, "choices").Array() {
						if choice.Get("finish_reason").Type == gjson.String && choice.Get("finish_reason").String() != "" {
							terminal = true
						}
					}
				}
				if !bytes.Equal(raw, []byte("[DONE]")) && !gjson.ValidBytes(raw) {
					return 0, fmt.Errorf("invalid compatible upstream SSE data")
				}
				if terminal || kind == "error" || kind == "response.failed" || kind == "response.incomplete" {
					b.terminal = true
				}
			}
			b.pending = append(append([]byte(nil), line...), '\n')
		}
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}
func compatibleCheckedResponse(account *Account, response *http.Response, limit int64) *http.Response {
	if response == nil || response.Body == nil || response.StatusCode >= 400 || !IsCompatibleProvider(account.Platform) {
		return response
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "application/json") && !strings.Contains(contentType, "text/event-stream") && !strings.Contains(contentType, "text/plain") {
		return response
	}
	protocol := account.GetAPIProtocol()
	observation := &compatibleUsageObservation{}
	if account.compatibleBinding != nil {
		observation = &account.compatibleBinding.observation
	}
	body := &compatibleUsageBody{source: response.Body, protocol: protocol, limit: limit, observation: observation}
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		body.scanner = bufio.NewScanner(response.Body)
		body.scanner.Buffer(make([]byte, 64*1024), int(limit))
	}
	response.Body = body
	return response
}
