package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type proxyGroupCache struct {
	rdb    *redis.Client
	prefix string
}

func NewProxyGroupCache(rdb *redis.Client) service.ProxyGroupCache {
	return &proxyGroupCache{rdb: rdb, prefix: "proxy_group:"}
}

func (c *proxyGroupCache) bindingKey(groupID, accountID int64) (string, error) {
	if groupID <= 0 || accountID <= 0 {
		return "", fmt.Errorf("binding requires positive group and account IDs")
	}
	return fmt.Sprintf("%sbind:{%d}:%d", c.prefix, groupID, accountID), nil
}

func bindingTTL(ttl time.Duration) (int64, error) {
	if ttl.Milliseconds() <= 0 {
		return 0, fmt.Errorf("TTL must be at least one millisecond")
	}
	return ttl.Milliseconds(), nil
}

func (c *proxyGroupCache) GetBinding(ctx context.Context, groupID, accountID int64) (int64, error) {
	key, err := c.bindingKey(groupID, accountID)
	if err != nil {
		return 0, err
	}
	value, err := c.rdb.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err == nil && value <= 0 {
		return 0, fmt.Errorf("invalid cached proxy binding")
	}
	return value, err
}

// GetBindingTTL returns zero for a missing binding; persistent keys are invalid.
func (c *proxyGroupCache) GetBindingTTL(ctx context.Context, groupID, accountID int64) (time.Duration, error) {
	key, err := c.bindingKey(groupID, accountID)
	if err != nil {
		return 0, err
	}
	ttl, err := c.rdb.PTTL(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if ttl == -2*time.Nanosecond {
		return 0, nil
	}
	if ttl < 0 {
		return 0, fmt.Errorf("proxy binding has no expiry")
	}
	return ttl, nil
}

var setProxyBindingNX = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current then return current end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2], 'NX')
return ARGV[1]
`)

func (c *proxyGroupCache) TrySetBindingNX(ctx context.Context, groupID, accountID, proxyID int64, ttl time.Duration) (int64, error) {
	key, err := c.bindingKey(groupID, accountID)
	if err != nil {
		return 0, err
	}
	if proxyID <= 0 {
		return 0, fmt.Errorf("proxy ID must be positive")
	}
	ms, err := bindingTTL(ttl)
	if err != nil {
		return 0, err
	}
	value, err := setProxyBindingNX.Run(ctx, c.rdb, []string{key}, proxyID, ms).Text()
	if err != nil {
		return 0, err
	}
	winner, err := strconv.ParseInt(value, 10, 64)
	if err == nil && winner <= 0 {
		return 0, fmt.Errorf("invalid cached proxy binding")
	}
	return winner, err
}

var touchProxyBinding = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
 return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0
`)
var deleteProxyBinding = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end
return 0
`)

func (c *proxyGroupCache) TouchBindingIfValue(ctx context.Context, groupID, accountID, proxyID int64, ttl time.Duration) (bool, error) {
	key, err := c.bindingKey(groupID, accountID)
	if err != nil {
		return false, err
	}
	if proxyID <= 0 {
		return false, fmt.Errorf("proxy ID must be positive")
	}
	ms, err := bindingTTL(ttl)
	if err != nil {
		return false, err
	}
	n, err := touchProxyBinding.Run(ctx, c.rdb, []string{key}, proxyID, ms).Int64()
	return n == 1, err
}

func (c *proxyGroupCache) DeleteBindingIfValue(ctx context.Context, groupID, accountID, proxyID int64) (bool, error) {
	key, err := c.bindingKey(groupID, accountID)
	if err != nil {
		return false, err
	}
	if proxyID <= 0 {
		return false, fmt.Errorf("proxy ID must be positive")
	}
	n, err := deleteProxyBinding.Run(ctx, c.rdb, []string{key}, proxyID).Int64()
	return n == 1, err
}

func (c *proxyGroupCache) cooldownKey(proxyID int64) string {
	return fmt.Sprintf("%s{cooldown}:%d", c.prefix, proxyID)
}
func (c *proxyGroupCache) cooledSetKey() string { return c.prefix + "{cooldown}:members" }

var setProxyCooldown = redis.NewScript(`
local inserted = redis.call('SET', KEYS[1], ARGV[1], 'NX', 'PX', ARGV[2])
redis.call('SADD', KEYS[2], ARGV[3])
if inserted then return 1 end
return 0
`)

func (c *proxyGroupCache) SetCooldownIfAbsent(ctx context.Context, cooldown service.ProxyCooldown, ttl time.Duration) (bool, error) {
	if cooldown.ProxyID <= 0 {
		return false, fmt.Errorf("proxy ID must be positive")
	}
	ms, err := bindingTTL(ttl)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	if cooldown.ObservedAt.IsZero() {
		cooldown.ObservedAt = now
	}
	cooldown.ExpiresAt = now.Add(ttl)
	data, err := json.Marshal(cooldown)
	if err != nil {
		return false, err
	}
	n, err := setProxyCooldown.Run(ctx, c.rdb, []string{c.cooldownKey(cooldown.ProxyID), c.cooledSetKey()}, data, ms, cooldown.ProxyID).Int64()
	return n == 1, err
}

// The read and expired-member removal share a script so cleanup cannot erase a
// newly inserted cooldown from the index between GET and SREM.
var getProxyCooldown = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if not value then redis.call('SREM', KEYS[2], ARGV[1]) end
return value
`)

func (c *proxyGroupCache) GetCooldown(ctx context.Context, proxyID int64) (*service.ProxyCooldown, error) {
	if proxyID <= 0 {
		return nil, fmt.Errorf("proxy ID must be positive")
	}
	value, err := getProxyCooldown.Run(ctx, c.rdb, []string{c.cooldownKey(proxyID), c.cooledSetKey()}, proxyID).Text()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cooldown service.ProxyCooldown
	if err := json.Unmarshal([]byte(value), &cooldown); err != nil {
		return nil, err
	}
	return &cooldown, nil
}

var clearProxyCooldown = redis.NewScript(`
redis.call('DEL', KEYS[1])
redis.call('SREM', KEYS[2], ARGV[1])
return 1
`)

func (c *proxyGroupCache) ClearCooldown(ctx context.Context, proxyID int64) error {
	if proxyID <= 0 {
		return fmt.Errorf("proxy ID must be positive")
	}
	return clearProxyCooldown.Run(ctx, c.rdb, []string{c.cooldownKey(proxyID), c.cooledSetKey()}, proxyID).Err()
}

func (c *proxyGroupCache) ListCooled(ctx context.Context) ([]service.ProxyCooldown, error) {
	ids, err := c.rdb.SMembers(ctx, c.cooledSetKey()).Result()
	if err != nil {
		return nil, err
	}
	out := make([]service.ProxyCooldown, 0, len(ids))
	for _, raw := range ids {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, err
		}
		value, err := c.GetCooldown(ctx, id)
		if err != nil {
			return nil, err
		}
		if value != nil {
			out = append(out, *value)
		}
	}
	return out, nil
}
