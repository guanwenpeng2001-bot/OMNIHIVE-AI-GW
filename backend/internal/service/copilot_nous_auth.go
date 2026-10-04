package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	CopilotAuthFineGrainedPAT = "fine_grained_pat"
	NousAuthInvocationToken   = "invocation_token"
	NousAuthOfficialProxy     = "official_proxy"
	CopilotIntegrationID      = "omnihive"
	CopilotModelsAPIVersion   = "2026-07-01"
)

// Errors deliberately contain no input values: a malformed URL may itself
// contain credentials. Callers may classify these with errors.Is.
var (
	ErrCopilotNousAuthMode           = errors.New("unsupported Copilot or Nous authentication mode")
	ErrCopilotNousBaseURL            = errors.New("invalid Copilot or Nous base URL")
	ErrCopilotNousCredential         = errors.New("invalid resolved Copilot or Nous credential")
	ErrCopilotNousRefreshUnsupported = errors.New("Copilot and Nous HTTP authentication does not support refresh credentials") //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
	ErrCopilotNousExpired            = errors.New("Nous invocation JWT unverified expiry has elapsed; supply a new token")     //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
	ErrCopilotNousScope              = errors.New("Nous invocation JWT requires an unverified inference:invoke scope claim")   //nolint:staticcheck // ST1005: preserve the existing externally surfaced provider error text.
	ErrCopilotNousRequest            = errors.New("unsupported Copilot or Nous outgoing request destination or method")
)

// CopilotNousAuthConfig contains no secrets. HasRefreshToken means the field is
// present, even if empty; check both inline and resolved credential maps. This
// metadata validation can run on saves containing legitimate secret references.
type CopilotNousAuthConfig struct {
	Platform        string
	AuthMode        string
	BaseURL         string
	HasRefreshToken bool
}

// CopilotNousEndpoints describes only Chat Completions and model discovery.
// Public Messages, Responses and Gemini bridges are separate gateway features;
// these helpers establish no native support for those protocols.
type CopilotNousEndpoints struct {
	ChatBaseURL string
	ChatURL     string
	ModelsURL   string
}

var copilotResidencyHost = regexp.MustCompile(`^copilot-api\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.ghe\.com$`)

// ResolveCopilotNousEndpoints performs pure syntax/mode validation, not egress
// authorization. In particular, a syntactically valid proxy URL is NOT approved
// for loopback/private access. Callers must retain the existing URL/SSRF, DNS,
// outbound proxy and transport policy before sending to any returned URL.
func ResolveCopilotNousEndpoints(config CopilotNousAuthConfig) (CopilotNousEndpoints, error) {
	var endpoints CopilotNousEndpoints
	if config.HasRefreshToken {
		return endpoints, ErrCopilotNousRefreshUnsupported
	}
	base := config.BaseURL
	switch config.Platform {
	case "github_copilot":
		if config.AuthMode != "" && config.AuthMode != CopilotAuthFineGrainedPAT {
			return endpoints, ErrCopilotNousAuthMode
		}
		if base == "" {
			base = "https://api.githubcopilot.com/v1"
		}
		parsed, err := parseCopilotNousBase(base)
		if err != nil || parsed.Scheme != "https" {
			return endpoints, ErrCopilotNousBaseURL
		}
		host := parsed.Host
		if host != "api.githubcopilot.com" && host != "api.enterprise.githubcopilot.com" && host != "api.business.githubcopilot.com" && !copilotResidencyHost.MatchString(host) {
			return endpoints, ErrCopilotNousBaseURL
		}
		if parsed.Path != "" && parsed.Path != "/v1" {
			return endpoints, ErrCopilotNousBaseURL
		}
		endpoints.ChatBaseURL = "https://" + host + "/v1"
		endpoints.ModelsURL = "https://" + host + "/models"
	case "nous_research":
		switch config.AuthMode {
		case NousAuthInvocationToken:
			const invocationBase = "https://inference-api.nousresearch.com/v1"
			if base != "" && base != invocationBase && base != invocationBase+"/" {
				return endpoints, ErrCopilotNousBaseURL
			}
			endpoints.ChatBaseURL = invocationBase
		case NousAuthOfficialProxy:
			parsed, err := parseCopilotNousBase(base)
			if err != nil {
				return endpoints, ErrCopilotNousBaseURL
			}
			endpoints.ChatBaseURL = parsed.String()
		default:
			return endpoints, ErrCopilotNousAuthMode
		}
		endpoints.ModelsURL = endpoints.ChatBaseURL + "/models"
	default:
		return endpoints, ErrCopilotNousAuthMode
	}
	endpoints.ChatURL = endpoints.ChatBaseURL + "/chat/completions"
	return endpoints, nil
}

