package scope

import "context"

type Authorized struct {
	WorkspaceID, ProjectID, ActorID, KeyID, KeyOwnerID, FundingUserID, Version int64
}

type contextKey struct{}

func With(ctx context.Context, s Authorized) context.Context {
	return context.WithValue(ctx, contextKey{}, s)
}

func From(ctx context.Context) (Authorized, bool) {
	s, ok := ctx.Value(contextKey{}).(Authorized)
	return s, ok && s.WorkspaceID > 0
}
