//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityBackportSelectionReleasesFailedHydration(t *testing.T) {
	svc := &GatewayService{schedulerSnapshot: &SchedulerSnapshotService{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	releases := 0
	selection, err := svc.newSelectionResult(ctx, &Account{ID: 42}, true, func() { releases++ }, nil)
	require.Error(t, err)
	require.Nil(t, selection)
	require.Equal(t, 1, releases)
}

func TestSecurityBackportSelectionRetainsSuccessfulSlot(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformAnthropic}
	cache := &snapshotHydrationCache{accounts: map[int64]*Account{account.ID: account}}
	svc := &GatewayService{schedulerSnapshot: NewSchedulerSnapshotService(cache, nil, nil, nil, nil)}
	releases := 0
	selection, err := svc.newSelectionResult(t.Context(), account, true, func() { releases++ }, nil)
	require.NoError(t, err)
	require.Zero(t, releases)
	selection.ReleaseFunc()
	require.Equal(t, 1, releases)
}
