package service

import (
	"maps"
	"strings"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

const (
	DefaultKiloBaseURL    = "https://api.kilo.ai/api/gateway"
	KiloAccountModeFree   = "free"
	KiloAccountModeAPIKey = "apikey"
)

func (a *Account) GetKiloAccountMode() string {
	if a == nil || a.Platform != PlatformKilo {
		return ""
	}
	mode := strings.TrimSpace(a.GetCredential("account_mode"))
	if mode == "" {
		return KiloAccountModeFree
	}
	return mode
}
func (a *Account) IsKiloFree() bool {
	return a != nil && a.Platform == PlatformKilo && a.GetKiloAccountMode() == KiloAccountModeFree
}
func publicModelLane(a *Account) bool { return a.IsOpenCodeZenFree() || a.IsKiloFree() }

// NormalizeFreeModelCredentials is shared by admin create/update and connection
// verification. Runtime accessors also handle old rows without mutating shared
// account snapshots. Paid OpenCode modes are deliberately untouched.
func NormalizeFreeModelCredentials(a *Account) error {
	if a == nil || (!a.IsOpenCodeZenFree() && a.Platform != PlatformKilo) {
		return nil
	}
	if a.Type != AccountTypeAPIKey {
		return infraerrors.BadRequest("INVALID_PLATFORM_ACCOUNT_TYPE", "Free model providers require an apikey account")
	}
	key, _ := a.Credentials["api_key"].(string)
	key = strings.TrimSpace(key)
	if a.IsOpenCodeZenFree() {
		if key != "" && key != "public" {
			return infraerrors.BadRequest("INVALID_PUBLIC_CREDENTIAL", "Zen Free uses the public credential")
		}
		a.Credentials = maps.Clone(a.Credentials)
		if a.Credentials == nil {
			a.Credentials = map[string]any{}
		}
		a.Credentials["api_key"] = "public"
		a.Credentials["account_mode"] = AccountModeZenFree
		return nil
	}
	switch a.GetKiloAccountMode() {
	case KiloAccountModeFree:
		if key != "" && key != "public" {
			return infraerrors.BadRequest("INVALID_KILO_CREDENTIAL", "Select apikey mode to use a Kilo API key")
		}
		key = ""
	case KiloAccountModeAPIKey:
		if key == "" || key == "public" {
			return infraerrors.BadRequest("INVALID_KILO_CREDENTIAL", "Kilo apikey mode requires an API key")
		}
	default:
		return infraerrors.BadRequest("INVALID_KILO_MODE", "Kilo account_mode must be free or apikey")
	}
	mode := a.GetKiloAccountMode()
	a.Credentials = maps.Clone(a.Credentials)
	if a.Credentials == nil {
		a.Credentials = map[string]any{}
	}
	a.Credentials["account_mode"], a.Credentials["api_key"] = mode, key
	return nil
}

func kiloBaseURL(a *Account) string {
	base := strings.TrimRight(strings.TrimSpace(a.GetCredential("base_url")), "/")
	if base == "" {
		return DefaultKiloBaseURL
	}
	for _, suffix := range []string{"/chat/completions", "/models"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return strings.TrimRight(base, "/")
}
func kiloEndpoint(a *Account, path string, validate func(string) (string, error)) (string, error) {
	base, err := validate(kiloBaseURL(a))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(base, "/") + path, nil
}

// An explicit transition to a public lane drops the previous private secret.
// An explicitly supplied private key remains a validation error.
func clearTransitionedPublicKey(a *Account, patch map[string]any, wasPublic bool) {
	if wasPublic || !publicModelLane(a) {
		return
	}
	if _, changed := patch["account_mode"]; !changed {
		return
	}
	key, _ := patch["api_key"].(string)
	if strings.TrimSpace(key) == "" || key == "public" {
		a.Credentials["api_key"] = ""
	}
}

// Bulk updates share a JSONB patch. Normalize only explicit secret changes or
// public-mode transitions, never copy an existing account's secret to its peers.
func normalizeFreeModelBulkCredentials(targets []*Account, input *BulkUpdateAccountsInput) error {
	if len(input.Credentials) == 0 {
		return nil
	}
	var normalizedKey *string
	for _, target := range targets {
		if target.Platform != PlatformKilo && target.Platform != PlatformOpenCodeGo {
			continue
		}
		if _, referenced := CredentialReference(target.Credentials); referenced {
			mode, supplied := input.Credentials["account_mode"]
			if supplied && mode != target.GetCredential("account_mode") {
				return infraerrors.BadRequest("PUBLIC_LANE_REFERENCED_BULK_UNSUPPORTED", "Use a single-account update to rotate encrypted public-lane credentials")
			}
			for key := range input.Credentials {
				if IsSensitiveCredentialKey(key) {
					return infraerrors.BadRequest("PUBLIC_LANE_REFERENCED_BULK_UNSUPPORTED", "Use a single-account update to rotate encrypted public-lane credentials")
				}
			}
		}
		candidate := *target
		candidate.Credentials = maps.Clone(target.Credentials)
		if candidate.Credentials == nil {
			candidate.Credentials = map[string]any{}
		}
		for key, value := range input.Credentials {
			candidate.Credentials[key] = value
		}
		clearTransitionedPublicKey(&candidate, input.Credentials, publicModelLane(target))
		if err := validateExpandedPlatformAccount(&candidate); err != nil {
			return err
		}
		_, keyChanged := input.Credentials["api_key"]
		_, modeChanged := input.Credentials["account_mode"]
		if publicModelLane(&candidate) && (keyChanged || modeChanged) {
			key := candidate.GetCredential("api_key")
			if normalizedKey != nil && *normalizedKey != key {
				return infraerrors.BadRequest("MIXED_PUBLIC_CREDENTIALS", "Update Zen Free and Kilo credentials separately")
			}
			normalizedKey = &key
		}
	}
	if normalizedKey != nil {
		for _, target := range targets {
			if target.Platform != PlatformKilo && target.Platform != PlatformOpenCodeGo {
				return infraerrors.BadRequest("MIXED_PUBLIC_CREDENTIALS", "Update public model credentials separately from other platforms")
			}
		}
		input.Credentials = maps.Clone(input.Credentials)
		input.Credentials["api_key"] = *normalizedKey
	}
	return nil
}
