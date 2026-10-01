package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

func normalizedProxyID(id *int64) *int64 {
	if id == nil || *id == 0 {
		return nil
	}
	copy := *id
	return &copy
}

func validateAccountProxyChoice(proxyID, groupID *int64) error {
	if (proxyID != nil && *proxyID < 0) || (groupID != nil && *groupID < 0) {
		return infraerrors.BadRequest("INVALID_PROXY_BINDING", "proxy identifiers must be non-negative")
	}
	if normalizedProxyID(proxyID) != nil && normalizedProxyID(groupID) != nil {
		return infraerrors.BadRequest("PROXY_BINDING_CONFLICT", "select either an IP node or an IP group")
	}
	return nil
}

// A nil patch preserves configuration; zero explicitly clears it. Validate the
// final pair before changing either field so errors cannot partially apply.
func applyAccountProxyChoice(account *Account, proxyID, groupID *int64) error {
	node, group := account.ProxyID, account.ProxyGroupID
	if proxyID != nil {
		node = normalizedProxyID(proxyID)
	}
	if groupID != nil {
		group = normalizedProxyID(groupID)
	}
	if err := validateAccountProxyChoice(proxyID, groupID); err != nil {
		return err
	}
	if err := validateAccountProxyChoice(node, group); err != nil {
		return err
	}
	account.ProxyID, account.ProxyGroupID = node, group
	if proxyID != nil {
		account.Proxy = nil
	}
	if groupID != nil {
		account.ProxyGroup = nil
	}
	if proxyID != nil || groupID != nil {
		account.egress = nil
		account.proxyConfigurationUpdated = true
		account.ProxyFallbackOriginID = nil
		account.ProxyFallbackOriginName = nil
	}
	return nil
}

func (a *Account) ValidateProxyConfiguration() error {
	return validateAccountProxyChoice(a.ProxyID, a.ProxyGroupID)
}

func (a *Account) ProxyConfigurationUpdated() bool { return a != nil && a.proxyConfigurationUpdated }
