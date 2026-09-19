package testresources

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisNamespaceHookKeylessAndIsolation(t *testing.T) {
	const prefix = "test:isolated-resource-adaptation:11111111-1111-4111-8111-111111111111:"
	const neighbor = "test:isolated-resource-adaptation:22222222-2222-4222-8222-222222222222:key"
	hook := namespaceHook{prefix: prefix, strict: true}
	ctx := context.Background()
	for _, command := range []string{"time", "multi", "exec", "discard", "unwatch"} {
		t.Run("keyless "+command+" reaches transport unchanged", func(t *testing.T) {
			cmd := redis.NewCmd(ctx, command)
			calls := 0
			err := hook.ProcessHook(func(_ context.Context, got redis.Cmder) error {
				calls++
				require.Equal(t, []any{command}, got.Args())
				return nil
			})(ctx, cmd)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
		})
	}
	t.Run("mapped keys cannot access neighboring namespace", func(t *testing.T) {
		cmd := redis.NewCmd(ctx, "get", neighbor)
		require.NoError(t, hook.prefixCmd(cmd))
		require.Equal(t, prefix+neighbor, cmd.Args()[1])
	})
	for _, command := range []struct {
		name string
		args []any
	}{
		{"rename", []any{"rename", prefix + "key", neighbor}},
		{"copy", []any{"copy", prefix + "key", neighbor}},
		{"scan", []any{"scan", 0}},
		{"flushdb", []any{"flushdb"}},
		{"flushall", []any{"flushall"}},
	} {
		t.Run(command.name+" rejected before transport", func(t *testing.T) {
			calls := 0
			cmd := redis.NewCmd(ctx, command.args...)
			err := hook.ProcessHook(func(context.Context, redis.Cmder) error { calls++; return nil })(ctx, cmd)
			require.Error(t, err)
			require.Zero(t, calls)
			err = hook.ProcessPipelineHook(func(context.Context, []redis.Cmder) error { calls++; return nil })(ctx, []redis.Cmder{redis.NewCmd(ctx, "multi"), redis.NewCmd(ctx, "time"), cmd, redis.NewCmd(ctx, "exec")})
			require.Error(t, err)
			require.Zero(t, calls)
		})
	}
}
