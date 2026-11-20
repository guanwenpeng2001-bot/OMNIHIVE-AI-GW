package service

import (
	"context"
	"strings"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

const (
	StagedAccountImportExtraKey   = "staged_account_import"
	StagedProxyDependencyExtraKey = "staged_proxy_dependency"
)

type stagedAccountImportContextKey struct{}

// WithStagedAccountImport is internal write policy, never a credential field.
// The repository requires persistent encryption while creating these accounts.
func WithStagedAccountImport(ctx context.Context) context.Context {
	return context.WithValue(ctx, stagedAccountImportContextKey{}, true)
}

func IsStagedAccountImport(ctx context.Context) bool {
	staged, _ := ctx.Value(stagedAccountImportContextKey{}).(bool)
	return staged
}

var ErrStagedProxyPending = infraerrors.BadRequest("STAGED_PROXY_CONFIGURATION_REQUIRED", "Configure a target-local proxy before using this staged account")

// Keep the declaration until a target-local route is configured. A missing or
// unloaded proxy must never silently become the empty URL meaning direct.
func (a *Account) StagedProxyPending() bool {
	if !a.hasStagedProxyDependency() {
		return false
	}
	return a.ProxyGroupID == nil && (a.ProxyID == nil || *a.ProxyID <= 0 || a.Proxy == nil || a.Proxy.ID != *a.ProxyID)
}

func (a *Account) hasStagedProxyDependency() bool {
	if a == nil {
		return false
	}
	dependency, _ := a.Extra[StagedProxyDependencyExtraKey].(string)
	return dependency == "required" || dependency == "unknown"
}

// CheckStagedCredentialUse belongs at credential/session/transport boundaries,
// including administrative reads. Inactive alone is not an egress restriction.
func (a *Account) CheckStagedCredentialUse() error {
	if a.StagedProxyPending() {
		return ErrStagedProxyPending
	}
	return nil
}

// An intentionally narrow migration format: unknown credentials, arbitrary
// nested metadata, source references and master keys fail closed. Only known
// secrets enter the encrypted payload; approved, bounded configuration is inline.
func ValidateStagedImportCredentials(credentials map[string]any) error {
	invalid := infraerrors.BadRequest("INVALID_STAGED_CREDENTIALS", "staging requires supported plaintext credential fields and safe configuration; source references, master keys and unknown fields are not accepted")
	hasSecret := false
	for field, value := range credentials {
		switch field {
		case "password", "sso_token", "sso", "sso-rw", "clearTextPassword", "cookie":
			return invalid // regular create strips these; staging must not lose them silently
		}
		if IsSensitiveCredentialKey(field) {
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return invalid
			}
			hasSecret = true
			continue
		}
		switch field {
		case "base_url":
			if _, err := normalizedStagedEndpoint(value); err != nil {
				return invalid
			}
		case "expires_at", "expires_in", "token_type", "scope", "email", "plan_type":
			switch value.(type) {
			case string, float64, int, int64:
			default:
				return invalid
			}
		default:
			if !validStagedIdentityField(field, value) && !validStagedConfiguration(field, value) {
				return invalid
			}
		}
	}
	if !hasSecret {
		return invalid
	}
	return nil
}

// Keep machine configuration limited to native Cursor. API-key accounts may
// retain valid IDs or let the existing local normalizer generate missing ones.
// OAuth must carry the original authorized identity; never synthesize it.
func validateStagedAccountCredentials(platform, accountType string, credentials map[string]any) error {
	if err := ValidateStagedImportCredentials(credentials); err != nil {
		return err
	}
	nativeCursor := platform == PlatformCursor && (accountType == AccountTypeAPIKey || accountType == AccountTypeOAuth)
	if platform == PlatformCursor {
		// Cursor has no tenant/project auth selectors. Restrict auth fields to
		// those actually consumed by the selected native/relay account type.
		for _, field := range stagedIdentityFields {
			if _, present := credentials[field]; present {
				return errStagedConfiguration
			}
		}
		for field := range credentials {
			if !IsSensitiveCredentialKey(field) {
				continue
			}
			allowed := (accountType == AccountTypeAPIKey || accountType == AccountTypeUpstream) && field == "api_key"
			allowed = allowed || accountType == AccountTypeOAuth && (field == "access_token" || field == "refresh_token")
			if !allowed {
				return errStagedConfiguration
			}
		}
		if nativeCursor {
			for _, field := range []string{"base_url", "api_protocol", "api_base_urls"} {
				if _, present := credentials[field]; present {
					return errStagedConfiguration
				}
			}
		}
	}
	for _, field := range []string{"machine_id", "mac_machine_id"} {
		if _, present := credentials[field]; present && !nativeCursor {
			return errStagedConfiguration
		}
	}
	if nativeCursor && accountType == AccountTypeOAuth {
		for _, field := range []string{"access_token", "refresh_token", "machine_id", "mac_machine_id"} {
			text, ok := credentials[field].(string)
			if !ok || strings.TrimSpace(text) == "" {
				return infraerrors.BadRequest("INVALID_STAGED_CURSOR_IDENTITY", "Cursor OAuth staging requires access_token, refresh_token and both original machine identifiers")
			}
		}
	}
	// Machine identifiers do not participate in duplicate fingerprints: changing
	// them cannot bypass the same endpoint/tenant/project/secret duplicate check.
	return nil
}
