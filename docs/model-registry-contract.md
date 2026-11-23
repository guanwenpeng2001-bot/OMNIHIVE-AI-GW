# 模型注册表、公开协议与运行一致性合同

本文列出注册表与公开协议的设计约束。已支持的端点见[协议矩阵](gateway-platform-protocol-matrix.md)。

## 运行事实源与不可变快照

服务层必须组装不可变 `ModelRegistrySnapshot`，供预设、目录、资格判断、能力展示和价格关联使用。它是既有来源的一代事实视图，不是第四张人工维护的模型表。

| 来源或标识 | 必须承担的职责 | 不得承担的职责 |
| --- | --- | --- |
| 平台预设 | 规范身份、同平台/托管画像候选、公开模型名和别名基础信息 | 证明某凭据获得授权，或直接填充公开可用目录 |
| 签名能力 catalog | 按准确身份补 task、输入输出模态、限额、推理控制、生命周期及来源证据 | 根据名称启发式授予能力，或把 unknown 批量改成 complete |
| 当前身份发现、声明、探测 | 账号资格证据；保留 identity/rules/snapshot revision 和来源 | 把另一账号、上一凭据或另一地域的发现当成当前授权 |
| 既有价格解析器与有效覆盖 | 按同一模型身份关联有效销售报价与成本；多单位、条件价卡必须保真 | 用同名异画像价格代替，缺价按免费处理，或对用户输出内部成本 |
| 组/Composite/Key 策略 | 在事实和绑定之上裁剪授权范围 | 改写原始能力事实、供应商 usage 或历史报价 |

模型身份必须区分平台、托管类型、实际 hosting provider、协议/画像、region/project/deployment 与凭据拥有者；内部部署标识不能作为公共 canonical 的替代。mode、套餐、协议必须正交，不得将 Coding 端点的权限等同开放 API。公开同名的多个绑定可以聚合，但不能抹去内部身份区别。

快照必须携带可追溯的制品 digest、release pin/generation 与来源 revision。一次请求必须冻结同一快照引用及对应报价；不得在调用中途混用新旧代事实。价格缺失、明确免费和有价必须区分；已冻结的历史收据不得随新包重算。前端必须先迁移到现有预设 API，再删除 Go 独立名单及 `admin_group.go` 的 Claude default；未知画像无候选必须返回空态并允许合法手工输入。

## 批准包重载与离线运行

### 已实现的逐字段覆盖与抑制合同

离线 `reviewed.json` 的 `overlays` 只定位已有的准确 `(profile, model_id)`，仅替换明确列出的字段，不新增重复身份；未替换的同记录字段保留旧来源、哈希与观察日期。覆盖字段分别携带 `source`、`content_hash`、`observed_at`、`confidence` 与 `proof_scope`，查找或合并时间不能冒充观察时间，也不能把历史 `observed` 提升为新 `verified`。

显式 `suppress_fields` 生成受 release pin 绑定的 `suppressed_fields`，仅允许 `context_window`、`context_limit_kind`、`max_input_tokens`、`max_output_tokens`。这些字段在价格基线与预设补全后仍须缺省，旧值保留在原始输入和 coverage 证据中；普通缺省仍允许原有补全，包括 `gpt-5-pro` 的既有行为。抑制按准确画像与模型生效，不能跨 CN/global、API/Coding 或原生/托管画像传播。未知或受抑制数字及其公开字段证据均省略，不填零。

预设补全只有在 confirmed 官方审阅记录的 platform、预设 profile、准确 model ID、field 与当前来源 URL 全部匹配时，才使用该字段的捕获哈希和日期。沿用模型原始来源的历史字段保留旧哈希、旧日期及 `observed`；其他无法匹配的 URL 保留未知哈希/日期，无字段 URL 时仅标识预设包来源，不能借用兄弟字段、其他模型或画像的新捕获。候选事实与逐字段可信度均不证明账号权限。

运行时重载采用管理员触发的批准本地包入口，保留既有签名、release pin 和审批链。不接受任意远程 URL，不以文件变化绕过审批，不新增自动启用资料的通道。定时采集只生成待审差异。

1. 必须完整校验制品、签名、pin、防降级规则、身份关联和解析结果，再原子替换快照；半包不得可见。
2. 并发 reload 必须按 generation 串行发布或拒绝过时提交；失败必须保留 last-good，已开始请求继续使用冻结引用。
3. 每实例必须部署同一批准制品，通知只提示重载/失效，不负责复制文件；实例缺包或校验失败不得声称已切换成功。
4. 回滚旧内容必须按防降级规则签成新的 generation，不允许关闭 pin 或直接接纳旧代。断网重启必须仍能加载已批准离线包。
5. “全量”必须带来源日期、地域、套餐、托管身份和对账范围；不得拿旧报告 498/407 等数字充作验收目标，不得编造私有部署 ID。

