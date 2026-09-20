# 模型注册表、公开协议与运行一致性合同

## 合同效力与来源

本合同是 `execution-contracts` 的实施基线，规定后续实现必须满足的行为，不表示这些行为已经实现。基线为 `guanwenpeng2001-bot/main-next-3`，SHA `52bbd9182f186caac1e87ba1893330952e05555a`。本次仅交付本文件和 [账号与模型访问合同](account-model-access.md)，不新增运行开关、配置项、接口实现或预期失败测试。

裁决依据为 [整合执行方案](C:/Users/Administrator/orca/workspaces/sub2api/main-work/.pi/plans/main-next-3-account-management/plans/plan-integrated-execution.md) 的「冲突裁决」「固定实现合同与 U 组落地」「P 组：18 平台动作覆盖的派发边界」「阶段依赖、派发规则与文件所有权」。编号以 [定稿登记册](C:/Users/Administrator/orca/workspaces/sub2api/main-work/.pi/plans/main-next-3-account-management/final-problem-register.md) 和 [审计合并稿](C:/Users/Administrator/orca/workspaces/sub2api/main-work/.pi/plans/main-next-3-account-management/audit-merge.md) 为准；旧报告建议不能覆盖已裁决的 U 组及补充决定。本文把字段和验收表达具体化，不重新裁决范围；发现工程硬冲突必须报告协调者。

后续验收必须同时满足派发合同任务交付、来源编号验收行和阶段验收要求。本文不是 302 项核销账本的替代品；D8/K1、D9/K2、D10/K3、D11/K4、D12/K5、D13/K6 等共用实现时仍须分别核销。

## 运行事实源与不可变快照

服务层必须组装不可变 `ModelRegistrySnapshot`（U4、D10、N2），供预设、目录、资格判断、能力展示和价格关联使用。它是既有来源的一代事实视图，不是第四张人工维护的模型表。

| 来源或标识 | 必须承担的职责 | 不得承担的职责 |
| --- | --- | --- |
| 平台预设 | 规范身份、同平台/托管画像候选、公开模型名和别名基础信息 | 证明某凭据获得授权，或直接填充公开可用目录 |
| 签名能力 catalog | 按准确身份补 task、输入输出模态、限额、推理控制、生命周期及来源证据 | 根据名称启发式授予能力，或把 unknown 批量改成 complete |
| 当前身份发现、声明、探测 | 账号资格证据；保留 identity/rules/snapshot revision 和来源 | 把另一账号、上一凭据或另一地域的发现当成当前授权 |
| 既有价格解析器与有效覆盖 | 按同一模型身份关联有效销售报价与成本；多单位、条件价卡必须保真 | 用同名异画像价格代替，缺价按免费处理，或对用户输出内部成本 |
| 组/Composite/Key 策略 | 在事实和绑定之上裁剪授权范围 | 改写原始能力事实、供应商 usage 或历史报价 |

模型身份必须区分平台、托管类型、实际 hosting provider、协议/画像、region/project/deployment 与凭据拥有者；内部部署标识不能作为公共 canonical 的替代。mode、套餐、协议必须正交，不得将 Coding 端点的权限等同开放 API。公开同名的多个绑定可以聚合，但不能抹去内部身份区别。

快照必须携带可追溯的制品 digest、release pin/generation 与来源 revision。一次请求必须冻结同一快照引用及对应报价；不得在调用中途混用新旧代事实。价格缺失、明确免费和有价必须区分；已冻结的历史收据不得随新包重算。前端必须先迁移到现有预设 API，再删除 Go 独立名单及 `admin_group.go` 的 Claude default（U4b）；未知画像无候选必须返回空态并允许合法手工输入。

## 批准包重载与离线运行

运行时重载采用管理员触发的批准本地包入口（U4a），保留既有签名、release pin 和审批链。不接受任意远程 URL，不以文件变化绕过审批，不新增自动启用资料的通道。O5 定时采集只生成待审差异。

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

求值必须遵循：**当前身份的发现/声明/探测证据 ∩ 动作实现 ∩ 账号规则 → 账号 Eligible → 组内可路由公开名并集 → 组/Composite 规则 → Key 规则交集**。完整访问规则见 [账号与模型访问合同](account-model-access.md)。U4c 要求公共目录仍以账号 Eligible 为准；模型事实不是凭据权限。

