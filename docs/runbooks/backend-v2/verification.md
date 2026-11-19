# BE-24A 后端发布验证

固定准备基线为 `2e9752eb95a347ce78ff7ad04ac40424a2d66aa6`。最终候选须包含 BE-22、BE-23 及独立发行验证，由发布负责人记录候选 SHA、镜像 digest 和迁移版本。范围与门槛见 [BE-24](../../refactor/backend/WORK_PACKAGES.md#be-24--full-scope-release-verification-and-cutover-gate--astra) 和 [API 契约](../../refactor/backend/API_CONTRACTS.md)。

## 只读检查

从干净 clone 根目录运行。需要 Bash、Git、Python 3 和满足 `backend/go.mod` 的 Go 工具链。Go 可从本地缓存或构建依赖代理取得依赖。生成器只写临时副本，退出时清理。单独运行 `check-openapi.sh` 时，任何生成错误或字节差异都会返回非零。

```sh
./contracts/release/backend/verify.sh
```

脚本返回 0 表示这次执行的只读检查全部通过。隔离检查逐项输出 pending。发布时，每个适用项都需要候选版本证据。pass 表示预期与实测一致，fail 表示实测偏离或检查执行失败，pending 表示未执行或缺少证据。每个状态都要写原因。

## 六步核心旅程

用同一项目、同一调用串起六步。[机器可读 fixture](../../../contracts/release/backend/journey.json) 包含方法、路径、请求体、状态和响应断言。默认执行使用合成响应。产品验证时在一次性候选部署中使用固定响应的本地上游，并禁用外部供应商、支付回调出站和代理回退。使用合成用户、种子数据、测试资金和本地测试身份。任何一次性 Key 值均仅在隔离环境内存中流转，证据中只保留 <redacted> 标记。

| ID | 操作及通过标准 | 产品状态及原因 |
| --- | --- | --- |
| J01 | 在授权项目查询可用模型，确认模型身份、可访问、可路由、已定价。并验证匿名目录投影及 compare | pending · 待部署候选 |
| J02 | 创建仅允许示例模型和 text.chat 的 Key。用同一幂等键重复创建，返回同一 ID 且 credential 为 null。列表和日志无明文 | pending · 待部署候选 |
| J03 | 通过 Playground 同步调用，验证原生 Chat 响应、请求号及详情链接。补跑标准 SDK 和 SSE，核对单终态、断开、取消及状态查询 | pending · 待部署候选 |
| J04 | 以 J03 的请求号读取详情，核对 workspace、project、Key、attempt、用量来源及价格版本 | pending · 待部署候选 |
| J05 | 使用包含请求的 UTC 半开窗口及 USD 过滤，核对详情、charges 和账本。金额与币种一致，账本支出同精度反号，并核对 settlement ID 及唯一 effect | pending · 待部署候选 |
| J06 | 从请求详情读取 route-explain，定位被排除候选、原因及策略版本。对失败请求补测原生错误及受权诊断，核对未授权角色无法重放 | pending · 待部署候选 |

收集六个响应后，使用下方命令运行同一套断言。捕获文件以 J01 至 J06 为键，每项形如 `{"status":200,"body":{"code":0,"data":{}}}`。按 fixture 种子选择对应记录。用同一份一对一映射将运行生成的 ID 和链接路径规范化为 fixture 值，保留跨响应的关联和字段值。使用候选时钟对应的账务窗口。先验证 Key 首次呈现和重复响应，再对首次响应中的 credential 原地脱敏。应保存经过审核的脱敏证据引用和捕获时间。

```sh
python3 contracts/release/backend/check-journey.py --observed /tmp/be24a-responses.json
```

采集样本覆盖 JSON 主线。表中附加场景和原生路由使用单独证据。完整发布旅程还包括计划 10.3 的接入上游、发布路由与凭据轮换，可使用下表的 HTTP 项记录证据。

## 隔离检查

使用独立网络中的一次性 PostgreSQL 和 Redis，逐轮记录部署、数据版本和配置摘要。先确认隔离实例、数据合成来源、可控时钟及无外部出站，再执行写入、断连、重启或恢复。旧版和候选需要共享协调命名空间的场景，只共享该轮专用的测试实例。既有测试可补充作证，并记录实际运行的测试名、跳过数和依赖类型。使用 miniredis 的用例只覆盖其能证实的行为，真实 Redis 恢复和失效传播需单独测量。

| ID | 检查及通过标准 | 状态及原因 |
| --- | --- | --- |
| HTTP | 验证六步及 wizard 验证/启用、轮换/排空、模拟差异/发布/回滚、公共状态、任务及产物。对照实际 HTTP、聚合 OpenAPI 和生成 TS。对原路径运行原生 SDK 与签名回调 fixture | pending · 待集成候选和本地上游 |
| MATRIX | 从 [协议矩阵](../../gateway-platform-protocol-matrix.md) 和候选适配器注册表逐项展开平台 × 账号/认证/部署模式 × 模型 × 协议 × 动作，每个组合记录允许条件及实测。覆盖 Chat、Responses POST/WS/compact、Messages、Gemini generate/count、已保留媒体与异步动作，以及非流式、SSE、工具、错误和拒绝路径。原生或桥接行为匹配声明，拒绝项无上游调用 | pending · 待候选能力清单和实测 |
| FINANCE | 分别在首次 receipt 前、持久化后入队前、消费提交前及提交后确认前注入故障。再测消费重复/并发、价格切换、未定价和预算竞争。落盘前未知需可对账（charge 为 null），落盘后恢复沿用快照/指纹，无重发推理，唯一金融 effect 与正确净额 | pending · 待真实 PG/Redis 和故障点 |
| ACCESS | 用不同 workspace、同 workspace 不同项目和不同动作权限，测直接 HTTP、内容/费用投影、导出下载、异步任务和缓存。不可见对象 404，可见无动作 403；校验公共响应不带私有字段；双实例预热缓存后测撤权传播 ≤60 秒 | pending · 待多租户、多实例与真实缓存 |
| MIGRATION | 演练旧版 → expand → 可续跑回填 → 混合版本读写 → 单权威切换 → 应用回滚。比对归属、隔离异常记录、历史迁移 checksum、receipt 指纹、金额和新交易。向另一套一次性实例恢复快照，测 RPO/RTO，回滚保留新增合法交易 | pending · 待 BE-22/23 与旧版快照 |
| LOAD | 同一机器、数据、请求比例、并发、随机种子及本地延迟分布做基线和候选各 2 小时。包含慢流和取消，记录吞吐、错误分类、P95 网关开销、资源与积压。目标候选 P95 ≤ 基线 × 1.10，冷却后租约、连接、队列和资源回到基线 | pending · 待成对实测和阈值记录 |

## 证据模板与灰度

复制 [evidence-template.md](evidence-template.md) 到核准的证据位置，按检查 ID 关联脱敏摘要。发现未授权选路、无法解释的金额差、重复金融效果、不可恢复的持久工作或协议偏离时，停止扩量，记录负责人与回退决策。证据支持后依次进入内部、1%、5%、20%、50%、100% 队列，并逐级记录观察窗和签核。