验收必须覆盖坏签名、旧 generation、并发重载、在途请求、双实例制品不一致、断网重启及批准回滚，并证明错误路径不会替换 last-good。

## 发现、候选、资格与公开集合

| 集合/状态 | 语义与验收要求 |
| --- | --- |
| `discovered_models` | 当前账号或草稿身份真实发现的有限模型集合；必须记录来源、revision、分页完整性，fallback 候选不得伪标 native discovery |
| `candidate_models` | 当前平台/托管画像的预设候选及允许手填的候选；仅用于配置，不等于当前凭据可调用 |
| `eligible_models` | 当前身份证据与已实现动作及账号规则共同允许的模型绑定；只能从有效证据求得 |
| `discovery_complete` | 发现遍历是否完整；与模型能力字段是否齐全独立 |
| `capability_status` | 能力证据的完整/稀疏/未知状态；不得反向阻断已有效发现的模型添加 |

求值必须遵循：**当前身份的发现/声明/探测证据 ∩ 动作实现 ∩ 账号规则 → 账号 Eligible → 组内可路由公开名并集 → 组/Composite 规则 → Key 规则交集**。完整访问规则见 [账号与模型访问合同](account-model-access.md)。公共目录仍以账号 Eligible 为准；模型事实不是凭据权限。

账号空数组必须保持存量语义，不能迁成“拒绝全部”；Key 未设置与显式空必须分开存储。Key 默认继承，存在策略时只能缩小组有效集。有限显式别名先按用户请求的公开名授权，再映射到上游模型并重查身份、动作、绑定；不得映射后绕过公开名规则。公开 `aliases` 和 `canonical` 必须在相同作用域内可解析到真实授权绑定，不公开隐藏目标或内部部署名。

列表、富目录、模型广场、可用渠道和监控的公共模型投影必须复用同一 resolver/revision；管理账号目录另保留权限内的证据明细。通配或透传只对实际请求求值，不可能枚举的无限集合必须明确边界，不得把全量候选当成枚举结果。未知透传仍必须经过账号、组、Key、动作适配及发送前绑定校验。

预览添加仅检查当前草稿身份、有效结果、可添加 ID 和非加载状态。完整发现且能力稀疏必须可应用；部分发现可添加选择，但不能删除旧完整快照。切换身份后旧异步响应不得回填；选择变化只更新规则 review，不毁掉发现列表；identity/rules/snapshot CAS 必须保留。没有发现接口时仍可同平台候选加手工映射保存，不强制联网或收费探测。

### 授权草稿生命周期

OAuth/Setup/Cursor 授权成功后必须仅交付服务端短期 opaque 草稿引用，复用现有 OAuth session/缓存设施保存授权材料；前端必须只持该引用及非敏感字段，不得持有完整 exchange secret。草稿必须绑定发起授权的会话并受 TTL 约束；其他会话不得预览或消费，取消或过期后未消费草稿必须失效且不得建号。

最终确认必须校验会话、TTL 和当前 identity/rules/snapshot revision，并将幂等消费草稿与创建账号作为一个原子操作；授权回调和预览不得提前建号。并发确认及确认成功后响应丢失的同一逻辑提交重试必须复用消费结果，不得重复写账号；确认失败不得留下已消费却未建号的半状态。

验收必须覆盖三种授权入口、跨会话引用、取消、过期、并发确认和确认响应丢失重试：最终确认前账号新增数必须为零，同一有效草稿成功确认的账号新增数必须恰为一，浏览器可见字段不得包含授权 secret。

## 极简模型列表与单模型字段

**截至 v0.3.2 的公开格式快照（2026-11-22，验收 HEAD `8e6e8a67e`）**：本节保留历史实施要求的背景，但公开行为以 `backend/internal/handler/model_list_serialization.go` 为准。普通条目已提供顶层已知保证值 `context_window`、`context_length`、`max_input_tokens`、`max_output_tokens`、`supports_reasoning`、`supported_reasoning_levels`、`supports_function_calling` 和输入/输出模态；未知值省略。这些附加事实不授予权限，富目录仍独立提供 `capabilities`、来源、不确定性及端点提示。客户调用示例见 [API 使用手册](usage-guide.md)。

