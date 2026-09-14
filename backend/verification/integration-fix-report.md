# 批次 2 集成修复验收报告

基线：`4d8f19880`；工作树：`C:/Users/Administrator/orca/workspaces/sub2api/batch2-integration-fix`。方案依据为 main-work 中 `.pi/plans/final-plan.md` 第 123–129、173–179、197–201 行（任务给出的 `apps/.pi/` 路径在此 checkout 不存在）。没有修改任何既有 `.pi/` 报告或迁移文件。

## 1. DTO 枚举与监控权限

首次复现：`TestAllPlatformDTOEnumsMatchAuthorities` 报告 `[openai anthropic gemini grok antigravity kimi zhipu deepseek qianwen commandcode minimax opencode_go cursor]` “should have 12 item(s), but has 13”。

根因是测试把数据库可存储集合直接当成可写监控权限集合。244 迁移允许 Cursor 的目的是保存历史行；`channel_monitor_validate.go` 的 `monitorProviders` 仍只有 12 项，三个监控 DTO binding 也仍只有 12 项，Cursor 更新、模板编辑和主动检测继续受服务保护。方案明确要求保留行而禁用运行，因此不能把 DTO 扩成 13 项。

修改：先用 ElementsMatch 完整核对迁移存储集合与 13 个 domain concrete 平台，再仅排除具名的历史兼容项 cursor，保留 12 项可写监控集合校验。原有 AST 全 DTO 扫描、逐字段精确枚举对比、group composite 例外、最少扫描 6 个字段的防漏检均保留。保持的校验意图是“DTO 权限集合与平台/监控权威集合一致”，同时明确存储兼容不授予新写入/主动探测权限；没有回退 Cursor 迁移兼容。

## 2. Realtime 首回执与冻结计价语义叠加

首次复现：`TestQianwenRealtimeDurablePerTurnIdempotencyAndRestart` 在第 64、78 行报 `conflicting realtime receipt`；`TestQianwenRealtimeTwoTurnsReplayAndDisconnectDrain/two_turns_and_replay` 在第 145 行报 `failed to get reader: failed to read frame header: EOF`。

根因：`freezeJobPricing` 在独立结算事务中把 PricingState / PricingSnapshot 写回 job.Billing；`EqualQianwenRealtimeReceipt` 原本对整个 Billing 做 DeepEqual。新重放输入来自会话，尚无结算输出，因而与已结算原回执比较失败；relay 随后关闭连接，表现为 EOF。冲突不在八位量化金额或轮次身份本身。

修改：判等使用 Billing 值拷贝，仅去除 PricingState / PricingSnapshot 两个结算派生输出后比较。所有原始费率、模型、倍率、区域、版本、订阅/额度开关，以及 session+responseID 身份、规范 usage、provider 状态、归属仍严格比较；不修改存储对象，不覆盖冻结解释，不改变首回执 INSERT 或独立结算事务。

新增 unit 断言验证重放保留原冻结解释、历史金额复算正确、修改倍率仍冲突且不多扣；新增真实 PG 断言在结算后/并发重放前读取持久化快照，比较重放后的快照仍原样并复算金额。原有同轮并发只扣一次、两轮独立收费、重启恢复、usage/status/费率冲突保留原记录、首次 INSERT 失败无空回执、收费回滚不丢 usage 等校验全部保留。

## 3. Cursor 增量 lint

首次实际 lint 为 14 项：13 个 errcheck 与 1 个 G704。Clone 与 DialContext 两处实际上是未检查类型断言的 ok 值，并不是它们的 error 返回值被丢弃。

修改：对 DefaultTransport 和 SOCKS ContextDialer 的断言进行检查，不可用时返回明确错误；建立/清除 CONNECT deadline 的错误向上传播，失败路径仍关闭连接；清理和取消回调的 Close 按既有 `_ =` 惯例显式忽略。测试响应写入检查错误，SOCKS mock 的 deadline/write 失败报告并退出，清理 Close 显式忽略。保持代理失败不直连、账号路由隔离、取消连接和受保护 header 的原有断言。

G704 命中通用 Do 包装器转交调用方 request 的位置；参考 `internal/pkg/servertiming/http.go` 同类包装器惯例，仅在该行说明请求目标由 native/relay 调用方确定的信任边界。没有关闭 errcheck、没有全文件豁免、没有更改 lint 配置。

## 验收执行环境与原始记录

Windows PowerShell，Go 原始命令；临时 `postgres:16-alpine` 容器 `pg-fix-verify`，Docker 自动映射 `127.0.0.1:49863`。六项验收同一个串行脚本运行，避免任务自身并发修改测试数据库；设置 `CI=true`、`UPSTREAM_JOB_TEST_DSN=postgres://postgres:postgres@127.0.0.1:49863/job_test?sslmode=disable`、`NEW_PLATFORMS_TEST_POSTGRES_DSN=postgres://postgres:postgres@127.0.0.1:49863/postgres?sslmode=disable`。

定向回归已退出 0，日志 `backend/verification/fixed-targeted.log`。首次失败日志保留于 `backend/verification/initial-targeted.log` 和 `backend/verification/initial-lint.log`；所有日志位于本工作树且不会被后续重跑覆盖。

## 六项 DoD 实际结果

以下命令均在 backend 目录运行，本轮全部首次通过，无不稳定失败或隔离重跑。汇总退出码原文在 `backend/verification/results.txt`。

1. `go test ./...`：退出 **0**；`dod-default.log`，service 包 132.815 秒。
2. `go test -tags=unit ./...`：退出 **0**；`dod-unit.log`，service 包 175.512 秒。
3. `go test -mod=readonly -tags=unit ./internal/repository -run ^TestUpstreamJobPostgres$ -count=1`：退出 **0**；`dod-postgres-jobs.log`，1.690 秒。
4. `go test -mod=readonly -tags=integration ./migrations -run ^TestNewPlatformMigrations$ -count=1`：退出 **0**；`dod-migrations.log`，5.535 秒。
5. `go test -mod=readonly -tags=integration ./internal/repository -run "^TestQianwenRealtimeReceiptPostgres$|^TestLegacyMonitor|^TestLegacyUpgradeDDLMeasurement$" -count=1`：退出 **0**；`dod-postgres-receipts.log`，99.957 秒。
6. `golangci-lint run --timeout=30m --new-from-rev=c51166151bbfda1e34b8d8b7ec77435a45648c24 ./...`：退出 **0**；`dod-lint.log`，原文 `0 issues.`。

完成时间：2026-09-14（本机 Asia/Shanghai）。`git diff --check` 通过。临时容器 `pg-fix-verify` 已清理。日志文件按仓库现有 `*.log` 规则保留在本工作树，报告与退出码记录纳入提交；首次失败的用例名和错误原文也已写入本报告。

剩余事项：协调者审阅并合入集成分支；本任务未合并 main，也不宣称完成授权真机供应商握手或生产发布验证。

