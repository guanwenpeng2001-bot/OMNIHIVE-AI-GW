package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"net/http"
)

func cursorAccountContext(ctx context.Context, account *Account) (context.Context, error) {
	if account != nil && account.ProxyGroupID != nil {
		proxyURL, err := account.EgressProxyURL(ctx)
		if err != nil {
			return ctx, err
		}
		return cursor.WithAccountProxy(ctx, proxyURL, false), nil
	}
	if account == nil || account.ProxyID == nil {
		return cursor.WithAccountProxy(ctx, "", false), nil
	}
	if account.Proxy == nil || account.Proxy.ID != *account.ProxyID {
		return cursor.WithAccountProxy(ctx, "", true), nil
	}
	return cursor.WithAccountProxy(ctx, account.Proxy.URL(), false), nil
}

func applyCursorRelayHeaders(headers http.Header, account *Account) {
	overrides := make(http.Header)
	account.ApplyHeaderOverrides(overrides)
	cursor.ApplyRelayHeaders(headers, overrides, account.GetCredential("api_key"))
}
