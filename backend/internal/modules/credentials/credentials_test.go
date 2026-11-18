package credentials

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExplicitMutations(t *testing.T) {
	for _, op := range []string{"keep", "clear"} {
		m, err := (Change{Operation: op}).Payload("api_key")
		require.NoError(t, err)
		require.Nil(t, m)
		_, err = (Change{Operation: op, Secret: "ambiguous"}).Payload("api_key")
		require.Error(t, err)
	}
	_, err := (Change{Operation: "set", Type: "oauth", Secret: "key"}).Payload("api_key")
	require.Error(t, err)
	m, err := (Change{Operation: "set", Type: "api_key", Secret: "key"}).Payload("api_key")
	require.NoError(t, err)
	require.Equal(t, "key", m["api_key"])
}
