package service

import (
	"context"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"net/http"
	"time"
)

type batchImageAccountEgressKey struct{}

func withBatchImageAccountEgress(ctx context.Context, job *BatchImageJob, account *Account) (context.Context, error) {
	if account == nil || (account.ProxyID == nil && account.ProxyGroupID == nil && (job == nil || job.EgressSnapshot == nil)) {
		return ctx, nil
	}
	if account.ProxyGroupID != nil || (job != nil && job.EgressSnapshot != nil) {
		if job == nil {
			return ctx, ErrProxyGroupUnavailable
		}
		if err := account.RestoreJobEgress(ctx, job.EgressSnapshot); err != nil {
			return ctx, err
		}
	}
	if _, err := account.EgressProxyURL(ctx); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, batchImageAccountEgressKey{}, account), nil
}

// Reuse the bounded shared proxy client pool while preserving the caller's
// timeout, redirect policy and cookie jar. No route means the original client.
func batchImageDo(req *http.Request, original *http.Client) (*http.Response, error) {
	account, _ := req.Context().Value(batchImageAccountEgressKey{}).(*Account)
	client := original
	if account != nil {
		proxyURL, err := account.EgressProxyURL(req.Context())
		if err != nil {
			return nil, err
		}
		if proxyURL != "" {
			pooled, err := httpclient.GetClient(httpclient.Options{ProxyURL: proxyURL, ResponseHeaderTimeout: 60 * time.Second})
			if err != nil {
				return nil, err
			}
			proxied := *original
			proxied.Transport = pooled.Transport
			client = &proxied
		}
	}
	return client.Do(req) //nolint:gosec // G704: provider clients construct URLs from fixed/operator-configured origins; Gemini response URLs pass validateGeminiDownloadHost. This adapter changes only the account proxy transport, preserving the caller's redirect policy.
}
