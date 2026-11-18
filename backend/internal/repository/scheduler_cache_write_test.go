package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSchedulerCacheAccountWriteContract(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	testSchedulerAccountWriteContract(t, rdb)

	t.Run("elapsed TTL", func(t *testing.T) {
		ctx := context.Background()
		cache := NewSchedulerCache(rdb)
		account := schedulerWriteTestAccount(9805, "expired")
		keys, payloads := schedulerWriteTestPayloads(t, account)
		require.NoError(t, cache.SetAccount(ctx, &account))
		for _, key := range keys {
			require.NoError(t, rdb.PExpire(ctx, key, time.Second).Err())
		}
		mr.FastForward(2 * time.Second)
		require.NoError(t, cache.SetAccount(ctx, &account))
		for i, key := range keys {
			require.Equal(t, payloads[i], rdb.Get(ctx, key).Val())
			require.Equal(t, time.Duration(-1), rdb.PTTL(ctx, key).Val())
		}
	})
}

// Also run against an isolated real Redis under the redis_integration tag.
func testSchedulerAccountWriteContract(t *testing.T, rdb *redis.Client) {
	ctx := context.Background()
	newCache := func(t *testing.T) *schedulerCache {
		t.Helper()
		// Separate pools model independent application instances, without any
		// process-local knowledge of the other writer's last payload.
		options := rdb.Options()
		client := redis.NewClient(&redis.Options{
			Addr: options.Addr, Network: options.Network, DB: options.DB,
			DialTimeout: options.DialTimeout, ReadTimeout: options.ReadTimeout,
			WriteTimeout: options.WriteTimeout, MaxRetries: -1,
		})
		t.Cleanup(func() { _ = client.Close() })
		cache, ok := NewSchedulerCache(client).(*schedulerCache)
		if !ok {
			t.Fatal("unexpected scheduler cache implementation")
		}
		return cache
	}

	t.Run("legacy JSON and independent payload changes", func(t *testing.T) {
		cache := newCache(t)
		account := schedulerWriteTestAccount(9801, "original")
		keys, payloads := schedulerWriteTestPayloads(t, account)
		// Seed exactly as the old, unconditional writer did, with no digest.
		for i, key := range keys {
			require.NoError(t, rdb.Set(ctx, key, payloads[i], 0).Err())
		}
		assertSchedulerKeysUnchanged(t, rdb, keys, func() {
			require.NoError(t, cache.SetAccount(ctx, &account))
		})

		// An older full Account in sched:meta remains readable and is replaced
		// by the existing slim projection without rewriting sched:acc.
		require.NoError(t, rdb.Set(ctx, keys[1], payloads[0], 0).Err())
		legacy, err := decodeCachedAccount(rdb.Get(ctx, keys[1]).Val())
		require.NoError(t, err)
		require.Equal(t, account.GroupIDs, legacy.GroupIDs)
		assertSchedulerKeysUnchanged(t, rdb, keys[:1], func() {
			require.NoError(t, cache.SetAccount(ctx, &account))
		})
		require.Equal(t, payloads[1], rdb.Get(ctx, keys[1]).Val())

		// Rotating a secret changes only the full payload.
		account.Credentials["access_token"] = "rotated-secret"
		assertSchedulerKeysUnchanged(t, rdb, keys[1:], func() {
			require.NoError(t, cache.SetAccount(ctx, &account))
		})
		full, err := cache.GetAccount(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, "rotated-secret", full.GetCredential("access_token"))
		require.Equal(t, account.Groups, full.Groups)
		require.Equal(t, account.AccountGroups, full.AccountGroups)

		// Scheduling, tenancy, membership and model selection changes must
		// still propagate through both cache representations.
		account.WorkspaceID++
		account.Schedulable = false
		account.Status = "disabled"
		account.GroupIDs = []int64{42}
		account.AccountGroups[0].GroupID = 42
		account.AccountGroups[0].Priority = 99
		account.Credentials["model_mapping"] = map[string]any{"new-model": "new-model"}
		require.NoError(t, cache.SetAccount(ctx, &account))
		_, changed := schedulerWriteTestPayloads(t, account)
		for i, key := range keys {
			require.Equal(t, changed[i], rdb.Get(ctx, key).Val())
		}

		// An intervening legacy SET must be observed even when this instance
		// last wrote the candidate successfully (A -> B -> A).
		for i, key := range keys {
			require.NoError(t, rdb.Set(ctx, key, payloads[i], 0).Err())
		}
		require.NoError(t, cache.SetAccount(ctx, &account))
		for i, key := range keys {
			require.Equal(t, changed[i], rdb.Get(ctx, key).Val())
		}
	})

	t.Run("TTL missing expired and malformed values", func(t *testing.T) {
		cases := []struct {
			name   string
			states [2]string
		}{
			{"persistent", [2]string{"equal", "equal"}},
			{"equal TTL", [2]string{"ttl", "ttl"}},
			{"full TTL", [2]string{"ttl", "equal"}},
			{"meta TTL", [2]string{"equal", "ttl"}},
			{"changed TTL", [2]string{"different", "different"}},
			{"missing full", [2]string{"missing", "equal"}},
			{"missing meta", [2]string{"equal", "missing"}},
			{"missing both", [2]string{"missing", "missing"}},
			{"expired full", [2]string{"expired", "equal"}},
			{"expired meta", [2]string{"equal", "expired"}},
			{"expired both", [2]string{"expired", "expired"}},
			{"wrong types", [2]string{"hash", "list"}},
		}
		for n, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cache := newCache(t)
				account := schedulerWriteTestAccount(int64(9900+n), "recovered")
				keys, payloads := schedulerWriteTestPayloads(t, account)
				for i, state := range tc.states {
					key := keys[i]
					switch state {
					case "equal":
						require.NoError(t, rdb.Set(ctx, key, payloads[i], 0).Err())
					case "ttl", "expired":
						require.NoError(t, rdb.Set(ctx, key, payloads[i], time.Minute).Err())
						if state == "expired" {
							require.NoError(t, rdb.PExpireAt(ctx, key, time.Now().Add(-time.Hour)).Err())
							require.Zero(t, rdb.Exists(ctx, key).Val())
						}
					case "different":
						require.NoError(t, rdb.Set(ctx, key, "invalid old JSON", time.Minute).Err())
					case "hash":
						require.NoError(t, rdb.HSet(ctx, key, "old", "value").Err())
					case "list":
						require.NoError(t, rdb.RPush(ctx, key, "old value").Err())
					case "missing":
						require.Zero(t, rdb.Exists(ctx, key).Val())
					}
				}
				require.NoError(t, cache.SetAccount(ctx, &account))
				for i, key := range keys {
					require.Equal(t, payloads[i], rdb.Get(ctx, key).Val())
					require.Equal(t, time.Duration(-1), rdb.PTTL(ctx, key).Val(), "SET(..., 0) clears TTL")
				}
				assertSchedulerKeysUnchanged(t, rdb, keys, func() {
					require.NoError(t, newCache(t).SetAccount(ctx, &account))
				})
			})
		}
	})

	t.Run("snapshot invalidation last used and script flush", func(t *testing.T) {
		cache, other := newCache(t), newCache(t)
		cache.writeChunkSize = 1 // Exercise multiple pipeline flushes and duplicate IDs.
		account := schedulerWriteTestAccount(9802, "snapshot")
		keys, _ := schedulerWriteTestPayloads(t, account)
		sideKey := schedulerLastUsedKey(strconv.FormatInt(account.ID, 10))
		bucket := service.SchedulerBucket{GroupID: 7, Platform: account.Platform, Mode: service.SchedulerModeSingle}
		token, err := cache.CaptureBucketWriteToken(ctx, bucket)
		require.NoError(t, err)
		publish := func() {
			require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []service.Account{account, account}))
		}
		publish()
		latest := time.Date(2026, 11, 18, 1, 2, 3, 0, time.UTC)
		require.NoError(t, other.UpdateLastUsed(ctx, map[int64]time.Time{account.ID: latest}))
		assertSchedulerKeysUnchanged(t, rdb, append(keys, sideKey), func() {
			publish()
			require.NoError(t, rdb.ScriptFlush(ctx).Err())
			require.NoError(t, other.SetAccount(ctx, &account))
		})
		snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
		require.NoError(t, err)
		require.True(t, hit)
		require.Len(t, snapshot, 1)
		require.Equal(t, latest, *snapshot[0].LastUsedAt)

		require.NoError(t, other.DeleteAccount(ctx, account.ID))
		_, hit, err = cache.GetSnapshot(ctx, bucket)
		require.NoError(t, err)
		require.False(t, hit)
		full, err := cache.GetAccount(ctx, account.ID)
		require.NoError(t, err)
		require.Nil(t, full)
		require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{account.ID: latest}))
		require.NoError(t, cache.SetSnapshotByAccountIDs(ctx, bucket, token, []int64{account.ID}))
		require.Zero(t, rdb.Exists(ctx, append(keys, sideKey)...).Val(), "ID-only reuse cannot resurrect deleted data")
		// Same bytes after deletion must repopulate, without a stale local hit.
		require.NoError(t, cache.SetAccount(ctx, &account))
		require.EqualValues(t, 2, rdb.Exists(ctx, keys...).Val())
		require.Zero(t, rdb.Exists(ctx, sideKey).Val())

		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		account.Name = "must not be published"
		assertSchedulerKeysUnchanged(t, rdb, keys, func() {
			require.ErrorIs(t, cache.SetAccount(cancelled, &account), context.Canceled)
		})
		account.ExpiresAt = ptrTime(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
		require.NoError(t, cache.SetAccount(ctx, &account))
		require.Zero(t, rdb.Exists(ctx, append(keys, sideKey)...).Val(), "unencodable SetAccount still invalidates")
	})

	t.Run("concurrent instances and deletion", func(t *testing.T) {
		first := schedulerWriteTestAccount(9803, "first")
		second := schedulerWriteTestAccount(9803, "second")
		keys, firstPayload := schedulerWriteTestPayloads(t, first)
		_, secondPayload := schedulerWriteTestPayloads(t, second)
		const writers = 8
		caches := make([]*schedulerCache, writers)
		for i := range caches {
			caches[i] = newCache(t)
		}
		require.NoError(t, caches[0].SetAccount(ctx, &first))
		runWriters := func(diverge bool) {
			start := make(chan struct{})
			errors := make(chan error, writers)
			var wg sync.WaitGroup
			for i, cache := range caches {
				wg.Add(1)
				go func(i int, cache *schedulerCache) {
					defer wg.Done()
					<-start
					account := first
					if diverge && i%2 == 1 {
						account = second
					}
					for j := 0; j < 20; j++ {
						if err := cache.SetAccount(ctx, &account); err != nil {
							errors <- err
							return
						}
					}
				}(i, cache)
			}
			close(start)
			// MGET observes a single Redis instant, unlike two separate GETs.
			for i := 0; i < 100; i++ {
				values, err := rdb.MGet(ctx, keys...).Result()
				require.NoError(t, err)
				isFirst := values[0] == firstPayload[0] && values[1] == firstPayload[1]
				isSecond := values[0] == secondPayload[0] && values[1] == secondPayload[1]
				require.True(t, isFirst || isSecond, "full/meta must belong to the same writer")
			}
			wg.Wait()
			close(errors)
			for err := range errors {
				require.NoError(t, err)
			}
		}
		assertSchedulerKeysUnchanged(t, rdb, keys, func() { runWriters(false) })
		runWriters(true)

		sideKey := schedulerLastUsedKey(strconv.FormatInt(first.ID, 10))
		for i := 0; i < 30; i++ {
			require.NoError(t, caches[0].SetAccount(ctx, &first))
			require.NoError(t, caches[0].UpdateLastUsed(ctx, map[int64]time.Time{first.ID: time.Now()}))
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() { <-start; results <- caches[0].SetAccount(ctx, &second) }()
			go func() { <-start; results <- caches[1].DeleteAccount(ctx, first.ID) }()
			close(start)
			require.NoError(t, <-results)
			require.NoError(t, <-results)
			values, err := rdb.MGet(ctx, append(keys, sideKey)...).Result()
			require.NoError(t, err)
			deleted := values[0] == nil && values[1] == nil
			republished := values[0] == secondPayload[0] && values[1] == secondPayload[1]
			require.True(t, deleted || republished, "DEL and account write must serialize without a half-pair")
			require.Nil(t, values[2], "account writes must never restore a deleted LastUsed side key")
		}
	})
}