`GET /v1/models` 和 `GET /v1/models/:id` 必须由请求协议选择 DTO，默认 OpenAI，存在 `anthropic-version` 时选择完整 Anthropic 协议；不得按组平台或 User-Agent 猜测。所有平台来源均可投影为 OpenAI 条目，不能外层 OpenAI、内层 Anthropic。响应必须带相应 `Vary: anthropic-version`，不得跨 Key 复用授权响应。

| 协议面 | 列表外层与字段 | 单模型字段及约束 |
| --- | --- | --- |
| 默认 OpenAI | `object: "list"`、`data: Model[]` | `id: string` 为公开名，`object: "model"`，可选 `created: integer` 为可信稳定 Unix 秒，`owned_by: string` 为公开归属，不得是账号/凭据拥有者 ID；可有上述客户端事实 |
| Anthropic | `data: Model[]`、`has_more: boolean`、`first_id: string 或 null`、`last_id: string 或 null`；空页两个 ID 为 null | `id: string`、`type: "model"`、`display_name: string`；可选 `created_at: string` 为可信时间的 UTC RFC3339 表达；可有上述客户端事实 |
| Google 原生 `/v1beta/models` 与单查 | `models: Model[]`、有后页才返回 `nextPageToken: string` | `name` 为原生公开资源名；`baseModelId`、`version`、`displayName`、`description`、`inputTokenLimit`、`outputTokenLimit` 按真实已知事实投影；未知可选值省略，不伪造版本或填 0；`supportedGenerationMethods` 来自真实动作，包括有实现的 `countTokens` |

普通 DTO 包含相应协议字段及上述已知客户端事实，不包含富目录的 `capabilities` 对象，也不公开内部 identity、pricing 或私有诊断。详情不存在与不可见必须一致返回 404，不能通过单查绕过列表权限。Codex manifest 使用既有专属入口；当前 `GatewayModelsHandler` 对非空 `client_version` 直接委托，带期限的 legacy helper 未接入该生产分派，不能把 helper 中的退出日期当作当前公开保证。

### 创建时间与分页规则

创建时间必须使用可信且稳定的来源；当前序列化在无可信持久时间时省略普通条目的 `created` / `created_at`，富目录标记 `created_at_source: "unavailable"`。不得使用响应时钟、刷新时间、账号创建时间或固定 2024 值冒充，亦不能承诺总有首次登记时间兜底。新可信来源替代旧来源时必须推进 revision，使排序和游标可检测变化。

Anthropic 必须支持 `limit`、`after_id`、`before_id`，按可信创建时间降序、ID 稳定消歧；两个方向互斥，非法/失效游标返回协议化 400。默认 OpenAI 完整有限列表不强加 Anthropic 分页字段。Google 必须支持 `pageSize/pageToken`，可以同源上游分页或对最终授权集合本地分页，但页令牌必须绑定来源与 revision。

所有分页必须先裁剪当前授权集合再切页。游标只定位，不能保存旧权限；后续页必须重新校验 Key、workspace、组、规则及目录 revision。篡改、跨作用域或 revision 不匹配的游标必须失败并要求重新开始，不得继续公开已撤权的旧快照。分页验收必须证明稳定排序、无重复遗漏、跨 Key 不能复用、撤权后不可通过旧游标取回条目。

## 富目录字段与能力聚合

v1 富目录为 `GET /v1/models/catalog`，按 ID 查询为同资源 `GET /v1/models/catalog/:id`。静态 catalog 路由必须先于 `/v1/models/:id` 注册，不新增 `/v1/model-info` 或 `/api/v1/model-catalog` 同义入口。`catalog` 是保留路径；包含 `/` 等字符的模型 ID 必须按单个路径参数转义，路由测试必须证明不会变成另一个端点或绕过授权。

### v2 模型广场与项目目录

v0.3.4 的 v2 列表和详情按 `(provider, exact model)` 标识。稳定 ID 继续使用 `m_[0-9a-f]{32}`。`provider/model` 在第一个斜线处分隔，保留模型名的大小写和其余斜线。详情优先匹配 ID 或明确的 provider slug，再尝试唯一的旧裸名。只有已公开且无冲突的规范名关系才会投影为不同的 `canonical_id`，别名保留各自的公开地址与绑定。

