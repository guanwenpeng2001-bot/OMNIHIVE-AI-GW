package service

import (
	"errors"
	"net/url"
	"strings"
)

// Exact official hosts pin region and prevent SSRF/credential forwarding to
// arbitrary base URLs. Replace compatible-mode paths instead of appending.
func qianwenRealtimeEndpoint(account *Account, model string) (string, string, error) {
	if account == nil || account.Platform != PlatformQianwen || !IsQianwenRealtimeModel(model) {
		return "", "", errors.New("unsupported DashScope realtime model")
	}
	base := DefaultQianwenDashScopeBaseURL
	if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok {
		if v, ok := bases["dashscope"].(string); ok && strings.TrimSpace(v) != "" {
			base = strings.TrimSpace(v)
		}
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", errors.New("verified HTTPS DashScope endpoint required")
	}
	region := ""
	switch u.Host {
	case "dashscope.aliyuncs.com":
		region = "beijing"
	case "dashscope-intl.aliyuncs.com":
		region = "singapore"
	default:
		return "", "", errors.New("unsupported DashScope realtime region")
	}
	u.Scheme = "wss"
	u.Path = "/api-ws/v1/realtime"
	u.RawPath = ""
	u.RawQuery = url.Values{"model": {model}}.Encode()
	return u.String(), region, nil
}
