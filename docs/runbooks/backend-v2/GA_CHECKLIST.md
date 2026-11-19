# BE-24 GA 清单

**决定为 HOLD。内部和 1% 灰度都暂缓。** 候选 `b80fe3170de114beab2cfa19c74ec264d171ffcb` 的项目 Key 无法完成付费 Playground 主流程。另一项真实 HTTP 检查发现，文本模型收到 embeddings 请求时，候选向本地上游发送了一次。发布负责人需取得缺陷复测、成对两小时结果及最终运行时和迁移签核后再考虑开放内部流量。

本轮于 2026-11-19 在隔离的 Linux 网络和 PID 命名空间运行。两个候选网关连接临时 PostgreSQL 16.15 和 Redis 7.0.15，另一套 PG16 接受 BE-09 恢复。上游为本地 TLS fixture。未使用生产数据或供应商凭据。发布验证由 BE-24（astra）执行，发行复核待签。[本轮总表](../../../contracts/release/backend/reports/20261119T075243Z-b80fe3170/summary.md) · [分项断言、完整工具链和时间](../../../contracts/release/backend/reports/20261119T075243Z-b80fe3170/results.json)

## 六步主流程

| 检查 | 状态 | 本轮观察与复测条件 | 负责包 |
| --- | --- | --- | --- |
| J01 | pass | 项目目录 HTTP 200。fixture/text 同时满足 allowed、eligible、priced | BE-05 sol |
| J02 | pass | HTTP 201 首次返回项目 Key，幂等重试返回同一 ID，凭据为 null | BE-01 astra |
| J03 | fail | 预期 200，实得 404 NOT_FOUND。J02 对应行无 group_id。另建同策略项目 Key，经受支持的分组接口赋组得到 200，再跑 Playground 得 403 ACTION_DENIED，本地上游计数为零。需复测两条路径 | BE-01、BE-13 astra |
| J04 | pending | J03 未产生执行 ID。本轮用未分配 UUID 探测得到 404，无法验证该次调用的权限、attempt、费用 | BE-06 astra、BE-14 sol |
| J05 | pending | USD 窗口查询得到 200，缺少 J03 请求号可供核对结算 ID、金额、账本 | BE-11、BE-16 astra |
| J06 | pending | 使用未分配 UUID 得到 404，缺少该次执行的策略版本和排除原因 | BE-08 astra、BE-14 sol |

另走原生 Responses 得到执行 ID，随后详情、费用和 route-explain 均返回 200 且 ID 一致。顺序读取时，详情处于 pending，费用随后进入 settled，金额为 USD 0.00225000。两处 price_version 均返回字面值 `:`。此原生执行的解释标注 partial，策略版本为空。对应证据为 PUBLIC_NATIVE。J04–J06 仍须以 J03 的同一请求号验收。[本轮 HTTP 字段](../../../contracts/release/backend/reports/20261119T075243Z-b80fe3170/results.json)

## 独立验收项

