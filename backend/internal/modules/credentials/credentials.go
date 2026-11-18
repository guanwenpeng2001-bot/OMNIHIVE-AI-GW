package credentials

import (
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

type Metadata struct {
	Version     int64   `json:"version,string"`
	State       string  `json:"state"`
	Fingerprint *string `json:"fingerprint"`
	LastFour    *string `json:"last_four"`
}

type Change struct {
	Operation    string `json:"operation"`
	Type         string `json:"type,omitempty"`
	Secret       string `json:"secret,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (c Change) Payload(auth string) (map[string]any, error) {
	switch c.Operation {
	case "keep", "clear":
		if c.Type != "" || c.Secret != "" || c.RefreshToken != "" {
			return nil, fault.Invalid("/operation")
		}
		return nil, nil
	case "set":
		if auth != c.Type || strings.TrimSpace(c.Secret) == "" || len(c.Secret) > 262144 {
			return nil, fault.Invalid("/secret")
		}
	default:
		return nil, fault.Invalid("/operation")
	}
	key := map[string]string{"api_key": "api_key", "oauth": "access_token", "setup-token": "access_token", "service_account": "service_account_json", "bedrock": "api_key"}[auth]
	if key == "" {
		return nil, fault.Invalid("/type")
	}
	result := map[string]any{key: c.Secret}
	if c.RefreshToken != "" {
		if auth != "oauth" {
			return nil, fault.Invalid("/refresh_token")
		}
		result["refresh_token"] = c.RefreshToken
	}
	return result, nil
}
