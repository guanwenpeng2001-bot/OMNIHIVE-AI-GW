package service

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (account *Account) IsCursor() bool {
	return account != nil && account.Platform == PlatformCursor
}

func (account *Account) IsCursorNative() bool {
	return account.IsCursor() && account.Type != AccountTypeUpstream
}

// NormalizeCursorAccount is shared by creation, editing and import. Derived
// session tokens are deliberately absent from persisted key credentials.
func NormalizeCursorAccount(account *Account) error {
	if !account.IsCursor() {
		return nil
	}
	invalid := func(message string) error { return infraerrors.BadRequest("INVALID_CURSOR_ACCOUNT", message) }
	if account.Type != AccountTypeAPIKey && account.Type != AccountTypeUpstream {
		return invalid("Cursor M1 supports official API keys and upstream keys; auto authentication is not available yet")
	}
	if strings.TrimSpace(account.GetCredential("api_key")) == "" {
		return invalid("Cursor API key is required")
	}
	if account.Credentials == nil {
		account.Credentials = make(map[string]any)
	}
	for _, key := range []string{"access_token", "refresh_token", "cached_access_token", "cached_expires_at"} {
		if account.GetCredential(key) != "" {
			return invalid("Cursor key accounts cannot contain session credentials")
		}
	}
	if account.Type == AccountTypeUpstream {
		baseURL, err := url.Parse(strings.TrimSpace(account.GetCredential("base_url")))
		if err != nil || baseURL.Hostname() == "" || (baseURL.Scheme != "https" && baseURL.Scheme != "http") || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
			return invalid("Cursor relay requires an HTTP(S) base URL without userinfo, query or fragment")
		}
		protocol := account.GetCredential("api_protocol")
		if protocol == "" {
			account.Credentials["api_protocol"] = "adaptive"
		} else if protocol != "adaptive" && protocol != "chat_completions" {
			return invalid("Cursor M1 relay supports adaptive or chat_completions")
		}
		for _, key := range []string{"machine_id", "mac_machine_id", "client_version"} {
			if account.GetCredential(key) != "" {
				return invalid("Cursor relay cannot contain native machine credentials")
			}
		}
		if account.Concurrency <= 0 {
			account.Concurrency = 1
		}
		return nil
	}
	if account.GetCredential("base_url") != "" || account.GetCredential("api_protocol") != "" {
		return invalid("Cursor official keys cannot configure relay endpoints")
	}
	for _, key := range []string{"machine_id", "mac_machine_id"} {
		machineID := account.GetCredential(key)
		if machineID == "" {
			identity := make([]byte, 32)
			if _, err := rand.Read(identity); err != nil {
				return err
			}
			account.Credentials[key] = hex.EncodeToString(identity)
			continue
		}
		decoded, err := hex.DecodeString(machineID)
		if err != nil || len(decoded) != 32 {
			return invalid("Cursor machine IDs must be 64 hexadecimal characters")
		}
	}
	account.Concurrency = 1
	return nil
}
