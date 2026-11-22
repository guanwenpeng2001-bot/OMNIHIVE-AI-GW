package service

import (
	"context"
	"errors"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"net/http"
	"strings"
)

type groupAccountUpstream struct {
	base    HTTPUpstream
	account *Account
}

// Keep the legacy transport exactly as supplied for unbound/single-node accounts.
func accountHTTPUpstream(base HTTPUpstream, account *Account) HTTPUpstream {
	if account != nil {
		key := strings.TrimSpace(account.GetCredential("api_key"))
		invalidPaidOpenCode := account.Platform == PlatformOpenCodeGo && !account.IsOpenCodeZenFree() && (key == "" || key == "public")
		if account.IsOpenCodeZenFree() || account.Platform == PlatformKilo || invalidPaidOpenCode {
			// Historical paid rows must pass the same credential guard as writes.
			base = &freeModelUpstream{base: base, account: account}
		}
	}
	if isCopilotNous(account) {
		base = &compatibleSpecialUpstream{base: base, account: account}
	}
	if account != nil && account.ProxyGroupID != nil {
		base = &groupAccountUpstream{base: base, account: account}
	}
	if account.hasStagedProxyDependency() {
		base = &stagedProxyGuardUpstream{base: base, account: account}
	}
	return base
}

// Check at send time, outside both the proxy-group and credential adapters.
// A supplied proxyURL cannot override an unresolved account dependency.
type stagedProxyGuardUpstream struct {
	base    HTTPUpstream
	account *Account
}

func (u *stagedProxyGuardUpstream) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	if err := u.account.CheckStagedCredentialUse(); err != nil {
		return nil, err
	}
	return u.base.Do(req, proxyURL, accountID, concurrency)
}

func (u *stagedProxyGuardUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if err := u.account.CheckStagedCredentialUse(); err != nil {
		return nil, err
	}
	return u.base.DoWithTLS(req, proxyURL, accountID, concurrency, profile)
}

func (u *groupAccountUpstream) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	return u.do(req, proxyURL, func(r *http.Request, p string) (*http.Response, error) {
		return u.base.Do(r, p, accountID, concurrency)
	})
}

func (u *groupAccountUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.do(req, proxyURL, func(r *http.Request, p string) (*http.Response, error) {
		return u.base.DoWithTLS(r, p, accountID, concurrency, profile)
	})
}

func (u *groupAccountUpstream) do(req *http.Request, _ string, send func(*http.Request, string) (*http.Response, error)) (*http.Response, error) {
	if _, err := u.account.EgressProxyURL(req.Context()); err != nil {
		return nil, err
	}
	state := u.account.egress
	state.mu.Lock()
	defer state.mu.Unlock()
	resolved := state.target.URL
	proxyURL := resolved
	if resolved != "" && u.account.IsCustomBaseURLEnabled() && req.URL.Query().Get("proxy") == resolved {
		proxyURL = ""
	}
	// Custom relay transports deliberately carry the selected URL in their
	// relay request. They do not prove a failure of this application's proxy.
	canAlternate := !state.committed && !state.alternated && resolved == proxyURL && proxyURL != "" && state.runtime != nil
	response, err := send(req, proxyURL)
	var proxyFailure *ProxyConnectFailure
	var handshakeFailure *ProxyTLSHandshakeFailure
	proxyOwnFailure := errors.As(err, &proxyFailure)
	routeFailure := errors.As(err, &handshakeFailure)
	if err == nil || response != nil || resolved != proxyURL || proxyURL == "" || state.runtime == nil || req.Context().Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (!proxyOwnFailure && !routeFailure) {
		state.committed = true
		return response, err
	}
	if proxyOwnFailure {
		if reportErr := state.runtime.ReportTransportFailure(req.Context(), *u.account.ProxyGroupID, egressBindingAccountID(u.account), state.target.ProxyID, err); reportErr != nil {
			return nil, reportErr
		}
	}
	// A transport may have consumed the body while failing to connect. Replay
	// only a body with an explicit reconstruction function (or no body).
	if !canAlternate || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		state.committed = true
		return response, err
	}
	state.alternated = true
	state.committed = true
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		body, bodyErr := req.GetBody()
		if bodyErr != nil {
			return nil, bodyErr
		}
		retry.Body = body
	}
	if req.Context().Err() != nil {
		if retry.Body != nil {
			_ = retry.Body.Close()
		}
		return response, err
	}
	// Reconstruct before mutating route-only bindings. A body factory failure
	// must not move a binding for an operation that cannot be retried.
	if routeFailure && !proxyOwnFailure {
		if _, deleteErr := state.runtime.cache.DeleteBindingIfValue(req.Context(), *u.account.ProxyGroupID, egressBindingAccountID(u.account), state.target.ProxyID); deleteErr != nil {
			if retry.Body != nil {
				_ = retry.Body.Close()
			}
			return nil, deleteErr
		}
	}
	target, resolveErr := state.runtime.resolveExcluding(req.Context(), u.account, state.target.ProxyID, routeFailure && !proxyOwnFailure)
	if resolveErr != nil || target.ProxyID == state.target.ProxyID {
		if retry.Body != nil {
			_ = retry.Body.Close()
		}
		if routeFailure || resolveErr == nil {
			return response, err
		}
		return nil, resolveErr
	}
	state.target = target
	return send(retry, target.URL)
}
