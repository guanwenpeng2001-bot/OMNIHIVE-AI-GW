package service

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

func isCopilotNous(account *Account) bool {
	return account != nil && (account.Platform == "github_copilot" || account.Platform == "nous_research")
}

func compatibleSpecialKeyOptional(account *Account) bool {
	return account.Platform == "nous_research" && account.GetCredential("auth_mode") == NousAuthOfficialProxy
}

// A mode transition changes api_key's recipient and meaning. Check the actual
// incoming patch before the generic secret-preserving merge can hide omission.
func validateCompatibleSpecialAuthTransition(account *Account, incoming map[string]any) error {
	if account.Platform != "nous_research" {
		return nil
	}
	mode, changed := incoming["auth_mode"]
	if !changed || mode == account.GetCredential("auth_mode") {
		return nil
	}
	key, supplied := incoming["api_key"].(string)
	if !supplied || (key == "" && mode != NousAuthOfficialProxy) || (key != "" && (key == account.GetCredential("api_key") || strings.Trim(key, "*• ") == "")) {
		return infraerrors.BadRequest("SPECIAL_AUTH_SECRET_REPLACEMENT_REQUIRED", "Changing authentication mode requires an explicit replacement API key or an empty key for an unauthenticated proxy")
	}
	return nil
}

// Metadata stays inline; api_key retains the existing encrypted/redacted owner.
// A separate protocol base would create two competing destination authorities.
func compatibleSpecialAuthConfig(account *Account) (CopilotNousAuthConfig, error) {
	config := CopilotNousAuthConfig{Platform: account.Platform}
	if raw, present := account.Credentials["api_key"]; present {
		if _, ok := raw.(string); !ok {
			return config, ErrCopilotNousCredential
		}
	}
	for key, target := range map[string]*string{"auth_mode": &config.AuthMode, "base_url": &config.BaseURL} {
		if value, exists := account.Credentials[key]; exists {
			text, ok := value.(string)
			if !ok {
				return config, ErrCopilotNousAuthMode
			}
			*target = text
		}
	}
	_, config.HasRefreshToken = account.Credentials["refresh_token"]
	if raw, exists := account.Credentials["api_base_urls"]; exists && raw != nil {
		bases, ok := raw.(map[string]any)
		if !ok {
			return config, ErrCopilotNousBaseURL
		}
		for _, value := range bases {
			if value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok || text != "" {
				return config, ErrCopilotNousBaseURL
			}
		}
	}
	return config, nil
}

func compatibleSpecialSecrets(account *Account) (secret, frontdoor string) {
	if compatibleSpecialKeyOptional(account) {
		return "", account.GetCredential("api_key")
	}
	return account.GetCredential("api_key"), ""
}

func validateCompatibleSpecialSecret(account *Account) error {
	config, err := compatibleSpecialAuthConfig(account)
	if err != nil {
		return err
	}
	secret, frontdoor := compatibleSpecialSecrets(account)
	_, err = ValidateCopilotNousResolvedSecret(config, secret, frontdoor, time.Now())
	return err
}

func applyCompatibleSpecialAuth(account *Account, request *http.Request) error {
	config, err := compatibleSpecialAuthConfig(account)
	if err != nil {
		return err
	}
	secret, frontdoor := compatibleSpecialSecrets(account)
	return ApplyCopilotNousAuth(request, config, secret, frontdoor, time.Now())
}

// Decorate the shared upstream, never construct a separate HTTP client. Placing
// this inside the proxy-group adapter rechecks expiry on each actual retry too.
type compatibleSpecialUpstream struct {
	base    HTTPUpstream
	account *Account
}

func (upstream *compatibleSpecialUpstream) send(request *http.Request, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if err := applyCompatibleSpecialAuth(upstream.account, request); err != nil {
		return nil, err
	}
	request = request.WithContext(WithHTTPUpstreamRedirectsDisabled(request.Context()))
	response, err := send(request)
	if err == nil && response != nil && response.StatusCode >= 300 && response.StatusCode < 400 {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, fmt.Errorf("Copilot/Nous upstream redirect refused (HTTP %d)", response.StatusCode)
	}
	return response, err
}

func (upstream *compatibleSpecialUpstream) Do(request *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	return upstream.send(request, func(request *http.Request) (*http.Response, error) {
		return upstream.base.Do(request, proxy, id, concurrency)
	})
}

func (upstream *compatibleSpecialUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.send(request, func(request *http.Request) (*http.Response, error) {
		return upstream.base.DoWithTLS(request, proxy, id, concurrency, profile)
	})
}
