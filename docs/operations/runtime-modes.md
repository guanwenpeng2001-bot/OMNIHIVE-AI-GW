# 运行模式与停机交接

部署使用同一份 `cmd/server` 二进制。单进程默认使用 `all`。分进程部署启动 `control`、`gateway` 和至少一个 `worker`，配置同一套 PostgreSQL、Redis、签名和加密密钥。每个进程绑定自己的 `server.host`、`server.port`。首次安装由 `all` 或 `control` 完成。

模式的优先级依次为 `-mode`、`OMNIHIVE_RUNTIME_MODE`、YAML 的 `runtime.mode`、默认值 `all`。无效值、监听端口冲突、PostgreSQL 或 Redis 无法连接都会使正常服务启动失败。

| 能力 | all | control | gateway | worker |
| --- | --- | --- | --- | --- |
| `GET /health`，`GET/HEAD /ready` | ✓ | ✓ | ✓ | ✓ |
| SPA、v1 登录和管理、支付回调、v2 公共目录与状态、管理与项目查询 | ✓ | ✓ | | |
| 标准推理、原生协议与历史根路径别名、v1 批量图片 API | ✓ | | ✓ | |
| 项目 Playground 全部路由、请求 replay、图片任务提交与 retry | ✓ | | ✓ | |
| 本进程的 请求观察、插件宿主、提示词审计 | ✓ | ✓ | ✓ | |
| 持久工作扫描、回执恢复、任务租约回收、状态聚合、保留期清理 | ✓ | | | ✓ |

`/ready` 在接收请求且两项必需依赖可用时返回 200，失败或排空期间返回 503。正常探针带 `X-OmniHive-Mode`。可选能力通过 `X-OmniHive-Degraded` 报告固定名称，含未配置独立恢复库的 `restore`、启动失败的 `plugins` 或 `prompt-audit`、最近一次扫描失败的 `image-task-recovery`。依赖检查合并并发探针，结果缓存两秒，单次探针最多等待一秒。详细运行数据沿用受限诊断端口。可选的 `OMNIHIVE_PPROF=1` 在本机回环地址开放诊断。

## 入口与历史链接

分流发生在原 Gin 处理链之前。入口代理应保持原方法、路径、会话凭据、客户端断开和流式响应。分流使用与 Gin 相同的原始路径，跨域预检采用 `Access-Control-Request-Method`。分进程访问非本角色入口返回 404。下表用 `P` 表示 `/api/v2/workspaces/{workspace_id}/projects/{project_id}`。

| 功能与历史链接 | 服务角色、权威来源和原有开关 | 回归依据及退出条件 |
| --- | --- | --- |
| 登录、TOTP、Passkey、用户、工作区、密钥，面板 `/profile`、`/keys`、`/workspaces` | control。v1 auth/user/workspace 及 身份权限与凭据状态 | `TestRuntimeSplitProcesses` 验证实际登录、profile、签发及撤销。Passkey、TOTP 和密钥路由另有独立测试。既有客户端仍可使用 v1。 |
| 账号、代理、分组、路由，`/admin/accounts`、`/admin/proxies`、`/admin/groups` | control。原 admin 写入、连接、路由策略，按原管理员授权、step-up 和作用域 | `proxy_group_routes_test.go`、`internal/control/policies/integration_test.go`。历史路由影子对比和回滚按策略分组执行。 |
| 标准客户端、模型目录与平台别名，`/v1`、`/v1beta`、`/backend-api/codex`、`/antigravity`、DashScope HTTP/WS、根别名 | gateway。沿用协议、执行、路由和原有 key 准入。嵌入面板构建的文档入口 `GET /usage` 走 control，API 使用 `/v1/usage` | `TestGatewayPlatformProtocolMatrix`、`TestGatewayUnsupportedProtocolContracts`。保留 `/v1/sub2api/billing`、`/sub2api/billing` 及其原有返回值。 |
| 订阅、订单、支付回调、兑换、优惠、返佣，`/subscriptions`、`/orders`、`/redeem`、`/affiliate` | control。原订阅/支付服务、账务与权益，回调保留原验签 | 分进程测试读取真实用户的订阅。`payment_webhook_integrity_test.go`、`subscription_bulk_action_routes_test.go` 和 `affiliate_repo_integration_test.go` 覆盖对应权威写入或权限。 |
| 批量图片与单图异步入口，`/batch-image`、`/v1/images/batches`、`/v1/images/tasks/{id}`，以及根别名 | 面板走 control。API 走 gateway。批处理由 worker 使用原队列和幂等结算。历史单图读取使用原 Redis 任务，存储配置控制新提交 | 分进程测试以真实 key 读取已有 batch。`image_task_handler_test.go` 验证历史提交和轮询。保留旧 job ID 和轮询链接，按原保留期退出。 |
| 项目任务与 Playground | gateway 直接处理 `P/playground/**`、`POST P/requests/{id}/replay`、`POST P/tasks`、`POST P/tasks/{id}/retry`。其余任务读、撤销和下载走 control | 项目会话、key、付费确认和幂等键均由原模块检查。项目级 HTTP 集成测试覆盖会话范围、执行和结果。 |
| 备份、更新、审计与运营，`/admin/backup`、`/admin/audit-logs`、`/admin/ops` | control。原管理 API、外部恢复日志、状态、作用域审计 | 备份/恢复测试使用独立演练库与日志库。已有 v1 恢复操作 ID 持续可查。更新沿用原服务与版本检查。 |
| 插件，`/admin/plugins`、`/api/v1/admin/plugins`、`/api/v2/admin/extensions` | control 管理、gateway 使用安装及绑定。插件清单来自 PostgreSQL。启用签名包前，各宿主需要同一制品与本地信任策略 | 分进程测试从真实安装行读取清单。`plugin_handler_test.go` 与插件合约覆盖管理和宿主。原生 v1 包、UI bridge 与制品摘要保留到迁移窗口结束。 |
| MCP 授权元数据、用户 onboarding、审计与路由偏好、两条 rankings | control。用户 API 需要登录。授权元数据、操作记录和偏好按用户保存在进程内，重启后清空。项目权限、项目审计与路由执行仍由 identity、audit 和 policies 提供。 | 两条公开 rankings 入口使用可选 JWT 和面板访问控制，缺少公开聚合数据源时返回 503。路由与权限见 `TestUserConsoleRoutes`、`TestRuntimeRoutesByRole`。 |
| 旧 `POST /api/v1/public/playground/chat` | 已移除。使用项目级 Playground runs 的认证、路由和计费入口。 | 升级时撤销原运营方 Playground 凭据。 |

