package logredact

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// maxRedactDepth 限制递归深度以防止栈溢出
const maxRedactDepth = 32

var credentialKeys = []string{
	// OAuth
	"access_token", "refresh_token", "id_token", "agent_private_key",
	// API Key 类
	"api_key", "session_key", "cookie",
	// Grok Web SSO / password (must never persist or echo after Build OAuth)
	"password", "sso_token", "sso", "sso-rw", "clearTextPassword",
	// 云服务凭据
	"aws_secret_access_key", "aws_session_token",
	"volcengine_access_key_id", "volcengine_secret_access_key", "byteplus_access_key_id", "byteplus_secret_access_key",
	"tencent_query_secret_id", "tencent_query_secret_key", "usage_query_token",
	"service_account_json", "service_account", "private_key",
}

func CredentialKeys() []string { return append([]string(nil), credentialKeys...) }

var defaultSensitiveKeyList = append([]string{
	"api-key", "x-api-key", "x-goog-api-key", "authorization", "proxy-authorization",
	"key", "token", "secret", "set-cookie", "authorization_code", "code",
	"code_verifier", "client_secret", "temp_token", "password", "secret_key",
}, credentialKeys...)

var defaultSensitiveKeys = func() map[string]struct{} {
	keys := make(map[string]struct{}, len(defaultSensitiveKeyList))
	for _, key := range defaultSensitiveKeyList {
		keys[normalizeKey(key)] = struct{}{}
	}
	return keys
}()

const maxRedactBytes = 256 << 10

type textRedactPatterns struct {
	reJSONLike  *regexp.Regexp
	reQueryLike *regexp.Regexp
	rePlain     *regexp.Regexp
}

var (
	reDiagnosticURI   = regexp.MustCompile(`(?i)(?:https?|socks5h?)://[^\s"<>]+`)
	reDiagnosticEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	reAuthorization   = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:bearer|basic)\s+[^\s,;"}]+`)
	reURIUser         = regexp.MustCompile(`(?i)((?:https?|socks5h?)://)[^/\s?#"<>]+@`)
	reOpenAIKey       = regexp.MustCompile(`\bsk-[0-9A-Za-z_-]{6,}`)
	reHTTPBody        = regexp.MustCompile(`(?i)\b(?:body|response)\s*[:=]\s*[\{<]`)
	reGOCSPX          = regexp.MustCompile(`GOCSPX-[0-9A-Za-z_-]{24,}`)
	reAIza            = regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)

	defaultTextRedactPatterns = compileTextRedactPatterns(nil)
	extraTextPatternCache     sync.Map // map[string]*textRedactPatterns
)

func RedactMap(input map[string]any, extraKeys ...string) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	keys := buildKeySet(extraKeys)
	redacted, ok := redactValueWithDepth(input, keys, getTextRedactPatterns(extraKeys), 0).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return redacted
}

func RedactJSON(raw []byte, extraKeys ...string) string {
	if len(raw) == 0 {
		return ""
	}
	if len(raw) > maxRedactBytes {
		return "<payload size limit exceeded>"
	}
	value, ok := decodeJSON(raw)
	if !ok {
		return "<non-json payload redacted>"
	}
	redacted := redactValueWithDepth(value, buildKeySet(extraKeys), getTextRedactPatterns(extraKeys), 0)
	return encodeJSON(redacted)
}

func decodeJSON(raw []byte) (any, bool) {
	if !json.Valid(raw) {
		return nil, false
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&value)
	return value, err == nil
}

func encodeJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "<redacted>"
	}
	return string(encoded)
}

// RedactText 对非结构化文本做轻量脱敏。
//
// 规则：
// - 如果文本本身是 JSON，则按 RedactJSON 处理。
// - 否则尝试对常见 key=value / key:"value" 片段做脱敏。
//
// 注意：该函数用于日志/错误信息兜底，不保证覆盖所有格式。
func RedactText(input string, extraKeys ...string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	if len(input) > maxRedactBytes {
		return "<payload size limit exceeded>"
	}
	raw := []byte(input)
	if json.Valid(raw) {
		return RedactJSON(raw, extraKeys...)
	}
	return redactPlainText(input, getTextRedactPatterns(extraKeys))
}

func redactPlainText(input string, patterns *textRedactPatterns) string {
	out := reAuthorization.ReplaceAllString(input, `${1}***`)
	out = reDiagnosticURI.ReplaceAllStringFunc(out, RedactURI)
	out = reOpenAIKey.ReplaceAllString(out, "sk-***")
	out = reGOCSPX.ReplaceAllString(out, "GOCSPX-***")
	out = reAIza.ReplaceAllString(out, "AIza***")
	out = patterns.reJSONLike.ReplaceAllString(out, `$1***$3`)
	out = patterns.reQueryLike.ReplaceAllString(out, `$1=***`)
	out = patterns.rePlain.ReplaceAllString(out, `$1$2***`)
	return out
}

