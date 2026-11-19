# WP7B 运维页面集成

| 路径 | 入口 | 路由集成 |
| --- | --- | --- |
| `/admin/audit` | `frontend/src/features/operations/ScopedAuditView.vue` | 在管理端注册 `AdminScopedAudit`，设置 `requiresAuth: true` 和 `requiresAdmin: true`，同步导航注册表。已有 `/admin/audit-logs` 是 v1 全局审计，保留其入口可支持旧链接。 |
| `/admin/backup` | `frontend/src/views/admin/BackupView.vue` | 现有 `AdminBackup` 入口。 |
| `/admin/ops` | `frontend/src/views/admin/ops/OpsDashboard.vue` | 现有 `AdminOps` 入口。 |
| `/profile` | `frontend/src/views/user/ProfileView.vue` | 现有 `Profile` 入口。 |

## 接口与验收

- 项目审计使用当前工作区和项目的 BE‑21 列表、详情和导出接口。时间范围为本地输入转换为 UTC 后以 `[from,to)` 查询，筛选变更会重置游标。导出重试沿用原幂等键。HTTP 410 会提示期限并允许重新申请。切换项目时清空详情和分页。
- 备份清单、归档设置和下载仍走 v1。恢复从已完成的备份发起 BE‑09 预览，确认后执行，并按操作编号轮询。生产恢复需要同一备份和摘要的成功隔离演练以及 TOTP 授权。成功状态要求后端同时返回 `succeeded`、`verified` 和报告。页面可按编号查询，也会在当前浏览器标签页内保留最近的操作编号或待确认的幂等请求。
- 运维独立读取 `/health`、`/ready`、`/api/v1/admin/ops/concurrency` 和 `/api/v1/admin/ops/realtime-traffic?window=5min`。健康区分进程存活和接收流量的就绪状态。各卡片独立刷新并区分关闭、空数据和读失败。
- 个人资料沿用现有改密和 TOTP 启停接口。TOTP 注册只显示认证器二维码。当前浏览器通过 `GET /api/v1/auth/me` 验证，使用 `POST /api/v1/auth/revoke-all-sessions` 撤销账号的所有会话。后端需要另行提供会话目录和按会话撤销，才能展示其他设备和支持单独撤销。
