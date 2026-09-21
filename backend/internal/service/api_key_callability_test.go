package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyCallability(t *testing.T) {
	now := time.Date(2026, time.September, 21, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	activeUser := &User{ID: 7, Status: StatusActive, Balance: 10}
	activeGroup := &Group{ID: 3, Status: StatusActive}

	tests := []struct {
		name       string
		key        *APIKey
		callable   bool
		wantReason APIKeyUnavailableReason
	}{
		{
			name:     "active key with no explicit limits is callable",
			key:      &APIKey{Status: StatusAPIKeyActive, User: activeUser},
			callable: true,
		},
		{
			name:       "manually disabled key is not callable",
			key:        &APIKey{Status: StatusAPIKeyDisabled, User: activeUser},
			wantReason: APIKeyUnavailableReasonDisabled,
		},
		{
			name:       "unknown status is not callable",
			key:        &APIKey{Status: "inactive", User: activeUser},
			wantReason: APIKeyUnavailableReasonDisabled,
		},
		{
			name:       "expired status is not callable",
			key:        &APIKey{Status: StatusAPIKeyExpired, User: activeUser, ExpiresAt: &past},
			wantReason: APIKeyUnavailableReasonExpired,
		},
		{
			name:       "active status past expiry is not callable",
			key:        &APIKey{Status: StatusAPIKeyActive, User: activeUser, ExpiresAt: &past},
			wantReason: APIKeyUnavailableReasonExpired,
		},
		{
			name:     "active status before expiry is callable",
			key:      &APIKey{Status: StatusAPIKeyActive, User: activeUser, ExpiresAt: &future},
			callable: true,
		},
		{
			name:       "quota exhausted status is not callable",
			key:        &APIKey{Status: StatusAPIKeyQuotaExhausted, User: activeUser, Quota: 5, QuotaUsed: 5},
			wantReason: APIKeyUnavailableReasonQuotaExhausted,
		},
		{
			name:       "active status with exhausted quota is not callable",
			key:        &APIKey{Status: StatusAPIKeyActive, User: activeUser, Quota: 5, QuotaUsed: 6},
			wantReason: APIKeyUnavailableReasonQuotaExhausted,
		},
		{
			name:       "inactive owner is not callable",
			key:        &APIKey{Status: StatusAPIKeyActive, User: &User{ID: 7, Status: StatusDisabled}},
			wantReason: APIKeyUnavailableReasonUserInactive,
		},
		{
			name:       "missing owner is not callable",
			key:        &APIKey{Status: StatusAPIKeyActive},
			wantReason: APIKeyUnavailableReasonUserInactive,
		},
		{
			name:       "disabled bound group is not callable",
			key:        &APIKey{Status: StatusAPIKeyActive, User: activeUser, GroupID: callabilityInt64Ptr(3), Group: &Group{ID: 3, Status: StatusDisabled}},
			wantReason: APIKeyUnavailableReasonGroupDisabled,
		},
		{
			name:       "deleted bound group is not callable",
			key:        &APIKey{Status: StatusAPIKeyActive, User: activeUser, GroupID: callabilityInt64Ptr(3), Group: nil},
			wantReason: APIKeyUnavailableReasonGroupDeleted,
		},
		{
			name:       "exclusive group the owner may not bind is not callable",
			key:        &APIKey{Status: StatusAPIKeyActive, User: activeUser, GroupID: callabilityInt64Ptr(3), Group: &Group{ID: 3, Status: StatusActive, IsExclusive: true}},
			wantReason: APIKeyUnavailableReasonGroupNotAllowed,
		},
		{
			name: "exclusive group listed on the owner is callable",
			key: &APIKey{
				Status:  StatusAPIKeyActive,
				User:    &User{ID: 7, Status: StatusActive, AllowedGroups: []int64{3}},
				GroupID: callabilityInt64Ptr(3),
				Group:   &Group{ID: 3, Status: StatusActive, IsExclusive: true},
			},
			callable: true,
		},
		{
			name:     "key without a group keeps the default routing fallback",
			key:      &APIKey{Status: StatusAPIKeyActive, User: activeUser, Group: activeGroup},
			callable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.key.Callability(now)
			require.Equal(t, tt.callable, got.Callable)
			require.Equal(t, tt.wantReason, got.Reason)
		})
	}

	t.Run("nil key is not callable", func(t *testing.T) {
		var key *APIKey
		got := key.Callability(now)
		require.False(t, got.Callable)
		require.Equal(t, APIKeyUnavailableReasonDisabled, got.Reason)
	})
}

func callabilityInt64Ptr(v int64) *int64 { return &v }
