package model

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKiloErrorPassthroughIdentity(t *testing.T) {
	require.Contains(t, AllPlatforms(), PlatformKilo)
	rule := ErrorPassthroughRule{Name: "Kilo", MatchMode: MatchModeAny, Platforms: []string{PlatformKilo}, ErrorCodes: []int{429}, PassthroughCode: true, PassthroughBody: true}
	require.NoError(t, rule.Validate())
}
