package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type moderationDeadlineProxyRepo struct{ ProxyRepository }

func (*moderationDeadlineProxyRepo) GetByID(ctx context.Context, _ int64) (*Proxy, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestModerationDeadlineIncludesDirectRequestAndProxyResolution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		<-request.Context().Done()
	}))
	defer server.Close()
	for _, proxy := range []bool{false, true} {
		moderation := &ContentModerationService{}
		cfg := &ContentModerationConfig{BaseURL: server.URL, Model: "moderation", TimeoutMS: 25}
		if proxy {
			proxyID := int64(1)
			cfg.ProxyID = &proxyID
			moderation.proxyRepo = &moderationDeadlineProxyRepo{}
		}
		started := time.Now()
		_, err := moderation.callModerationOnceWithInput(context.Background(), cfg, "test", "text", nil)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Less(t, time.Since(started), time.Second)
	}
}
