package service

import (
	"errors"
	"net/http"
	"strings"
)

// Diagnostics are deliberately fixed text. Never copy transport errors, URLs,
// credential values or upstream response bodies into verification snapshots.
const probeURLDetail = "Endpoint rejected by URL security policy. Use a valid HTTPS public endpoint; when the allowlist is enabled, configure SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS."

type connectionProbeURLError struct{ err error }

func (e *connectionProbeURLError) Error() string { return e.err.Error() }
func (e *connectionProbeURLError) Unwrap() error { return e.err }

// FailedConnectionProbe is shared by native and compatible endpoint probes.
func FailedConnectionProbe(reason string) ConnectionProbe {
	detail := "Could not reach the upstream endpoint. Check DNS, TLS, proxy and timeout settings; redirects are not followed."
	if reason == "URL_NOT_ALLOWED" {
		detail = probeURLDetail
	}
	return ConnectionProbe{Network: "failed", Authorization: "unknown", Discovery: "unknown", Models: []string{}, NetworkReason: reason, Detail: detail}
}

// ConnectionProbeTransportFailure classifies a transport error without exposing it.
func ConnectionProbeTransportFailure(err error) ConnectionProbe {
	// The pinned transport also enforces public IP policy after DNS resolution.
	if strings.Contains(err.Error(), "not allowed") {
		return FailedConnectionProbe("URL_NOT_ALLOWED")
	}
	return FailedConnectionProbe("PROBE_FAILED")
}

func connectionProbeBuildFailure(err error) ConnectionProbe {
	result := ConnectionProbe{Network: "unknown", Authorization: "unknown", Discovery: "unknown", Models: []string{}}
	var policyErr *connectionProbeURLError
	if errors.As(err, &policyErr) {
		return FailedConnectionProbe("URL_NOT_ALLOWED")
	}
	var syncErr *UpstreamModelSyncError
	if errors.As(err, &syncErr) {
		switch syncErr.Kind {
		case UpstreamModelSyncErrorNotAdapted, UpstreamModelSyncErrorAccountType, UpstreamModelSyncErrorUnsupported, UpstreamModelSyncErrorDiscoveryUnconfirmed:
			result.Discovery = "unsupported"
			result.DiscoveryReason = "CHECK_UNSUPPORTED"
			result.Detail = "Model discovery is not supported for this provider or account type."
			return result
		case UpstreamModelSyncErrorCredentials:
			result.Authorization = "failed"
			result.AuthorizationReason = "AUTH_REJECTED"
			result.Detail = "Credentials are missing or unusable. Check the configured authentication fields."
			return result
		}
	}
	result.Discovery = "failed"
	result.DiscoveryReason = "PROBE_FAILED"
	result.Detail = "Could not prepare the probe request. Check the endpoint, credentials and provider configuration."
	return result
}

// ConnectionProbeHTTPStatus records reachability independently of authorization.
func ConnectionProbeHTTPStatus(status int) ConnectionProbe {
	result := ConnectionProbe{Network: "passed", Authorization: "unknown", Discovery: "unknown", Models: []string{}}
	switch {
	case status >= 300 && status < 400:
		return FailedConnectionProbe("PROBE_FAILED")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		result.Authorization = "failed"
		result.AuthorizationReason = "AUTH_REJECTED"
		result.DiscoveryReason = "AUTH_REJECTED"
		result.Detail = "Upstream rejected the credentials (HTTP 401/403). Check the key or token and its permissions."
	case status == http.StatusTooManyRequests:
		result.AuthorizationReason = "UPSTREAM_RATE_LIMITED"
		result.DiscoveryReason = "UPSTREAM_RATE_LIMITED"
		result.Detail = "Upstream rate-limited the probe (HTTP 429). Wait and retry or check the upstream quota."
	case status < 200 || status >= 300:
		result.DiscoveryReason = "PROBE_FAILED"
		result.Detail = "Upstream did not return a successful model discovery response. Check endpoint availability and provider configuration."
	}
	return result
}
