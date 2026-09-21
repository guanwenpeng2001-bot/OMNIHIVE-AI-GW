-- 为客户端显式提供的会话关联标识建索引，使运维/对账能按 session_id 定位一次会话的用量行，
-- 而不是整表扫描 usage_logs。
--
-- 形状选择依据真实数据 EXPLAIN：在 200 万行、堆 385 MB、26 个既有索引的 usage_logs 上，
-- 不带索引的 `WHERE session_id = $1 ORDER BY created_at DESC LIMIT 20` 需要 58k buffers / 约 800 ms
-- （反向扫 created_at 索引后逐行过滤）；部分 btree 索引后降到 43 buffers / 亚毫秒，
-- 且比 (session_id, created_at DESC) 复合索引小约 5 倍。带时间窗的会话查询仍走既有
-- created_at 索引，因此不额外付出复合索引的写放大。无会话标识的历史行不进索引。
--
-- CONCURRENTLY 让这张只追加的热表在构建期间保持可写；失败残留的 invalid 索引由
-- runner 的非事务索引重试逻辑先清理（见 migrations_runner.go 的 prepareNonTransactionalMigration）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_session_id
    ON usage_logs (session_id)
    WHERE session_id IS NOT NULL;
