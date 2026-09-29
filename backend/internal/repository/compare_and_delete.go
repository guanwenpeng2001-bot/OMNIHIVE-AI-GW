package repository

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// A stale owner must not delete a lease that another owner has acquired.
var compareAndDeleteScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// compareAndDelete atomically deletes the full key only if its value is owner.
// Missing keys and ownership mismatches are successful no-ops.
func compareAndDelete(ctx context.Context, rdb *redis.Client, key, owner string) error {
	return compareAndDeleteScript.Run(ctx, rdb, []string{key}, owner).Err()
}
