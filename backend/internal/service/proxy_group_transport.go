package service

import (
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"net/http"
)

type groupAccountUpstream struct {
	base    HTTPUpstream
	account *Account
}

// Keep the legacy transport exactly as supplied for unbound/single-node accounts.
func accountHTTPUpstream(base HTTPUpstream, account *Account) HTTPUpstream {
	if account == nil || account.ProxyGroupID == nil {
		return base
	}
	return &groupAccountUpstream{base: base, account: account}
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

func (u *groupAccountUpstream) do(req *http.Request, proxyURL string, send func(*http.Request, string) (*http.Response, error)) (*http.Response, error) {
	resolved, err := u.account.EgressProxyURL(req.Context())
	if err != nil {
		return nil, err
	}
	state := u.account.egress
	state.mu.Lock()
	defer state.mu.Unlock()
	resolved = state.target.URL
	proxyURL = resolved
	if resolved != "" && u.account.IsCustomBaseURLEnabled() && req.URL.Query().Get("proxy") == resolved {
		proxyURL = ""
	}
	// Custom relay transports deliberately carry the selected URL in their
	// relay request. They do not prove a failure of this application's proxy.
	canAlternate := !state.committed && !state.alternated && resolved == proxyURL && proxyURL != "" && state.runtime != nil
	response, err := send(req, proxyURL)
	var proxyFailure *ProxyConnectFailure
	if err == nil || response != nil || resolved != proxyURL || proxyURL == "" || state.runtime == nil || req.Context().Err() != nil || !errors.As(err, &proxyFailure) {
		state.committed = true
		return response, err
	}
	if reportErr := state.runtime.ReportTransportFailure(req.Context(), *u.account.ProxyGroupID, egressBindingAccountID(u.account), state.target.ProxyID, err); reportErr != nil {
		return nil, reportErr
	}
	// A transport may have consumed the body while failing to connect. Replay
	// only a body with an explicit reconstruction function (or no body).
	if !canAlternate || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		state.committed = true
		return response, err
	}
	state.alternated = true
	target, resolveErr := state.runtime.resolve(req.Context(), u.account)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if target.ProxyID == state.target.ProxyID {
		return response, err
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		body, bodyErr := req.GetBody()
		if bodyErr != nil {
			return nil, bodyErr
		}
		retry.Body = body
	}
	state.target = target
	state.committed = true
	return send(retry, target.URL)
}
