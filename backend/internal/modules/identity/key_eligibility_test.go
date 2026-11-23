package identity

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/stretchr/testify/require"
)

func TestKeyLifecyclePrecedence(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		status                string
		expiry                *time.Time
		exhausted, credential bool
		state                 KeyState
	}{
		{"inactive", &now, true, false, KeyDisabled},
		{"disabled", nil, false, true, KeyDisabled},
		{"quota_exhausted", &now, true, true, KeyExpired},
		{"expired", nil, true, true, KeyExpired},
		{"active", nil, true, false, KeyQuotaExhausted},
		{"active", nil, false, false, KeyNoCredential},
		{"active", nil, false, true, KeyActive},
		{"other", nil, false, false, KeyNoCredential},
		{"other", nil, false, true, KeyUnknown},
	} {
		require.Equal(t, test.state, KeyLifecycle(test.status, test.expiry, now, test.exhausted, test.credential))
	}
	group := int64(10)
	b, err := json.Marshal(ProjectKeyRow{Key: Key{ID: 1, OwnerID: 2, Version: 1}, GroupID: &group, Kind: LegacyGroupKey, Status: "expired", State: KeyExpired})
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"1","owner_id":"2","version":"1","name":"","prefix":"","kind":"legacy_group","status":"expired","state":"expired","expires_at":null,"group_id":"10","credential":null}`, string(b))
}

func TestEffectiveKeyActionsRejectEmptyIntersection(t *testing.T) {
	owner := Access{Actions: []string{"models.read"}}
	actions, err := EffectiveKeyActions([]string{"text.chat"}, owner)
	require.ErrorIs(t, err, fault.Forbidden)
	require.Empty(t, actions)
	actions, err = EffectiveKeyActions([]string{"models.read", "text.chat"}, owner)
	require.NoError(t, err)
	require.Equal(t, []string{"models.read"}, actions)
}