| 视角 | 路径 | 合同 |
| --- | --- | --- |
| 公开 | `GET /api/v2/public/models` | 使用 `availability` 和 `billing` 筛选新状态。旧筛选和排序仍可用。 |
| 公开 | `GET /api/v2/public/models/{id}` | 可传 `catalog_version` 固定到当前列表版本。版本已变化时返回 410 `SNAPSHOT_EXPIRED`。 |
| 项目 | `GET /api/v2/workspaces/{workspace_id}/projects/{project_id}/models` 与 `GET .../models/{id}` | 以项目与操作者权限裁剪。可传十进制字符串 `api_key_id` 选择已获授权的 Key。 |
| 对比 | `POST .../models/compare` | 接受当前视角内 1–3 个稳定 ID，可选 `protocol`、`action`、`scenario`、`price_at`。项目请求可在 body 传 `api_key_id`。 |

- `source` 描述字段来源与候选、发现成员关系。签名能力记录为 `builtin_reviewed`，签名预设为 `platform_preset`。内置补充为 `builtin_supplement`，pi 能力和参考费率为 `pi_ai_reference`。账号原生发现和身份绑定的申报分别为 `account_discovery` 与 `account_declaration`。匿名 `discovered` 为 `null`。
- `availability` 分别报告可用性、公开发布与访问。匿名条目返回 `requires_scope`，即使已发布且可宣传销售价。作用域内的 `live` 至少需要一个获准的当前可用绑定。原因区分仅候选、已发现但无资格、已发布但无可用绑定、枚举不完整和访问受限。
- `billing` 独立报告 `priced`、`free`、`conditional`、`partial`、`conflict`、`unpriced`、`error` 或 `unknown`。销售价与参考价可同时出现。费率使用十进制字符串、币种、计量单位和 `per`。参考价没有可执行的费率版本。缺失费率、缺少必要计量维度、多 offer 缺价或冲突以及读取失败各有原因码。
- 详情的 `provenance[]` 逐字段记录已知、未知、过期、冲突或抑制状态。原生精确观测和身份绑定申报先于签名能力、预设、内置补充和 pi。明确的 `false`、空集合和抑制记录阻挡低优先级补齐。跨 profile 或绑定的值与上下文基准有分歧时，`common_facts` 的共同字段为 `null`。公开 provenance 仅提供字段、状态、来源类别、安全版本与观测时间。项目账号证据需要 `connections.read`。

同一视角的列表和详情共享未分页、未过滤的 `catalog_version`。作用域响应另外返回十进制字符串 `scope_version`。列表游标绑定视角、Key、筛选、排序和版本，有效期 15 分钟。后续页重新读取当前权限和投影，失效版本返回 410。末页 `next_cursor` 为 `null`。

旧 `availability_state` 继续用于兼容筛选。公开 `live` 表示存在旧版的有效 offer，真实可用性取 `availability.state`。旧 `price_state` 将冲突、部分、未知及缺价归为 `unpriced`，兼容的上下文或分时卡仍可使用旧字段。保留 `effective_scope` 和详情顶层旧 `reference_price` 投影。

### v1 富目录和兼容入口

`GET /v1/models/catalog` 从当前 Key 和协议解析获得成员及 `fully_enumerable`。列表返回 `data`、`revision`、`fully_enumerable`、`next_cursor`，详情返回 `model`、`revision`、`fully_enumerable`。条目保留公开 `id`、`canonical_id`、`aliases`、可信创建时间、能力、可用端点和已有模型版本字段。富目录继续使用公开模型名作为 ID。无法确定单一安全规范名的条目以自身为规范名。网关协议继续使用原有响应结构、分页和 `X-Model-Catalog-Revision`。

`/api/v1/model-plaza` 和 `/api/v1/channels/available` 保留分组或渠道顺序、原有鉴权及功能开关。已公开的渠道确认模型和可用渠道中的已配置模型保留各自的兼容成员规则。公开与项目目录复用同一份已批准注册表快照和绑定解析。账号或凭据身份、内部部署、原始证据 URL、成本和余额由各视角的白名单筛掉。
### 保证值、可用值与未知值

每项能力使用 `{guaranteed, available, unknown}` 三部分：`guaranteed` 是所有当前合格绑定的共同保证，`available` 是至少一个当前合格绑定的已知可用值/变体，`unknown: boolean` 明确是否存在证据缺口。这不是把 true/false/unknown 压成一个“支持”布尔值；字段值按能力类型定义。仅有 available 的要求必须在选路时筛选兼容绑定，不能随机投给不支持者。没有任何合格绑定时不输出模型条目，不对空集合推导“全部保证”；全部证据未知时 guaranteed/available 都为 null，unknown=true。

