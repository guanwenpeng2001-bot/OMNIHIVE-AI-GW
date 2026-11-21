# 安全暂存账号导入

`POST /api/v1/admin/accounts/data` 新增可选 `stage_inactive: true`。省略或传 `false` 时仍走原导入流程（active、既有代理处理和导入后回调均不变）。这是后端 API 能力，没有新增迁移工具或启用账号的 UI。

## 请求示例

沿用目标实例的管理员认证；安全暂存始终要求有效的 `Idempotency-Key` 和已认证操作者，同一批重试必须沿用同一个 key 和原始请求。旧导入保持原有行为。

请求必须只有一个完整 JSON 文档。模式选择前，按 DataImportRequest → DataPayload → accounts/proxies 数组中的结构体语义，递归拒绝已知字段的重复或大小写碰撞（包括转义后的同名键）。这包含三个顶层安全开关，以及嵌套的 proxy_dependency、proxy_key、credentials、extra 等字段，不采用“最后一个值覆盖”的行为。模型映射等字典不按结构体字段做大小写折叠，Model/model 两个模型名保持独立。`stage_inactive` 必须是 JSON boolean；合法省略或单个 false 继续走旧导入流程。

```json
{
  "stage_inactive": true,
  "data": {
    "type": "sub2api-data",
    "version": 1,
    "proxies": [],
    "accounts": [
      {
        "name": "laptop-staged-openai-01",
        "platform": "openai",
        "type": "oauth",
        "credentials": {
          "access_token": "<待传入的凭据，不要写入日志>",
          "refresh_token": "<待传入的凭据，不要写入日志>",
          "expires_at": "1795000000"
        },
        "proxy_dependency": "required",
        "concurrency": 1,
        "priority": 50
      }
    ]
  }
}
```

成功响应保留原计数字段，另有 `staged_inactive: true`。不返回凭据或账号对象。

## 强制行为

- 整批使用已有账号写事务。首次账号 INSERT 即 `status=inactive`、`schedulable=false`；不经历 active 状态。任一条失败，账号、密文、引用、绑定、审计和调度 outbox 一起回滚。
- 不查询、创建、复用或更新代理；不绑定任何分组（包括平台默认分组）。即使显式传 `skip_default_group_bind: false` 也不会绑定。
- 不提取 ID token 信息、不设置隐私、不安排 Grok 探测、不注册导入后的网络回调。停用账号不进入后台 OAuth 刷新及 Grok 对账候选。数据库内的调度 outbox 是正常的原子写入，用于发布停用状态，不是网络探测。
- 必须显式声明每个账号的 `proxy_dependency`：`none` 表示已确认无需代理；`required` 表示需要代理；`unknown` 表示源端依赖尚不确定。后二者保存为 `extra.staged_proxy_dependency`，未配置目标本地代理时，统一出口解析返回 `STAGED_PROXY_CONFIGURATION_REQUIRED`，不会用空代理 URL 假装直连。普通 extra 编辑不能清掉这个标记。配置目标本地代理/代理组后按该本地配置路由；其 fallback 策略需要另行审查。
- 该保护也作用于管理端主动用量查询、Cursor session/key exchange、CN 余额代理解析以及 HTTP/带 TLS 配置的发送边界，含无代理组的账号。`inactive` 本身不是网络沙箱；`required/unknown` 在代理待配置时不能读取或写入派生 Cursor session，也不能利用调用方传入的 proxy URL 绕过依赖检查。`none` 和普通账号继续保留原有管理查询行为。
- 专用入口同样在凭据消费前检查：Google/Grok 账号代理解析、各平台账号刷新、共享 OAuthRefreshAPI、五类 token provider、OpenAI 专用 quota/reset-credit（含凭据母账号解析）、Grok/CN 配额以及隐私请求。对未配置代理的暂存账号，token 缓存读/写/删/加锁及上游客户端创建均不会发生；不依赖它们是否经过 accountHTTPUpstream。
- 不能根据导出数据缺少 `proxy_key` 推断 `none`：原导出接口在 `include_proxies=false` 时会丢失代理关联信息。无法确认时用 `unknown`。

## 允许的数据与拒绝条件

每批 1–1000 条。调用方必须先按源账号身份去重；接口按 **platform/type + 规范化端点 + 上游 tenant/project 等身份选择字段 + 敏感凭据集合** 检测批内重复。三个 Kaggle 账号共用 API key、base_url 不同可以一起导入，也允许相同显示名称。相同端点、身份和凭据即使改名或修改模型映射，仍会拒绝。

