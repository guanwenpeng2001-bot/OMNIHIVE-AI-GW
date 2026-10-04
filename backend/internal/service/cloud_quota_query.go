package service

import (
	"context"
	"strings"
)

func validateCloudQueryKeys(result *CNProviderQuotaProbeResult, access, secret string) bool {
	if access == "" || secret == "" {
		result.Status, result.CredentialValid, result.Error = "missing_credentials", false, "Cloud quota requires separate management credentials"
		return false
	}
	if len(access) > 512 || len(secret) > 4096 || strings.ContainsAny(access, "\r\n\t ,/=") || strings.ContainsAny(secret, "\r\n") {
		result.Status, result.CredentialValid, result.Error = "invalid_credentials", false, "Invalid cloud quota credentials"
		return false
	}
	return true
}

// Fixed callers supply official endpoints; this is not a configurable URL facility.
func (s *CNProviderQuotaService) cloudQueryEndpoint(ctx context.Context, account *Account, target string, result *CNProviderQuotaProbeResult) (string, string, bool) {
	endpoint, err := cnValidateProbeURL(s.cfg, target)
	if err != nil {
		result.Status, result.Error = "url_rejected", "Quota management host rejected by outbound URL policy"
		return "", "", false
	}
	proxy, err := s.resolveProxyURL(ctx, account)
	if err != nil || (account.ProxyID != nil && proxy == "") {
		result.Status, result.Error = "upstream_error", "Quota proxy binding could not be resolved"
		return "", "", false
	}
	return strings.TrimRight(endpoint, "/") + "/", proxy, true
}

func (s *CNProviderQuotaService) persistCloudQuery(ctx context.Context, account *Account, result *CNProviderQuotaProbeResult, updates map[string]any) {
	err := persistAccountRuntimeSnapshot(ctx, s.accountRepo, account, updates, nil)
	if err != nil {
		result.discardChangedIdentity(err)
	} else {
		result.Persisted = true
	}
}
