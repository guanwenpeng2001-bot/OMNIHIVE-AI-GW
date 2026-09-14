package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"net/http"
)

func cursorAccountContext(ctx context.Context, account *Account) context.Context {
	if account == nil || account.ProxyID == nil {
		return cursor.WithAccountProxy(ctx, "", false)
	}
	if account.Proxy == nil || account.Proxy.ID != *account.ProxyID {
		return cursor.WithAccountProxy(ctx, "", true)
	}
	return cursor.WithAccountProxy(ctx, account.Proxy.URL(), false)
}

func applyCursorRelayHeaders(headers http.Header, account *Account) {
	overrides := make(http.Header)
	account.ApplyHeaderOverrides(overrides)
	cursor.ApplyRelayHeaders(headers, overrides, account.GetCredential("api_key"))
}