| 能力字段 | 值类型与聚合 | 可执行验收 |
| --- | --- | --- |
| `reasoning`、`supports_vision`、`supports_function_calling`、`supports_prompt_caching`、`supports_pdf` | guaranteed/available 为 boolean 或 null；未知绑定存在时 guaranteed 为 null，已知真可使 available=true；全部已知假才可判全部不支持 | true+unknown 必须为 guaranteed=null、available=true、unknown=true；true+false 为 false/true/false，不得丢字段 |
| `context.input`、`context.total`、`max_output_tokens` | 分别以 token 为量纲；guaranteed 为全路径已知同量纲上限的 min，available 为已知路径可达上限；有缺口 guaranteed=null，另以 `observed_minimum` 保留已知最小值 | input=32k 与 total=64k 不得合并成一个 32k 保证；64k+unknown 不能宣传全路径保证 64k |
| `context.unspecified` | 保留已知数值及 `kind: "unspecified"` 的变体，不猜 input/total，不参与这两列保证计算 | 有 context 数值但无 `context_limit_kind` 时不得直接删除已知数值 |
| `input_modalities`、`output_modalities`、`tasks`、`actions` | guaranteed 为已知全路径集合交集，available 为已知集合并集；有未知路径 guaranteed=null，available 仍保留已知项，unknown=true | 集合顺序变化不得丢字段，未知数组不得变成已知空数组 |
| `embedding_dimensions` | guaranteed/available 为维度集合或 null，另保留 `variants` 的维度及可配置范围/选项；固定维度是离散属性，只求真实共同选项，不取 min | 固定 768 与固定 1536 必须保留两个变体，不能把 768 当全路径保证；known+unknown 的 guaranteed=null |
| `task_limits` | 按动作、字段和单位保留分辨率/时长/文件大小/批量等约束；只对同量纲同语义上限取 min，枚举交并分列，未知显式标记 | 视频秒数不得与图片像素、音频采样率或 embedding 维度混合求 min |
| `reasoning_control` | 有类型的模式/值域和能力变体；未知不补假默认 | 参数级支持必须有证据，不能只因模型名带 thinking 就允许所有控制值 |

布尔能力在“已知 false + 未知”时 guaranteed=null、available=null、unknown=true，不能把“尚无已知支持”发布成“已知全不支持”。集合能力的 available 只表达已知并集，unknown=true 时不能宣称它枚举了所有可能能力；数值 available 仅表达已知路径可达上限，实际请求还必须满足同一条路径的联合约束，不能拼接不同账号各自的最大值。

模态值必须先规范化大小写、来源同义值、排序和去重到共享 taxonomy，再聚合；使用已支持的 `text/image/audio/video/pdf/embedding` 等规范值，未知供应商词保留管理诊断但不直接作为公开枚举。task/action 与模态分开：rerank/realtime/code/tts/stt 不得仅靠 other/text 分类承载；视觉输入不等于图片生成。启发式只能补展示，不授权、不计价、不硬拒绝。

管理能力仍仅使用 `/api/v1/admin/accounts/:id/models/catalog` 的 `display_metadata`；不得增加第二个 capabilities API 或吞 404 做功能检测。公共目录不包含管理证据，管理详情必须在账号/workspace 授权内显示来源、缺口和 variants。

## 动作矩阵与发送前解析

动作权威必须扩展现有 `domain/platform_capabilities.go`。矩阵键为平台 × 托管类型 × 协议/画像 × action，并在实际模型绑定上校验要求；每格必须关联 endpoint adapter、凭据策略、模型/TaskLimits、usage/pricing 和任务生命周期。必查动作是 chat、messages、responses、embeddings、rerank、image.generate/edit/variation、video.generate/edit/extend、audio.tts/stt/translate、voice、realtime、search，以及既有批任务/资源动作。

| 步骤 | 必须行为 |
| --- | --- |
| 公共入口 | routes 只注册路径和中间件；handler/service 解析动作、参数和公开模型，先执行公开名授权 |
| 目标解析 | Composite 先解析有权限的显式目标，再复用相同动作解析；Cursor 不得默认归一成 OpenAI 池；Bedrock 非 Claude body 不送 Claude parser |
| 资格选择 | 当前身份证据、动作实现、账号/组/Key 规则、真实模型能力及请求要求共同确定候选；填入普通凭据无需“未验证”人工解锁 |
| 发送前重验 | 每次发送/换路必须重查绑定、身份与要求；已知不支持当前输入或动作时必须在调用上游前返回协议化 400，上游 fixture 调用数为零 |
| 未知证据 | unknown 不得当 false 拒绝；有合法绑定和适配路径时继续保守执行，不能借 unknown 绕过权限、缺价或适配器缺失，也不能把未知标为已支持 |
| 暴露控制 | `allow_image_generation` 等既有开关只控制暴露，不能替代 adapter；保存/迁移必须保留旧明确设置，新账号默认按实际能力生成 |
| 生命周期 | 所有 HTTP/媒体/搜索/异步/WS 均建 request plan；同步按 request、异步按 job、实时按 session+turn 冻结授权、动作、模型、报价、attempt budget、lease/receipt |

