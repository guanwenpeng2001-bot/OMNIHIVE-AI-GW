# 后端 v2 与全栈发布验收清单（GA Checklist）

## 最新复验：`v0.2.5` 发布候选闭环（2026-11-20）

1. **核心旅程 J01–J06 与真实多上游闭环**：
   - 无显式 `group_id` 创建项目级 API Key 自动绑定可用标准分组；通过同一凭证连续跑通 Gemini、Kimi、OpenAI、DeepSeek 四大真实上游模型及 `/v1/chat/completions`、`/v1/messages`、`/v1/responses` 三大协议（全部 `200 OK`）。
   - 原生 Playground 同步调用返回 `200 OK`（`outcome=succeeded`），关联请求详情（`/requests/{id}`）、选路轨迹解释（`/route-explain`）与精确 8 位小数账本分录（`ledger_entry_ids`）一致。
   - 连接向导 OAuth 草稿 `keep` 凭据保存、兼容协议探针、资源池 `POST/PATCH/DELETE` 闭环、路由策略首次无快照自动采集模拟与 `If-Match` 发布/回滚闭环全部实测通过。
2. **独立容器化部署与多视口门禁**：
   - 多阶段生产 `Dockerfile`（含 `check:i18n`、`vue-tsc -b`、`vite build` 与 Go `1.27.4` 内嵌静态资源编译）在独立 Docker Compose 环境（PostgreSQL 18 + Redis 8）健康运行，`/api/v1/admin/system/version` 与顶栏 `VersionBadge` 正常校验 GitHub Release 更新。
   - 真实浏览器覆盖 `1920×1080`、`1440×900`、`375×812` 巡检 28 条核心路由与重定向：页面运行时错误 `0`、横向溢出 `0px`、表格双滚动锁死 `0`。
3. **自动化门禁与生产灰度说明**：
   - `contracts`、`generated-drift`、`independence`、`golangci-lint`、前端全量单测与后端 PostgreSQL 15/16/18 集成矩阵持续守卫。
   - 面向正式生产租户切换前的长时压测（`PAIRED_2H` 7,200 秒成对基线对比）与生产流量 `CUTOVER` 仍按下方运维规程在目标生产环境执行签核（本地/CI 执行产出的临时报告目录 `contracts/release/backend/reports/*/` 已纳入 `.gitignore`，不再签入源码库）。

## 隔离短跑基线记录（2026-11-19）

在隔离 loopback 网络和 PID 命名空间中通过 [`verify.sh`](../../../contracts/release/backend/verify.sh) 与 [`journey.json`](../../../contracts/release/backend/journey.json) 完成验证：**J01–J06 全部通过**，`OPENAPI`、`GROUPED_PLAYGROUND`、`SURFACES_HTTP`、`PROTOCOL_HTTP`、`PUBLIC_NATIVE`、`CALLBACK_HTTP`、`ACCESS`、`LOAD`、`MATRIX`、`REGISTRY`、`STATUS_MODULE`、`FINANCE`、`FINANCE_MODULES`、`SIGNED_CALLBACK`、`ACCESS_MODULES`、`CONTROL_MODULES`、`MIGRATION`、`RESTORE`、`CUTOVER_FIXTURE` 全部通过。

## 六步主流程

| 检查 | 状态 | 验收观察结果 | 负责模块 |
| --- | --- | --- | --- |
| J01 | pass | 项目目录 HTTP 200；公开目录补齐协议、动作与上下文元数据，同时满足 allowed、eligible、priced | `catalog` |
| J02 | pass | HTTP 201 首次返回项目 Key 并自动绑定默认可用分组，幂等重试返回同一 ID 且凭据为 null | `identity` |
| J03 | pass | 项目 Key 原生网关（Chat / Messages / Responses）与受控 Playground 均返回 HTTP 200 | `gateway`、`playground` |
| J04 | pass | 按 J03 同一请求号读取详情，workspace、project、Key、attempt、用量来源与价格版本一致 | `execution`、`requests` |
| J05 | pass | USD 窗口查询核对详情、charges、预留/估算额与账本分录，8 位精度金额及唯一结算效果一致 | `billing`、`settlement` |
| J06 | pass | 同一请求号 `/route-explain` 返回 HTTP 200 及完整选路决策轨迹与候选排除原因 | `routing`、`policy` |

