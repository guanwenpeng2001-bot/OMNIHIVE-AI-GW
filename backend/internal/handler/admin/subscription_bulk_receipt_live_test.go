//go:build unit

package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionBulkReceiptSurvivesCleanupAndConcurrentRetry(t *testing.T) {
	fixture := newBulkLiveFixture(t)
	key := fixture.key()
	body := fixture.body("extend", []int64{fixture.ids[0], fixture.ids[1], 2147483647}, 7)
	first := fixture.request(context.Background(), body, key, fixture.token)
	bulkLiveResult(t, first, 2, 1)
	for index := 0; index < 2; index++ {
		require.NoError(t, fixture.redis.Set(context.Background(), fixture.cacheKey(index), "stale-after-crash", time.Minute).Err())
	}
	_, err := fixture.db.Exec(`DELETE FROM idempotency_records WHERE scope='admin.subscriptions.bulk-action' AND idempotency_key_hash=$1`, service.HashIdempotencyKey(key))
	require.NoError(t, err)
	responses := make(chan *httptest.ResponseRecorder, 12)
	var workers sync.WaitGroup
	for index := 0; index < 12; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			responses <- fixture.request(context.Background(), body, key, fixture.token)
		}()
	}
	workers.Wait()
	close(responses)
	for response := range responses {
		if response.Code == 200 {
			require.JSONEq(t, first.Body.String(), response.Body.String())
		} else {
			require.Equal(t, 409, response.Code, response.Body.String())
		}
	}
	replayed := fixture.request(context.Background(), body, key, fixture.token)
	require.JSONEq(t, first.Body.String(), replayed.Body.String())
	for index := 0; index < 2; index++ {
		require.True(t, fixture.initial.AddDate(0, 0, 7).Equal(fixture.expiry(index)), "master cleanup must not erase durable execution identity")
		require.EqualValues(t, 0, fixture.redis.Exists(context.Background(), fixture.cacheKey(index)).Val())
	}
	var durable bool
	var itemCount int
	require.NoError(t, fixture.db.QueryRow(`SELECT expires_at='infinity'::timestamptz,jsonb_array_length(response_body::jsonb) FROM idempotency_records WHERE scope='admin.subscriptions.bulk-action.receipt' AND idempotency_key_hash=$1`, service.HashIdempotencyKey(key)).Scan(&durable, &itemCount))
	require.True(t, durable)
	require.Equal(t, 3, itemCount)
}

type failingLiveBulkReceiptStore struct {
	service.UserSubscriptionRepository
	service.SubscriptionBulkReceiptRepository
	fail bool
}

func (store *failingLiveBulkReceiptStore) SaveBulkReceipt(ctx context.Context, receipt *service.SubscriptionBulkReceipt) error {
	if err := store.SubscriptionBulkReceiptRepository.SaveBulkReceipt(ctx, receipt); err != nil {
		return err
	}
	if store.fail {
		return errors.New("injected failure after actual receipt SQL write")
	}
	return nil
}

