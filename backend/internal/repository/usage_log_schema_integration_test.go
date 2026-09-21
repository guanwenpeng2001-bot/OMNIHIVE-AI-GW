//go:build integration

package repository

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUsageLogInsertColumnsExistInMigratedSchema 断言手写 INSERT 与实际迁移出的 usage_logs 表完全对齐：
// 每个 INSERT 列都必须存在于真实表（否则生产启动后第一次写入就会失败），
// 反过来真实表也不允许有 INSERT 未覆盖的列（唯一例外是自增主键 id）。
func TestUsageLogInsertColumnsExistInMigratedSchema(t *testing.T) {
	tx := testTx(t)
	insertColumns := captureUsageLogInsertColumns(t)
	require.NotEmpty(t, insertColumns)

	rows, err := tx.QueryContext(context.Background(), `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = 'usage_logs'
`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var migrated []string
	for rows.Next() {
		var column string
		require.NoError(t, rows.Scan(&column))
		migrated = append(migrated, column)
	}
	require.NoError(t, rows.Err())
	sort.Strings(migrated)
	require.NotEmpty(t, migrated)

	require.Empty(t, usageLogColumnsMissingFrom(migrated, insertColumns),
		"手写 INSERT 引用了迁移尚未创建的列：新增列必须先落 backend/migrations/*.sql")
	require.Equal(t, []string{"id"}, usageLogColumnsMissingFrom(insertColumns, migrated),
		"usage_logs 除自增主键 id 外，不应存在 INSERT 未覆盖的列")
}

// TestUsageLogSessionIDIndexIsValidPartialBtree 断言 session_id 查询索引真实存在、有效，且形状与迁移声明一致。
func TestUsageLogSessionIDIndexIsValidPartialBtree(t *testing.T) {
	tx := testTx(t)

	var (
		definition string
		valid      bool
		ready      bool
	)
	require.NoError(t, tx.QueryRowContext(context.Background(), `
SELECT pg_get_indexdef(i.indexrelid), i.indisvalid, i.indisready
FROM pg_class idx
JOIN pg_index i ON i.indexrelid = idx.oid
JOIN pg_class tbl ON tbl.oid = i.indrelid
JOIN pg_namespace ns ON ns.oid = tbl.relnamespace
WHERE ns.nspname = 'public'
  AND tbl.relname = 'usage_logs'
  AND idx.relname = $1
`, usageLogsSessionIDIndex).Scan(&definition, &valid, &ready))
	require.True(t, valid, "%s 必须是有效索引（CONCURRENTLY 构建失败会留下 invalid 索引）", usageLogsSessionIDIndex)
	require.True(t, ready, "%s 必须已就绪", usageLogsSessionIDIndex)
	require.Contains(t, definition, "USING btree (session_id)")
	require.Contains(t, definition, "WHERE (session_id IS NOT NULL)",
		"索引必须是 session_id IS NOT NULL 的部分索引，避免把大量 NULL 历史行写进索引")
}

// TestUsageLogSessionIDIndexServesSessionLookups 用 EXPLAIN 证明按会话查询真的能走该索引。
// 小表上规划器默认仍可能选顺序扫描，因此显式关闭 seqscan 只验证索引可用性；
// 生产规模（200 万行）的选择依据见 273 迁移文件注释与交付报告里的 EXPLAIN 记录。
func TestUsageLogSessionIDIndexServesSessionLookups(t *testing.T) {
	tx := testTx(t)
	_, err := tx.ExecContext(context.Background(), "SET LOCAL enable_seqscan = off")
	require.NoError(t, err)

	rows, err := tx.QueryContext(context.Background(),
		"EXPLAIN SELECT id, created_at FROM usage_logs WHERE session_id = $1 ORDER BY created_at DESC LIMIT 20",
		"sess-plan-probe")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
	}
	require.NoError(t, rows.Err())
	joined := strings.Join(plan, "\n")
	require.Contains(t, joined, usageLogsSessionIDIndex,
		"按 session_id 的查询必须能用上 %s，实际计划：\n%s", usageLogsSessionIDIndex, joined)
}

// TestUsageLogSessionIDIndexCoversEveryLeafRelation 断言每个真正存行的关系（未分区时就是 usage_logs 本身，
// 手工分区后则是所有分区）都有以 session_id 为前导列的可用索引。
// 父表建索引会自动附加到已有分区，因此这条断言在两种部署形态下都成立。
func TestUsageLogSessionIDIndexCoversEveryLeafRelation(t *testing.T) {
	tx := testTx(t)

	var leafCount, coveredCount int
	require.NoError(t, tx.QueryRowContext(context.Background(), `
WITH leaves AS (
	SELECT relid FROM pg_partition_tree('public.usage_logs'::regclass) WHERE isleaf
	UNION ALL
	-- pg_partition_tree 对未分区关系不返回任何行，此时关系本身就是唯一的“存行关系”。
	SELECT 'public.usage_logs'::regclass WHERE NOT EXISTS (
		SELECT 1 FROM pg_partitioned_table WHERE partrelid = 'public.usage_logs'::regclass
	)
), indexes AS (
	SELECT i.indrelid, i.indisvalid, i.indisready, pg_get_indexdef(i.indexrelid) AS definition
	FROM pg_index i
)
SELECT
	(SELECT count(*) FROM leaves),
	(SELECT count(*) FROM leaves l WHERE EXISTS (
		SELECT 1 FROM indexes x
		WHERE x.indrelid = l.relid
		  AND x.indisvalid AND x.indisready
		  AND x.definition LIKE '% USING btree (session_id%'
	))
`).Scan(&leafCount, &coveredCount))
	require.Greater(t, leafCount, 0, "usage_logs 必须至少有一个存行关系")
	require.Equal(t, leafCount, coveredCount,
		"usage_logs 的每个分区都必须有以 session_id 为前导列的可用索引；手工分区部署需按分区补齐该索引")
}
