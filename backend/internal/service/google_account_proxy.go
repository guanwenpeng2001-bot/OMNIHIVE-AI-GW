package service

import (
	"context"
	"fmt"
)

// resolveGoogleAccountProxyURL keeps background Google requests on the account's
// configured egress. A failed lookup must not silently turn a proxy binding into
// a direct connection. ProxyFallbackOriginID is bookkeeping; ProxyID is the
// current route selected by the existing expiry/fallback service.
func resolveGoogleAccountProxyURL(ctx context.Context, account *Account, repo ProxyRepository) (string, error) {
	if account.ProxyID == nil {
		return "", nil
	}
	if repo != nil {
		proxy, err := repo.GetByID(ctx, *account.ProxyID)
		if err != nil {
			return "", fmt.Errorf("resolve account proxy %d: %w", *account.ProxyID, err)
		}
		if proxy != nil {
			return proxy.URL(), nil
		}
	} else if account.Proxy != nil && account.Proxy.ID == *account.ProxyID {
		return account.Proxy.URL(), nil
	}
	return "", fmt.Errorf("account proxy %d is unavailable", *account.ProxyID)
}