func compileTextRedactPatterns(extraKeys []string) *textRedactPatterns {
	keyAlt := buildKeyAlternation(extraKeys)
	return &textRedactPatterns{
		// JSON-like: "access_token":"..."
		reJSONLike: regexp.MustCompile(`(?i)("(?:` + keyAlt + `)"\s*:\s*")((?:\\.|[^"\\])*)("|$)`),
		// Query-like: access_token=...
		reQueryLike: regexp.MustCompile(`(?i)\b((?:` + keyAlt + `))=([^&\s]+)`),
		// Plain: access_token: ... / access_token = ...
		rePlain: regexp.MustCompile(`(?i)\b((?:` + keyAlt + `))\b(\s*[:=]\s*)([^,\s]+)`),
	}
}

func getTextRedactPatterns(extraKeys []string) *textRedactPatterns {
	normalizedExtraKeys := normalizeAndSortExtraKeys(extraKeys)
	if len(normalizedExtraKeys) == 0 {
		return defaultTextRedactPatterns
	}

	cacheKey := strings.Join(normalizedExtraKeys, ",")
	if cached, ok := extraTextPatternCache.Load(cacheKey); ok {
		if patterns, ok := cached.(*textRedactPatterns); ok {
			return patterns
		}
	}

	compiled := compileTextRedactPatterns(normalizedExtraKeys)
	actual, _ := extraTextPatternCache.LoadOrStore(cacheKey, compiled)
	if patterns, ok := actual.(*textRedactPatterns); ok {
		return patterns
	}
	return compiled
}

func normalizeAndSortExtraKeys(extraKeys []string) []string {
	if len(extraKeys) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(extraKeys))
	keys := make([]string, 0, len(extraKeys))
	for _, key := range extraKeys {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		keys = append(keys, normalized)
	}
	sort.Strings(keys)
	return keys
}

func buildKeyAlternation(extraKeys []string) string {
	keys := make([]string, 0, len(defaultSensitiveKeyList)+len(extraKeys))
	for _, key := range append(append([]string(nil), defaultSensitiveKeyList...), extraKeys...) {
		quoted := regexp.QuoteMeta(key)
		quoted = strings.NewReplacer("_", "[-_]?", "-", "[-_]?").Replace(quoted)
		keys = append(keys, quoted)
	}
	return strings.Join(keys, "|")
}

func buildKeySet(extraKeys []string) map[string]struct{} {
	keys := make(map[string]struct{}, len(defaultSensitiveKeys)+len(extraKeys))
	for k := range defaultSensitiveKeys {
		keys[k] = struct{}{}
	}
	for _, key := range extraKeys {
		normalized := normalizeKey(key)
		if normalized == "" {
			continue
		}
		keys[normalized] = struct{}{}
	}
	return keys
}

func redactValueWithDepth(value any, keys map[string]struct{}, patterns *textRedactPatterns, depth int) any {
	if depth > maxRedactDepth {
		return "<depth limit exceeded>"
	}

	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			if isSensitiveKey(k, keys) {
				out[k] = "***"
				continue
			}
			out[k] = redactValueWithDepth(val, keys, patterns, depth+1)
		}
		return out
	case string:
		if len(v) > maxRedactBytes {
			return "<payload size limit exceeded>"
		}
		trimmed := strings.TrimSpace(v)
		if len(trimmed) > 0 && strings.ContainsRune(`[{"`, rune(trimmed[0])) {
			if inner, ok := decodeJSON([]byte(trimmed)); ok {
				return encodeJSON(redactValueWithDepth(inner, keys, patterns, depth+1))
			}
		}
		return redactPlainText(v, patterns)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactValueWithDepth(item, keys, patterns, depth+1)
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string, keys map[string]struct{}) bool {
	_, ok := keys[normalizeKey(key)]
	return ok
}

func normalizeKey(key string) string {
	return strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(key)))
}

// RedactDiagnostic removes credentials and private routing/identity details from
// a human-readable upstream reason. Never return an HTTP body verbatim to an SDK.
func RedactDiagnostic(message string) string {
	if reHTTPBody.MatchString(message) {
		return "Upstream request failed"
	}
	if json.Valid([]byte(message)) {
		return "Upstream request failed"
	}
	message = RedactText(message, "account_id", "user_id", "organization_id", "project_id", "host", "hostname")
	lowered := strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(lowered, "dial tcp") || strings.Contains(lowered, "dial udp") || strings.Contains(lowered, "lookup ") {
		return "Upstream connection failed"
	}
	if strings.Contains(lowered, "<html") || strings.Contains(lowered, "<!doctype") || strings.HasPrefix(lowered, "http/") {
		return "Upstream request failed"
	}
	message = reDiagnosticURI.ReplaceAllString(message, "[upstream]")
	return reDiagnosticEmail.ReplaceAllString(message, "[identity]")
}

// RedactURI parses query names before matching so encoded Gemini keys are covered.
func RedactURI(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return reURIUser.ReplaceAllString(raw, `${1}***@`)
	}
	changed := false
	if parsed.User != nil {
		parsed.User = url.User("***")
		changed = true
	}
	query := parsed.Query()
	for key := range query {
		if isSensitiveKey(key, defaultSensitiveKeys) {
			query.Set(key, "***")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