端点比较统一 scheme/host 大小写、默认端口、末尾斜杠；保留路径大小写、路径内部分隔和转义斜杠。Compatible provider 使用与转发相同的纯本地端点解析，因此 Kaggle 的站点根和其 `/v1` 前缀等价，也会考虑 `api_base_urls` 的实际覆盖端点。不进行 DNS/HTTP 请求。不跨请求或跨目标库自动合并账号；令牌轮换后的身份不能仅凭凭据可靠识别。本接口只创建新账号，不更新已有账号。

身份选择字段均为最长 256 字节的无控制字符字符串，比较时去首尾空白，空值等同未指定：tenant、tenant_id、project、project_id、organization_id、org_id、account_id、chatgpt_account_id、chatgpt_user_id、chatgpt_workspace_id、client_id、region、aws_region、aws_access_key_id、antigravity_project_id、zhipu_organization、zhipu_project。它们属于上游身份；不接受目标本地 workspace/group/proxy ID。

Cursor 例外：其实际转发不使用上述 tenant/project 等选择器，因此暂存时拒绝这些字段（包括空值）。native Cursor 还拒绝 base_url/api_protocol/api_base_urls；**relay（upstream）允许 api_base_urls**，复用 cursorRelayURL 按 api_protocol、模型发现和各转发路径计算实际 URL。已被覆盖的根地址及当前协议不使用的覆盖地址不会制造不同指纹；不同的有效覆盖端点会区分。三个不同 base_url 的 Kaggle 及其他平台既有端点/身份规则保持不变。

Cursor 认证字段额外按类型限制：apikey/upstream 只接受 api_key；oauth 只接受 access_token 与 refresh_token。无关 id_token、session_key、云密钥等敏感字段即使属于全局敏感字段清单，也不能附加到 Cursor 账号改变指纹，提供时整批拒绝。

允许原账号结构中的 name、notes、platform、type、credentials、concurrency、priority、tpm_limit、rate_multiplier、expires_at、auto_pause_on_expired，以及新增的 proxy_dependency。name/notes 等是明文安全元数据，不应放密钥。`data.proxies` 必须是空数组；不要上传源代理。

暂存使用严格 JSON schema：未知字段、分组/账号绑定 ID、代理 ID/代理组 ID、源主密钥等直接拒绝。任何非空 proxies、非 null 的 proxy_key（含空字符串）都拒绝；extra 仅允许下表中的静态配置。不静默丢弃这些依赖，不接受原备份直接整包透传；Composite 分组同样不得迁移。

credentials 仅接受现有敏感凭据清单中的非空字符串，排除创建流程本来就会删除的 password、sso_token、sso、sso-rw、clearTextPassword、cookie。支持 access_token、refresh_token、id_token、api_key、session_key、agent_private_key、private_key、AWS secret/session token 及已有管理查询密钥等。service_account/service_account_json 也必须是字符串；不支持嵌套对象形式的凭据。

原非敏感字段 expires_at、expires_in、token_type、scope、email、plan_type 继续支持。base_url 及协议端点必须是最长 2048 字节的 HTTP(S) URL，禁止用户认证、查询串、fragment、无效端口和控制字符。具体允许项以 `ValidateStagedImportCredentials` 为准。

只读核对范围是仓库前端配置构造器、后端消费逻辑、本地 Kaggle 合同测试，以及 post_activation 下三个明确标为 synthetic 的 Cursor/Grok 样本的字段名（未输出凭据值）。这些样本包含 model_mapping 和源目录/基线等 extra。没有读取生产或 Magpie 的真实账号清单，因此不能声称已盘点全部实际字段。发现并补齐以下白名单：

| 位置 / 字段 | 类型及边界 |
| --- | --- |
| credentials.model_mapping / compact_model_mapping | 扁平字符串映射，最多 1024 项；键和值各 1–256 字节，禁止空白、控制字符及嵌套对象；原样保存模型名和映射 |
| credentials.account_mode | payg / coding / go / zen；创建时继续校验该平台是否支持 |
| credentials.api_protocol | adaptive / chat_completions / anthropic / responses；创建时继续校验平台协议 |
| credentials.api_base_urls | 仅 chat_completions / anthropic / responses 三种协议到安全 URL 的映射 |
| credentials.billing_plan / client_version | 非空无控制字符字符串，最多 128 字节；商业计划继续走平台校验 |
| credentials.machine_id / mac_machine_id | 仅 native Cursor（apikey/oauth），必须是恰好 64 位十六进制字符串；沿用既有非秘密客户端标识的存储语义，不是会话 token |
| credentials.protocol_rules | 最多 64 个仅含 pattern/protocol 的对象；pattern 最长 128 字节、仅允许末尾单个星号；协议为三种 native 协议 |
| credentials.openai_capabilities | 1–2 项字符串数组，仅 chat_completions / embeddings |
| credentials.intercept_warmup_requests / pool_mode / custom_error_codes_enabled | boolean，不接受字符串或数字代替 |
| credentials.pool_mode_retry_count / account_scheduling_threshold | JSON 整数，分别 0–10 / 1–100 |
| credentials.pool_mode_retry_status_codes / custom_error_codes | 最多 100 个 400–599 的整数状态码 |
| extra.custom_base_url_enabled / mixed_scheduling / openai_passthrough / openai_oauth_passthrough / anthropic_passthrough | boolean，仅保存静态选择；不会使暂存账号启用 |
| extra.openai_compact_mode | auto / force_on / force_off |
| extra.openai_responses_mode | auto / force_responses / force_chat_completions |