账号空数组必须保持存量语义，不能迁成“拒绝全部”；Key 未设置与显式空必须分开存储。Key 默认继承，存在策略时只能缩小组有效集。有限显式别名先按用户请求的公开名授权，再映射到上游模型并重查身份、动作、绑定；不得映射后绕过公开名规则。公开 `aliases` 和 `canonical` 必须在相同作用域内可解析到真实授权绑定，不公开隐藏目标或内部部署名。

列表、富目录、模型广场、可用渠道和监控的公共模型投影必须复用同一 resolver/revision；管理账号目录另保留权限内的证据明细。通配或透传只对实际请求求值，不可能枚举的无限集合必须明确边界，不得把全量候选当成枚举结果。未知透传仍必须经过账号、组、Key、动作适配及发送前绑定校验。

预览添加仅检查当前草稿身份、有效结果、可添加 ID 和非加载状态。完整发现且能力稀疏必须可应用；部分发现可添加选择，但不能删除旧完整快照。切换身份后旧异步响应不得回填；选择变化只更新规则 review，不毁掉发现列表；identity/rules/snapshot CAS 必须保留。没有发现接口时仍可同平台候选加手工映射保存，不强制联网或收费探测。

## 极简模型列表与单模型字段

`GET /v1/models` 和 `GET /v1/models/:id` 必须由请求协议选择 DTO，默认 OpenAI，存在 `anthropic-version` 时选择完整 Anthropic 协议；不得按组平台或 User-Agent 猜测。所有平台来源均可投影为 OpenAI 条目，不能外层 OpenAI、内层 Anthropic。响应必须带相应 `Vary: anthropic-version`，不得跨 Key 复用授权响应。

| 协议面 | 列表外层与字段 | 单模型字段及约束 |
| --- | --- | --- |
| 默认 OpenAI | `object: "list"`、`data: Model[]` | `id: string` 为公开名，`object: "model"`，`created: integer` 为稳定 Unix 秒，`owned_by: string` 为公开归属，不得是账号/凭据拥有者 ID |
| Anthropic | `data: Model[]`、`has_more: boolean`、`first_id: string 或 null`、`last_id: string 或 null`；空页两个 ID 为 null | `id: string`、`type: "model"`、`display_name: string`、`created_at: string` 为同一可信时间的 UTC RFC3339 表达 |
| Google 原生 `/v1beta/models` 与单查 | `models: Model[]`、有后页才返回 `nextPageToken: string` | `name` 为原生公开资源名；`baseModelId`、`version`、`displayName`、`description`、`inputTokenLimit`、`outputTokenLimit` 按真实已知事实投影；未知可选值省略，不伪造版本或填 0；`supportedGenerationMethods` 来自真实动作，包括有实现的 `countTokens` |

极简 DTO 只能包含相应协议字段；不得注入 `capabilities`、`catalog_source`、内部 identity、pricing、evidence 或诊断。详情不存在与不可见必须一致返回 404，不能通过单查绕过列表权限。内部消费者先迁富目录，再停止极简元数据扩展。Codex manifest 必须迁到既有专属入口，旧显式 `client_version` 只作有迁移期限文档和测试的兼容委托；普通查询参数不得偶然改列表 schema，不在本合同虚构退出日期。

### 创建时间与分页规则

创建时间必须优先使用身份匹配且可信的上游创建时间，否则使用持久首次登记时间。首次登记时间跨刷新、重启和换页稳定；不得使用响应时钟、刷新时间、账号创建时间或固定 2024 值冒充。时间来源必须在富目录/管理证据中标明，极简 DTO 不增加非协议字段。新可信来源替代旧来源时必须推进 revision，使排序和游标可检测变化。

Anthropic 必须支持 `limit`、`after_id`、`before_id`，按可信创建时间降序、ID 稳定消歧；两个方向互斥，非法/失效游标返回协议化 400。默认 OpenAI 完整有限列表不强加 Anthropic 分页字段。Google 必须支持 `pageSize/pageToken`，可以同源上游分页或对最终授权集合本地分页，但页令牌必须绑定来源与 revision。