| 证据键 | 状态 | 验收结果或阻断 | 负责人 |
| --- | --- | --- | --- |
| OPENAPI、TYPESCRIPT | pass | 聚合 OpenAPI 逐字节相同。openapi-typescript 7.10.1 生成的 TS 声明与签入文件摘要相同 | BE-24、集成负责人 |
| SURFACES_HTTP、LIVE | pass | 公共与项目目录及 compare、公共状态、项目配额和任务列表返回符合断言的业务字段。BE-04 的向导、连接验证、启用、凭据轮换与撤销 HTTP 冒烟通过 | BE-04、07、15、16 astra，BE-05、18 sol |
| 策略草稿与模拟 | pass | 创建草稿 201。无图返回 410 SNAPSHOT_EXPIRED。用 BE-10 的 SnapshotWriter 准备合成候选图后模拟返回 200，execution、hold、receipt、钱包事件计数均未变。发布及回滚由模块测试覆盖 | BE-10 astra |
| SDK_HTTP、PROTOCOL_HTTP | fail | OpenAI Node 4.104.0 同步 Chat 和 SSE 通过，单个 stop。OpenAI Key 的 Chat、Responses、Gemini 走到本地上游。关闭 Messages 后按预期拒绝。文本模型的 embeddings 预期零发送，实得一次 POST 及上游 404。六种动作的只读项目 Key 全部 403，发送数为零 | 网关账号与模型绑定负责人、BE-01/05 |
| REGISTRY、MATRIX | pass | 346 种声明身份 × 29 种动作，共 10,034 格。另有 2,401 条代表模型端点绑定，889 条 implemented、1497 条 unverified、15 条 hard_conflict。绑定矩阵按声明校验绑定、原生拒绝、stream/tools 组合。真实网关结果见上一行 | 网关适配器负责人、BE-24 |
| FINANCE | pass | SIGKILL 前未落盘则 unconfirmed、missing_receipt、charge=null、余额 100。落盘后入队前、消费提交前、提交后丢确认三处恢复，重复并发投递后各有一个 outbox 和资金变动，余额 99.99775000 | BE-06、BE-11 astra |
| FINANCE_MODULES、SIGNED_CALLBACK | pass（补测） | 首轮构建标签选错，缺失测试的检查如实报 fail。同一 SHA 上以正确的 unit 标签在全新 PG16 补测，冻结价格恢复、丢确认、重复消费、预算并发等 40 个测试及子项通过，零跳过。支付模块另外验证签名、幂等和账务写入 | BE-11、BE-19 astra |
| CALLBACK_HTTP | pass | 原 Stripe webhook 同一签名事件投递两次均 200，订单完成，账户 10 → 11.25，仅一笔 USD 1.25 钱包变动。原入口对应的 provider event 表新增 0 行，独立的事件归档契约仍由 BE-19 复核 | BE-19 astra |
| ACCESS、ACCESS_MODULES、CONTROL_MODULES、STATUS_MODULE | pass | 同 workspace 跨项目导出、跨 workspace 导出/读取、跨用户 Key 均 404。两个实例预热后撤权，分别在 1.13 和 2.18 ms 返回 401，再读仍拒绝。模块断言覆盖详情、费用及审计字段投影、任务下载、Playground 和公开状态 | BE-01/15/16/21 astra，BE-14/18 sol |
| MIGRATION | pass | 从 b0bf5b9e1 的 334 个 SQL 迁到 354 个，保留历史 checksum，与空库清单完全一致，再跑无差异。快照余额 9.12345678 精确保留，恢复加首次升级 2.848 秒 | BE-23 astra |
| RESTORE | pass | BE-09 在独立 PG 恢复 4/4 行，预览至围栏恢复及核对 0.103 秒。静止快照 RPO 为 0 秒，快照后业务写入数为 0。丢确认后进入 unknown，已提交写入的确认丢失由模块用例验证 | BE-09 astra |
| CUTOVER_FIXTURE | pass | 验证旧库清单、扩展、检查点、隔离映射、脱敏和有限权限。混合版本双进程与切换后新交易的运行态回滚还须在最终候选演练 | BE-22、BE-23 astra |
| LOAD | pass | 300 秒，300 请求，0 错误。Chat 100，目录、权限、慢流、取消各 50。端到端 P50/P95 为 17.49/178.49 ms，200 个推理样本的开销 P95 为 55.69 ms。两秒冷却后待结算与预算占用均为 0，PG 连接 25 → 15，两实例 RSS 合计 291,212 → 306,244 KiB | BE-24 astra |
| PAIRED_2H、WIRE_PARITY、CUTOVER | pending | 同机基线和候选各 7,200 秒、P95 开销 ≤1.10 倍与冷却后资源回落。WS、compact、媒体及其余供应商的成功报文。BE-22/23 最终集成与独立发行签核 | BE-24、网关及发行负责人 |

[财务补测结果](../../../contracts/release/backend/reports/20261119T075243Z-b80fe3170/finance-recheck.json) · [注册表逐身份、模式、协议和动作](../../../contracts/release/backend/reports/20261119T075243Z-b80fe3170/registry.json) · [逐模型绑定与测试结果](../../../contracts/release/backend/reports/20261119T075243Z-b80fe3170/matrix.json) · [短跑与两小时复现命令](verification.md#完整隔离执行)

## 灰度与停止条件

| 队列 | 当前决定 | 进入条件与负责人 |
| --- | --- | --- |
| 内部 | HOLD | BE-01/13 与网关负责人完成失败项修复。BE-24 在最终候选重跑 J01–J06、协议、财务和两小时门禁。BE-22/23 及发行负责人签核 |
| 1% → 5% → 20% → 50% → 100% | 均未开放 | 发布负责人在每一级记录观察起止、流量和对账差异。检查前一级的协议、资金、请求身份、积压和撤权结果后逐级签核 |

发现未授权选路、无法解释的金额差、重复资金效果、持久工作无法恢复或协议偏离时，停止扩量。BE-23 记录当前权威写入方、检查点及 rollback floor，发行负责人按经验证的运行回滚方案保留切换后新交易。该候选未发生流量切换或生产回滚。
