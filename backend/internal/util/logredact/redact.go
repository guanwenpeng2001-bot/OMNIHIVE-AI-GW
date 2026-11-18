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

var defaultSensitiveKeys = map[string]struct{}{
	"api_key":             {},
	"api-key":             {},
	"apikey":              {},
	"x-api-key":           {},
	"x-goog-api-key":      {},
	"authorization":       {},
	"proxy-authorization": {},
	"key":                 {},
	"token":               {},
	"secret":              {},
	"cookie":              {},
	"set-cookie":          {},

	"authorization_code": {},
	"code":               {},
	"code_verifier":      {},
	"access_token":       {},
	"refresh_token":      {},
	"id_token":           {},
	"client_secret":      {},
	"password":           {},
}

var defaultSensitiveKeyList = []string{
	"api_key",
	"api-key",
	"apikey",
	"x-api-key",
	"x-goog-api-key",
	"authorization",
	"proxy-authorization",
	"key",
	"token",
	"secret",
	"cookie",
	"set-cookie",

	"authorization_code",
	"code",
	"code_verifier",
	"access_token",
	"refresh_token",
	"id_token",
	"client_secret",
	"password",
}

type textRedactPatterns struct {
	reJSONLike  *regexp.Regexp
	reQueryLike *regexp.Regexp
	rePlain     *regexp.Regexp
}

var (
	reDiagnosticURI   = regexp.MustCompile(`https?://[^\s"<>]+`)
	reDiagnosticEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	reAuthorization   = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:bearer|basic)\s+[^\s,;"}]+`)
	reURIUser         = regexp.MustCompile(`(https?://)[^/\s:@]+:[^/\s@]+@`)
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
	redacted, ok := redactValueWithDepth(input, keys, 0).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return redacted
}

func RedactJSON(raw []byte, extraKeys ...string) string {
	if len(raw) == 0 {
		return ""
	}
	if !json.Valid(raw) {
		return "<non-json payload redacted>"
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "<non-json payload redacted>"
	}
	keys := buildKeySet(extraKeys)
	redacted := redactValueWithDepth(value, keys, 0)
	encoded, err := json.Marshal(redacted)
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

	raw := []byte(input)
	if json.Valid(raw) {
		return RedactJSON(raw, extraKeys...)
	}

	patterns := getTextRedactPatterns(extraKeys)

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
		normalized := normalizeKey(key)
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
	seen := make(map[string]struct{}, len(defaultSensitiveKeyList)+len(extraKeys))
	keys := make([]string, 0, len(defaultSensitiveKeyList)+len(extraKeys))
	for _, k := range defaultSensitiveKeyList {
		seen[k] = struct{}{}
		keys = append(keys, regexp.QuoteMeta(k))
	}
	for _, k := range extraKeys {
		n := normalizeKey(k)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		keys = append(keys, regexp.QuoteMeta(n))
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

func redactValueWithDepth(value any, keys map[string]struct{}, depth int) any {
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
			out[k] = redactValueWithDepth(val, keys, depth+1)
		}
		return out
	case string:
		patterns := getTextRedactPatterns(nil)
		redacted := patterns.reQueryLike.ReplaceAllString(v, `$1=***`)
		redacted = reOpenAIKey.ReplaceAllString(redacted, "sk-***")
		redacted = reGOCSPX.ReplaceAllString(redacted, "GOCSPX-***")
		redacted = reAIza.ReplaceAllString(redacted, "AIza***")
		redacted = reAuthorization.ReplaceAllString(redacted, `${1}***`)
		return reDiagnosticURI.ReplaceAllStringFunc(redacted, RedactURI)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactValueWithDepth(item, keys, depth+1)
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
	return strings.ToLower(strings.TrimSpace(key))
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