所有分页必须先裁剪当前授权集合再切页。游标只定位，不能保存旧权限；后续页必须重新校验 Key、workspace、组、规则及目录 revision。篡改、跨作用域或 revision 不匹配的游标必须失败并要求重新开始，不得继续公开已撤权的旧快照。分页验收必须证明稳定排序、无重复遗漏、跨 Key 不能复用、撤权后不可通过旧游标取回条目。

## 富目录字段与能力聚合

公共富目录固定为 `GET /v1/models/catalog`，按 ID 查询为同资源 `GET /v1/models/catalog/:id`。静态 catalog 路由必须先于 `/v1/models/:id` 注册，不新增 `/v1/model-info` 或 `/api/v1/model-catalog` 同义入口。`catalog` 是保留路径；包含 `/` 等字符的模型 ID 必须按单个路径参数转义，路由测试必须证明不会变成另一个端点或绕过授权。

列表与单查使用以下字段合同。字段命名用于后续 DTO 实施；只在富目录中增加这些字段，不扩展供应商极简模型对象。`null` 表示未知或不能保证，`[]` 表示已知空集，false/0 只可用于有事实支撑的实际值；未知数字不得填 0。

| 位置/字段 | 类型 | 必须满足的语义 |
| --- | --- | --- |
| 列表 `data` | 条目数组 | 当前 Key 可见、可绑定的有限公开模型集；稳定公开 ID 升序 |
| 列表 `revision` | 不透明字符串 | 当前目录事实与授权版本的标识，不编码明文内部身份 |
| 列表 `has_more`、`next_cursor` | boolean、string/null | `limit/cursor` 在授权后集合分页；游标绑定稳定 ID、revision、作用域与筛选条件；末页 cursor 为 null |
| 单查/条目 `id`、`display_name` | string | 当前公开模型名及显示名称 |
| 条目 `canonical`、`aliases` | string/null、string[] | 仅输出当前授权域内可解析的公开规范名与别名；无可公开 canonical 时为 null |
| 条目 `created`、`created_source` | integer、`upstream` 或 `first_registered` | 与极简 DTO 相同稳定时间；来源不是账号创建时间 |
| 条目 `revision` | 不透明字符串 | 此次投影所属版本；单查同样校验权威版本 |
| 条目 `capabilities` | 有类型对象 | 按下表聚合当前授权且可路由的绑定，不掺入候选或无关账号 |
| 条目 `pricing` | object/null | 有效销售价：币种、计量单位、条件/阶梯及有效期；确认为免费时明确零价；未解析到销售价为 null，调用计价仍必须报缺价，不降为免费 |
| 条目 `lifecycle` | object/null | 有来源的状态/到期等生命周期事实，按 variant 保留差异；未知为 null |

`pricing` 非空时字段为 `currency: string`、`rates: array`、`valid_from/valid_until: UTC时间或null`；每条 rate 必须有 `unit: string`、`amount: 十进制定点字符串`、`conditions: object`，金额精度沿用既有解析器，计量单位必须含计费基数（例如每百万 token、每张、每秒），不能只写无量纲数字。conditions 复用既有价卡的公开条件维度，保留 native/thinking/cache/batch 及阶梯边界，不把成本条件整包暴露。`lifecycle` 非空时包含有来源的 `status: string`、`expires_at: UTC时间或null`；不同变体状态不一致时用 `variants` 保留公开语义差异，不捏造统一到期时间。嵌套未知值同样不得填零或猜默认。

公开 pricing 只能投影调用者有效销售报价，不包含上游成本、利润控制阈值或账号总额度。无法确定统一销售报价时必须保留按公开条件区分的 rates，不能随机取一账号报价。供应商 usage 原始事实不得为了代理优惠重分类。所有字段均采用白名单投影，不能递归透传管理对象；禁止内部部署 ID、账号 ID、项目秘密、凭据、secret reference、内部 host、原始证据 URL/错误和诊断泄漏。

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
| 发送前重验 | 每次发送/换路必须重查绑定、身份与要求；已知不支持当前输入或动作时必须在调用上游前返回协议化 400（D13），上游 fixture 调用数为零 |
| 未知证据 | unknown 不得当 false 拒绝；有合法绑定和适配路径时继续保守执行，不能借 unknown 绕过权限、缺价或适配器缺失，也不能把未知标为已支持 |
| 暴露控制 | `allow_image_generation` 等既有开关只控制暴露，不能替代 adapter；保存/迁移必须保留旧明确设置，新账号默认按实际能力生成 |
| 生命周期 | 所有 HTTP/媒体/搜索/异步/WS 均建 request plan；同步按 request、异步按 job、实时按 session+turn 冻结授权、动作、模型、报价、attempt budget、lease/receipt |

