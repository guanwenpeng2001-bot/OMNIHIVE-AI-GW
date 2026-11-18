package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	maxStagedModelMappings  = 1024
	maxStagedModelNameBytes = 256
)

var errStagedConfiguration = infraerrors.BadRequest("INVALID_STAGED_CONFIGURATION", "unsupported staged configuration or invalid field type/range; runtime evidence, group and proxy bindings cannot be imported")

// These are upstream identity selectors, never local resource/group/proxy IDs.
// Display names, model rules, modes, scopes and expiry times are deliberately
// excluded: changing them must not make a duplicate endpoint/key look unique.
var stagedIdentityFields = []string{
	"tenant", "tenant_id", "project", "project_id", "organization_id", "org_id",
	"account_id", "chatgpt_account_id", "chatgpt_user_id", "chatgpt_workspace_id",
	"client_id", "region", "aws_region", "aws_access_key_id", "antigravity_project_id",
	"zhipu_organization", "zhipu_project",
}

func stagedText(value any, limit int, empty bool) bool {
	text, ok := value.(string)
	return ok && len(text) <= limit && (empty || strings.TrimSpace(text) != "") && !strings.ContainsFunc(text, unicode.IsControl)
}

func validStagedIdentityField(field string, value any) bool {
	for _, candidate := range stagedIdentityFields {
		if candidate == field {
			return stagedText(value, 256, true)
		}
	}
	return false
}

// Canonicalize only URL components with known endpoint equivalence. Preserve
// path case, escaped separators and internal path structure; never contact DNS.
func normalizedStagedEndpoint(value any) (string, error) {
	text, ok := value.(string)
	if !ok || !stagedText(text, 2048, false) {
		return "", errStagedConfiguration
	}
	u, err := url.Parse(strings.TrimSpace(text))
	if err != nil || u.Opaque != "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", errStagedConfiguration
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errStagedConfiguration
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", errStagedConfiguration
		}
		port = strconv.Itoa(number)
		if (u.Scheme == "https" && number == 443) || (u.Scheme == "http" && number == 80) {
			port = ""
		}
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	// Work with the escaped path so /a%2Fb and /a/b remain distinct.
	return u.Scheme + "://" + host + strings.TrimRight(u.EscapedPath(), "/"), nil
}

func stagedStringMap(raw any, limit int, validate func(string, string) bool) bool {
	switch values := raw.(type) {
	case map[string]any:
		if len(values) > limit {
			return false
		}
		for key, value := range values {
			text, ok := value.(string)
			if !ok || !validate(key, text) {
				return false
			}
		}
	case map[string]string:
		if len(values) > limit {
			return false
		}
		for key, value := range values {
			if !validate(key, value) {
				return false
			}
		}
	default:
		return false
	}
	return true
}

func stagedEnum(value any, choices ...string) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, choice := range choices {
		if text == choice {
			return true
		}
	}
	return false
}

func stagedInteger(value any, min, max int) bool {
	var number float64
	switch value := value.(type) {
	case int:
		number = float64(value)
	case int64:
		number = float64(value)
	case float64:
		number = value
	default:
		return false
	}
	return number >= float64(min) && number <= float64(max) && math.Trunc(number) == number
}

func validStagedConfiguration(field string, value any) bool {
	switch field {
	case "machine_id", "mac_machine_id":
		// Native Cursor's existing normalizer uses 32-byte hex identifiers.
		// These are non-secret client configuration, not cached session tokens.
		text, ok := value.(string)
		if !ok || len(text) != 64 {
			return false
		}
		_, err := hex.DecodeString(text)
		return err == nil
	case "model_mapping", "compact_model_mapping":
		return stagedStringMap(value, maxStagedModelMappings, func(name, target string) bool {
			return stagedText(name, maxStagedModelNameBytes, false) && stagedText(target, maxStagedModelNameBytes, false) &&
				!strings.ContainsFunc(name, unicode.IsSpace) && !strings.ContainsFunc(target, unicode.IsSpace)
		})
	case "account_mode":
		return stagedEnum(value, "payg", "coding", "go", "zen")
	case "api_protocol":
		return stagedEnum(value, APIProtocolAdaptive, APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses)
	case "billing_plan", "client_version":
		return stagedText(value, 128, false)
	case "api_base_urls":
		return stagedStringMap(value, 3, func(protocol, endpoint string) bool {
			_, err := normalizedStagedEndpoint(endpoint)
			return isNativeModelProtocol(protocol) && err == nil
		})
	case "protocol_rules":
		items, err := modelProtocolRuleItems(value)
		if err != nil {
			return false
		}
		for _, item := range items {
			rule, ok := item.(map[string]any)
			if !ok || len(rule) != 2 || !stagedText(rule["pattern"], maxModelProtocolPatternLength, false) {
				return false
			}
			if _, ok := rule["protocol"]; !ok {
				return false
			}
		}
		_, err = parseModelProtocolRules(value)
		return err == nil
	case "openai_capabilities":
		switch items := value.(type) {
		case []any:
			if len(items) < 1 || len(items) > 2 {
				return false
			}
		case []string:
			if len(items) < 1 || len(items) > 2 {
				return false
			}
		default:
			return false
		}
		_, _, err := normalizeBulkOpenAIEndpointCapabilities(value)
		return err == nil
	case "intercept_warmup_requests", "pool_mode", "custom_error_codes_enabled":
		_, ok := value.(bool)
		return ok
	case "pool_mode_retry_count":
		return stagedInteger(value, 0, maxPoolModeRetryCount)
	case "account_scheduling_threshold":
		return stagedInteger(value, 1, 100)
	case "pool_mode_retry_status_codes", "custom_error_codes":
		items, ok := value.([]any)
		if !ok || len(items) > 100 {
			return false
		}
		for _, item := range items {
			if !stagedInteger(item, 400, 599) {
				return false
			}
		}
		return true
	}
	return false
}

