package service

import (
	"net/url"
	"strings"
)

func buildOpenAIEndpointURL(base string, endpoint string) string {
	normalized := strings.TrimSpace(base)
	parsed, err := url.Parse(normalized)
	return buildOpenAIEndpointURLFromParsed(normalized, endpoint, parsed, err)
}

func (account *Account) buildOpenAIEndpointURL(base, endpoint string) string {
	normalized := strings.TrimSpace(base)
	parsed, err := account.parsedBaseURL(normalized)
	return buildOpenAIEndpointURLFromParsed(normalized, endpoint, parsed, err)
}

func buildOpenAIEndpointURLFromParsed(normalized, endpoint string, parsed *url.URL, err error) string {
	endpoint = "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	relative := strings.TrimPrefix(endpoint, "/v1")
	if err != nil {
		return strings.TrimRight(normalized, "/") + endpoint
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, endpoint) && !strings.HasSuffix(path, relative) {
		if openAIEndpointPathHasVersionSuffix(path) {
			path += relative
		} else {
			path += endpoint
		}
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.Fragment = ""
	return parsed.String()
}

// The endpoint builder already has a parsed path; parsing it as a URL again
// only allocates and cannot provide a scheme or authority.
func openAIEndpointPathHasVersionSuffix(path string) bool {
	slash := strings.LastIndex(path, "/")
	return slash >= 0 && isOpenAIAPIVersionSegment(path[slash+1:])
}

func buildOpenAIResponsesInputTokensURL(base string) string {
	return buildOpenAIEndpointURL(base, "/v1/responses/input_tokens")
}

func openAIBaseURLHasVersionSuffix(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}

	pathValue := ""
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		pathValue = parsed.Path
	} else if slash := strings.Index(trimmed, "/"); slash >= 0 {
		pathValue = trimmed[slash:]
	}

	pathValue = strings.TrimRight(pathValue, "/")
	if pathValue == "" {
		return false
	}
	lastSlash := strings.LastIndex(pathValue, "/")
	segment := pathValue
	if lastSlash >= 0 {
		segment = pathValue[lastSlash+1:]
	}
	return isOpenAIAPIVersionSegment(segment)
}

func isOpenAIAPIVersionSegment(segment string) bool {
	s := strings.ToLower(strings.TrimSpace(segment))
	if len(s) < 2 || s[0] != 'v' || !isASCIIDigit(s[1]) {
		return false
	}

	i := 1
	for i < len(s) && isASCIIDigit(s[i]) {
		i++
	}
	if i == len(s) {
		return true
	}
	if s[i] == '.' {
		i++
		if i == len(s) || !isASCIIDigit(s[i]) {
			return false
		}
		for i < len(s) && isASCIIDigit(s[i]) {
			i++
		}
		return i == len(s)
	}

	suffix := s[i:]
	return strings.HasPrefix(suffix, "alpha") ||
		strings.HasPrefix(suffix, "beta") ||
		strings.HasPrefix(suffix, "preview")
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
