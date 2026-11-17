# Cumora 管理员代用户创建 API Key

`POST /api/v1/admin/users/:id/api-keys` 对应 Cumora `server/src/sub2api.ts`
中的 `provisionUser` 第三步。沿用管理员路由组的认证、面板限流、审计和合规确认，
支持已配置的管理员 `x-api-key` 或有效管理员 JWT；普通用户 JWT、用户 API Key
及仅有 workspace 管理权限的用户无权调用。无需登录为目标用户。

## 请求与响应

路径 `id` 为目标用户的正整数 ID。JSON 只接受以下字段：

```json
{"name":"cumora · Alice","group_id":7}
```

`name` 必须为非空白字符串。`group_id` 必须为正整数，属于默认 workspace，且分组启用。
成功返回 HTTP 200，格式与 Cumora 的 `adminFetch<ApiKeyResponse>` 精确对应：

```json
{"code":0,"message":"success","data":{"id":123,"key":"<本次生成的完整密钥>"}}
```

`data` 只有 `id` 和 `key`，不是脱敏管理 DTO，也不是用户端的
`{api_key, secret}` 结构。响应带 `Cache-Control: no-store`。

密钥归属路径指定的目标用户。创建调用现有 `APIKeyService.Create`，保留随机生成、
事务提交、SHA-256 凭证摘要和缓存失效机制；数据库不保存明文，后续详情、列表仍只返回
脱敏数据，审计不保存响应密钥。共用创建服务会拒绝未启用的用户和分组。

## 权限和计费边界

- 目标用户必须存在且启用；不存在或已软删除返回 404，未启用返回 403。
- 普通分组遵循目标用户的 `AllowedGroups`、`RestrictPublicGroups` 和专属分组规则。
  管理员身份不会替目标用户取得分组权限，也不会自动添加授权。
- 订阅分组必须已有有效订阅。单纯在 `allowed_groups` 中列出该组不能代替订阅；
  无有效订阅返回 403 `GROUP_NOT_ALLOWED`。Cumora 应先完成订阅分配。
- 默认 workspace 外的分组不可通过本接口绑定；查询在明确的默认 workspace scope 下执行，
  外部或不存在的分组返回 404 `GROUP_NOT_FOUND`。未启用分组返回 400 `GROUP_NOT_ACTIVE`。
- 创建密钥不充值、不赋予订阅、不重置用量，也不保证密钥可调用模型。零余额用户可以先创建
  密钥，但网关继续执行现有余额、订阅限额、用户/分组状态及其他安全检查。已有 simple mode
  的部署策略由原网关配置决定，本接口不修改它。

错误沿用 `{code, message, reason, metadata}` envelope，`code` 非零且 HTTP 状态非成功。
缺失认证返回 401，普通用户 JWT 返回 403，未完成管理员合规确认仍返回 423。

## 明确不支持

- 未配置 Cumora tier 的 `group_id: null`、缺省、0 或负数：返回 400
  `API_KEY_GROUP_REQUIRED`。Cumora 当前允许 `groupId = 0` 的分支不能在此仓库成功发钥，
  需配置有效的默认 workspace tier 分组。不能以创建无分组密钥绕过该约束。
- workspace 选择或跨 workspace 代发；没有
  `/api/v1/workspaces/:workspace_id/users/:id/api-keys` 接口。
- 自定义 secret、`user_id` / `workspace_id` 覆盖、配额/预算/余额/订阅覆盖及其他扩展字段。
  未知 JSON 字段和多个 JSON 对象均返回 400，不会静默生效。
- 明文查询、恢复和响应重放。本接口不使用幂等响应存储，`Idempotency-Key` 不提供去重；
  每次成功 POST 都创建新密钥。网络中断后无法重新读取第一次的 secret，应核对现有列表，
  使用现有管理流程处置多余密钥，再决定是否重新创建。
- 此次实现不改变 Cumora 的失败回退或用户同步策略，不执行部署、迁移或数据回填。

## 本地验证

新增测试覆盖 handler 契约和脱敏、输入/目标校验、真实生产路由与管理员鉴权、审计无 secret、
网关余额和订阅撤销拦截，以及服务层分组授权。SQLite 内存数据库测试验证真实仓库的
workspace 隔离、仅存摘要、后续读取无 secret 和凭证插入失败后的事务回滚。
这些测试无需服务器或已有数据库数据。

本次在 `backend` 目录通过以下检查（没有运行依赖 PostgreSQL 的集成测试）：

```powershell
go test -tags unit ./internal/handler/admin ./internal/server/routes ./internal/service ./internal/repository ./internal/server/middleware ./internal/handler/dto -run 'TestAdminAPIKey|TestAPIKey|TestAdminAuth|TestWorkspace|TestValidateCreateAPIKeyRequest' -skip 'TestWorkspaceMigrationAndRepositoryIsolation|TestWorkspaceLegacyBackfillBatchesAreIdempotent' -count=1
go test -tags unit ./internal/handler -run 'TestAPIKey' -count=1
go test ./cmd/server -run '^$'
go run github.com/google/wire/cmd/wire@v0.7.0 check ./cmd/server
```

修改的 Go 文件已执行 `gofmt`；工作树 `git diff --check` 通过。