// A flat, explicit set of administrator routing choices. Never import model
// catalogs, capability evidence/releases, usage, probes, groups or proxy state.
func ValidateStagedImportExtra(extra map[string]any) error {
	for field, value := range extra {
		switch field {
		case "custom_base_url_enabled", "mixed_scheduling", "openai_passthrough", "openai_oauth_passthrough", "anthropic_passthrough":
			if _, ok := value.(bool); !ok {
				return errStagedConfiguration
			}
		case "openai_compact_mode":
			if !stagedEnum(value, "auto", "force_on", "force_off") {
				return errStagedConfiguration
			}
		case "openai_responses_mode":
			if !stagedEnum(value, "auto", "force_responses", "force_chat_completions") {
				return errStagedConfiguration
			}
		default:
			return errStagedConfiguration
		}
	}
	return nil
}

// The digest is request-local and must never be logged, persisted or returned.
func StagedAccountImportFingerprint(platform, accountType string, credentials map[string]any) ([32]byte, error) {
	if err := validateStagedAccountCredentials(platform, accountType, credentials); err != nil {
		return [32]byte{}, err
	}
	endpoints, identity := make(map[string]string), make(map[string]string)
	if raw, exists := credentials["base_url"]; exists {
		endpoints["base_url"], _ = normalizedStagedEndpoint(raw)
	}
	if raw, exists := credentials["api_base_urls"]; exists {
		stagedStringMap(raw, 3, func(protocol, endpoint string) bool {
			endpoints[protocol], _ = normalizedStagedEndpoint(endpoint)
			return true
		})
	}
	if IsCompatibleProvider(platform) {
		// Use the same pure endpoint resolution as forwarding: Kaggle's root and
		// /v1 prefix are equivalent, including explicit per-protocol overrides.
		copy := make(map[string]any, len(credentials))
		for key, value := range credentials {
			copy[key] = value
		}
		if raw, exists := credentials["api_base_urls"]; exists {
			bases := make(map[string]any)
			stagedStringMap(raw, 3, func(protocol, endpoint string) bool { bases[protocol] = endpoint; return true })
			copy["api_base_urls"] = bases
		}
		account := &Account{Platform: platform, Type: accountType, Credentials: copy}
		_, mode, err := account.compatibleMode()
		if err != nil {
			return [32]byte{}, errStagedConfiguration
		}
		endpoints = make(map[string]string)
		for _, endpoint := range mode.Endpoints {
			base, err := account.compatibleBase(endpoint)
			if err != nil {
				return [32]byte{}, errStagedConfiguration
			}
			endpoints[endpoint.Protocol], err = normalizedStagedEndpoint(base)
			if err != nil {
				return [32]byte{}, errStagedConfiguration
			}
		}
	} else if platform == PlatformCursor && accountType == AccountTypeUpstream {
		copy := make(map[string]any, len(credentials))
		for key, value := range credentials {
			copy[key] = value
		}
		if raw, exists := credentials["api_base_urls"]; exists {
			bases := make(map[string]any)
			stagedStringMap(raw, 3, func(protocol, endpoint string) bool { bases[protocol] = endpoint; return true })
			copy["api_base_urls"] = bases
		}
		account := &Account{Platform: platform, Type: accountType, Credentials: copy}
		if err := NormalizeCursorAccount(account); err != nil {
			return [32]byte{}, errStagedConfiguration
		}
		endpoints = make(map[string]string)
		for protocol, endpoint := range map[string]string{"chat_completions": "/chat/completions", "anthropic": "/messages", "responses": "/responses", "models": "/models"} {
			if protocol != "models" && account.GetCredential("api_protocol") == APIProtocolChatCompletions {
				endpoint = "/chat/completions"
			}
			resolved, err := normalizedStagedEndpoint(cursorRelayURL(account, endpoint))
			if err != nil {
				return [32]byte{}, errStagedConfiguration
			}
			endpoints[protocol] = resolved
		}
	}
	for _, field := range stagedIdentityFields {
		if value, ok := credentials[field].(string); ok && strings.TrimSpace(value) != "" {
			identity[field] = strings.TrimSpace(value)
		}
	}
	_, secrets := SplitCredentialSecrets(credentials)
	encoded, err := json.Marshal([]any{platform, accountType, endpoints, identity, secrets})
	if err != nil {
		return [32]byte{}, errStagedConfiguration
	}
	return sha256.Sum256(encoded), nil
}
