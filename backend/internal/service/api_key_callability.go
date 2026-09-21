package service

import "time"

// APIKeyUnavailableReason names the single fact that currently prevents a key
// from serving gateway traffic. The values are part of the /v1/usage contract:
// clients use them to tell "expired", "quota exhausted" and "manually disabled"
// apart instead of collapsing everything into one "unusable" state.
type APIKeyUnavailableReason string

const (
	APIKeyUnavailableReasonDisabled        APIKeyUnavailableReason = "key_disabled"
	APIKeyUnavailableReasonExpired         APIKeyUnavailableReason = "key_expired"
	APIKeyUnavailableReasonQuotaExhausted  APIKeyUnavailableReason = "key_quota_exhausted"
	APIKeyUnavailableReasonUserInactive    APIKeyUnavailableReason = "user_inactive"
	APIKeyUnavailableReasonGroupDeleted    APIKeyUnavailableReason = "group_deleted"
	APIKeyUnavailableReasonGroupDisabled   APIKeyUnavailableReason = "group_disabled"
	APIKeyUnavailableReasonGroupNotAllowed APIKeyUnavailableReason = "group_not_allowed"
)

// APIKeyCallability is the answer to "would this credential be admitted by the
// gateway right now?". It mirrors the authentication half of the gateway
// middleware (credential state, owning user, bound group) so that read-only
// endpoints such as /v1/usage can report isValid honestly while still answering
// for keys that are currently restricted.
//
// Wallet balance and subscription windows are deliberately out of scope: those
// are account-level limits that the caller already receives as remaining/
// subscription fields, and treating a subscription key's empty wallet as
// "invalid key" would be wrong.
type APIKeyCallability struct {
	Callable bool
	Reason   APIKeyUnavailableReason
}

// Callability evaluates the credential, its owner and its bound group at now.
// A nil key or an unknown status is not callable.
func (k *APIKey) Callability(now time.Time) APIKeyCallability {
	if k == nil {
		return APIKeyCallability{Callable: false, Reason: APIKeyUnavailableReasonDisabled}
	}
	if k.Status != StatusActive {
		switch k.Status {
		case StatusAPIKeyExpired:
			return APIKeyCallability{Callable: false, Reason: APIKeyUnavailableReasonExpired}
		case StatusAPIKeyQuotaExhausted:
			return APIKeyCallability{Callable: false, Reason: APIKeyUnavailableReasonQuotaExhausted}
		default:
			return APIKeyCallability{Callable: false, Reason: APIKeyUnavailableReasonDisabled}
		}
	}
	if k.User == nil || !k.User.IsActive() {
		return APIKeyCallability{Callable: false, Reason: APIKeyUnavailableReasonUserInactive}
	}
	if reason, ok := k.groupUnavailableReason(); ok {
		return APIKeyCallability{Callable: false, Reason: reason}
	}
	if k.ExpiresAt != nil && !now.Before(*k.ExpiresAt) {
		return APIKeyCallability{Callable: false, Reason: APIKeyUnavailableReasonExpired}
	}
	if k.Quota > 0 && k.QuotaUsed >= k.Quota {
		return APIKeyCallability{Callable: false, Reason: APIKeyUnavailableReasonQuotaExhausted}
	}
	return APIKeyCallability{Callable: true}
}

// groupUnavailableReason reports why the key's bound group blocks serving. An
// unbound key (no group) is callable: the gateway falls back to the default
// routing policy for it.
func (k *APIKey) groupUnavailableReason() (APIKeyUnavailableReason, bool) {
	if k.GroupID == nil {
		return "", false
	}
	group := k.Group
	if group == nil || group.Status == "deleted" {
		return APIKeyUnavailableReasonGroupDeleted, true
	}
	if !group.IsActive() {
		return APIKeyUnavailableReasonGroupDisabled, true
	}
	if k.User == nil || group.IsSubscriptionType() {
		return "", false
	}
	if !k.User.CanBindGroup(group.ID, group.IsExclusive) {
		return APIKeyUnavailableReasonGroupNotAllowed, true
	}
	return "", false
}
