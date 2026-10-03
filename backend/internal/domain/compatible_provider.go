package domain

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// CompatibleEndpoint describes a native protocol base, not a request URL.
// Presets use BaseURL; account-sourced endpoints append Path to the user resource
// base. Runtime resolves account overrides and appends the operation suffix.
type CompatibleEndpoint struct {
	Protocol string `json:"protocol"`
	BaseURL  string `json:"base_url,omitempty"`
	Path     string `json:"path,omitempty"`
	Auth     string `json:"auth"`
}

// CompatibleMode owns all transport and metadata facts for one account mode.
// Bridges name public gateway conversions, never additional vendor endpoints.
type CompatibleMode struct {
	Mode              string               `json:"mode"`
	DefaultProtocol   string               `json:"default_protocol"`
	Endpoints         []CompatibleEndpoint `json:"endpoints"`
	Bridges           []string             `json:"bridges"`
	Discovery         string               `json:"discovery"`
	DiscoveryProtocol string               `json:"discovery_protocol,omitempty"`
	CapabilityProfile string               `json:"capability_profile"`
}

// CompatibleProvider retains its own platform/budget identity. It cannot confer
// first-party credentials, model entitlement, pricing, quota or active probes.
type CompatibleProvider struct {
	ID               string           `json:"id"`
	Label            string           `json:"label"`
	LabelZh          string           `json:"label_zh"`
	BaseSource       string           `json:"base_source"`
	DefaultMode      string           `json:"default_mode"`
	Modes            []CompatibleMode `json:"modes"`
	EvidenceURL      string           `json:"evidence_url"`
	EvidenceObserved string           `json:"evidence_observed"`
}

const (
	CompatibleBridgeMessagesToChat      = "messages_to_chat"
	CompatibleBridgeResponsesToChat     = "responses_to_chat"
	CompatibleBridgeGeminiToChat        = "gemini_to_chat"
	CompatibleBridgeChatToMessages      = "chat_to_messages"
	CompatibleBridgeResponsesToMessages = "responses_to_messages"
	CompatibleBridgeGeminiToMessages    = "gemini_to_messages"
	CompatibleBridgeChatToResponses     = "chat_to_responses"
	CompatibleBridgeMessagesToResponses = "messages_to_responses"
	CompatibleBridgeGeminiToResponses   = "gemini_to_responses"
)

//go:embed data/compatible_providers/*.json
var compatibleProviderFiles embed.FS

type compatibleRegistry struct{ rows []CompatibleProvider }

// Published snapshots are immutable. Every public value is a deep copy.
var compatibleProviders = func() *atomic.Pointer[compatibleRegistry] {
	rows, err := loadCompatibleProviders(compatibleProviderFiles)
	if err != nil {
		panic("invalid embedded compatible providers: " + err.Error())
	}
	registry := &atomic.Pointer[compatibleRegistry]{}
	registry.Store(&compatibleRegistry{rows: rows})
	return registry
}()

var compatibleName = regexp.MustCompile(`^[a-z][a-z0-9]*([_-][a-z0-9]+)*$`)

