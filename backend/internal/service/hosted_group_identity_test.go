//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostedGroupTypeRoundTripAndExplicitClear(t *testing.T) {
	repository := &groupRepoStubForAdmin{createID: 21}
	admin := &adminServiceImpl{groupRepo: repository}
	created, err := admin.CreateGroup(context.Background(), &CreateGroupInput{Name: "Hosted", Platform: PlatformAnthropic, AccountType: AccountTypeBedrock, RateMultiplier: 1})
	require.NoError(t, err)
	require.Equal(t, AccountTypeBedrock, repository.created.AccountType)
	require.False(t, created.AllowsAccount(&Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}))
	repository.getByIDByID = map[int64]*Group{21: created}
	updated, err := admin.UpdateGroup(context.Background(), 21, &UpdateGroupInput{Name: "Renamed"})
	require.NoError(t, err)
	require.Equal(t, AccountTypeBedrock, updated.AccountType, "omission must preserve subtype")
	cleared := ""
	updated, err = admin.UpdateGroup(context.Background(), 21, &UpdateGroupInput{AccountType: &cleared})
	require.NoError(t, err)
	require.Empty(t, updated.AccountType)
}

func TestHostedPlazaKeepsDirectModelsOutOfRestrictedGroups(t *testing.T) {
	channel := plazaPricedChannel(1, "hosted", []int64{10}, PlatformBedrock, "hosted-model")
	channel.ModelPricing = append(channel.ModelPricing, ChannelModelPricing{Platform: PlatformAnthropic, Models: []string{"direct-model"}})
	for _, group := range []Group{{ID: 10, Platform: PlatformBedrock}, {ID: 10, Platform: PlatformAnthropic, AccountType: AccountTypeBedrock}} {
		plaza := newPlazaService([]Channel{channel}, []Group{group}, nil)
		groups, err := plaza.ListGroups(context.Background())
		require.NoError(t, err)
		require.Len(t, groups, 1)
		require.Len(t, groups[0].Models, 1)
		require.Equal(t, "hosted-model", groups[0].Models[0].Name)
	}
}

func TestHostedRouteOmissionKeepsSubtypeAndExplicitClearRemovesIt(t *testing.T) {
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{{ID: 2, GroupID: 1, PublicModel: "public", TargetPlatform: PlatformAnthropic, AccountType: AccountTypeBedrock}}}
	admin := &adminServiceImpl{
		groupRepo:          &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{1: {ID: 1, Platform: PlatformComposite}}},
		compositeRouteRepo: routes,
	}
	input := CompositeRouteInput{PublicModel: "public", TargetPlatform: PlatformAnthropic}
	updated, err := admin.UpdateCompositeRoute(context.Background(), 1, 2, input)
	require.NoError(t, err)
	require.Equal(t, AccountTypeBedrock, updated.AccountType)
	input.AccountTypeSet = true
	updated, err = admin.UpdateCompositeRoute(context.Background(), 1, 2, input)
	require.NoError(t, err)
	require.Empty(t, updated.AccountType)
}

func TestHostedSchedulerRebuildRefreshesBothAliasBuckets(t *testing.T) {
	for _, platform := range []string{PlatformBedrock, PlatformAnthropic} {
		account := &Account{ID: 1, Platform: platform, Type: AccountTypeBedrock, GroupIDs: []int64{12}}
		cache := newBulkEventSnapshotCache()
		repository := newBulkEventAccountRepo(account)
		scheduler := newBulkEventTestService(cache, repository)
		require.NoError(t, scheduler.handleBulkAccountEvent(context.Background(), bulkEventPayload([]int64{1}, []int64{11}), make(map[batchSeenKey]struct{})))
		require.ElementsMatch(t, schedulerBucketsForTest([]int64{11, 12}, PlatformAnthropic, PlatformBedrock), cache.capturedBuckets())
		cache = newBulkEventSnapshotCache()
		scheduler = newBulkEventTestService(cache, repository)
		require.NoError(t, scheduler.rebuildByAccount(context.Background(), account, []int64{12}, "hosted identity fixture", make(map[batchSeenKey]struct{})))
		require.ElementsMatch(t, schedulerBucketsForTest([]int64{12}, PlatformAnthropic, PlatformBedrock), cache.capturedBuckets())
	}
}
