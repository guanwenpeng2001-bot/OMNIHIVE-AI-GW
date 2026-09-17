package cursor

import (
	"context"
	"net/http"
)

type generationSendKey struct{}

// WithGenerationSendHook instruments only Run, excluding token exchange,
// model discovery and heartbeat/control frames on the duplex connection.
func WithGenerationSendHook(ctx context.Context, hook func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error)) context.Context {
	return context.WithValue(ctx, generationSendKey{}, hook)
}

func sendGeneration(client HTTPDoer, request *http.Request) (*http.Response, error) {
	send := func(r *http.Request) (*http.Response, error) { return Do(client, r, true) }
	if hook, ok := request.Context().Value(generationSendKey{}).(func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error)); ok {
		return hook(request, send)
	}
	return send(request)
}
