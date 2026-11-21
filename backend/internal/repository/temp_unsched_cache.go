package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9"
)

const tempUnschedPrefix = "temp_unsched:account:"

const openAIAPIKeyHealthFailurePrefix = "openai_apikey_health:"

const (
	runtimeCooldownPrefix      = "runtime_cooldown:"
	runtimeCooldownProbePrefix = "runtime_cooldown_probe:"
)

// runtimeCooldownProbeRenewScript extends a probe lease only while the caller
// still owns it and only when the remaining TTL is running low. The return value
// reports ownership, so a probe whose lease already expired (and may have been
// taken over by another instance) cannot keep extending it forever.
var runtimeCooldownProbeRenewScript = redis.NewScript(`
	if redis.call('GET', KEYS[1]) ~= ARGV[1] then
		return 0
	end
	if redis.call('PTTL', KEYS[1]) < tonumber(ARGV[3]) then
		redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[2]))
	end
	return 1
`)

// runtimeCooldownProbeReleaseScript releases a probe lease only when the caller
// still owns it (compare-and-delete by owner token), so a previous holder whose
// lease already expired cannot delete the new owner's lease.
var runtimeCooldownProbeReleaseScript = redis.NewScript(`
	if redis.call('GET', KEYS[1]) == ARGV[1] then
		return redis.call('DEL', KEYS[1])
	end
	return 0
`)

var openAIAPIKeyHealthFailureScript = redis.NewScript(`
	local key = KEYS[1]
	local sequence_key = key .. ':sequence'
	local now = redis.call('TIME')
	local now_ms = (tonumber(now[1]) * 1000) + math.floor(tonumber(now[2]) / 1000)
	local window_ms = tonumber(ARGV[1]) * 60 * 1000
	local threshold = tonumber(ARGV[2])
	local sequence = redis.call('INCR', sequence_key)

	redis.call('ZREMRANGEBYSCORE', key, '-inf', now_ms - window_ms)
	redis.call('ZADD', key, now_ms, tostring(now_ms) .. ':' .. tostring(sequence))
	local count = redis.call('ZCARD', key)
	local ttl = math.max(60, (tonumber(ARGV[1]) + 1) * 60)
	redis.call('EXPIRE', key, ttl)
	redis.call('EXPIRE', sequence_key, ttl)
	if count >= threshold then
		redis.call('DEL', key, sequence_key)
		return {count, 1}
	end
	return {count, 0}
`)

var tempUnschedSetScript = redis.NewScript(`
	local key = KEYS[1]
	local new_until = tonumber(ARGV[1])
	local new_value = ARGV[2]
	local new_ttl = tonumber(ARGV[3])

	local existing = redis.call('GET', key)
	if existing then
		local ok, existing_data = pcall(cjson.decode, existing)
		if ok and existing_data and existing_data.until_unix then
			local existing_until = tonumber(existing_data.until_unix)
			if existing_until and new_until <= existing_until then
				return 0
			end
		end
	end

	redis.call('SET', key, new_value, 'EX', new_ttl)
	return 1
`)

type tempUnschedCache struct {
	rdb *redis.Client
}

func NewTempUnschedCache(rdb *redis.Client) service.TempUnschedCache {
	return &tempUnschedCache{rdb: rdb}
}

