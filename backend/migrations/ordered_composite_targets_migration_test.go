package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrderedCompositeTargetsMigrationIsAdditiveAndIdempotent(t *testing.T) {
	content, err := FS.ReadFile("273_add_ordered_composite_targets.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")

	// 只加不减：新列必须 IF NOT EXISTS，且不得删除/重命名既有单目标列。
	require.Contains(t, sql, "ALTER TABLE composite_model_routes ADD COLUMN IF NOT EXISTS ordered_targets JSONB")
	require.NotContains(t, sql, "DROP COLUMN")
	require.NotContains(t, sql, "RENAME COLUMN")

	// 幂等回填：只处理尚未写入有序目标的行。
	require.Contains(t, sql, "WHERE ordered_targets IS NULL")

	// 单目标等效迁移：一个旧路由迁移成一个启用目标，权重沿用旧等效行为。
	require.Contains(t, sql, "'position', 0")
	require.Contains(t, sql, "'target_platform', target_platform")
	require.Contains(t, sql, "'scheduling_weight', 100")
	require.Contains(t, sql, "'latency_preference', 'default'")
	require.Contains(t, sql, "'enabled', true")
}

func TestOrderedCompositeTargetsEntProjectionMatchesMigration(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join("..", "ent", "migrate", "schema.go"))
	require.NoError(t, err)
	require.Contains(t, string(schema), "ordered_targets",
		"ent migrate projection must expose the ordered_targets column added by migration 273")
}