func parseCopilotNousBase(base string) (*url.URL, error) {
	// Reject URL adornments rather than normalizing away a conflicting target.
	if (!strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://")) || strings.TrimSpace(base) != base || strings.ContainsAny(base, "?#%\\") {
		return nil, ErrCopilotNousBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, ErrCopilotNousBaseURL
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	if parsed.Path != "" && (path.Clean(parsed.Path) != parsed.Path || !strings.HasPrefix(parsed.Path, "/")) {
		return nil, ErrCopilotNousBaseURL
	}
	return parsed, nil
}

// NousUnverifiedInvocationClaims is a local shape/expiry hint only. No signature,
// issuer, audience, subscription or permission is authenticated here; the Nous
// upstream must authenticate the token. Never use these claims for authorization.
type NousUnverifiedInvocationClaims struct {
	UnverifiedExpiresAt time.Time
}

// ValidateCopilotNousResolvedSecret accepts plaintext AFTER the existing
// account/workspace-bound secret resolver. Never pass a reference string here or
// reject a generic account save just because its api_key has not been resolved.
// resolvedSecret is the PAT/invocation JWT; proxyFrontdoorSecret is a separate,
// optional operator Bearer secret, used only in official_proxy mode. Integration
// may store either in the existing sensitive api_key slot, selecting its meaning
// by mode; do not create an unredacted credential field for the proxy secret.
func ValidateCopilotNousResolvedSecret(config CopilotNousAuthConfig, resolvedSecret, proxyFrontdoorSecret string, now time.Time) (NousUnverifiedInvocationClaims, error) {
	var claims NousUnverifiedInvocationClaims
	if _, err := ResolveCopilotNousEndpoints(config); err != nil {
		return claims, err
	}
	if config.Platform == "nous_research" && config.AuthMode == NousAuthOfficialProxy {
		if resolvedSecret != "" || (proxyFrontdoorSecret != "" && !copilotNousBearerShape(proxyFrontdoorSecret)) {
			return claims, ErrCopilotNousCredential
		}
		return claims, nil
	}
	if proxyFrontdoorSecret != "" || !copilotNousBearerShape(resolvedSecret) {
		return claims, ErrCopilotNousCredential
	}
	if config.Platform == "github_copilot" {
		if !strings.HasPrefix(resolvedSecret, "github_pat_") || len(resolvedSecret) == len("github_pat_") {
			return claims, ErrCopilotNousCredential
		}
		return claims, nil
	}
	segments := strings.Split(resolvedSecret, ".")
	if len(segments) != 3 {
		return claims, ErrCopilotNousCredential
	}
	decoded := make([][]byte, 3)
	for index, segment := range segments {
		value, err := base64.RawURLEncoding.Strict().DecodeString(segment)
		if err != nil || len(value) == 0 {
			return claims, ErrCopilotNousCredential
		}
		decoded[index] = value
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if json.Unmarshal(decoded[0], &header) != nil || header.Algorithm == "" || strings.EqualFold(header.Algorithm, "none") {
		return claims, ErrCopilotNousCredential
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(decoded[1], &payload) != nil || payload == nil {
		return claims, ErrCopilotNousCredential
	}
	var expiration float64
	if json.Unmarshal(payload["exp"], &expiration) != nil || math.IsNaN(expiration) || math.IsInf(expiration, 0) || expiration <= 0 || expiration >= math.Exp2(63) {
		return claims, ErrCopilotNousCredential
	}
	seconds, fraction := math.Modf(expiration)
	claims.UnverifiedExpiresAt = time.Unix(int64(seconds), int64(fraction*float64(time.Second)))
	if !claims.UnverifiedExpiresAt.After(now) {
		return NousUnverifiedInvocationClaims{}, ErrCopilotNousExpired
	}
	hasInvoke := false
	for _, field := range []string{"scope", "scp"} {
		raw, exists := payload[field]
		if !exists {
			continue
		}
		var scopeValue any
		if json.Unmarshal(raw, &scopeValue) != nil {
			return NousUnverifiedInvocationClaims{}, ErrCopilotNousScope
		}
		var values []string
		switch value := scopeValue.(type) {
		case string:
			values = []string{value}
		case []any:
			for _, item := range value {
				text, ok := item.(string)
				if !ok {
					return NousUnverifiedInvocationClaims{}, ErrCopilotNousScope
				}
				values = append(values, text)
			}
		default:
			return NousUnverifiedInvocationClaims{}, ErrCopilotNousScope
		}
		for _, value := range values {
			for _, scope := range strings.Fields(strings.ReplaceAll(value, ",", " ")) {
				if scope == "inference:invoke" {
					hasInvoke = true
				}
			}
		}
	}
	if !hasInvoke {
		return NousUnverifiedInvocationClaims{}, ErrCopilotNousScope
	}
	return claims, nil
}

func copilotNousBearerShape(secret string) bool {
	if secret == "" {
		return false
	}
	for _, character := range secret {
		if character <= 32 || character >= 127 {
			return false
		}
	}
	return true
}

// ApplyCopilotNousAuth must run AFTER all account/client header overrides and
// immediately before sending. It binds auth to an exact method and URL, checks
// expiry again, and strips conflicting credentials and first-party identities.
// On error the request is unchanged and MUST NOT be sent. It does not replace
// egress validation. Use CopilotNousCheckRedirect on the actual sending client.
func ApplyCopilotNousAuth(request *http.Request, config CopilotNousAuthConfig, resolvedSecret, proxyFrontdoorSecret string, now time.Time) error {
	endpoints, err := ResolveCopilotNousEndpoints(config)
	if err != nil {
		return err
	}
	if request == nil || request.URL == nil {
		return ErrCopilotNousRequest
	}
	target := request.URL.String()
	catalog := request.Method == http.MethodGet && target == endpoints.ModelsURL
	chat := request.Method == http.MethodPost && target == endpoints.ChatURL
	if (!catalog && !chat) || (request.Host != "" && request.Host != request.URL.Host) || request.RequestURI != "" {
		return ErrCopilotNousRequest
	}
	if _, err := ValidateCopilotNousResolvedSecret(config, resolvedSecret, proxyFrontdoorSecret, now); err != nil {
		return err
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	// Iterate actual keys because Header.Del alone misses noncanonical keys
	// inserted by a map-based account override (including duplicate auth keys).
	for name := range request.Header {
		switch strings.ToLower(name) {
		case "authorization", "proxy-authorization", "x-api-key", "api-key", "x-goog-api-key", "cookie", "x-nous-refresh-token", "x-anonymous-api-secret", "copilot-integration-id", "x-github-api-version", "editor-version", "editor-plugin-version", "user-agent", "copilot-user-agent", "openai-organization", "openai-project", "host":
			delete(request.Header, name)
		}
	}
	secret := resolvedSecret
	if config.AuthMode == NousAuthOfficialProxy {
		secret = proxyFrontdoorSecret
	}
	if secret != "" {
		request.Header.Set("Authorization", "Bearer "+secret)
	}
	if config.Platform == "github_copilot" {
		request.Header.Set("Copilot-Integration-Id", CopilotIntegrationID)
		if catalog {
			request.Header.Set("X-GitHub-Api-Version", CopilotModelsAPIVersion)
		}
	}
	return nil
}

// CopilotNousCheckRedirect refuses all redirects, including same-origin ones.
// This keeps method/path policy and bearer destination binding intact; callers
// receive the original 3xx response and must treat it as an upstream failure.
// Preserve the configured Transport/egress policy when installing this callback.
func CopilotNousCheckRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}