必须保留 Claude/OpenAI 两主干，不新增第三流水线。最大尝试为首次加既有 `max_account_switches`，平台修复/重试也计入一个预算并受总时限约束。心跳不算语义内容；首语义内容后不得跨上游重放。取消必须传给上游，已观察 usage/interrupted 必须留收据；媒体超时可能已提交时先查幂等键/任务 ID，不盲目重发。任务查询、取消、下载必须校验 owner Key/workspace、版本与上游绑定。

### 全部产品的适配归属与硬冲突处理

每个产品必须核证全部动作格，下表不是能力白名单或已完成声明；实施时由供应商任务固定第一方资料/脱敏 wire 样本和证据时间。无真实密钥不等于无能力，U5 以本地协议夹具验收，不宣称线上地区授权/风控已验证。

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
| 托管身份归位（U1/U1a–d） | 所有实例先双读旧值；一次性幂等迁移仅更新当前配置；Bedrock 新写 `platform=anthropic,type=bedrock`，Vertex 按承载协议归 `anthropic/gemini,type=service_account`；hosting provider/region/project/deployment/SigV4/SA 保留 | 旧 Bedrock 限定组仍仅允许 Bedrock 子类型，不扩大为全部 Anthropic；回滚最低为能读新 type 的兼容代码 |
| 历史统计（U1c） | account/Key/group ID 不变；历史 usage/价格收据不重写；读取映射保留 Bedrock 子类，每行只计一次；配额已用量不清 | 仅允许核对和兼容读取，不得通过回滚重写历史金额或覆盖迁移后新建配置 |
| 消费点切换（U1b） | 常量、筛选、选择器、路由、目录、图标/徽章、统计、同步/导入及夹具同批切换；所有写入口执行绑定 payload/revision 的混合渠道 409 | 不得只改平台枚举而遗漏 N 组消费点 |
| 同 ID 凭据轮换（U2b） | 使用从属凭据记录和摘要查找，同一 `api_keys.id` 最多 current+previous 有效；默认 24h 可配宽限，第二次轮换终止更旧宽限；创建/轮换只返回一次 secret | 期限/撤销逐次校验并审计，轮换不重置权限/预算/历史归属；不能回生撤销凭据 |
| 明文转摘要 | 先凭据表、摘要双读和掩码 DTO 全实例兼容；再回填原 Key 的摘要并验证可鉴权；新凭据只存摘要，停止明文回读/写入；最后分批清旧明文并记录退出证据 | 原明文列可空，唯一性转摘要，用独立显示后缀，不塞假 secret；清明文后不能回滚到明文鉴权二进制，不从备份复活旧凭据；本轮必须退出临时双读 |
| secret 引用 | 默认 inline 读取兼容；明确转换后使用 workspace 内加密实体、既有 SecretEncryptor、OAuth 刷新 CAS；版本变化统一失效且 diff 脱敏 | 无明文 GET、引用不跨域；不得自动退回历史明文，不额外接多家 Vault SDK |

每个迁移实施 PR 必须记录最低回滚代码 SHA/schema 范围/制品 digest/是否已有新语义写入，并验证 fresh/旧库/二次执行。预算和作用域迁移见配套访问合同，不能恢复忽略已生效规则的旧二进制。

## 基线门禁状态与后续验收

[基线门禁记录](C:/Users/Administrator/orca/workspaces/sub2api/main-work/.pi/plans/main-next-3-account-management/baseline-gates.md) 于 2026-09-20 10:20 记录上述基线 SHA **当前全绿、无已知失败项**：后端 `go build ./...`、`go test ./...`，前端 typecheck/check:i18n/lint:check/test:run/build，以及 required usage-coverage/catalog/affinity 均通过，affinity 使用真实 Redis CAS。此处引用的是既有基线证据，不是后续功能完成声明。

