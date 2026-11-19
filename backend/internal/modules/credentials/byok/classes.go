// Package byok describes provider credential classes supported by tenant enrollment.
package byok

import (
	"encoding/json"
	"github.com/golang-jwt/jwt/v5"
	"slices"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/credentials"
)

type Class struct {
	Provider  string `json:"provider"`
	Auth      string `json:"auth_type"`
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
}

// Classes enumerates the connection registry, including classes which lack a
// protected catalog, default endpoint or tenant-compatible credential adapter.
func Classes() []Class {
	out := []Class{}
	for _, schema := range connections.Schemas("", "") {
		for _, auth := range schema.AuthTypes {
			out = append(out, Class{Provider: schema.Provider, Auth: auth, Supported: supported(schema.Provider, auth)})
		}
	}
	return out
}

func Supported(provider, auth string) bool {
	return provider != "" && auth != "" && len(connections.Schemas(provider, auth)) > 0 && supported(provider, auth)
}
func supported(provider, auth string) bool {
	switch auth {
	case "api_key":
		if p, ok := domain.LookupCompatibleProvider(provider); ok {
			if p.BaseSource != "preset" {
				return false
			}
			for _, mode := range p.Modes {
				if mode.Mode == p.DefaultMode {
					return mode.Discovery == "openai_models"
				}
			}
			return false
		}
		return slices.Contains([]string{
			domain.PlatformAnthropic, domain.PlatformGemini, domain.PlatformOpenAI, domain.PlatformGrok,
			domain.PlatformKimi, domain.PlatformZhipu, domain.PlatformDeepseek, domain.PlatformMiniMax,
			domain.PlatformOpenCodeGo, domain.PlatformQianwen, domain.PlatformCommandCode, domain.PlatformVolcengine,
			domain.PlatformQianfan, domain.PlatformHunyuan, domain.PlatformMistral, domain.PlatformOpenRouter,
		}, provider)
	case "oauth":
		return slices.Contains([]string{"anthropic", "gemini", "openai", "grok", "cursor", "antigravity"}, provider)
	case "bedrock":
		return provider == "bedrock"
	case "setup-token":
		return provider == "anthropic"
	case "service_account":
		return provider == "gemini" || provider == "anthropic"
	}
	return false
}

func Payload(provider, auth, secret string) (map[string]any, error) {
	fields, err := (credentials.Change{Operation: "set", Type: auth, Secret: secret}).Payload(auth)
	if err != nil {
		return nil, err
	}
	if provider == "nous_research" {
		fields["auth_mode"] = "invocation_token"
	}
	if auth == "bedrock" {
		var keys map[string]string
		if json.Unmarshal([]byte(secret), &keys) != nil || strings.TrimSpace(keys["aws_access_key_id"]) == "" || strings.TrimSpace(keys["aws_secret_access_key"]) == "" || strings.TrimSpace(keys["aws_region"]) == "" {
			return nil, fault.Invalid("/secret")
		}
		fields = map[string]any{}
		for k, v := range keys {
			if !slices.Contains([]string{"aws_access_key_id", "aws_secret_access_key", "aws_region", "aws_session_token"}, k) {
				return nil, fault.Invalid("/secret")
			}
			fields[k] = v
		}
	}
	if auth == "service_account" {
		var key struct {
			Type        string `json:"type"`
			ProjectID   string `json:"project_id"`
			PrivateKey  string `json:"private_key"`
			ClientEmail string `json:"client_email"`
		}
		if json.Unmarshal([]byte(secret), &key) != nil || key.ProjectID == "" || key.PrivateKey == "" || key.ClientEmail == "" || key.Type != "service_account" {
			return nil, fault.Invalid("/secret")
		}
		if provider == "anthropic" {
			fields["location"] = "global"
		}
	}
	return fields, nil
}

// BearerExpiry narrows the verification window when an imported token states an
// earlier expiry. Authorization still requires an independent provider check.
func BearerExpiry(auth string, fields map[string]any) *time.Time {
	if auth != "oauth" && auth != "setup-token" {
		return nil
	}
	raw, _ := fields["access_token"].(string)
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(raw, claims); err != nil {
		return nil
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return nil
	}
	return &exp.Time
}
