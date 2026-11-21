# 后端 v2 与全栈发布验收清单（GA Checklist）

> 本清单以“诚实区分已验证证据与生产切换前必须由真实客户环境补齐的证据”为原则。
> 状态口径：`verified` = 有可复验运行物；`verified-local` = 在本地隔离实例已实测；`gated` = 已有自动化门禁但存在缺口；`pending-production` = 必须真实客户生产环境签核；`open` = 未关闭。

## 门禁分类与诚实性声明（D02 / I04 / I05 / E02）

### A. 自动化与集成已验证门禁（Automated & Integration Verified Gates）

这些门禁可由 CI 或本地隔离实例复验，不依赖真实客户生产数据。**每一项都必须附可重复证据，且不得把“包级 ok”等同于“用例全执行”。**

| 门禁 | 状态 | 证据与边界 |
| --- | --- | --- |
| 前端全量单测 / ESLint / vue-tsc / 生产构建 | verified | CI 475 个测试文件、5,461 用例、0 skip；覆盖率超阈值；生产构建成功。 |
| golangci-lint / govulncheck | verified | `golangci-lint` 0 issues；`govulncheck` 调用路径 0 漏洞。 |
| `contracts` / `generated-drift` / `independence` | verified | CI quality-gate 三阶段 PASS；`independence` 的 `git grep \|\| true` 吞错问题记为 P2 待修。 |
| PostgreSQL 15/16/18 选中矩阵 + required-round | verified | 选中测试强制 PASS/不得 skip；但未下载的 36+9 个明细日志未独立核对。 |
| 协议桥接契约（Chat / Responses / Messages / Gemini） | gated | 契约测试通过，但 F07 仍静默丢弃服务端工具与 `tool_choice`（P0 开放），修复前不能宣称协议无损。 |
| 五层策略交集与无候选零发送（F16） | gated | planner 侧五层交集已实现；原生调度尚未合成本次请求层约束（P0 开放）。 |
| 账本不变量（M01/M05/M07） | verified | 结算唯一效果、项目/Key 双层准入、pending 保留预留有 PG 回归。 |
| 账本不变量（M02/M03/M04/M06/M08） | open | 未知价按零金额、pending→needs_review 不释放预留、到期回收忽略 pending receipt、同步媒体 journal 不全、BYOK 付款来源未进原生发送点，均为 P0。 |
| 本地真实推理与记账闭环 | verified-local | 见 §“Wave3 本地实况复验”；三协议 + 幂等去重 + 结算入账在本机 Magpie 网关实测通过。 |
| Docker Compose 构建与健康 | pending-production | 需在 `cumora-laptop`（或等效 Docker 环境）复验；WSL 无 Docker，本清单不再把未运行项写成 pass。 |
| unit 标签 PG/Redis no-skip 门禁 | open | T-CI-1：unit 段无 DSN，15 个 unit-tag DB 套件/36 个 `Test*` 在 CI 永远 skip，且 skip 不导致失败。 |
| quality-gate 六阶段全量 | open | CI 只跑 3/6 stage；smoke 从未在 CI 执行。 |

### B. 生产切换前必须由真实客户生产环境签核的门禁（Pre-Production Cutover Gates）

这些门禁 **无法** 由 CI、本地 QA 实例或代码审查替代。在目标生产环境完成并归档脱敏证据前，生产 GA 保持 **HOLD**。

| 门禁 | 状态 | 需要的真实环境证据 |
| --- | --- | --- |
| `PAIRED_2H` | pending-production | 目标生产环境 7,200 秒成对基线对比，P95 开销 ≤1.10 倍；配对冷却门槛见 `contracts/release/backend/full.py`。 |
| `CUTOVER` | pending-production | 正式生产流量切换签核，含回滚演练与切换后新交易保留。 |
| `I04` 生产 8-cohort 截止时点对账 | pending-production | 真实旧实例按相同 cutoff 逐表核对 `users/credentials(accounts)/api_keys/balances(wallets)/orders/tasks(jobs)/subscriptions/audit_logs`，逐币种期初+effects=期末，`unexplained_differences=0`。本地只读对账已归档，见 [cohort-reconciliation-report.md](../../operations/cohort-reconciliation-report.md)，但 **不能替代生产对账**。 |
| 生产支付 Webhook 密钥与全渠道乱序回放 | pending-production | 真实支付 provider secret、逐渠道重放、退款/争议/订阅/兑换对账同窗样本（M09 仍 PARTIAL）。 |
| `I05` / `E02` 两版本退役窗口起点 | pending-production | 当前 **无已启动日期**；87 个兼容项 deployed-version/调用者 census 均为 unknown，实际退出数为 0。详见 [retirement README](../../../contracts/migrations/retirement/README.md)。 |

## Wave3 本地实况复验（2026-11-20）

验收组 WAVE2 曾因唯一 Key `quota_exhausted` 且明文不可读而无法复验三协议计费。主代理提供的 `provision-acceptance-key.sh`（位于 `/home/administrator/omnihive-local/`，非 git 仓库）已在该实例完成：

