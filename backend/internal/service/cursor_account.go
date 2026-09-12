package service

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"

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
	if account.Type == AccountTypeOAuth {
		for _, key := range []string{"api_key", "base_url", "api_protocol", "api_base_urls", "cached_access_token", "cached_expires_at"} {
			if _, present := account.Credentials[key]; present {
				return invalid("Cursor auto accounts cannot contain relay/key fields: " + key)
			}
		}
		for _, key := range []string{"access_token", "refresh_token", "machine_id", "mac_machine_id"} {
			if strings.TrimSpace(account.GetCredential(key)) == "" {
				return invalid("Cursor auto requires " + key)
			}
		}
		for _, key := range []string{"machine_id", "mac_machine_id"} {
			decoded, err := hex.DecodeString(account.GetCredential(key))
			if err != nil || len(decoded) != 32 {
				return invalid("Cursor " + key + " must be 64 hexadecimal characters")
			}
		}
		if expiry, err := cursor.TokenExpiry(account.GetCredential("access_token")); err == nil {
			account.Credentials["expires_at"] = strconv.FormatInt(expiry.Unix(), 10)
		} else {
			// Unknown expiry must enter the refresh window, including edited tokens.
			delete(account.Credentials, "expires_at")
		}
		account.Concurrency = 1
		return nil
	}
	if account.Type != AccountTypeAPIKey && account.Type != AccountTypeUpstream {
		return invalid("Cursor supports auto, official API keys and upstream keys")
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