func loadCompatibleProviders(files fs.FS) ([]CompatibleProvider, error) {
	names, err := fs.Glob(files, "data/compatible_providers/*.json")
	if err != nil {
		return nil, err
	}
	rows := []CompatibleProvider{}
	for _, name := range names { // fs.Glob returns sorted filenames.
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		batch, err := ParseCompatibleProviders(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		rows = append(rows, batch...)
	}
	if err := ValidateCompatibleProviders(rows); err != nil {
		return nil, err
	}
	sort.Slice(rows, func(left, right int) bool { return rows[left].ID < rows[right].ID })
	return rows, nil
}

// ParseCompatibleProviders rejects unknown fields and trailing JSON. It does not
// install rows; admission is exclusively the embedded, reviewed table.
func ParseCompatibleProviders(data []byte) ([]CompatibleProvider, error) {
	var rows []CompatibleProvider
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rows); err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, fmt.Errorf("provider table must be a JSON array")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("provider table has trailing JSON")
	}
	if err := ValidateCompatibleProviders(rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ValidateCompatibleProviders validates the complete table, including IDs across
// files. It does not normalize, mutate, or install caller-owned values.
func ValidateCompatibleProviders(rows []CompatibleProvider) error {
	seen := make(map[string]bool, len(rows))
	for _, provider := range rows {
		if !compatibleName.MatchString(provider.ID) || reservedCompatibleIdentity(provider.ID) || seen[provider.ID] {
			return fmt.Errorf("invalid, reserved or duplicate provider id %q", provider.ID)
		}
		seen[provider.ID] = true
		if strings.TrimSpace(provider.Label) == "" || strings.TrimSpace(provider.LabelZh) == "" {
			return fmt.Errorf("%s: labels are required", provider.ID)
		}
		if provider.BaseSource != "preset" && provider.BaseSource != "account" {
			return fmt.Errorf("%s: invalid base_source", provider.ID)
		}
		if !validCompatibleHTTPS(provider.EvidenceURL, true) {
			return fmt.Errorf("%s: invalid evidence_url", provider.ID)
		}
		if _, err := time.Parse("2006-01-02", provider.EvidenceObserved); err != nil {
			return fmt.Errorf("%s: invalid evidence_observed", provider.ID)
		}
		modes := map[string]bool{}
		for _, mode := range provider.Modes {
			if (mode.Mode != "payg" && mode.Mode != "coding") || modes[mode.Mode] {
				return fmt.Errorf("%s: invalid or duplicate mode %q", provider.ID, mode.Mode)
			}
			modes[mode.Mode] = true
			if err := validateCompatibleMode(provider.BaseSource, mode); err != nil {
				return fmt.Errorf("%s/%s: %w", provider.ID, mode.Mode, err)
			}
		}
		if !modes[provider.DefaultMode] {
			return fmt.Errorf("%s: default_mode must name an existing mode", provider.ID)
		}
	}
	return nil
}

func reservedCompatibleIdentity(identity string) bool {
	if identity == PlatformComposite || identity == "vertex" {
		return true
	}
	for _, descriptor := range platformDescriptors {
		if descriptor.ID == identity {
			return true
		}
	}
	return false
}

func foreignCompatibleProfile(profile string) bool {
	if reservedCompatibleIdentity(profile) {
		return true
	}
	switch profile {
	case "openai-codex", "google", "vertex-gemini", "vertex_ai", "vertex_ai-embedding-models", "vertex_ai-language-models", "xai", "grok-oauth",
		"alibaba", "alibaba-cn", "alibaba-coding-plan-cn", "moonshot", "moonshotai-cn", "kimi-for-coding", "minimax-cn", "minimax-coding-plan", "minimax-cn-coding-plan", "opencode-go", "zai", "zai-coding-plan", "zhipuai", "zhipuai-coding-plan", "text-completion-openai":
		return true
	}
	return false
}

func validateCompatibleMode(baseSource string, mode CompatibleMode) error {
	if !compatibleName.MatchString(mode.CapabilityProfile) || foreignCompatibleProfile(mode.CapabilityProfile) {
		return fmt.Errorf("invalid or foreign capability_profile %q", mode.CapabilityProfile)
	}
	protocols := map[string]bool{}
	for _, endpoint := range mode.Endpoints {
		if compatibleProtocolAction(endpoint.Protocol) == "" || protocols[endpoint.Protocol] {
			return fmt.Errorf("invalid or duplicate native protocol %q", endpoint.Protocol)
		}
		protocols[endpoint.Protocol] = true
		switch endpoint.Auth {
		case "bearer", "x_api_key", "api_key_header":
		default:
			return fmt.Errorf("invalid auth %q", endpoint.Auth)
		}
		if baseSource == "preset" {
			if endpoint.Path != "" || !validCompatibleHTTPS(endpoint.BaseURL, false) {
				return fmt.Errorf("preset endpoint requires an HTTPS base_url and no path")
			}
		} else if endpoint.BaseURL != "" || !validCompatiblePath(endpoint.Path) {
			return fmt.Errorf("account endpoint requires a path and no preset base_url")
		}
	}
	if !protocols[mode.DefaultProtocol] {
		return fmt.Errorf("default_protocol must name a native endpoint")
	}
	switch mode.Discovery {
	case "none":
		if mode.DiscoveryProtocol != "" {
			return fmt.Errorf("discovery_protocol requires discovery")
		}
	case "openai_models":
		if baseSource == "account" || !protocols[mode.DiscoveryProtocol] {
			return fmt.Errorf("discovery requires a native protocol on a preset source")
		}
	default:
		return fmt.Errorf("invalid discovery %q", mode.Discovery)
	}
	bridges := map[string]bool{}
	sources, targets := map[string]bool{}, map[string]bool{}
	for _, bridge := range mode.Bridges {
		source, target, ok := compatibleBridge(bridge)
		if !ok || bridges[bridge] || !protocols[target] {
			return fmt.Errorf("invalid, duplicate or missing-target bridge %q", bridge)
		}
		bridges[bridge], sources[source], targets[compatibleProtocolAction(target)] = true, true, true
	}
	for source := range sources {
		if targets[source] {
			return fmt.Errorf("bridge cycles and chains are not supported")
		}
	}
	return nil
}

func validCompatibleHTTPS(raw string, evidence bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || strings.ContainsAny(raw, "\\ \t\r\n") {
		return false
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return false
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return false
		}
	}
	if !evidence && (parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#")) {
		return false
	}
	return parsed.Path == "" || validCompatiblePath(parsed.EscapedPath())
}