## 独立验收项

| 证据键 | 状态 | 验收结果或阻断 | 负责人 |
| --- | --- | --- | --- |
| OPENAPI、TYPESCRIPT | pass | 聚合 OpenAPI 逐字节一致；生成的 TypeScript 声明与签入文件摘要完全一致（`generated-drift` PASS） | 契约与前端工程负责人 |
| SURFACES_HTTP、LIVE | pass | 公共与项目目录及 compare、公共状态、项目配额、资源池 CRUD 和任务中心返回符合契约的业务字段；连接向导、兼容协议探针、启用、凭据轮换与撤销冒烟通过 | 控制面与连接模块负责人 |
| 策略草稿、模拟与发布 | pass | 首次无快照自动采集项目候选图并返回 `simulation_id`，`If-Match` 发布与版本回滚闭环返回 200，零副作用污染账本 | 路由策略模块负责人 |
| PROTOCOL_HTTP | pass | Chat Completions、Responses（含子路径与 WS 准入）、Anthropic Messages、Gemini GenerateContent 均按动作矩阵精准路由或快速拒绝 | 网关协议与适配器负责人 |
| REGISTRY、MATRIX | pass | 346 种声明身份 × 29 种动作，共 10,034 格；代表模型端点绑定与原生拒绝、stream/tools 组合全部对齐 | 网关适配器负责人 |
| FINANCE、FINANCE_MODULES | pass | 项目硬预算与 Key 预算双层准入、未决审查态（`needs_review`）释放预留、待结算保留预留、精确 8 位十进制结算与恢复幂等全部通过 | 账务与结算模块负责人 |
| CALLBACK_HTTP、SIGNED_CALLBACK | pass | 支付签名校验、重复回调幂等投递与唯一钱包变动校验通过 | 支付模块负责人 |
| ACCESS、CONTROL_MODULES、STATUS_MODULE | pass | 跨 workspace/project 越权统一 404，撤权毫秒级生效；审计投影、任务受控票据下载、Playground 与公开状态校验通过 | 身份权限与安全审计负责人 |
| MIGRATION、RESTORE、CUTOVER_FIXTURE | pass | SQL 迁移链 checksum 一致、8 位小数余额精确保留；围栏恢复 RPO=0s；旧库清单脱敏与检查点校验通过 | 数据迁移与恢复负责人 |
| LOAD | pass | 隔离负载压测 0 错误，冷却后待结算与预算占用归零，连接池与内存平稳回落 | 发布验证负责人 |
| PAIRED_2H、CUTOVER | pending（生产切换前执行） | 目标生产环境 7,200 秒成对基线对比（P95 开销 ≤1.10 倍）及正式生产流量切换签核 | 发布与运维负责人 |

验证入口脚本：[`contracts/release/backend/verify.sh`](../../../contracts/release/backend/verify.sh) · 核心旅程契约：[`contracts/release/backend/journey.json`](../../../contracts/release/backend/journey.json) · [执行规程](verification.md)

## 灰度与停止条件

| 队列 | 当前决定 | 进入条件与负责人 |
| --- | --- | --- |
| 内部与测试环境 | READY | J01–J06、多协议真实上游、财务账本与容器化部署验证已全部通过 |
| 1% → 5% → 20% → 50% → 100% | 均未开放 | 发布负责人在每一级记录观察起止、流量和对账差异。检查前一级的协议、资金、请求身份、积压和撤权结果后逐级签核 |

发现未授权选路、无法解释的金额差、重复资金效果、持久工作无法恢复或协议偏离时，停止扩量。BE-23 记录当前权威写入方、检查点及 rollback floor，发行负责人按经验证的运行回滚方案保留切换后新交易。该候选未发生流量切换或生产回滚。