func TestSubscriptionBulkReceiptWriteFailureRollsBackBusiness(t *testing.T) {
	fixture := newBulkLiveFixture(t)
	store := &failingLiveBulkReceiptStore{
		UserSubscriptionRepository:        fixture.repo,
		SubscriptionBulkReceiptRepository: fixture.repo.(service.SubscriptionBulkReceiptRepository),
		fail:                              true,
	}
	cache := service.NewBillingCacheService(repository.NewBillingCache(fixture.redis), nil, fixture.repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(cache.Stop)
	subscriptionService := service.NewSubscriptionService(repository.NewGroupRepository(fixture.client, fixture.db), store, cache, fixture.client, nil)
	t.Cleanup(subscriptionService.Stop)
	fixture.handler.subscriptionService = subscriptionService
	require.NoError(t, fixture.redis.Set(context.Background(), fixture.cacheKey(0), "retain-on-rollback", time.Minute).Err())
	key := fixture.key()
	body := fixture.body("extend", fixture.ids[:1], 7)
	response := fixture.request(context.Background(), body, key, fixture.token)
	require.Equal(t, 503, response.Code, response.Body.String())
	require.True(t, fixture.initial.Equal(fixture.expiry(0)))
	require.EqualValues(t, 1, fixture.redis.Exists(context.Background(), fixture.cacheKey(0)).Val())
	var savedItems int
	require.NoError(t, fixture.db.QueryRow(`SELECT jsonb_array_length(response_body::jsonb) FROM idempotency_records WHERE scope='admin.subscriptions.bulk-action.receipt' AND idempotency_key_hash=$1`, service.HashIdempotencyKey(key)).Scan(&savedItems))
	require.Zero(t, savedItems)
	store.fail = false
	_, err := fixture.db.Exec(`UPDATE idempotency_records SET locked_until=NOW()-INTERVAL '1 minute' WHERE scope='admin.subscriptions.bulk-action' AND idempotency_key_hash=$1`, service.HashIdempotencyKey(key))
	require.NoError(t, err)
	bulkLiveResult(t, fixture.request(context.Background(), body, key, fixture.token), 1, 0)
	require.True(t, fixture.initial.AddDate(0, 0, 7).Equal(fixture.expiry(0)))
	require.EqualValues(t, 0, fixture.redis.Exists(context.Background(), fixture.cacheKey(0)).Val())
}

func TestSubscriptionBulkReceiptRefusesUnverifiableLegacyRecovery(t *testing.T) {
	fixture := newBulkLiveFixture(t)
	key := fixture.key()
	payload := service.BulkSubscriptionActionInput{SubscriptionIDs: fixture.ids[:1], Action: "extend", Days: 7}
	fingerprint, err := service.BuildIdempotencyFingerprint("POST", "/api/v1/admin/subscriptions/bulk-action", fmt.Sprintf("admin:%d", fixture.adminID), payload)
	require.NoError(t, err)
	expired := time.Now().Add(-time.Hour)
	created, err := repository.NewIdempotencyRepository(fixture.client, fixture.db).CreateProcessing(context.Background(), &service.IdempotencyRecord{
		Scope: "admin.subscriptions.bulk-action", IdempotencyKeyHash: service.HashIdempotencyKey(key),
		RequestFingerprint: fingerprint, Status: service.IdempotencyStatusProcessing,
		LockedUntil: &expired, ExpiresAt: expired,
	})
	require.NoError(t, err)
	require.True(t, created)
	response := fixture.request(context.Background(), fixture.body("extend", fixture.ids[:1], 7), key, fixture.token)
	require.Equal(t, 503, response.Code, response.Body.String())
	require.True(t, fixture.initial.Equal(fixture.expiry(0)))
	var receiptCount int
	require.NoError(t, fixture.db.QueryRow(`SELECT count(*) FROM idempotency_records WHERE scope='admin.subscriptions.bulk-action.receipt' AND idempotency_key_hash=$1`, service.HashIdempotencyKey(key)).Scan(&receiptCount))
	require.Zero(t, receiptCount)
}

func TestSubscriptionBulkReceiptExpiredOwnerConcurrencyAndCancellation(t *testing.T) {
	fixture := newBulkLiveFixture(t)
	key := fixture.key()
	body := fixture.body("extend", fixture.ids[:1], 7)
	entered := make(chan struct{})
	release := make(chan struct{})
	var paused atomic.Bool
	var releaseOnce sync.Once
	fixture.client.UserSubscription.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, mutation dbent.Mutation) (dbent.Value, error) {
			value, err := next.Mutate(ctx, mutation)
			if err == nil && paused.CompareAndSwap(false, true) {
				close(entered)
				<-release
			}
			return value, err
		})
	})
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responses := make(chan *httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		workers.Wait()
	})
	workers.Add(1)
	go func() {
		defer workers.Done()
		responses <- fixture.request(requestCtx, body, key, fixture.token)
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first transaction did not reach the pause boundary")
	}
	_, err := fixture.db.Exec(`UPDATE idempotency_records SET locked_until=NOW()-INTERVAL '1 minute',expires_at=NOW()-INTERVAL '1 minute' WHERE scope='admin.subscriptions.bulk-action' AND idempotency_key_hash=$1`, service.HashIdempotencyKey(key))
	require.NoError(t, err)
	workers.Add(1)
	go func() {
		defer workers.Done()
		responses <- fixture.request(context.Background(), body, key, fixture.token)
	}()
	require.Eventually(t, func() bool {
		var waiting int
		err := fixture.db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%idempotency_records%'`).Scan(&waiting)
		return err == nil && waiting > 0
	}, 5*time.Second, 20*time.Millisecond)
	cancel()
	releaseOnce.Do(func() { close(release) })
	workers.Wait()
	require.ErrorIs(t, requestCtx.Err(), context.Canceled)
	first, second := <-responses, <-responses
	bulkLiveResult(t, first, 1, 0)
	bulkLiveResult(t, second, 1, 0)
	require.JSONEq(t, first.Body.String(), second.Body.String())
	require.True(t, fixture.initial.AddDate(0, 0, 7).Equal(fixture.expiry(0)))
}

func TestSubscriptionBulkReceiptBindsPayloadAfterMasterCleanup(t *testing.T) {
	fixture := newBulkLiveFixture(t)
	key := fixture.key()
	bulkLiveResult(t, fixture.request(context.Background(), fixture.body("extend", fixture.ids[:1], 7), key, fixture.token), 1, 0)
	_, err := fixture.db.Exec(`DELETE FROM idempotency_records WHERE scope='admin.subscriptions.bulk-action' AND idempotency_key_hash=$1`, service.HashIdempotencyKey(key))
	require.NoError(t, err)
	conflict := fixture.request(context.Background(), fixture.body("extend", fixture.ids[1:2], 7), key, fixture.token)
	require.Equal(t, 409, conflict.Code, conflict.Body.String())
	require.True(t, fixture.initial.Equal(fixture.expiry(1)), "different targets must not reuse a durable key")
}