后续子分支必须按共通规格运行既有全门禁与相邻行为测试；required 必须实际 PASS，不得把 Skip/缺依赖算通过。身份/权限/预算/结算另跑对应隔离集成测试。U5 不要求真实供应商实机，不得因此声称线上权限或地区风控已经验证。本地 3000/8080 浏览器验证由协调者统一执行，worker 不自启/重启主实例。过程证据只写主工作树 `.pi`，本合同不新增扫描器、完成度开关或测试百分比门槛。

## 附录：共享文件锁与夹具归属

### 文件锁的取得、释放与串行顺序

同一时刻一个共享文件只允许当前获派任务写入；表中箭头表示顺序所有者，不是同时授权。前一任务必须合并并验收，释放文件后后一任务才基于该 SHA 增量接线。没有独占文件授权的任务只交自己的模块，跨表重叠也必须串行；协调者维护实际锁状态。`execution-contracts` 本次仅锁两份合同文档，不修改现有协议矩阵或任何源码。

| 共享区（源码根相对路径或派发合同中的文件简称） | 顺序所有者/唯一写者 |
| --- | --- |
| `gateway_handler.go`、`openai_gateway_handler.go`、`gateway_service.go`、`openai_gateway_service.go` 及共享流工具 | `stream-cancellation-repair` → `gateway-protocol-contract`/`request-lifecycle` 按互斥接线顺序 → `action-dispatch` → `multimodal-ingress`/治理所属任务 → `shared-architecture-cleanup`；同文件禁止并行 |
| `admin_account.go`、`account_repo.go`、`group_repo.go` | `account-preview-repair` → `account-runtime-identity`/`catalog-event-propagation` → `hosted-account-identity` → `oauth-credential-drafts` → `workspace-resource-scope` → 对应 access/secret 任务；跨阶段每次释放后接线 |
| `admin_group.go` | `hosted-account-identity` → `model-registry-runtime` → `model-access-groups` |
| `api_key_service.go`（实际服务文件见下表）、API Key schema/repo/auth | `gateway-protocol-contract`（协议错误）→ `model-access-groups` → `key-credential-lifecycle` → `key-budget-periods`/`gateway-admission-controls` 各自接线 |
| `config.go` | `deployment-config-alignment` 对齐原值/提示；已要求的治理配置先由对应治理任务定义再串行接线；不得同时编辑或改变四个安全默认 |
| `routes/*.go` | auth 归 `oauth-identity-boundary`，workspace 归 `workspace-resource-scope`；gateway 由 `action-dispatch` 薄委托后交 `multimodal-ingress` 集中入口；18 adapters 不写 routes |
| `ent/schema/*`、Wire、Ent/Wire 生成物、迁移 | 当前持有 schema/DI 的任务独占生成并与源定义同批提交；协调者分配未用前缀，不手改生成物、不改历史迁移号 |
| Create/Edit/Bulk、模型选择组件 | `account-preview-repair` → `preset-api-source` → `account-workflow` → workspace/access/secret 插槽；`model-modal-presentation` 等相关组件必须等待 workflow 释放 |
| `SettingsView.vue`、`GroupsView.vue` | `settings-view-modules` 拆分 → 治理对应接线 → `frontend-accessibility-language`；视觉 token 沿用原设计 |
| `frontend/src/types/index.ts` 与域 DTO | `frontend-domain-contracts` 主责拆分/re-export，其他域实施按其合并点顺序迁移，不并行重写聚合器 |
| `frontend/src/api/client.ts`、错误规范化工具 | `panel-error-contract` 唯一主责；后续消费者按统一错误工具接线 |
| router、i18n 聚合器 | 按当前界面所属任务取得独占锁：身份/工作流 → session → workspace/access/credential/budget → accessibility/client guides；该顺序不得覆盖各任务硬前置 |
| 根/后端 Makefile | `test-entry-repair` 唯一主责修入口；其他任务复用已有目标，不另造门禁 |
| CI/release/security、package/lock、忽略规则 | `build-and-dependency-health` 主责；`usage-display-export` 的 XLSX 依赖改动必须先合并释放；deployment 的 Docker/Compose 单独归 `deployment-config-alignment` |
| 共享预设 JSON、能力 catalog 与采集输入 | `model-registry-data` 唯一数据写者；adapter 数据需求交其顺序补入，release/pin/runtime 接线归 `model-registry-runtime` |

### 精确接线文件的顺序所有者

以下精确索引沿用派发合同，以避免简称误指文件；未存在路径为派发合同拟议新增文件，同责模块已存在时就近复用。表内原有版本式文件名只是既有路径，不是新命名许可。

