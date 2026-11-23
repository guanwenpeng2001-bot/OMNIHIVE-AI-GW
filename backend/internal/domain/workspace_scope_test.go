package domain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkspaceWriteReplayNamespacesPreserveLegacyAndSeparateDomains(t *testing.T) {
	for _, actor := range []string{"admin:42", "user:42"} {
		legacy := WorkspaceActorScope(context.Background(), actor)
		defaultDomain := WorkspaceActorScope(WithWorkspaceScope(WithDefaultWorkspaceID(context.Background(), 17), 17), actor)
		delegated := WorkspaceActorScope(WithWorkspaceScope(WithDefaultWorkspaceID(context.Background(), 17), 1), actor)
		another := WorkspaceActorScope(WithWorkspaceScope(context.Background(), 3), actor)
		require.Equal(t, actor, legacy)
		require.Equal(t, legacy, defaultDomain, "upgrade must not make existing default-domain writes execute twice")
		require.NotEqual(t, legacy, delegated)
		require.NotEqual(t, delegated, another, "same user and idempotency key cannot replay another domain's resource")
	}
}
