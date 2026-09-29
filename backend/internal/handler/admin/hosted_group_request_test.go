package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostedRouteRequestDistinguishesOmissionFromExplicitClear(t *testing.T) {
	for _, example := range []struct {
		payload     string
		accountType string
		present     bool
	}{
		{`{"public_model":"public","target_platform":"anthropic"}`, "", false},
		{`{"public_model":"public","target_platform":"anthropic","account_type":""}`, "", true},
		{`{"public_model":"public","target_platform":"anthropic","account_type":"bedrock"}`, "bedrock", true},
	} {
		var request CompositeRouteRequest
		require.NoError(t, json.Unmarshal([]byte(example.payload), &request))
		input := compositeRouteRequestToInput(request, true)
		require.Equal(t, example.accountType, input.AccountType)
		require.Equal(t, example.present, input.AccountTypeSet)
	}
}