func validCompatiblePath(raw string) bool {
	decoded, err := url.PathUnescape(raw)
	if err != nil || !strings.HasPrefix(decoded, "/") || strings.HasPrefix(decoded, "//") || strings.ContainsAny(decoded, "\\?#\r\n\t ") {
		return false
	}
	for _, character := range decoded {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." || strings.Contains(segment, "%") {
			return false
		}
	}
	return true
}

func copyCompatibleMode(mode CompatibleMode) CompatibleMode {
	mode.Endpoints = append([]CompatibleEndpoint(nil), mode.Endpoints...)
	if mode.Bridges != nil {
		mode.Bridges = append([]string{}, mode.Bridges...)
	}
	return mode
}

func copyCompatibleProvider(provider CompatibleProvider) CompatibleProvider {
	provider.Modes = append([]CompatibleMode(nil), provider.Modes...)
	for index := range provider.Modes {
		provider.Modes[index] = copyCompatibleMode(provider.Modes[index])
	}
	return provider
}

// CompatibleProviders returns deep copies ordered by ID, independent of file order.
func CompatibleProviders() []CompatibleProvider {
	rows := compatibleProviders.Load().rows
	result := make([]CompatibleProvider, len(rows))
	for index, provider := range rows {
		result[index] = copyCompatibleProvider(provider)
	}
	return result
}

func LookupCompatibleProvider(id string) (CompatibleProvider, bool) {
	for _, provider := range compatibleProviders.Load().rows {
		if provider.ID == id {
			return copyCompatibleProvider(provider), true
		}
	}
	return CompatibleProvider{}, false
}

// LookupMode is exact; only ResolveMode supplies the declared default for empty input.
func (provider CompatibleProvider) LookupMode(mode string) (CompatibleMode, bool) {
	for _, candidate := range provider.Modes {
		if candidate.Mode == mode {
			return copyCompatibleMode(candidate), true
		}
	}
	return CompatibleMode{}, false
}

func (provider CompatibleProvider) ResolveMode(mode string) (CompatibleMode, bool) {
	if mode == "" {
		mode = provider.DefaultMode
	}
	return provider.LookupMode(mode)
}

func (mode CompatibleMode) LookupEndpoint(protocol string) (CompatibleEndpoint, bool) {
	for _, endpoint := range mode.Endpoints {
		if endpoint.Protocol == protocol {
			return endpoint, true
		}
	}
	return CompatibleEndpoint{}, false
}

func compatibleProtocolAction(protocol string) string {
	switch protocol {
	case APIProtocolChatCompletions:
		return "chat"
	case APIProtocolAnthropic:
		return "messages"
	case APIProtocolResponses:
		return "responses"
	}
	return ""
}

func compatibleBridge(bridge string) (source, target string, ok bool) {
	switch bridge {
	case CompatibleBridgeMessagesToChat:
		return "messages", APIProtocolChatCompletions, true
	case CompatibleBridgeResponsesToChat:
		return "responses", APIProtocolChatCompletions, true
	case CompatibleBridgeGeminiToChat:
		return "generate_content", APIProtocolChatCompletions, true
	case CompatibleBridgeChatToMessages:
		return "chat", APIProtocolAnthropic, true
	case CompatibleBridgeResponsesToMessages:
		return "responses", APIProtocolAnthropic, true
	case CompatibleBridgeGeminiToMessages:
		return "generate_content", APIProtocolAnthropic, true
	case CompatibleBridgeChatToResponses:
		return "chat", APIProtocolResponses, true
	case CompatibleBridgeMessagesToResponses:
		return "messages", APIProtocolResponses, true
	case CompatibleBridgeGeminiToResponses:
		return "generate_content", APIProtocolResponses, true
	}
	return "", "", false
}

