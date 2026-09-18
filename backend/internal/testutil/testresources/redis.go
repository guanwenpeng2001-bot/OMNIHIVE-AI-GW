package testresources

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisClient scopes keys and cleanup to this invocation, reusing the repository
// integration harness key rewriting mechanism.
func RedisClient(test *testing.T, options *redis.Options) *redis.Client {
	return RedisClientWithNamespace(test, options, "test:isolated-resource-adaptation:"+uuid.NewString()+":")
}

// RedisClientWithNamespace lets child processes share the parent's fixture.
// Only the parent registers cleanup; children attach RedisNamespaceHook directly.
func RedisClientWithNamespace(test *testing.T, options *redis.Options, prefix string) *redis.Client {
	test.Helper()
	if External() {
		if err := ValidateRedis(options.Addr); err != nil {
			test.Fatal(err)
		}
	}
	raw := redis.NewClient(options)
	client := redis.NewClient(options)
	client.AddHook(RedisNamespaceHook(prefix))
	test.Cleanup(func() {
		defer func() { _ = raw.Close() }()
		defer func() { _ = client.Close() }()
		if err := ClearRedisNamespace(context.Background(), raw, prefix); err != nil {
			test.Error(err)
		}
	})
	return client
}

func validRedisNamespace(prefix string) bool {
	identifier := strings.TrimSuffix(strings.TrimPrefix(prefix, "test:isolated-resource-adaptation:"), ":")
	_, err := uuid.Parse(identifier)
	return err == nil && prefix == "test:isolated-resource-adaptation:"+identifier+":"
}

func RedisNamespaceHook(prefix string) redis.Hook {
	if !validRedisNamespace(prefix) {
		panic("invalid test Redis namespace")
	}
	return namespaceHook{prefix: prefix, strict: External()}
}

// ClearRedisNamespace simulates cache loss without flushing neighbors' keys.
func ClearRedisNamespace(ctx context.Context, raw *redis.Client, prefix string) error {
	if External() {
		if err := ValidateRedis(raw.Options().Addr); err != nil {
			return err
		}
	}
	if !validRedisNamespace(prefix) {
		return fmt.Errorf("invalid test Redis cleanup namespace")
	}
	var cursor uint64
	for {
		keys, next, err := raw.Scan(ctx, cursor, prefix+"*", 500).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := raw.Unlink(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

type namespaceHook struct {
	prefix string
	strict bool
}

func (h namespaceHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h namespaceHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.prefixCmd(cmd); err != nil {
			return err
		}
		return next(ctx, cmd)
	}
}

func (h namespaceHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.prefixCmd(cmd); err != nil {
				return err
			}
		}
		return next(ctx, cmds)
	}
}

func (h namespaceHook) prefixCmd(cmd redis.Cmder) error {
	args := cmd.Args()
	command := strings.ToLower(cmd.Name())
	if command == "flushdb" || command == "flushall" {
		if h.strict {
			return fmt.Errorf("global Redis cleanup forbidden in test namespace")
		}
		return nil
	}
	switch command {
	case "hello", "auth", "ping", "quit":
		return nil
	case "client":
		if len(args) > 1 && (fmt.Sprint(args[1]) == "setinfo" || fmt.Sprint(args[1]) == "setname" || fmt.Sprint(args[1]) == "maint_notifications") {
			return nil
		}
	case "script":
		if len(args) > 1 && (fmt.Sprint(args[1]) == "load" || fmt.Sprint(args[1]) == "exists") {
			return nil
		}
	}

	prefixOne := func(i int) {
		if i < 0 || i >= len(args) {
			return
		}

		switch v := args[i].(type) {
		case string:
			if v != "" && !strings.HasPrefix(v, h.prefix) {
				args[i] = h.prefix + v
			}
		case []byte:
			s := string(v)
			if s != "" && !strings.HasPrefix(s, h.prefix) {
				args[i] = []byte(h.prefix + s)
			}
		}
	}

	switch command {
	case "get", "set", "setnx", "setex", "psetex", "incr", "decr", "incrby", "expire", "pexpire", "ttl", "pttl",
		"hgetall", "hget", "hset", "hdel", "hincrbyfloat", "hscan", "hmget", "hincrby",
		"sadd", "srem", "smembers", "scard", "sismember", "sscan",
		"zadd", "zcard", "zrange", "zrangebyscore", "zrem", "zremrangebyscore", "zrevrange", "zrevrangebyscore", "zscore":
		prefixOne(1)
	case "mget", "exists":
		for i := 1; i < len(args); i++ {
			prefixOne(i)
		}
	case "del", "unlink":
		for i := 1; i < len(args); i++ {
			prefixOne(i)
		}
	case "eval", "evalsha", "eval_ro", "evalsha_ro":
		if len(args) < 3 {
			return nil
		}
		numKeys, err := strconv.Atoi(fmt.Sprint(args[2]))
		if err != nil || numKeys <= 0 {
			return nil
		}
		for i := 0; i < numKeys && 3+i < len(args); i++ {
			prefixOne(3 + i)
		}
	case "scan":
		matched := false
		for i := 2; i+1 < len(args); i++ {
			if strings.EqualFold(fmt.Sprint(args[i]), "match") {
				prefixOne(i + 1)
				matched = true
				break
			}
		}
		if !matched && h.strict {
			return fmt.Errorf("test namespace SCAN requires a match pattern")
		}
	default:
		if h.strict {
			return fmt.Errorf("redis command %s has no test namespace key mapping", command)
		}
	}
	return nil
}
