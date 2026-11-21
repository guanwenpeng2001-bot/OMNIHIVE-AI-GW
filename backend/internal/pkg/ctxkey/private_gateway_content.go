package ctxkey

import "context"

type privateGatewayContentKey struct{}

// WithPrivateGatewayContent marks requests whose content must be kept out of diagnostics.
func WithPrivateGatewayContent(ctx context.Context) context.Context {
	return context.WithValue(ctx, privateGatewayContentKey{}, true)
}

func PrivateGatewayContent(ctx context.Context) bool {
	return ctx != nil && ctx.Value(privateGatewayContentKey{}) == true
}