### 请求生命周期与恢复边界

必须保留 Claude/OpenAI 两主干，不新增第三流水线。最大尝试为首次加既有 `max_account_switches`，平台修复/重试也计入一个预算并受总时限约束。客户端取消和总 deadline 必须持续传到上游，重试不得重置总 deadline；独立短尾处理必须仅持久化已取得的计费证据，不得继续生成或 drain 上游以等待更多用量。

换路必须同时满足“明确未提交副作用”和“未输出语义内容”两个前提，缺一不得换路；心跳不算语义内容，但仅收到心跳不能证明未提交副作用。首语义内容后不得跨上游重放；提交结果不明也不得按未提交处理。媒体超时可能已提交时必须先查幂等键/任务 ID，不得盲目重发。任务查询、取消、下载必须校验 owner Key/workspace、版本与上游绑定。

已观察 usage/interrupted 必须留收据；计费失败及 panic 恢复必须复用现有 receipt/outbox 幂等重试、补结算与回收机制，不得重复扣款、丢弃已取得证据或保留绕过该机制的 nil settlement 旧分叉。取消不能承诺供应商不收费；供应商流无通用 resume，协议说明不得宣称已实现供应商未提供的续传，也不得新增无限 SSE 缓存。部分 200 断流必须按协议给出 error/terminal 边界，幂等重试不得透明重放或再次起生成。

验收必须覆盖换路双前提的全部组合：仅二者同时成立才允许换路；只有心跳但副作用已提交或未知时，上游新增发送数必须为零。慢滴流和重试必须受原总 deadline 截止；取消后短尾不得继续生成，计费失败及 panic 恢复必须保留证据且同一收据最多结算一次。

### 全部产品的适配归属与硬冲突处理

每个产品必须核证全部动作格，下表不是能力白名单或已完成声明；实施时由供应商任务固定第一方资料/脱敏 wire 样本和证据时间。无真实密钥不等于无能力，本地协议夹具覆盖协议行为，不宣称线上地区授权/风控已验证。

| 产品入口 | 唯一适配任务 | 必须保留的边界 |
| --- | --- | --- |
| Anthropic | `actions-anthropic` | Messages/视觉/countTokens/批量消息；生成类不可伪装成 Messages；OAuth 与 API Key 区分 |
| OpenAI | `actions-openai` | 标准 audio、Realtime、图像/embedding/Files/Batch；Codex 不等于开放 API 全权限 |
| Gemini（含 Vertex Gemini） | `actions-gemini` | 原生/兼容图像、TTS、embedding/批量、分页/alt；SA 项目/区域 |
| Antigravity | `actions-antigravity` | 内部协议、母账号/项目、映射/quota scope；不能借 Gemini API Key 权限 |
| Grok | `actions-grok` | 图片/视频生成编辑扩展、TTS/STT/voice/realtime/search，接公共计划 |
| Kimi | `actions-kimi` | payg/coding 实际发布范围逐格核证 |
| Zhipu GLM | `actions-zhipu` | 图像/视频/语音/向量/任务；coding 与开放平台分域 |
| DeepSeek | `actions-deepseek` | Chat/Responses/Messages 与实际多模态证据；分类不是生图/TTS 证据 |
| MiniMax | `actions-minimax` | 原生音视频/图像/音色/异步任务；国内/国际及 GroupId |
| OpenCode Go/Zen | `actions-opencode` | 代理公开子集、套餐/协议正交，不沿用底层厂商全量授权 |
| Qianwen/DashScope | `actions-qianwen` | 图像/Wan/音频/实时/embedding/rerank/files/batches，原生计量 |
| Command Code | `actions-commandcode` | 代理动作、模式、端点/头覆写，不能整个平台标 text-only 关闭 |
| Baidu Qianfan | `actions-qianfan` | 实际原生动作与独立鉴权，不能混同通用 Bearer |
| Tencent Hunyuan | `actions-hunyuan` | SSE/自定义 URL 及实际媒体 API 认证与任务关联 |
| Mistral | `actions-mistral` | embedding/语音/图片相关工具按真实协议，非直接图片接口不套 `/images` |
| AWS Bedrock | `actions-bedrock` | hosting profile/SigV4/serving family，保留宿主子类型 |
| Volcengine | `actions-volcengine` | Seedream/即梦/Seedance/向量/语音，Endpoint ID、AK/SK/Bearer、地域与 polling |
| Cursor | `actions-cursor` | gRPC/Agent、视觉/文件和实际动作，保留 Cursor 调度身份 |

