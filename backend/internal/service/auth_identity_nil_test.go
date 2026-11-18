package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityBackportMissingEmailIdentity(t *testing.T) {
	for _, created := range []bool{false, true} {
		require.NotPanics(t, func() {
			require.False(t, (&AuthService{}).shouldApplyEmailFirstBindDefaults(context.Background(), 1, nil, created))
		})
	}
}
