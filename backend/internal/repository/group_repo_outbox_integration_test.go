//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupConfigurationOutboxAtomicity(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	// The independent SQL executor must never be used for these writes.
	forbiddenSQL := &forbidSQLExecutor{}
	repo := newGroupRepositoryWithSQL(client, forbiddenSQL)
	outbox := NewSchedulerOutboxRepository(integrationDB)
	groupName := fmt.Sprintf("group-outbox-atomic-%d", time.Now().UnixNano())
	configured := &service.Group{
		Name: groupName, Platform: service.PlatformAnthropic,
		RateMultiplier: 1, Status: service.StatusActive,
		SubscriptionType: service.SubscriptionTypeStandard,
	}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE group_id IN (SELECT id FROM groups WHERE name = $1)", groupName)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE name = $1", groupName)
	})

	consumeRefresh := func(afterID int64, expectedMultiplier float64) int64 {
		t.Helper()
		events, err := outbox.ListAfterAndReleaseDedup(ctx, afterID, 10000)
		require.NoError(t, err)
		var refreshID int64
		for _, event := range events {
			if event.GroupID == nil || *event.GroupID != configured.ID {
				continue
			}
			require.Zero(t, refreshID, "one pending refresh per group")
			require.Equal(t, service.SchedulerOutboxEventGroupChanged, event.EventType)
			require.Nil(t, event.AccountID)
			refreshID = event.ID
			refreshed, err := repo.GetByIDLite(ctx, *event.GroupID)
			require.NoError(t, err)
			require.Equal(t, expectedMultiplier, refreshed.RateMultiplier)
		}
		require.Greater(t, refreshID, afterID, "consumer must receive a refreshable group event")
		return refreshID
	}

	watermark, err := outbox.MaxID(ctx)
	require.NoError(t, err)
	t.Run("create failure rolls back group and notification", func(t *testing.T) {
		restore := rejectGroupConfigurationOutbox(t, groupName)
		defer restore()
		err := repo.Create(ctx, configured)
		require.ErrorContains(t, err, "forced group configuration outbox failure")
		require.Zero(t, configured.ID, "failed create must not publish an ID")
		require.True(t, configured.CreatedAt.IsZero())
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM groups WHERE name = $1", groupName).Scan(&count))
		require.Zero(t, count)
		latest, err := outbox.MaxID(ctx)
		require.NoError(t, err)
		require.Equal(t, watermark, latest)
	})

	require.NoError(t, repo.Create(ctx, configured))
	watermark = consumeRefresh(watermark, 1)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE group_id = $1", configured.ID)
	require.NoError(t, err)

	t.Run("update failure preserves persisted configuration", func(t *testing.T) {
		restore := rejectGroupConfigurationOutbox(t, groupName)
		defer restore()
		previousUpdatedAt := configured.UpdatedAt
		configured.RateMultiplier = 2
		err := repo.Update(ctx, configured)
		require.ErrorContains(t, err, "forced group configuration outbox failure")
		require.Equal(t, previousUpdatedAt, configured.UpdatedAt)
		persisted, err := repo.GetByIDLite(ctx, configured.ID)
		require.NoError(t, err)
		require.Equal(t, float64(1), persisted.RateMultiplier)
		var eventCount int
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE group_id = $1", configured.ID).Scan(&eventCount))
		require.Zero(t, eventCount)
	})

	require.NoError(t, repo.Update(ctx, configured))
	watermark = consumeRefresh(watermark, 2)
	// Consuming releases dedup so another successful update can notify again.
	configured.RateMultiplier = 3
	require.NoError(t, repo.Update(ctx, configured))
	consumeRefresh(watermark, 3)
	require.False(t, forbiddenSQL.called)
}

func TestGroupConfigurationOutboxCallerTransaction(t *testing.T) {
	ctx := context.Background()
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprintf("commit=%t", commit), func(t *testing.T) {
			transaction := testEntTx(t)
			repo := newGroupRepositoryWithSQL(transaction.Client(), integrationDB)
			configured := &service.Group{
				Name:     fmt.Sprintf("group-outbox-caller-%d", time.Now().UnixNano()),
				Platform: service.PlatformAnthropic, RateMultiplier: 1,
				Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard,
			}
			t.Cleanup(func() {
				_ = transaction.Rollback()
				_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE group_id = $1", configured.ID)
				_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", configured.ID)
			})
			require.NoError(t, repo.Create(ctx, configured))
			configured.RateMultiplier = 2
			require.NoError(t, repo.Update(ctx, configured))
			assertCounts := func(expected int) {
				t.Helper()
				var groupCount, eventCount int
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM groups WHERE id = $1", configured.ID).Scan(&groupCount))
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE group_id = $1", configured.ID).Scan(&eventCount))
				require.Equal(t, expected, groupCount)
				require.Equal(t, expected, eventCount)
			}
			assertCounts(0)
			if commit {
				require.NoError(t, transaction.Commit())
				assertCounts(1)
			} else {
				require.NoError(t, transaction.Rollback())
				assertCounts(0)
			}
		})
	}
}

func rejectGroupConfigurationOutbox(t *testing.T, groupName string) func() {
	t.Helper()
	ctx := context.Background()
	functionName := fmt.Sprintf("reject_group_configuration_outbox_%d", time.Now().UnixNano())
	// groupName is generated by this test, never supplied externally.
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF EXISTS (SELECT 1 FROM groups WHERE id = NEW.group_id AND name = '%s') THEN
				RAISE EXCEPTION 'forced group configuration outbox failure';
			END IF;
			RETURN NEW;
		END;
		$$`, functionName, groupName))
	require.NoError(t, err)
	restore := func() {
		_, _ = integrationDB.ExecContext(ctx, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON scheduler_outbox", functionName))
		_, _ = integrationDB.ExecContext(ctx, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	}
	t.Cleanup(restore)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION %s()", functionName, functionName))
	require.NoError(t, err)
	return restore
}