func schedulerWriteTestAccount(id int64, name string) service.Account {
	group := &service.Group{ID: 7, Name: "retained-group"}
	return service.Account{
		ID: id, Name: name, WorkspaceID: 3,
		Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "secret-" + name, "model_mapping": map[string]any{"model": "model"}},
		Extra:       map[string]any{"base_rpm": 10, "rpm_strategy": "tiered"},
		GroupIDs:    []int64{7}, Groups: []*service.Group{group},
		AccountGroups: []service.AccountGroup{{AccountID: id, GroupID: 7, Priority: 4, Group: group}},
	}
}

func schedulerWriteTestPayloads(t *testing.T, account service.Account) ([]string, []string) {
	t.Helper()
	id := strconv.FormatInt(account.ID, 10)
	full, err := json.Marshal(account)
	require.NoError(t, err)
	meta, err := json.Marshal(buildSchedulerMetadataAccount(account))
	require.NoError(t, err)
	return []string{schedulerAccountKey(id), schedulerAccountMetaKey(id)}, []string{string(full), string(meta)}
}

// Comparing GET results alone would also pass for the old unconditional SET.
// WATCH aborts even for SET of identical bytes, so a successful read-only EXEC
// proves no watched key was modified by the action.
func assertSchedulerKeysUnchanged(t *testing.T, rdb *redis.Client, keys []string, action func()) {
	t.Helper()
	ctx := context.Background()
	err := rdb.Watch(ctx, func(tx *redis.Tx) error {
		action()
		_, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.MGet(ctx, keys...)
			return nil
		})
		return err
	}, keys...)
	require.NoError(t, err, "unexpected write to %v", keys)
}
