package service

import "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"

// SanitizeStoredCredentials strips secrets that must never be persisted on the
// account credentials map after conversion to OAuth tokens (Grok Web SSO / password).
// Call from admin create/update/import/apply-oauth paths.
//
// Cookie is always stripped: bulk paths may pass an empty platform label, and
// session-jar residue must never sit next to OAuth tokens on any platform.
// Compatible single-account writes also consume explicit billing-plan clear
// markers. Bulk writes pass an empty platform and retain markers for SQL merge.
func SanitizeStoredCredentials(platform string, creds map[string]any) map[string]any {
	if creds == nil {
		return nil
	}
	if IsCompatibleProvider(platform) {
		domain.ClearEmptyBillingPlan(creds)
	}
	for _, key := range []string{
		"password", "sso_token", "sso", "sso-rw", "clearTextPassword", "cookie",
	} {
		delete(creds, key)
	}
	return creds
}
