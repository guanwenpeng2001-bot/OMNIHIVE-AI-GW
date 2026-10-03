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

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/tidwall/gjson"
)

// A request binding never changes persisted identity, budgets, proxy or credentials.
type compatibleRequestBinding struct {
	Endpoint domain.CompatibleEndpoint
	Base     string
	Action   string
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
	if strings.TrimSpace(account.GetCredential("api_key")) == "" {
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
func applyCompatibleRequestAuth(account *Account, request *http.Request) error {
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
	_, mode, err := account.compatibleMode()
	if err != nil {
		return nil, newUpstreamModelSyncConfigError(err.Error(), err)
	}
	if mode.Discovery != "openai_models" {
		return nil, newUpstreamModelSyncUnsupportedError("Model discovery is unsupported for this account mode; declare models manually", nil)
	}
	endpoint, ok := mode.LookupEndpoint(mode.DiscoveryProtocol)
	if !ok {
		return nil, newUpstreamModelSyncUnsupportedError("No discovery endpoint; declare models manually", nil)
	}
	if strings.TrimSpace(account.GetCredential("base_url")) == "" {
		if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok && len(bases) > 0 {
			selected, _ := bases[endpoint.Protocol].(string)
			if strings.TrimSpace(selected) == "" {
				return nil, newUpstreamModelSyncUnsupportedError("Custom endpoint has no matching discovery override; declare models manually", nil)
			}
		}
	}
	base, err := account.compatibleBase(endpoint)
	if err != nil {
		return nil, err
	}
	base, err = validate(base)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(account.GetCredential("api_key"))
	if key == "" {
		return nil, newUpstreamModelSyncConfigError("Missing API key", nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	account.ApplyHeaderOverrides(req.Header)
	setCompatibleAuth(req.Header, endpoint.Auth, key)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func validateCompatibleAccount(account *Account) error {
	_, mode, err := account.compatibleMode()
	if err != nil {
		return err
	}
	if strings.TrimSpace(account.GetCredential("api_key")) == "" {
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
	switch bound.GetAPIProtocol() {
	case APIProtocolAnthropic:
		return s.testCNProviderAnthropicConnection(c, bound, account.GetMappedModel(model))
	case APIProtocolResponses:
		return s.testOpenCodeGoResponsesConnection(c, bound, account.GetMappedModel(model))
	default:
		return s.testCNProviderChatCompletionsConnection(c, bound, model, prompt)
	}
}

// Require observed counters, including legitimate zero values, before reporting
// a successful compatible generation. The wrapper preserves incremental SSE and
// prevents existing converters from treating absent counters as a billed zero.
type compatibleUsageBody struct {
	source                               io.ReadCloser
	scanner                              *bufio.Scanner
	pending                              []byte
	protocol                             string
	limit                                int64
	initialized, inputKnown, outputKnown bool
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
		b.inputKnown = true
	}
	if _, ok := openRouterCounter(raw, prefix+output); ok && gjson.GetBytes(raw, "type").String() != "message_start" {
		b.outputKnown = true
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
			if !b.inputKnown || !b.outputKnown {
				return 0, newOpenAIUpstreamStreamReadError(fmt.Errorf("compatible upstream usage is unknown"))
			}
			b.pending = raw
		} else {
			if !b.scanner.Scan() {
				if err := b.scanner.Err(); err != nil {
					return 0, err
				}
				if !b.inputKnown || !b.outputKnown {
					return 0, newOpenAIUpstreamStreamReadError(fmt.Errorf("compatible upstream usage is unknown"))
				}
				return 0, io.EOF
			}
			line := b.scanner.Bytes()
			// Native Messages considers the event header itself terminal. Withhold it
			// until both counters have been seen, not merely the following data line.
			if bytes.HasPrefix(line, []byte("event:")) && strings.TrimSpace(string(bytes.TrimPrefix(line, []byte("event:")))) == "message_stop" && (!b.inputKnown || !b.outputKnown) {
				return 0, newOpenAIUpstreamStreamReadError(fmt.Errorf("compatible upstream usage is unknown"))
			}

			if bytes.HasPrefix(line, []byte("data:")) {
				raw := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
				b.observe(raw)
				kind := gjson.GetBytes(raw, "type").String()
				terminal := bytes.Equal(raw, []byte("[DONE]")) || kind == "message_stop" || kind == "response.completed"
				if terminal && (!b.inputKnown || !b.outputKnown) {
					return 0, newOpenAIUpstreamStreamReadError(fmt.Errorf("compatible upstream usage is unknown"))
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
	protocol := account.GetAPIProtocol()
	body := &compatibleUsageBody{source: response.Body, protocol: protocol, limit: limit}
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		body.scanner = bufio.NewScanner(response.Body)
		body.scanner.Buffer(make([]byte, 64*1024), int(limit))
	}
	response.Body = body
	return response
}