Kaggle 的配置片段示例（放入每个账号的 credentials，三个账号分别填写不同端点）：

```json
{
  "api_key": "<通过安全请求传入>",
  "base_url": "https://notebook-a.example.invalid",
  "account_mode": "payg",
  "api_protocol": "chat_completions",
  "model_mapping": {"local-qwen": "Qwen/Qwen3.5-9B"}
}
```

下列字段仍不支持。**接口会整批明确拒绝，不会静默跳过；来源整理时必须明确剔除或在目标端重新配置。**

| 字段类别 | 拒绝原因 |
| --- | --- |
| header_overrides / header_override_enabled、cookie、SSO/password | 自定义请求头可能含未识别密钥；其余是既有流程会清除的临时认证材料，不能承诺安全静态加密和完整迁移 |
| upstream_model_catalog / upstream_model_metadata / model_availability / model_probe_budget / upstream_model_catalog_state | 源账号的目录、探测和能力观察证据，不能当作目标实例证据 |
| upstream_model_allowlist / model_selection_baseline_frozen | 与源目录/基线关联，原创建仓库也会剥离；需要来源确认后整理为显式 model_mapping，不自动扩大可用模型 |
| model_capability_defaults / model_capability_release | 含源身份指纹、版本、发布或作用域绑定，需目标端重新确认 |
| temp_unschedulable_*、探测/隐私/刷新开关与快照、额度用量、错误和冷却状态 | 跨实例运行态或可引入后台行为；不能随静态配置迁移 |
| cached_access_token / cached_expires_at | 派生会话缓存不迁移；native Cursor API key 不应携带会话凭据 |
| groups/group_ids、任何代理/代理组绑定、源 credential references、主密钥、未知字段 | 实例本地关系或未审查结构，避免误接和泄漏 |

### Native Cursor 暂存

已补齐小范围的机器标识白名单，继续使用原 `NormalizeCursorAccount`，不改变默认导入、授权或刷新流程：

- `type=apikey`：保留合法的 machine_id/mac_machine_id；未提供的标识由既有逻辑在目标端本地随机生成，不发起网络请求。API key 仍以目标实例的加密引用保存。
- `type=oauth`：必须同时具备 access_token、refresh_token 和两个原机器标识；缺失材料在整批写入前明确拒绝，不合成 OAuth 身份。只检查结构，不声称令牌当前有效。
- 两个标识仅在 native Cursor 中允许；Cursor relay（upstream）和其他平台拒绝携带它们。改变机器标识不能绕过重复凭据检查；native 账号也不能附加无关敏感字段绕过去重。
- 模型映射照常保存，账号首次写入仍 inactive、不可调度、无分组/代理绑定，不进入自动刷新候选。派生缓存继续拒绝。

本地 synthetic OAuth 样本未提供机器标识，依然不能直接当成完整授权导入。真实迁移前必须通过已授权来源取得完整材料；这项修复不执行该获取操作，也不自动启用账号。

源凭据引用一律拒绝（不是复制后再解绑），包括实际字段 `credential_secret_reference`、`credential_secret_version`，以及 `credential_ref` 等未知字段。若源为引用存储，需要经源端授权导出得到已解析的实际凭据，由调用方构造上述最小请求；不要把源引用/主密钥混入请求。

## 加密与安全边界

复用目标实例现有的持久加密设施（`TOTP_ENCRYPTION_KEY`）。不修改或从请求接收主密钥。没有已配置的持久密钥时返回 `encryption_key_required`，不降级明文。

首次账号 INSERT 仅含批准的非敏感配置。随后在同一事务中生成新的目标本地 UUID 引用，用目标加密器加密敏感凭据，写 credential_secrets，再向 accounts.credentials 写 `credential_secret_reference` / `credential_secret_version`。密文信封绑定目标 workspace_id/account_id/reference/version。无需先提交账号再调用独立的 Convert 事务。