// ResolveAction returns one native endpoint and whether the public action needs
// conversion. A selected native protocol is strict. Empty/adaptive prefers native
// ingress, then the default endpoint, then fixed protocol order. It never chains
// bridges or falls back to another mode, base, credential, or capability profile.
func (mode CompatibleMode) ResolveAction(action, protocol string) (CompatibleEndpoint, EndpointSupport, bool) {
	resolve := func(target string) (CompatibleEndpoint, EndpointSupport, bool) {
		endpoint, ok := mode.LookupEndpoint(target)
		if !ok {
			return CompatibleEndpoint{}, EndpointRejected, false
		}
		if nativeAction := compatibleProtocolAction(target); nativeAction != "" && action == nativeAction {
			return endpoint, EndpointNative, true
		}
		for _, bridge := range mode.Bridges {
			source, destination, valid := compatibleBridge(bridge)
			if valid && source == action && destination == target {
				return endpoint, EndpointBridge, true
			}
		}
		return CompatibleEndpoint{}, EndpointRejected, false
	}
	if protocol != "" && protocol != APIProtocolAdaptive {
		return resolve(protocol)
	}
	for _, target := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		if compatibleProtocolAction(target) == action {
			if endpoint, support, ok := resolve(target); ok {
				return endpoint, support, true
			}
		}
	}
	for _, target := range []string{mode.DefaultProtocol, APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		if endpoint, support, ok := resolve(target); ok {
			return endpoint, support, true
		}
	}
	return CompatibleEndpoint{}, EndpointRejected, false
}

// Action is the account-mode bound. Empty mode selects DefaultMode; unknown modes
// fail closed. Profile, when present, must belong to this mode. Custom endpoint
// metadata trust and model permissions remain runtime responsibilities.
func (provider CompatibleProvider) Action(mode string, identity ActionIdentity, action string) ActionDescriptor {
	descriptor := ActionDescriptor{Identity: identity, Action: action, Status: ActionUnverified, Evidence: "No declared compatible-provider path for this mode, protocol and credential"}
	selected, ok := provider.ResolveMode(mode)
	if !ok || identity.Platform != provider.ID || (identity.HostedAccountType != "" && identity.HostedAccountType != AccountTypeAPIKey) || (identity.Profile != "" && identity.Profile != selected.CapabilityProfile) {
		return descriptor
	}
	endpoint, support, ok := selected.ResolveAction(action, identity.Protocol)
	if !ok {
		return descriptor
	}
	switch action {
	case "chat", "messages", "responses":
		descriptor.Adapter = "openai." + action
	case "generate_content":
		descriptor.Adapter = "gateway.generate_content_compat"
	default:
		return descriptor
	}
	descriptor.Status = ActionImplemented
	descriptor.Evidence = fmt.Sprintf("Declared %s path to %s in mode %s; %s (observed %s); runtime account/model validation required", support, endpoint.Protocol, selected.Mode, provider.EvidenceURL, provider.EvidenceObserved)
	descriptor.CredentialPolicy = "API key account; selected endpoint authentication and account base overrides"
	descriptor.ModelPolicy = "resolved account model evidence and task limits"
	descriptor.UsagePolicy = "existing adapter usage and frozen request pricing"
	descriptor.Lifecycle = "request plan"
	return descriptor
}

func (provider CompatibleProvider) descriptor() PlatformDescriptor {
	descriptor := PlatformDescriptor{ID: provider.ID, PassiveMonitor: true, ChatOnly: true}
	protocols := map[string]bool{}
	for _, mode := range provider.Modes {
		if mode.Discovery == "openai_models" {
			descriptor.ModelCatalog = true
		}
		if len(mode.Bridges) != 0 {
			descriptor.ChatOnly = false
		}
		for _, endpoint := range mode.Endpoints {
			protocols[endpoint.Protocol] = true
			if endpoint.Protocol != APIProtocolChatCompletions {
				descriptor.ChatOnly = false
			}
		}
		for action, destination := range map[string]*EndpointSupport{"responses": &descriptor.ResponsesPOST, "messages": &descriptor.MessagesPOST} {
			_, support, ok := mode.ResolveAction(action, "")
			if ok && (*destination == EndpointRejected || support == EndpointNative) {
				*destination = support
			}
		}
	}
	descriptor.MultiProtocol = len(protocols) > 1
	descriptor.NativeMultiProtocolResponses = descriptor.MultiProtocol && protocols[APIProtocolResponses]
	return descriptor
}

// PlatformAction lacks an account mode: this is only a provider-wide ingress
// bound. Runtime must call provider.Action with the selected account mode.
func (provider CompatibleProvider) ingressAction(identity ActionIdentity, action string) ActionDescriptor {
	descriptor := provider.Action(provider.DefaultMode, identity, action)
	if descriptor.Status == ActionImplemented {
		return descriptor
	}
	for _, mode := range provider.Modes {
		if candidate := provider.Action(mode.Mode, identity, action); candidate.Status == ActionImplemented {
			return candidate
		}
	}
	return descriptor
}