// SetTempUnsched 设置临时不可调度状态（只延长不缩短）
func (c *tempUnschedCache) SetTempUnsched(ctx context.Context, accountID int64, state *service.TempUnschedState) error {
	key := fmt.Sprintf("%s%d", tempUnschedPrefix, accountID)

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	ttl := time.Until(time.Unix(state.UntilUnix, 0))
	if ttl <= 0 {
		return nil // 已过期，不设置
	}

	ttlSeconds := int(ttl.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	_, err = tempUnschedSetScript.Run(ctx, c.rdb, []string{key}, state.UntilUnix, string(stateJSON), ttlSeconds).Result()
	return err
}

// GetTempUnsched 获取临时不可调度状态
func (c *tempUnschedCache) GetTempUnsched(ctx context.Context, accountID int64) (*service.TempUnschedState, error) {
	key := fmt.Sprintf("%s%d", tempUnschedPrefix, accountID)

	val, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var state service.TempUnschedState
	if err := json.Unmarshal([]byte(val), &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	return &state, nil
}

// DeleteTempUnsched 删除临时不可调度状态
func (c *tempUnschedCache) DeleteTempUnsched(ctx context.Context, accountID int64) error {
	key := fmt.Sprintf("%s%d", tempUnschedPrefix, accountID)
	return c.rdb.Del(ctx, key).Err()
}

func (c *tempUnschedCache) openAIAPIKeyHealthKey(accountID int64) string {
	// The hash tag keeps the rolling window and sequence key in one Redis
	// Cluster slot even though the Lua script derives the latter dynamically.
	return fmt.Sprintf("%s{%d}:failures", openAIAPIKeyHealthFailurePrefix, accountID)
}

func (c *tempUnschedCache) RecordOpenAIAPIKeyHealthFailure(ctx context.Context, accountID int64, windowMinutes, threshold int) (int64, bool, error) {
	if windowMinutes < 1 {
		windowMinutes = 1
	}
	if threshold < 1 {
		threshold = 1
	}
	result, err := openAIAPIKeyHealthFailureScript.Run(ctx, c.rdb, []string{c.openAIAPIKeyHealthKey(accountID) + ":" + service.AccountRuntimeCacheSuffix(ctx, accountID)}, windowMinutes, threshold).Slice()
	if err != nil {
		return 0, false, fmt.Errorf("record OpenAI API key health failure: %w", err)
	}
	if len(result) != 2 {
		return 0, false, fmt.Errorf("record OpenAI API key health failure: unexpected result length %d", len(result))
	}
	count, countOK := result[0].(int64)
	tripped, trippedOK := result[1].(int64)
	if !countOK || !trippedOK {
		return 0, false, fmt.Errorf("record OpenAI API key health failure: unexpected result types %T/%T", result[0], result[1])
	}
	return count, tripped == 1, nil
}

// Runtime cooldowns reuse the temporary scheduling Redis connection and TTLs.
// The stored value stays the cooldown deadline; the key itself outlives that
// deadline by service.RuntimeCooldownRecoveryWindow so the gateway can tell
// "still cooling down" apart from "cooldown expired, at most one instance probes".
func (cache *tempUnschedCache) GetRuntimeCooldown(ctx context.Context, key string) (time.Time, bool, error) {
	value, err := cache.rdb.Get(ctx, runtimeCooldownPrefix+key).Int64()
	if err == redis.Nil {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return time.UnixMilli(value), true, nil
}
func (cache *tempUnschedCache) SetRuntimeCooldown(ctx context.Context, key string, until time.Time) error {
	ttl := time.Until(until)
	if ttl <= 0 {
		return nil
	}
	return cache.rdb.Set(ctx, runtimeCooldownPrefix+key, until.UnixMilli(), ttl+service.RuntimeCooldownRecoveryWindow).Err()
}
func (cache *tempUnschedCache) DeleteRuntimeCooldown(ctx context.Context, key string) error {
	return cache.rdb.Del(ctx, runtimeCooldownPrefix+key).Err()
}

// AcquireRuntimeCooldownProbe is the cross-instance "single winner" primitive:
// SET NX plus TTL elects exactly one probing instance per recovery window.
func (cache *tempUnschedCache) AcquireRuntimeCooldownProbe(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, nil
	}
	return cache.rdb.SetNX(ctx, runtimeCooldownProbePrefix+key, owner, ttl).Result()
}

func (cache *tempUnschedCache) RenewRuntimeCooldownProbe(ctx context.Context, key, owner string, ttl, renewBelow time.Duration) (bool, error) {
	result, err := runtimeCooldownProbeRenewScript.Run(ctx, cache.rdb, []string{runtimeCooldownProbePrefix + key},
		owner, ttl.Milliseconds(), renewBelow.Milliseconds()).Int64()
	if err != nil {
		return false, fmt.Errorf("renew runtime cooldown probe: %w", err)
	}
	return result == 1, nil
}

func (cache *tempUnschedCache) ReleaseRuntimeCooldownProbe(ctx context.Context, key, owner string) error {
	return runtimeCooldownProbeReleaseScript.Run(ctx, cache.rdb, []string{runtimeCooldownProbePrefix + key}, owner).Err()
}

func (cache *tempUnschedCache) ResetRuntimeHealth(ctx context.Context, accountID int64) error {
	key := cache.openAIAPIKeyHealthKey(accountID) + ":" + service.AccountRuntimeCacheSuffix(ctx, accountID)
	return cache.rdb.Del(ctx, key, key+":sequence").Err()
}