这是敏感字段的静态加密，不是整个请求端到端加密：实际凭据仍在受认证的请求和进程内存中短暂出现，非敏感配置和安全元数据仍是明文。应使用受保护连接，调用方及反向代理不得记录原始请求。审计中间件按真实路由对管理员和工作空间的 `accounts/data` 导入整体省略 body，覆盖暂存、旧格式、无效和超大请求，不依赖客户端 stage_inactive 值。省略分支不读取/截断请求体，也不生成 SubmittedConfigurationDiff，避免未知字段值或字段名进入审计；handler 仍收到原始完整请求。其他路由携带同名字段不会触发整体隐藏，继续保留普通审计证据并按既有规则脱敏密钥。操作者、方法、路由、请求 ID 和响应状态仍记入审计；工作空间的账号导出也记录敏感读取事件。凭据审计仅记字段变更、不记值；暂存错误不回显底层驱动/加密异常文本。

本模式不验证凭据有效性、不自动启用，也不提供批量启用或代理匹配功能。启用前需要另行审查目标代理及 fallback、分组和凭据生命周期。引用账号激活后可以进入后台刷新候选：批量读取会校验并解密目标实例的凭据，真实 refresh token、账号状态和冷却条件仍需满足；损坏或撤销的引用不会被当作有效刷新材料。不要把“暂存成功”当作“可安全启用”。

## 引用账号的批量读取与刷新

批量读取按账号、工作空间、引用和版本验证后集中解密，保留持久态的 CAS revision；普通批量读取遇到损坏、撤销或不匹配的引用会整批失败。批量用量查询仅在明确的凭据引用错误时逐账号恢复，最多处理 1000 个去重账号，共用 20 秒预算；数据库和上下文错误仍使整批失败。后台 OAuth 扫描先按持久态筛选，再解密并确认真实 refresh token、状态和冷却条件；扫描游标来自原始 ID 页，过滤后的空页仍能推进。

引用账号的 scheduler full 与 metadata 缓存均保留引用标记并在序列化时剥离明文秘密。此保护只覆盖新写入；升级前已存在的不安全缓存必须通过受控重写或删除对应缓存项后再核验。

## 安全暂存事务回执

迁移 `289_add_staged_account_import_receipts.sql` 包含专用成功回执表 `staged_account_import_receipts` 和共享模式占用表 `account_import_key_modes`；不新增 290，288 保留给既有终态索引迁移。两表使用迁移 SQL 和事务内原生查询，不生成 Ent 实体。仅 `stage_inactive=true` 绕开通用 coordinator；普通导入先检查共享占用，再沿用原 coordinator、active 创建及同模式重试行为，其他写入不变。

两个模式都先使用 credentialDrafts 的 Ent client 开启 READ COMMITTED 短事务，在同一 workspace/规范化 key 的 PostgreSQL 事务 advisory lock 下检查并持久化模式占用，提交成功后才开始各自业务流程。mode、actor 和 payload 都不参与锁键。占用表唯一键为 `(workspace_id, idempotency_key_hash)`，绑定首次 actor 和 staged/legacy 模式；没有请求内容或请求摘要。

占用提交后不可改模式，也没有 TTL 或资源删除级联。后续账号事务失败、进程退出、coordinator 写成功状态失败，都不释放该占用；同模式/同 actor 可继续原重试流程。占用提交前回滚不留下占用，任何模式可重新争取；占用提交返回异常则报 `ACCOUNT_IMPORT_KEY_OUTCOME_UNKNOWN`，不执行业务，也不假设占用未提交。已有同模式占用只证明模式归属，不伪造 legacy 成功回执。

暂存业务另开 READ COMMITTED 顶层事务，在相同 advisory lock 下，通过 TxFromContext 将账号、密文、审计、outbox 和成功回执绑定到同一提交。拒绝嵌入另一个尚未提交的事务。锁等待上限 5 秒，超时返回可重试的 503，不绕锁执行。两个新表的唯一约束提供数据库最终保护。

回执只明文保存 workspace、操作、key hash、actor、版本和时间。AES-GCM 信封包含用途、版本、完整行身份、POST 方法、规范化请求摘要和固定计数响应，不包含请求或 token。摘要只存在于内存及受认证密文，不交给 SQL、通用 coordinator 或日志。复用持久加密器；缺密钥、临时密钥、解密失败、篡改或未知版本都拒绝执行。密钥轮换需保留解密能力或先迁移回执密文。