有官方/已有协议证据的动作必须本轮实现；未知格必须核证，不能自动写 unsupported。若核证确无原生/可保真桥接能力，必须记录平台×动作硬冲突及证据，由协调者提供“使用用户已配置的 Composite 目标完成该动作”的替代建议；未获范围变更确认前该期望格保持未核销，不自行宣告 P 全部完成。不能偷偷换另一供应商的密钥。

新增通用 rerank、标准 audio、Gemini embedding、标准 Realtime 和必要原生入口由 `multimodal-ingress` 集中注册一次；18 个适配任务不得并发改 routes 或共享模型 JSON。图像同步/异步、视频编辑/扩展、任务状态/内容必须全部支持解析后的 Composite。标准 files/batches/message batches/moderations 由 `gateway-resource-actions` 复用任务与存储；其它 T1 补充资源动作必须有明确兼容/不等价边界，不能拿另一响应形状宣称 SDK 兼容。

## 管理错误与协议错误

管理面必须固定为 `{code: HTTP整数, reason: 稳定机器码, message: string, metadata: object}`，HTTP 状态与 code 一致，无补充信息时 metadata 为空对象。读取兼容期可以接受旧字符串 code/error，但前端只规范化一次、后续统一消费错误工具。混合渠道 409 必须可达 ConfirmDialog，确认绑定当前 payload/revision，取消无写入，不收紧既有 Step-Up 默认。

| 错误面 | 外层合同 | 余额不足与权限不足 |
| --- | --- | --- |
| OpenAI | `{error: {message, type, param, code}}`，param 无关联参数时为 null | 硬余额不足为 HTTP 429、`code: "insufficient_quota"`；权限不足为 HTTP 403 的权限错误，不得泛化成 `billing_error` |
| Anthropic | `{type: "error", error: {type, message}}`，请求标识按协议规则输出 | 余额不足使用 HTTP 400、`invalid_request_error` 和明确余额原因；权限不足为 HTTP 403、`permission_error` |
| Google | `{error: {code: HTTP整数, message, status}}`，details 仅投影协议允许且已脱敏的信息 | 余额耗尽为 HTTP 429、`RESOURCE_EXHAUSTED`；权限不足为 HTTP 403、`PERMISSION_DENIED` |

各协议最外层入口必须统一序列化鉴权、白名单、读体 400/413、计费、路由、流前/流中错误；管理信封不能流入 SDK。已提交响应后必须用该协议 stream error/terminal 事件收尾，不再写第二个 HTTP JSON 响应。网关英文消息和机器码必须稳定，面板按机器码翻译；error_passthrough 不能绕过凭据/身份/URI 脱敏，先擦除再截断。

429 的 `Retry-After` 必须来源于实际限额窗口、队列等待策略或上游有效退避信息；已知恢复时间可以返回真实等待秒数/HTTP 日期。没有确定 reset 时只可使用现有有界退避策略并明确这是建议等待，不能伪造确定重置时刻；硬余额不足不得伪造短时可恢复的 Retry-After。limit/remaining/reset 头只描述调用者实际有效额度，不公开上游账户总量；过载 503 与权限 403 不伪装成同一种 429。

Google 原生 `/v1beta` 的 query `key` 继续兼容且全链 URI/日志/错误脱敏，帮助页推荐 header；`/v1` 明确不支持 query key。CORS 和四项已定安全默认保持，不以协议统一为由收紧。Messages 的 max_tokens、Google alt、usage chunk 是否下发等协议规则由 `gateway-protocol-contract` 同批验收。

## 缓存身份、失效与容量