| 精确路径（相对源码根；未存在者标新增） | 顺序所有者与具体改动 |
| --- | --- |
| `backend/internal/handler/admin/account_handler.go` | panel-error-contract：409；account-preview-repair：preview/catalog/幂等；oauth-credential-drafts：最终确认；workspace-resource-scope：scope；各次仅薄接线 |
| `backend/internal/service/admin_account.go` | preview：头/复制；runtime：身份变更；hosted：类型；draft：原子创建；workspace/secret：授权/引用，按阶段串行 |
| `backend/internal/service/admin_group.go` | hosted：subtype限制；registry-runtime：删Claude default；access：模型规则；保留同一调用链 |
| `backend/internal/handler/gateway_handler.go`、`backend/internal/handler/openai_gateway_handler.go` | stream修复→protocol→request-lifecycle→action-dispatch→治理策略，控制各分支接线行，不整文件格式化 |
| `backend/internal/service/gateway_upstream_response.go`、`backend/internal/service/gateway_anthropic_passthrough.go`、`backend/internal/service/openai_gateway_messages_anthropic_native.go`、`backend/internal/service/antigravity_gateway_streaming.go`、`backend/internal/service/bedrock_stream.go` | stream-cancellation-repair修所有取消/心跳入口；request-lifecycle统一真实usage与结算，不能只修一个provider |
| `backend/internal/handler/openai_images.go`、`backend/internal/handler/grok_media.go`、`backend/internal/handler/openai_embeddings.go`、`backend/internal/handler/grok_audio.go`、`backend/internal/handler/openai_alpha_search.go`、`backend/internal/handler/openai_live.go` | request-lifecycle先接plan与deadline；protocol任务接错误/Origin；各provider adapter保留专有处理；multimodal-ingress最后统一入口 |
| `backend/internal/handler/qianwen_realtime_relay.go`、`backend/internal/service/qianwen_realtime_billing.go`、`backend/internal/service/qianwen_native_audio_billing.go` | lifecycle按turn/receipt接入；actions-qianwen补原生动作；model-pricing-facts串行处理单位/冻结报价 |
| `backend/internal/util/logredact/redact.go`、`backend/internal/handler/request_body_parse_log.go`、`backend/internal/service/error_passthrough_runtime.go` | gateway-protocol-contract统一凭据/身份/URL脱敏，先擦除后截断；不得把HTTP原文原样交用户 |
| `backend/internal/server/middleware/api_key_auth.go`、`backend/internal/server/middleware/api_key_auth_google.go`、`backend/internal/server/middleware/group_model_allowlist.go` | protocol先统一错误；access组同resolver；key生命周期双凭据/期限；admission统一限额反馈，全部协议不留旁路 |
| `backend/internal/server/middleware/panel_rate_limit.go`、`backend/internal/server/middleware/session_binding.go`、`backend/internal/handler/auth_dingtalk_client.go` | oauth-identity-boundary处理限流/旧token边界；bounded-runtime-resources处理DingTalk共享token |
| `backend/internal/securityaudit/prompt_outbound_security.go`、`backend/internal/config/config.go` | deployment-config-alignment只对齐已有选项/提示与显式配置执行；四个安全默认不改；可靠性必需配置由对应治理分支先定义再串行接线 |
| `backend/ent/schema/api_key_credential.go`（新增）、`backend/internal/repository/api_key_credential_repo.go`（新增） | key-credential-lifecycle：摘要唯一索引、Key关联、current/grace、valid_until/revoked；原key明文列转nullable兼容后停止使用，唯一性转摘要，不往原列塞假secret |
| `backend/ent/schema/api_key.go`、`backend/internal/service/api_key.go`、`backend/internal/repository/api_key_repo.go` | access规则→credential revision→budget policy/period串行；原ID/owner/group不变，用户/管理员解绑一致 |
| `backend/ent/schema/workspace.go`、`backend/ent/schema/workspace_member.go`、`backend/internal/service/workspace_service.go`、`backend/internal/repository/workspace_repo.go`、`backend/internal/server/routes/workspaces.go`（均新增） | workspace-resource-scope：实体/成员/作用域服务/资源路由，既有account/group/proxy/channel/monitor查询显式scope |
| `backend/ent/schema/model_access_group.go`、`backend/internal/service/model_access_group.go`、`backend/internal/repository/model_access_group_repo.go`、`backend/internal/handler/admin/model_access_group_handler.go`（均新增） | model-access-groups：具名集合、引用关系、规则revision、有效集和影响预览 |
| `backend/ent/schema/credential_secret.go`、`backend/internal/service/credential_secret.go`、`backend/internal/repository/credential_secret_repo.go`（均新增） | credential-reference-audit：workspace加密secret、版本CAS、引用轮换、没有明文GET；复用TOTP等现有SecretEncryptor接口而非混表 |
| `backend/internal/service/audit_log_service.go`、`backend/internal/repository/audit_log_repo.go`、`backend/internal/handler/admin/audit_log_handler.go` | credential-reference-audit：配置脱敏diff/actor/workspace/revision，与业务事务结果一致；key生命周期先提交轮换事件类型 |
| `backend/internal/service/usage_settlement_service.go`、`backend/internal/repository/usage_settlement_receipt_repo.go`、`backend/internal/repository/usage_billing_repo.go` | lifecycle→budget串行：强制receipt、预留/实扣、幂等恢复、冻结period，取消/失败不丢实际账 |
| `backend/internal/handler/admin/dashboard_handler.go`、`backend/internal/handler/admin/dashboard_snapshot_v2_handler.go`、`backend/internal/repository/usage_log_repo_dashboard.go` | usage-display-export补billing_mode/成本；admin-observation-truth补真实realtime，不各自维护假stats |
| `backend/internal/handler/admin/group_handler.go`、`backend/internal/handler/admin/proxy_handler.go`、`backend/internal/handler/admin/redeem_handler.go`、`backend/internal/service/proxy_service.go`、`backend/internal/service/redeem_service.go` | admin-observation-truth接真实聚合/实际网络，scope取前序workspace实现 |
| `backend/internal/handler/admin/channel_handler.go`、`frontend/src/api/admin/channels.ts` | model-pricing-facts补复杂价卡bind/response；frontend-domain-contracts保持精确类型 |
| `frontend/src/components/account/CreateAccountModal.vue`、`frontend/src/components/account/EditAccountModal.vue`、`frontend/src/components/account/BulkEditAccountModal.vue` | preview小修→preset API→workflow区块→workspace/访问组/secret插槽，四组写者串行 |
| `frontend/src/components/account/ModelWhitelistSelector.vue`、`frontend/src/components/account/ModelCatalogDetails.vue`、`frontend/src/utils/modelCategories.ts` | preview返回集合→model-modal-presentation分组/大列表/TaskLimits；覆盖率横幅所在显示逻辑不改 |
| `frontend/src/views/admin/SettingsView.vue`、`frontend/src/views/admin/GroupsView.vue` | settings-view-modules按已有tab/表单块抽取；治理接线在拆分后，视觉token不另设 |
| `frontend/src/views/user/KeysView.vue`、`frontend/src/components/admin/user/UserApiKeysModal.vue`、`frontend/src/api/keys.ts`、`frontend/src/api/admin/apiKeys.ts` | 错误/幂等→session→access→credential→budget→账单/状态顺序，显式保留子分支回滚边界 |
| `frontend/src/views/admin/UsageView.vue`、`frontend/src/views/user/UsageView.vue`、`frontend/src/api/admin/usage.ts`、`frontend/src/api/admin/dashboard.ts` | usage-display-export抽共用页面块/筛选DTO，服务端导出并保留XLSX能力 |
| `frontend/src/views/user/CustomPageView.vue`、`frontend/src/views/HomeView.vue`、`frontend/src/utils/embedded-url.ts`、`frontend/vite.config.ts` | browser-session-boundary移除主JWT、同消毒/沙箱、生命周期与HTML注入转义，随后a11y只补label |
| `frontend/src/views/auth/WechatCallbackView.vue`、`frontend/src/views/auth/OidcCallbackView.vue`、`frontend/src/views/auth/DingTalkCallbackView.vue`、`frontend/src/views/auth/LinuxDoCallbackView.vue` | frontend-session-lifecycle共用换票/会话恢复状态，provider专有参数保留 |
| `frontend/src/api/channelMonitorV2.ts`、`frontend/src/api/admin/channelMonitor.ts`、`frontend/src/features/channel-monitor-v2/MonitorSettingsPanel.vue`、`frontend/src/features/channel-monitor-v2/normalizeConfig.ts` | admin-observation-truth补完整指标/模式错误/候选来源；不因既有文件名v2另造新版本模块 |
| `backend/internal/service/wire.go`、`backend/internal/repository/wire.go`、`backend/internal/handler/wire.go`、`backend/internal/server/middleware/wire.go`、`backend/cmd/server/wire_gen.go`、`backend/ent/migrate/schema.go` | 每个schema/DI所有者顺序生成；`make -C backend generate`使用现有流程，生成diff与源定义同批审查 |

