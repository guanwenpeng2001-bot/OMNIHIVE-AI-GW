# 数据管理接口移除说明（data-management）

适用范围：`/api/v1/admin/data-management/*` 管理接口及其前端 API 客户端。

## 移除了什么

`DataManagementService` 在移除前已经是一个"恒废弃"的占位实现：所有方法（配置读写、
Postgres/Redis/S3 profile 管理、备份任务创建/查询、S3 校验）都直接返回
`DATA_MANAGEMENT_DEPRECATED`（HTTP 503），只有 `GET /agent/health` 返回
`enabled=false` 的诊断信息，没有任何真实数据管理能力。因此本次清理删除的是这条
**只有错误返回、没有有效功能**的通路：

- 路由：`admin/registerDataManagementRoutes`（17 条路由注册，挂载在 `/api/v1/admin/data-management/...`，含 agent/health、config、sources profiles、s3 profiles、backups）；
- Handler：`internal/handler/admin/data_management_handler.go` 及其测试；
- Service / gRPC 契约类型：`internal/service/data_management_service.go`、`data_management_grpc.go` 及其测试；
- DI：`service.NewDataManagementService`、`handler.ProvideAdminHandlers` 的参数与
  `AdminHandlers.DataManagement` 字段（wire 生成物同步重生成）；
- 审计映射：`GET /api/v1/admin/data-management/s3/config` 的敏感读映射；
- 前端：`frontend/src/api/admin/dataManagement.ts`、`api/admin/index.ts` 的导出，以及
  只被它使用的中英 i18n 文案块 `admin.overview.dataManagement`。

## 对现有部署的影响

| 影响面 | 变化 |
| --- | --- |
| `GET /api/v1/admin/data-management/agent/health` | 由 200 + `enabled=false` 变为 404（路由不再注册） |
| 其余 `/api/v1/admin/data-management/*` | 由 503 `DATA_MANAGEMENT_DEPRECATED` 变为 404 |
| 管理后台面板 | 这些接口在前端已无任何调用方；不需要面板侧配置变更 |
| 宿主机 sidecar `datamanagementd` | 不再需要：网关从未真正调用它，健康检查恒为不可用 |
| 数据库/迁移 | 无 schema 变更，无需迁移；已存配额、备份、订单数据不受影响 |

自建脚本或监控若直接轮询 `data-management` 路径，请改为使用数据库备份接口
`/api/v1/admin/backups/*`（真实实现，未受影响）。

## 仍需后续处理（不属于源码清理范围）

- `deploy/DATAMANAGEMENTD_CN.md`、`deploy/install-datamanagementd.sh`、
  `deploy/sub2api-datamanagementd.service` 与 `deploy/README.md`、`README.md`、
  `README_CN.md` 中的 `datamanagementd` 说明仍然存在，归部署/文档分支清理。
