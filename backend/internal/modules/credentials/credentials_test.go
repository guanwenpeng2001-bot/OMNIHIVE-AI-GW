package credentials

import (
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"

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
	for _, tc := range []struct {
		change Change
		field  string
	}{
		{Change{Operation: "set", Type: "oauth", Secret: "key"}, "/type"},
		{Change{Operation: "set", Type: "api_key", Secret: "  "}, "/secret"},
		{Change{Operation: "set", Type: "api_key", Secret: strings.Repeat("a", 262145)}, "/secret"},
	} {
		_, err := tc.change.Payload("api_key")
		var f *fault.Fault
		require.ErrorAs(t, err, &f)
		require.Equal(t, tc.field, f.Field)
	}
	m, err := (Change{Operation: "set", Type: "api_key", Secret: "key"}).Payload("api_key")
	require.NoError(t, err)
	require.Equal(t, "key", m["api_key"])
}

func TestMetadataRequiresSet(t *testing.T) {
	for _, operation := range []string{"keep", "clear"} {
		for _, metadata := range []map[string]any{{}, {"site": "global"}} {
			_, err := (Change{Operation: operation, Metadata: metadata}).Payload("oauth")
			var f *fault.Fault
			require.ErrorAs(t, err, &f)
			require.Equal(t, "/operation", f.Field)
		}
	}
}