- 管理员钱包 ≥ `$500`；项目硬预算 USD `$200/calendar_month`；Key 预算 USD `$200/calendar_month`，`quota` 无限制。
- 上游账号 `Magpie Local`（`active`，指向本地 Magpie 网关）绑定分组 `Magpie Local`（`active`，`allow_messages_dispatch=true`），含 4 个真实计价模型。
- 项目级 Key `Wave3 Acceptance Test Key` 明文写入 `/home/administrator/omnihive-local/ACCEPTANCE_TEST_KEY.txt`（chmod 644，供只读验收用户读取）。
- 实测结果：`POST /v1/chat/completions`、`POST /v1/responses`、`POST /v1/messages` 均返回成功；相同 `Idempotency-Key` 重放返回同一响应 ID 且 **只产生一次扣款**。
- PostgreSQL 核对：该 Key 8 条 `usage_logs`、8 条 `usage_settlement_receipts` 全部 `state='settled'`，0 条未结算，8 条 `usage_billing_dedup`，8 条 `api_key_budget_reservations` 全部 `settled`；钱包事件流水与 `users.balance` 一致（无重复扣款）。

## 六步主流程（每步标注验证层级）

| 检查 | 状态 | 验证层级与诚实结论 |
| --- | --- | --- |
| J01 项目目录与公开目录 | verified-local | 公开/项目目录 HTTP 200；允许、合格、定价投影本地实测。 |
| J02 项目 Key 创建与幂等 | verified-local | 创建返回一次性明文、幂等重试同 ID；本地实测。 |
| J03 原生网关三协议 + Playground | verified-local | Chat/Responses/Messages 三协议本地真实上游返回 200；Playground 同步调用本地实测。 |
| J04 同一请求号详情/用量一致 | verified-local | `usage_logs`/receipt 按 request_id 关联一致，本地核对。 |
| J05 USD 窗口账本分录 | verified-local | 8 位小数 `charged_amount` 精确入账，幂等重放单次扣款，本地核对。 |
| J06 `/route-explain` 决策轨迹 | gated | 有实现；原生 Chat 尝试控制未统一（B01 P0 开放），同一故障序列预算结果一致性仍需补测。 |

## 独立验收项

| 证据键 | 状态 | 验收结果或阻断 |
| --- | --- | --- |
| OPENAPI、TYPESCRIPT | verified | 聚合 OpenAPI 与 TypeScript 声明一致（`generated-drift` PASS）。 |
| SURFACES_HTTP、LIVE | gated | 公共/项目目录、状态、资源池 CRUD、连接向导冒烟通过；公开排行仍为固定 503 桩。 |
| 策略草稿、模拟与发布 | verified | 首次无快照自动采集、`If-Match` 发布/回滚回归通过。 |
| PROTOCOL_HTTP | gated | 三协议按动作矩阵路由或拒绝；F07 Anthropic→Chat 桥接丢服务端工具为 P0，修复前不宣称无损。 |
| REGISTRY、MATRIX | gated | 10,034 个声明格由矩阵生成，**不等于 10,034 次真实发送**。 |
| FINANCE、FINANCE_MODULES | gated | M01/M05/M07 已验；M02/M03/M04/M06/M08 开放，财务通过范围不得覆盖未测转态。 |
| CALLBACK_HTTP、SIGNED_CALLBACK | gated | 签名与幂等投递有回归；生产支付 webhook secret 与全渠道乱序样本未提供（M09 PARTIAL）。 |
| ACCESS、CONTROL_MODULES、STATUS_MODULE | verified | 跨 workspace/project 越权 404、撤权、审计投影与公开状态回归通过。 |
| MIGRATION、RESTORE、CUTOVER_FIXTURE | gated | SQL 迁移链 checksum、围栏恢复有自动化证据；生产 8-cohort 对账（I04）未执行，本地对账见 §“八类对账”。 |
| LOAD | gated | 隔离负载 0 错误、冷却后占用归零有脚本断言；2 小时成对生产压测（PAIRED_2H）未执行。 |
| PAIRED_2H、CUTOVER | pending-production | 仅在生产环境执行并归档后方可签核。 |

## 八类存量数据对账（I04）

生产 cutoff 对账 SQL/CLI 步骤与本地实证见 [cohort-reconciliation-report.md](../../operations/cohort-reconciliation-report.md)。

要点：

- BE-23 只读 CLI：`omnihivectl refactor inventory --format=json` 与 `status --format=json`（读 `OMNIHIVE_REFACTOR_DSN`）覆盖 BE-23 源集合；`migrate/reconcile/cutover` 仍为 planned。
- 八个业务 cohort：`users`、`credentials/accounts`、`api_keys`、`balances/wallets`、`orders`、`tasks/jobs`、`subscriptions`、`audit_logs`。
- 每 cohort 必须：静默写入 → 统一 cutoff 快照 → 比基数/状态/版本边界 → 逐币种 `opening + effects = closing` → 记录 `rollback_floor` 与例外 → `unexplained_differences = 0` 才 `eligible_for_switch`。
- 本地实例八类基数与余额一致（`users.balance` = 钱包事件流水 = `499.99227650`），已归档；生产签核仍为阻断项。

## 灰度与停止条件

| 队列 | 当前决定 | 进入条件与负责人 |
| --- | --- | --- |
| 内部与测试环境 | READY（本地） | J01–J06、三协议真实上游、财务账本、八类本地对账均在本机/隔离实例验证；Docker Compose 需 `cumora-laptop` 复验。 |
| 1% → 5% → 20% → 50% → 100% | 均未开放 | 发布负责人在每一级记录观察起止、流量和对账差异。检查前一级的协议、资金、请求身份、积压和撤权结果后逐级签核。 |

发现未授权选路、无法解释的金额差、重复资金效果、持久工作无法恢复或协议偏离时，停止扩量。BE-23 记录当前权威写入方、检查点及 rollback floor。退役窗口（I05/E02）当前无起点日期，兼容路径实际退出数为 0。