### 测试环境与夹具的唯一写者

| 夹具/资源 | 所有者与调度规则 | 验收边界 |
| --- | --- | --- |
| PostgreSQL `stats_test`（127.0.0.1:5433） | 协调者分配独占执行时段；当前门禁 worker 在时段内运行 required usage-coverage，`test-entry-repair` 仅修测试入口，统计行为夹具随对应 usage 任务 | 使用共通规格 `USAGE_STATS_TEST_DSN`；后端测试与三个 required 串行，不得两个分支同时 reset；不得接 dev/业务库 |
| Redis 测试实例 127.0.0.1:6399 | 协调者分配独占执行时段；affinity 当前测试运行者独占，identity/admission 的 fixture 各归行为任务 | `AFFINITY_REDIS_TEST_ADDR=127.0.0.1:6399`；不是 dev 6380；并发只允许已证明互相隔离的 namespace，禁止共享实例 FLUSH 互相干扰 |
| `job_test` 与迁移测试管理连接 | `upstream-job-integrity`/schema 所属任务及 required catalog 当前运行者按协调者时段独占；变量分别为既有 `UPSTREAM_JOB_TEST_DSN`、`NEW_PLATFORMS_TEST_POSTGRES_DSN` | required catalog 的 PostgreSQL reconfirmation/CAS 哨兵也必须设置 `UPSTREAM_JOB_TEST_DSN` 指向独占 job_test；仅设置 stats_test 和 Redis 不足以通过；未知 DSN 不猜、不向日志输出凭据 |
| HTTP 模型列表/错误/授权夹具 | `gateway-protocol-contract` 主责协议序列化，`visible-model-policy` 主责有效集合，`action-dispatch` 主责动作负例；共享 fixture 文件按上述顺序释放 | 三协议严格 schema、时间/游标、404/400、撤权、别名，不用候选充授权 |
| HTTP 身份/发现/catalog/reload 夹具 | preview → discovery → identity → registry 各任务只写相邻行为夹具；同文件按依赖串行 | 身份 CAS、partial 保旧、坏签名、换代/断网、双实例失效 |
| SSE 取消/心跳/断流夹具 | `stream-cancellation-repair` → `request-lifecycle`，协议事件归 `gateway-protocol-contract` 串行接线，供应商 wire 归对应 `actions-*` | 心跳后可安全换路、语义内容后不重放，取消传上游，已发生 usage 不丢 |
| WS/Realtime、multipart、二进制供应商夹具 | 每个 `actions-*` 独占本供应商 fixture；公共 session/turn/receipt 由 `request-lifecycle`，入口矩阵由 `multimodal-ingress` 汇总 | owner/取消/超时/价格/重复任务正反例；仅有 404 不能证明已有动作支持 |
| 协议/全平台集成矩阵 | `multimodal-ingress` 集中核验，`integrated-acceptance` 最终复核；缺陷交原所有者修复 | 保留 18 平台全部动作格，硬冲突未裁决不得写全完成 |
| 本地 Vite 3000 / 后端 8080 与双实例演练 | 协调者统一启动、点选和分配；worker 不自起/重启主服务，额外 8081 仅由协调者调度 | 三协议请求、页面、数据库/收据一致；模拟外部连通必须标为夹具 |

HTTP/SSE/WS 测试服务器必须使用独立临时端口并在测试结束释放，真实供应商凭据不得放入 fixture。共享 fixture 若归属重叠，必须等待前一写者释放，不借“测试文件”名义并行修改共享源码。全部过程日志和核销证据只归主工作树 `.pi`；正式相邻测试在其行为实施任务提交，本契约任务不落预期失败测试。