指纹对完整解码后的 DataImportRequest 做确定性 JSON 序列化，在生成 Cursor 标识等创建操作前冻结。JSON 空白和对象键顺序不影响重放；数组顺序、凭据、配置、名称和请求选项都参与比较。可选指针字段的缺失与 null 按既有 decoder 等价；显式 false 与未提供可以不同。后续修改解码/默认值语义必须维护 v1 合同，不得覆盖旧回执。

actor 来自认证中间件，workspace 来自经授权的服务端上下文；body/header 不能自行指定。全局管理员沿用默认 workspace=1。同 workspace/key 换 actor 或模式返回 409；暂存已成功请求换 payload 也返回 409；不同 workspace 隔离。重放仍经过认证授权，返回原计数及 `X-Idempotency-Replayed: true`，不再次产生账号写入或回调。

成功回执没有 TTL，不参与通用清理，不随账号、actor、workspace 删除级联清除。账号删除或修改不会释放 key。提交后响应丢失、进程退出、通用 coordinator 不可用，都不影响已提交回执的重放。Commit 错误返回 `STAGED_IMPORT_OUTCOME_UNKNOWN`，不声称没有写入；以相同 key 和请求重试。回执写入失败时账号事务一起回滚。

### 切换限制

新协议不能重建旧版本丢失的提交证据。暂存尚无占用/回执但同 scope/key 有旧 coordinator 记录时，返回 `STAGED_IMPORT_LEGACY_OUTCOME_UNKNOWN`。普通导入采纳旧 key 时，在共享锁内核对原 coordinator 指纹；错误 actor 或 payload 不得抢先留下模式占用，以免阻断原操作者重试。只在内存中计算兼容指纹，不把裸请求摘要写入占用表。对新协议处理过的 key，模式保留不受普通幂等 TTL 清理影响；对升级前已经清理的历史记录，仍无法重建证明。

普通导入没有 key 时仍遵循原 ObserveOnly/RequireKey 策略；有 key 时即使 coordinator 禁用，也必须经过模式占用检查。普通导入的既有凭据落库和 coordinator 指纹格式不在此修复中改变；新增两表不存明文 token。普通导入提交后 coordinator 失败时的同模式 processing/TTL 语义保持原样，本修复只保证不能借此切换到另一模式；不把普通导入升级为永久成功回执。

上线前必须排空两个模式的旧导入写入并统一切换实例；旧版本不取得共享锁，不能混跑或回退后继续接收导入请求。历史批次，尤其记录已清理者，须人工核对；本补丁不回填旧摘要、不合并已有账号。新请求使用从未使用的 key，不能换 key 重试结果不明的旧导入。回执必须读写同一 PostgreSQL 主库，加密密钥必须跨副本一致。

### 验证入口

仅指向可丢弃 PostgreSQL，设置 `CREDENTIAL_SECRET_TEST_DSN` 后运行：

```powershell
go test -tags unit ./internal/repository -run '^Test(StagedReceipt|AccountImportMode)Postgres' -count=1
go test -tags unit ./internal/handler/admin ./internal/service ./internal/repository ./internal/server/middleware -run 'Test(Staged|AccountImportMode|DataImport|ImportData|ExportData|AccountWorkflowImportFailure|AccountCredentialDraft|Idempotency|RedactAuditBody|.*Audit|ConfigurationDiff|SubmittedConfigurationDiff)' -count=1
go test -tags unit ./cmd/server -run '^$' -count=1
```

专项覆盖真实 PostgreSQL 并发、回滚、唯一约束、锁等待、workspace/actor 隔离、响应写失败、账号软删除后重放，以及子进程在提交前/后直接退出。提交连接异常由 Ent commit hook 在真实提交前/后注入，未使用网络断连代理。数据库相关整行、应用日志和测试 PostgreSQL 日志检查完整 synthetic token 与裸请求摘要泄漏。未设置 DSN 的 skip 不能算数据库验证通过。


跨模式专项覆盖 staged→legacy（false/省略）、legacy→staged、两个方向的受控并发、不同 key、workspace/actor 隔离、禁用 coordinator、旧成功记录采纳、错误 actor/payload 不能抢占旧 key、占用提交前后进程退出、占用提交结果不明、业务回滚后同模式恢复、legacy MarkSucceeded 失败后仍不能切换模式。

数据库崩溃恢复用例分两次运行 `TestAccountImportModePostgresDatabaseRestart`：使用相同 `IMPORT_MODE_DB_RESTART_RUN` 标识，先设置 `IMPORT_MODE_DB_RESTART_PHASE=create`，仅对专属可丢弃集群执行 immediate stop/start，恢复后以 `replay` 再运行。测试代码本身不控制 PostgreSQL 进程。
