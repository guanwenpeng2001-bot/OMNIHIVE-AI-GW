package identity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectKeyModelsRead(t *testing.T) {
	for _, mode := range []string{"inherit", "allow_list", "deny_all"} {
		p := KeyPolicy{Mode: mode, Models: []string{"a"}}
		require.False(t, p.Allows("models.read", nil))
		p.Actions = []string{"models.read"}
		require.True(t, p.Allows("models.read", nil), mode)
		require.False(t, p.Allows("text.chat", []string{"a"}))
	}
}