v1 注册清单以 `internal/server/routes/{common,auth,user,admin,workspaces,model_access,model_plaza,payment,gateway}.go` 和 `handler.RegisterPageRoutes` 为准。其余 v2 路由由 `internal/server/router.go` 调用模块的 `Register`，沿用模块的验证、授权和开关。上述路径均相对 `backend/`。

## 作业归属与关闭顺序

所有模式构建相同服务图。每个进程有自己的 supervisor 和准入状态。服务节点继续维护本地认证失效订阅、缓存、日志/审计写入和请求提交后的落盘。

| 作业 | 执行与重复保护 |
| --- | --- |
| 恢复 | 配置 `OMNIHIVE_RESTORE_JOURNAL_DSN` 和 `OMNIHIVE_RESTORE_ISOLATED_DSN` 后，all/worker 调用 `StartWorker`。恢复日志、目标互斥和 generation 由 restore 维护。部分配置或初始化错误将阻止启动。 |
| 图片任务 | 启动时以及上次扫描结束五分钟后执行 `Store.Recover`。过期运行租约转为 `unknown`，递增 generation，清除到期 retry 密文。后续 worker 使用同一条件，失效的执行者无法提交旧结果。 |
| 公共状态 | all/worker 调用 `RunCollector`，按原发布开关处理完结时间桶，以事务锁串行聚合。control 读取已发布结果。 |
| 审计观察与保留期 | 每个服务进程启动 `Observer.Run`，固定计数器记录队列溢出、写入和扫描失败。all/worker 运行 `RunRetention`，清理证据与保留策略沿用项目事务。 |
| 结算、上游 job、批量图片、失效 outbox、运维与到期清理 | all/worker 扫描已持久化回执、到期上游作业和原批处理队列。沿用模块的行锁、唯一键、leader 锁、任务版本及 CAS。并发槽按 Redis 到期时间回收，滚动重启保留其他进程的活跃槽位。 |

收到 SIGTERM/SIGINT 后，进程立即关闭准入、取消活动 HTTP/SSE 上下文及注册的实时会话。异步任务共享准入组，结算回执的持久化使用自身预算。常规进程的总预算为 20 秒，其中前约 13.3 秒用于 HTTP、已升级连接和已接收工作的排空。到达子期限会关闭长连接和 hijack 后仍在等待的套接字。随后取消并等待 supervisor，执行各服务的 Stop，排空结算池和缓存写入，停定时器，再关闭 Redis 和主数据库。超出总期限会带错误退出。发生 SIGKILL 时按持久化回执、租约和原 job ID 恢复，已受理但上游结果不明的任务保留 `unknown`。

v1 管理页的内置扩展参数由旧处理器在启动时读入。修改后需让各 gateway 重新加载。插件安装包和绑定具有独立的持久化清单与宿主协调逻辑。

旧单图异步入口使用处理器私有的内存池和 Redis 任务记录。它的恢复语义是按已有 ID 在截止时间后查询未知结果。旧仪表盘的后台回填由原服务自行启动。两者仍需在滚动重启演练中单独核对，尤其是进程退出时的在途请求、回执和数据库连接。

## 验证运行模式

在 `backend/` 中执行下列命令。进程测试需独占的 PostgreSQL `job_test` 和 Redis，分别通过 `UPSTREAM_JOB_TEST_DSN`、`AFFINITY_REDIS_TEST_ADDR` 提供。它会执行迁移、创建测试数据并向子进程发信号。公共状态基线测试要求全新的空库，应与已启动聚合器的进程测试分开运行。

```sh
go build ./...
go vet ./...
go test ./...
go test -tags=unit -count=1 ./cmd/server ./internal/modules/runtime
go test -tags=integration -count=1 ./cmd/server -run '^TestRuntimeSplitProcesses$' -v
go test -count=2 ./internal/integrationtests -run '^TestCoreModelFlowPostgres$'
```

进程测试以相同二进制分别从默认值、YAML、环境变量及 CLI 启动。它检查核心依赖故障、可选恢复能力诊断、v1/v2 真实读、control 签发后 gateway 使用/撤销、worker 路由隔离以及并发槽。测试持有回执行锁，通过 PostgreSQL 锁等待确认 worker 已进入结算后发送 SIGKILL，再启动两个 worker。断言基于数据库终态、余额和用量行数，图片任务另行检查 generation 和 retry 密文。SSE 与 WS 测试覆盖已观察回执、排空顺序和卡住的 hijack 连接。

旧版仍在运行时沿用共享协调键。旧单进程版本启动可能清除其他实例的并发槽，混合版本演练应先排空旧实例再重启它。
