package byok

import (
	"net/http"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
)

// CheckSharedFunding rejects tenant selectors on native adapters that use a
// shared account. Check every selector independently so conflicting body and
// header values cannot turn a tenant request into a wallet request.
func CheckSharedFunding(field string, value any) error {
	switch strings.ToLower(field) {
	case "byok", "byok_credential_id", "byok_credential_ref", "credential_ref", "credential_reference", "byok_connection_id":
		return fault.Forbidden
	case "funding_source", "funding_mode":
		if value != "wallet" && value != "subscription" {
			return fault.Forbidden
		}
	case "allow_shared_fallback", "allow_platform_fallback":
		if value != true {
			return fault.Forbidden
		}
	}
	return nil
}

// CheckSharedText handles the string representation in headers, query and forms.
func CheckSharedText(field, value string) error {
	if strings.EqualFold(field, "allow_shared_fallback") || strings.EqualFold(field, "allow_platform_fallback") {
		return CheckSharedFunding(field, value == "true")
	}
	return CheckSharedFunding(field, value)
}

func CheckSharedHeaders(headers http.Header) error {
	for _, pair := range []struct{ header, field string }{
		{"X-Funding-Source", "funding_source"}, {"X-Funding-Mode", "funding_mode"},
		{"X-BYOK-Credential-ID", "byok_credential_id"}, {"X-BYOK-Credential-Ref", "byok_credential_ref"},
		{"X-Allow-Shared-Fallback", "allow_shared_fallback"},
	} {
		for _, header := range []string{pair.header, "X-OmniHive-" + strings.TrimPrefix(pair.header, "X-")} {
			for _, value := range headers.Values(header) {
				if err := CheckSharedText(pair.field, value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// checkFundingGrant binds the exact grant selected by the transaction to the
// native account and payer before vault resolution or transport.
func checkFundingGrant(a scope.Authorized, d Dispatch, c Credential) error {
	if a.WorkspaceID < 1 || c.Workspace != a.WorkspaceID || c.ID != d.CredentialID || c.State != "verified" {
		return fault.Forbidden
	}
	switch d.Source {
	case "byok":
		if d.AccountID == c.ID {
			return nil
		}
	case "wallet":
		if c.Fallback && d.Consent && d.AccountID != c.ID {
			return nil
		}
	}
	return fault.Forbidden
}