| 合同来源 | 键、事务与失效要求 | 必须验证的反例 |
| --- | --- | --- |
| 冷却身份（M1） | `groupID + credentialOwnerID + identityRevision + 实际模型/有证据的quota scope`；复用 Redis 跨实例冷却，本地有界缓存仅同键降级；成功仅清同键 | 不同组/凭据拥有者互不污染；相同上游 thinking 别名按证据共享域，Gemini 不得无证据全族连坐 |
| 身份更换（M2/M5） | 凭据、端点、协议/画像、HeaderOverride、代理引用及代理记录、secret revision、母/影子账号身份变化必须推进 revision；清 usage/monitor 额度缓存、探针、token、凭据失败/健康/超时/403/500 窗口 | 旧异步响应、计数和缓存回填必须检查 revision；普通备注修改不得清缓存，账本、已消费额度、运营停用和真实欠费绝不清 |
| 目录提交（M3） | 业务事务同时推进持久 generation 和写 outbox，含旧新组、删除事实/父子关系；本机只在提交后清理 | 搬组必须使两组及关联 Composite 失效；删除后仍有旧归属可定位；回滚事务不发布新事实 |
| 跨实例一致性（M3） | 通知必须可重连，乱序幂等；每次缓存命中响应前校验权威 generation，失败先回源，回源仍失败则报错；未分组用独立签名，不伪造组 | 断线/漏消息后撤权仍立即生效；不能盗用另一 outbox consumer 的消费水位，TTL 或通知本身不构成正确性保证 |
| 隔离域（M6） | 公共资料下载按来源+代理/信任域；账号富化再含账号或草稿身份及 revision；singleflight/cache/stale 使用同一隔离键 | 不得把未保存草稿都合并为 accountID=0，不得借 stale 或 flight 命中越过身份边界 |
| 有界资源（M8） | usage/目录/冷却/客户端池必须有有界 TTL/LRU 和回收；沿用既有配置/设施，不为本任务加开关 | 高基数后容量可回落，连接淘汰只关闭空闲连接，在途 HTTP/SSE/WS 不被中断 |

目录事实快照与权限缓存必须区分：请求可以冻结已批准事实，但不能用冻结的旧授权游标绕过撤权。API Key 鉴权缓存还必须含凭据期限/撤销、Key policy/workspace revision，期限逐请求校验；后台清理不是到期生效的前提。

## 身份与凭据迁移兼容期

所有迁移必须采用 expand → 兼容读取 → 幂等回填 → 新写入 → 退出旧来源，使用既有 runner；记录迁移前当前配置映射用于核对，不改已执行 SQL/checksum。迁移前缀由协调者分配。

| 迁移 | 必须保留与执行的顺序 | 回滚边界 |
| --- | --- | --- |
| 托管身份归位 | 所有实例先双读旧值；一次性幂等迁移仅更新当前配置；Bedrock 新写 `platform=anthropic,type=bedrock`，Vertex 按承载协议归 `anthropic/gemini,type=service_account`；hosting provider/region/project/deployment/SigV4/SA 保留 | 旧 Bedrock 限定组仍仅允许 Bedrock 子类型，不扩大为全部 Anthropic；回滚最低为能读新 type 的兼容代码 |
| 历史统计 | account/Key/group ID 不变；历史 usage/价格收据不重写；读取映射保留 Bedrock 子类，每行只计一次；配额已用量不清 | 仅允许核对和兼容读取，不得通过回滚重写历史金额或覆盖迁移后新建配置 |
| 消费点切换 | 常量、筛选、选择器、路由、目录、图标/徽章、统计、同步/导入及夹具同批切换；所有写入口执行绑定 payload/revision 的混合渠道 409 | 不得只改平台枚举而遗漏 N 组消费点 |
| 同 ID 凭据轮换 | 使用从属凭据记录和摘要查找，同一 `api_keys.id` 最多 current+previous 有效；默认 24h 可配宽限，第二次轮换终止更旧宽限；创建/轮换只返回一次 secret | 期限/撤销逐次校验并审计，轮换不重置权限/预算/历史归属；不能回生撤销凭据 |
| 明文转摘要 | 先凭据表、摘要双读和掩码 DTO 全实例兼容；再回填原 Key 的摘要并验证可鉴权；新凭据只存摘要，停止明文回读/写入；最后分批清旧明文并记录退出证据 | 原明文列可空，唯一性转摘要，用独立显示后缀，不塞假 secret；清明文后不能回滚到明文鉴权二进制，不从备份复活旧凭据；本轮必须退出临时双读 |
| secret 引用 | 默认 inline 读取兼容；明确转换后使用 workspace 内加密实体、既有 SecretEncryptor、OAuth 刷新 CAS；版本变化统一失效且 diff 脱敏 | 无明文 GET、引用不跨域；不得自动退回历史明文，不额外接多家 Vault SDK |

每个迁移实施 PR 必须记录最低回滚代码 SHA/schema 范围/制品 digest/是否已有新语义写入，并验证 fresh/旧库/二次执行。预算和作用域迁移见配套访问合同，不能恢复忽略已生效规则的旧二进制。
