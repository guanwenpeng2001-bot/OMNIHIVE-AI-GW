# 新平台运行时与验证边界

这份说明描述本地实现合同，不是厂商账号实测报告。五个平台按火山、千帆、混元、Mistral、Bedrock 的顺序接入；未调用真实厂商账号、未部署，也未迁移旧账号。

| 平台 | 账号及已接入口 | 目录与资格 | 仍未验证 |
| --- | --- | --- | --- |
| 火山方舟 `volcengine` | API key；`/v1/chat/completions` 转发到保存的 `/api/v3` runtime，保留精确模型/部署目标 | 自动发现不受支持；保存具体 mapping 目标后明确声明可用性 | 厂商 SSE 终态、工具增量、细粒度错误与余额/额度；本地 SSE fixture 不代表厂商回执 |
| 千帆 `qianfan` | 国内 API key；`/v1/chat/completions` 使用 `/v2` 上游 | `/v2/models` 保留输出上限、模态等字段；未知分页或损坏响应不覆盖完整快照 | 真实账号可调用范围、国内流终态、细粒度错误与余额/额度 |
| 混元 `hunyuan` | Bearer API key；默认广州 TokenHub，白名单接受广州/新加坡主域及备用 `.cn`，已存旧兼容端点仍可读取；Chat 非流及 SSE 共用上游管线，不支持 TC3 | TokenHub `GET /v1/models` 适配器按 `status: online` 发现，空/失败显式错误且不回退旧名单；生产发现入口接线待共享 runtime 任务合并，当前入口仍为候选模式 | 无真实凭据；以脱敏 HTTP/SSE 样本验证尾包用量和错误后已观察用量保留，缺失用量不得零结算；两站 Key 不互通，线上权限/额度未验证 |
| Mistral `mistral` | API key；`/v1/chat/completions` 的文本/函数工具、非流和 SSE 管线 | 模型卡保留未知/false/空集合区别；模型卡不是其它 ID 的调用授权 | 真实账号、细粒度流增量、错误负例与余额/额度 |
| Bedrock `bedrock` | 独立 `bedrock` 账号类型；区域 IAM 或 runtime API key；`/v1/messages`，以及既有 Chat/Responses 文本桥 | 区域 foundation、availability 和分页 profile 目录；列表可见不等于 Invoke 权限，profile/ARN 需要具体目标证据 | 真实 AWS 权限、区域/profile/ARN 运行回执、余额/额度 |

## 资格、用量与存储

- 声明管理接口为管理员的 GET、PUT、DELETE `/api/v1/admin/accounts/:id/model-availability`。必须先保存账号；声明仍受身份/规则修订、映射、出口、有效期、状态及 allowlist 约束，不修改 discovered 或价格。
- 前四个平台只开放已实现的 Chat Completions 入口。Bedrock 使用 Messages 原生证据及已有文本桥。新平台的专用媒体、embedding、语音、WebSocket、compact、count-tokens 等未实现入口不会因登记或声明而获得授权。
- Mistral 在共享请求处理之后移除未获合同确认的 `stream_options`。输入/输出 token 数缺失、null、仅有一侧或为负时属于 unknown，非流及流式均不得产出可信零用量结算结果；明确的 `0/0` 与缺失值不同。已发送请求不因此自动免费重放。
- 五个平台默认不开价格同步，不推断上游余额、免费资格或无限额度。被动监控、用户平台配额和调度快照使用公共平台集合；监控存储枚举兼容全部具体平台，主动探测/上游额度轮询仍按实际能力门控制。
- 旧 `anthropic + bedrock` 账号保留原平台、分组、映射与价格语义。独立 Bedrock 平台不推测或改写保存的 profile/ARN 目标。
- 新平台没有审核过的模型预设时明确显示未配置，不使用其它平台的默认模型列表补齐。

## 只读目录与开关

账号目录的 `display_metadata` 是独立的只读展示投影，范围为已发现模型与具体 mapping 目标的并集，不写入同步快照、审核摘要、CAS 或授权集合。`MODEL_CAPABILITY_OFFLINE=1` 只停止公开能力目录的网络补缺，已审核资源仍可展示。

`bounded_model_probes_enabled` / `BOUNDED_MODEL_PROBES_ENABLED` 默认 `false`，启动前交给已有 runner，变更后重启生效。代码提供开关不代表已获真实上游探测授权，本次未在运行配置中启用。

Antigravity API-key 目录仅使用保存的同一 `/antigravity` 兼容网关：`/v1/models` 与 `/v1beta/models`。后者依据本项目 Google 鉴权路由使用 `x-goog-api-key`；不将此合同外推到官方 CloudCode 或其它主机。任一路失败时，成功部分只能预览，不生成可应用的审核令牌，完整历史快照保持不变。

## 可复验入口

后端维护测试包括 `TestVolcenginePlatformFlow`、`TestQianfanPlatformFlow`、`TestHunyuanPlatformFlow`、`TestMistralPlatformFlow`、`TestBedrockPlatformRuntime`、`TestBedrockLegacyCompatibility`、`TestAccountModelCatalogAntigravityAPIKeyGemini` 和 `TestAccountModelCatalogEndpointSyncFixtures`。它们使用明确标为合成的 HTTP/流 fixture，不能替代真实上游验收。

增量迁移以隔离 schema 测试新建、旧行保留、重复执行及 18 平台精确集合；禁止建删数据库的外部资源模式下，全新数据库的完整历史迁移链仍是未覆盖项。首次真实启用前需另有账号、允许动作/预算和运行环境授权。
