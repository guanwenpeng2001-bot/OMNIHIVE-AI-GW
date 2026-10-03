# 管理员供应商接入参考

本文面向配置上游账号的管理员；用户调用本站请阅读[API 使用手册](usage-guide.md)。**本站公开 Base URL 是 `https://omnihive2.com/v1`**。下文供应商地址仅用于后台账号配置，不能替换用户客户端中的本站地址；本站 Key 与供应商 Key 分别签发、分别管理。

核对日期：2026-10-04。兼容供应商合同基于候选源码 `09ba86e8f`；供应商资料观察日期为 2026-10-03。目录包含 **70 个新增兼容供应商 ID、71 个模式**，下方完整归一化表覆盖 **113 个原始名称项**。名称数、供应商 ID 数、模式数与账号数不是同一概念。

## 目录

- [配置步骤](#配置步骤)
- [证据与能力的读法](#证据与能力的读法)
- [完整名称归一化](#完整名称归一化)
- [新增供应商合同](#新增供应商合同70-个-id)
- [既有平台的区域、套餐和认证配置](#既有平台的区域套餐和认证配置)
- [特定账号限制与未知项](#特定账号限制与未知项)
- [媒体与公开入口](#媒体与公开入口)

## 配置步骤

1. 在供应商控制台确认账号所属站点、区域、套餐和实际 API 授权，再取得该合同对应的凭据。消费者订阅、桌面会员和 API 额度不能互相推定。
2. 在本站后台选择正确供应商及模式。PAYG、Coding、Token Plan 不共用默认密钥范围；同名模型也不证明同一能力或价格。Azure 使用资源地址和部署名称；KAT-Coder 需要按实际路由区分 Endpoint ID 与模型 ID。
3. 按下面合同核对协议和 base。表内新供应商 base 是最终操作路径之前的前缀；Messages 追加 `/messages`，Chat 追加 `/chat/completions`，Responses 追加 `/responses`。保留 `/codex/v1`、`/api/coding/v3` 等完整前缀。已保存的自定义地址、代理和协议覆盖应逐项核对，不自动改写到另一个官方域名。
4. 维护该账号已确认的模型与映射，挂入允许的组，按本站销售规则配置价格；再用该组签发的本站 Key 查看 `/v1/models` 与 `/v1/models/catalog`。模型显示名不能替代请求 ID；目录可见不保证此刻有可调度账号。
5. 按授权条件分别验证非流式、流式、工具、推理、缓存及所需媒体动作。保留脱敏的模型、协议、状态、时间和 request ID。本文没有使用真实供应商凭据执行推理调用，不声称这些配置均在线实测成功。

## 证据与能力的读法

- **供应商文档**是对应主机和合同的说明；**CC Switch 固定预设**是客户端配置证据，两者都不是账号访问权或运行认证。CC Switch 固定提交为 [`793e67d9b3210eb527e7d36f2c866a56fae9dec7`](https://github.com/farion1231/cc-switch/tree/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config)。旧资料与固定预设冲突时，本表明确保留选中的主机，不尝试跨域发送密钥。
- **原生**在下面新 ID 表中表示候选合同记录了上游操作，不表示每个模型均可执行。**网关转换**是本站请求格式到上游格式的转换。**CC Switch 本地转换**发生在客户端本机，是第三层；`wire_api = responses` 不能单独证明上游存在 `/responses`。
- `discovery = none` 表示没有启用主动模型发现，并非模型不可调用；按已确认的模型映射使用。已启用发现也只提供目录证据，不能越过账号/组授权。不能把另一个域名的模型、余额或价格接口当作当前域名的后备接口。
- 新供应商记录不自动开启余额、额度、价格同步或主动探测。没有数据用“未知 / 未接入”表示，不能展示为零、无限或免费。公开目录、示例模型和营销价格不构成账号权益；文档有余额接口也不代表本站已实现该接口。
- `/v1/messages`、`/v1/chat/completions`、`/v1/responses` 和 `/v1beta/models/{model}:generateContent`、`:streamGenerateContent` 是本站公开协议入口。是否可用取决于组、账号模式、实际选中的原生协议/转换与模型。新增 ID 未声明供应商原生 Gemini 操作；Gemini 到文本协议的转换不能改称供应商原生 Gemini。
- 工具参数、SSE 结束事件、thinking/reasoning 取值、上下文限制和 cache 读写计费均按实际路径核对。目录的 `guaranteed`、`available`、`unknown` 是不同事实强度；缺省字段不是否定值。转换不保证供应商所有扩展参数或完整无损映射。

## 完整名称归一化

“既有平台配置”指同一平台内的地区、套餐、认证或模型选择，不新增供应商 ID；既有配置的详细来源见后文。特殊鉴权待集成验证项没有计入 70 个新增 ID。

| 请求名称 | 本站 ID / 状态 | 范围说明 | 主要来源 |
| --- | --- | --- | --- |
| OpenRouter | `openrouter` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1630) |
| AiHubMix | [`aihubmix`](#provider-aihubmix) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1565) |
| DMXAPI | [`dmxapi`](#provider-dmxapi) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L846) |
| APINebula | [`apinebula`](#provider-apinebula) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L214) |
| APIKEY.FUN | [`apikey_fun`](#provider-apikey_fun) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L412) |
| A6API | [`a6api`](#provider-a6api) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L669) |
| 胜算云/Shengsuanyun | [`shengsuanyun`](#provider-shengsuanyun) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L295) |
| PIPELLM | [`pipellm`](#provider-pipellm) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1776) |
| TheRouter | [`therouter`](#provider-therouter) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1648) |
| CrazyRouter | [`crazyrouter`](#provider-crazyrouter) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L829) |
| TeamoRouter | [`teamorouter`](#provider-teamorouter) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L482) |
| SubRouter | [`subrouter`](#provider-subrouter) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L367) |
| RelayCode | 身份未解决 | 未找到唯一身份；不能当作 RelaxyCode 别名。 | [配置来源](https://github.com/farion1231/cc-switch/tree/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config) |
| RelaxyCode | [`relaxycode`](#provider-relaxycode) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1601) |
| CherryIN | [`cherryin`](#provider-cherryin) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1583) |
| JieKou AI | [`jiekou`](#provider-jiekou) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1897) |
| E FlowCode/E-FlowCode | [`eflowcode`](#provider-eflowcode) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1614) |
| ETok.ai | 仅历史记录 | 2026-10-02 从预设移除；历史记录不证明当前可接入，也不证明服务已关闭。 | [配置来源](https://github.com/farion1231/cc-switch/tree/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config) |
| Cubence | [`cubence`](#provider-cubence) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L807) |
| FennoAI | [`fennoai`](#provider-fennoai) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L263) |
| PatewayAI | [`pateway`](#provider-pateway) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L247) |
| RunAPI | [`runapi`](#provider-runapi) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L279) |
| ZetaAPI | [`zetaapi`](#provider-zetaapi) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L199) |
| Amux | [`amux`](#provider-amux) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L916) |
| Soshow | [`soshow`](#provider-soshow) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L948) |
| Micu | [`micu`](#provider-micu) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L774) |
| RightCode | [`rightcode`](#provider-rightcode) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L791) |
| XycAi | [`xycai`](#provider-xycai) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L897) |
| SoleAPI | [`soleapi`](#provider-soleapi) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L758) |
| AiCodeWith | [`aicodewith`](#provider-aicodewith) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1919) |
| AICodeMirror | [`aicodemirror`](#provider-aicodemirror) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L230) |
| AIGoCode | [`aigocode`](#provider-aigocode) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L315) |
| AICoding | [`aicoding`](#provider-aicoding) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L350) |
| 9527CODE | [`code_9527`](#provider-code_9527) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L432) |
| Code0 | [`code0`](#provider-code0) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L467) |
| ClaudeAPI | [`claudeapi`](#provider-claudeapi) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L452) |
| ClaudeCN | [`claudecn`](#provider-claudecn) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L524) |
| CCSub | [`ccsub`](#provider-ccsub) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L722) |
| SSSAiCode | [`sssaicode`](#provider-sssaicode) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L737) |
| SudoCode.chat | [`sudocode_chat`](#provider-sudocode_chat) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L862) |
| SudoCode.us | [`sudocode_us`](#provider-sudocode_us) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L882) |
| PackyCode | [`packycode`](#provider-packycode) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L177) |
| FluxA Token Plan | [`fluxa`](#provider-fluxa) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L382) |
| OpenCode Go | `opencode_go` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1007) |
| Astron Coding/Astron Coding Plan | [`astron_coding`](#provider-astron_coding) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2804) |
| Novita AI | [`novita`](#provider-novita) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1666) |
| PPIO | [`ppio`](#provider-ppio) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L502) |
| SiliconFlow | [`siliconflow_cn`](#provider-siliconflow_cn) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L629) |
| SiliconFlow en | [`siliconflow_global`](#provider-siliconflow_global) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L649) |
| ModelScope | [`modelscope`](#provider-modelscope) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1440) |
| Azure OpenAI | [`azure_openai`](#provider-azure_openai) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1332) |
| AWS Bedrock AKSK | `bedrock` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1829) |
| AWS Bedrock API Key | `bedrock` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1868) |
| GitHub Copilot | GitHub Copilot：待集成验证 | 特殊鉴权方案已确定但待集成验证，不列为已支持；不能用 GitHub Models/BYOK 替代 Copilot 权益。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1688) |
| Nvidia | [`nvidia`](#provider-nvidia) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1757) |
| Together AI | [`together`](#provider-together) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/tree/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config) |
| Nous Research | Nous Research：待集成验证 | 特殊鉴权方案已确定但待集成验证，不列为已支持；不导入 Portal refresh token 或冒用 Hermes 客户端。 | [配置来源](https://github.com/farion1231/cc-switch/tree/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config) |
| KAT-Coder | [`kat_coder`](#provider-kat_coder) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1457) |
| Vanchin Endpoint ID | [`kat_coder`](#provider-kat_coder) | KAT-Coder 的 Endpoint ID 配置字段，不是另一个供应商或密钥；按下方当前路由合同配置。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1457) |
| Compshare/优云智算 | [`compshare`](#provider-compshare) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L684) |
| Compshare Coding Plan | [`compshare_coding`](#provider-compshare_coding) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L703) |
| Qiniu/七牛云 | [`qiniu`](#provider-qiniu) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L333) |
| AtlasCloud | [`atlascloud`](#provider-atlascloud) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L929) |
| Command Code | `commandcode` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1934) |
| Claude Official | `anthropic` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L77) |
| OpenAI Official | `openai` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L133) |
| Google Official Gemini | `gemini` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/geminiProviderPresets.ts#L36) |
| Gemini Native | `gemini` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L967) |
| xAI Grok | `grok` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3021) |
| xAI Grok OAuth | `grok` | 既有 Grok OAuth 认证流程；会话与 API-key 服务不可混用，新增范围不作承诺。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1737) |
| Codex | `openai` | OpenAI 的 Codex OAuth 产品/鉴权变体，不是新的 API-key 供应商。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1707) |
| Kimi | `kimi` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L94) |
| Kimi Global | `kimi` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L115) |
| Kimi For Coding | `kimi` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L134) |
| Kimi For Coding Global | `kimi` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L158) |
| Kimi K3/K2.7 Code | `kimi` | 模型选择标签；区域及 PAYG / Kimi Code 权益需另选，不新增平台 ID。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L94) |
| Moonshot Kimi K3 | [`nvidia`](#provider-nvidia) | 预设中的 NVIDIA 模型 moonshotai/kimi-k3，不等于 Moonshot 直连账号。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1757) |
| DeepSeek | `deepseek` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L988) |
| Zhipu GLM | `zhipu` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1198) |
| Zhipu GLM en | `zhipu` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1216) |
| Baidu Qianfan | `qianfan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1504) |
| Baidu Qianfan Coding Plan | `qianfan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1234) |
| Baidu Qianfan Token Plan | `qianfan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1254) |
| 千问AI平台 | `qianwen` | 固定预设对应国内 DashScope；不能静默迁移到新的千问 MaaS 域名。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1279) |
| 千问AI平台 Coding Plan | `qianwen` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1301) |
| 千问AI平台 Token Plan | `qianwen` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1315) |
| QwenCloud | `qianwen` | 固定预设是国际 DashScope，归 qianwen；新的 MaaS 服务另用 qwencloud。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1342) |
| QwenCloud For Coding | `qianwen` | 国际 DashScope Coding；不等于 qwencloud MaaS。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1361) |
| QwenCloud Token Plan | [`qwencloud_token`](#provider-qwencloud_token) | 固定预设使用新加坡 maas.aliyuncs.com；MaaS Token Plan 是另一个 qwencloud_maas_token。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1380) |
| Tencent Hunyuan | `hunyuan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1910) |
| Tencent Token Plan | `hunyuan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1032) |
| Tencent Token Plan Intl | `hunyuan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1059) |
| Tencent Token Plan Enterprise Pro | `hunyuan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1087) |
| Tencent Token Plan Enterprise Pro Intl | `hunyuan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1115) |
| Tencent Token Plan Enterprise Lite | `hunyuan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1144) |
| Tencent Token Plan Enterprise Lite Intl | `hunyuan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1170) |
| Tencent TokenHub | `hunyuan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/tree/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config) |
| Tencent TokenHub Intl | `hunyuan` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/tree/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config) |
| 火山 Agent Plan | `volcengine` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L539) |
| 火山 Coding Plan | `volcengine` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L561) |
| Volcengine Doubao | `volcengine` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L606) |
| BytePlus | [`byteplus`](#provider-byteplus) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L583) |
| MiniMax | `minimax` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1503) |
| MiniMax en | `minimax` | 既有平台配置；不同区域、套餐与鉴权范围独立。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1526) |
| StepFun | [`stepfun_plan`](#provider-stepfun_plan) | 短名称按固定预设指 Step Plan，中国站；不是 PAYG。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1402) |
| StepFun en | [`stepfun_plan_intl`](#provider-stepfun_plan_intl) | 短名称按固定预设指 Step Plan，国际站；不是 PAYG。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1421) |
| StepFun API | [`stepfun_api`](#provider-stepfun_api) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2524) |
| StepFun API en | [`stepfun_api_intl`](#provider-stepfun_api_intl) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2553) |
| StepFun Step Plan | [`stepfun_plan`](#provider-stepfun_plan) | 与 StepFun 中国 Step Plan 同一配置，不再增加 ID。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2405) |
| Longcat | [`longcat`](#provider-longcat) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1483) |
| BaiLing | [`bailing`](#provider-bailing) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1549) |
| Xiaomi MiMo | [`mimo`](#provider-mimo) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1793) |
| Xiaomi MiMo Token Plan China | [`mimo_token_cn`](#provider-mimo_token_cn) | 新增兼容供应商；区域/模式见下方合同。 | [配置来源](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1811) |

### 为独立合同增设的 ID

原始名称之外，以下区别必须保留：`dmxapi_com`（.com）、`dmxapi_ssvip`（SSVIP）不同于 `dmxapi`（.cn）；`crazyrouter_intl` 不同于 `crazyrouter`；`teamorouter_com` 不同于 `teamorouter`；`qwencloud`（MaaS）与固定预设名称 QwenCloud 对应的 `qianwen`（国际 DashScope）不同；`qwencloud_maas_token` 与 `qwencloud_token` 不同。具体主机、协议与证据在下方逐项列出，不推定密钥互通。

## 新增供应商合同（70 个 ID）

表内 `Authorization: Bearer`、`x-api-key`、`api-key` 是**上游**鉴权，不改变用户访问本站时的鉴权格式。Messages 还使用运行时配置的 Anthropic 版本头。默认模式与默认协议来自候选合同；显式选择协议可能改变转换路线。转换名表示“公开入口 → 原生目标”，不是额外供应商 URL。

<a id="provider-a6api"></a>

### A6API — `a6api`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L669)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `anthropic` | `https://api.a6api.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.a6api.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `chat_completions` | `https://api.a6api.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_chat`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1002)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1221)。

<a id="provider-aicodemirror"></a>

### AICodeMirror — `aicodemirror`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L230)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.aicodemirror.ai/api/claudecode/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.aicodemirror.ai/api/codex/backend-api/codex` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L423)。

<a id="provider-aicodewith"></a>

### AiCodeWith — `aicodewith`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1919)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.aicodewith.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.aicodewith.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3345)。

<a id="provider-aicoding"></a>

### AICoding — `aicoding`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L350)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.aicoding.inc/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.aicoding.inc/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L543)。

<a id="provider-aigocode"></a>

### AIGoCode — `aigocode`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L315)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.aigocode.app/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.aigocode.app/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L506)。

<a id="provider-aihubmix"></a>

### AiHubMix — `aihubmix`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1565)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://aihubmix.com/v1` | `x-api-key` |
| `payg` / `anthropic` | `responses` | `https://aihubmix.com/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://aihubmix.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3205)；[供应商文档 2](https://aihubmix.com/openapi.json)。

<a id="provider-amux"></a>

### Amux — `amux`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L916)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `anthropic` | `https://api.amux.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.amux.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `chat_completions` | `https://api.amux.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_chat`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1264)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1612)。

<a id="provider-apikey_fun"></a>

### APIKEY.FUN — `apikey_fun`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L412)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.apikey.fan/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.apikey.fan/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L658)。

<a id="provider-apinebula"></a>

### APINebula — `apinebula`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L214)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://apinebula.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://apinebula.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://apinebula.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L400)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L527)。

<a id="provider-astron_coding"></a>

### Astron Coding — `astron_coding`

默认模式：`coding`。来源：[供应商主要文档](https://www.xfyun.cn/doc/spark/CodingPlan.html)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `chat_completions` | `chat_completions` | `https://maas-coding-api.cn-huabei-1.xf-yun.com/v2` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `anthropic` | `https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `responses` | `https://maas-coding-api.cn-huabei-1.xf-yun.com/v1` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

<a id="provider-atlascloud"></a>

### AtlasCloud — `atlascloud`

默认模式：`payg`。来源：[供应商主要文档](https://www.atlascloud.ai/docs/llm-protocols)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.atlascloud.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.atlascloud.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.atlascloud.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://www.atlascloud.ai/docs/public-api)；[供应商文档 2](https://www.atlascloud.ai/docs/faq)；[供应商文档 3](https://www.atlascloud.ai/docs/en/models/llm)。

<a id="provider-azure_openai"></a>

### Azure OpenAI — `azure_openai`

默认模式：`payg`。来源：[供应商主要文档](https://learn.microsoft.com/en-us/azure/foundry/openai/latest)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://{resource}.openai.azure.com/openai/v1` | `api-key` |
| `payg` / `chat_completions` | `responses` | `https://{resource}.openai.azure.com/openai/v1` | `api-key` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/responses)。

Azure 资源地址由管理员提供，没有通用全球 base。请求 `model` / 映射目标填写实际部署名；同一账号可以映射多个部署，不新增强制 deployment_id 凭据。此表是 Azure v1 合同，不能与旧日期版本 deployment URL 混用；没有全局模型发现。

<a id="provider-bailing"></a>

### BaiLing — `bailing`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2831)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.ant-ling.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.ant-ling.com/anthropic/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1549)。

<a id="provider-byteplus"></a>

### BytePlus Coding — `byteplus`

默认模式：`coding`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1154)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `chat_completions` | `chat_completions` | `https://ark.ap-southeast.bytepluses.com/api/coding/v3` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `anthropic` | `https://ark.ap-southeast.bytepluses.com/api/coding/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `responses` | `https://ark.ap-southeast.bytepluses.com/api/coding/v3` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L583)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L861)。

<a id="provider-ccsub"></a>

### CCSub — `ccsub`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L722)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://www.ccsub.net/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://www.ccsub.net/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://www.ccsub.net/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1056)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1254)。

<a id="provider-cherryin"></a>

### CherryIN — `cherryin`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1583)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://open.cherryin.net/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://open.cherryin.net/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://open.cherryin.net/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3222)；[供应商文档 2](https://docs.cherryin.ai/en/docs/newapi/getting-started/)。

<a id="provider-claudeapi"></a>

### ClaudeAPI — `claudeapi`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L452)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://gw.apito.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://gw.apito.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://gw.apito.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_chat`, `gemini_to_responses`。

操作级来源：[供应商文档 1](https://apito.ai/en/blog/tools/lobehub-claudeapi-apito/)；[供应商文档 2](https://apito.ai/en/blog/news/gpt-6-astra-agent-judgment/)。

<a id="provider-claudecn"></a>

### ClaudeCN — `claudecn`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L524)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://claudecn.top/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://claudecn.top/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L774)。

<a id="provider-code0"></a>

### Code0 — `code0`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L467)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `anthropic` | `https://code0.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://code0.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `chat_completions` | `https://code0.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_chat`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L705)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L955)。

<a id="provider-code_9527"></a>

### 9527CODE — `code_9527`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L432)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://9527.codes/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://9527.codes/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L685)。

<a id="provider-compshare"></a>

### Compshare — `compshare`

默认模式：`payg`。来源：[供应商主要文档](https://www.compshare.cn/docs/modelverse/models/text_api/model-competi)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.modelverse.cn/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.modelverse.cn/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.modelverse.cn/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://www.compshare.cn/docs/modelverse/models/common/certificate)；[供应商文档 2](https://www.compshare.cn/docs/modelverse/models/text_api/models)；[供应商文档 3](https://www.compshare.cn/docs/modelverse/models/text_api/response_api)；[供应商文档 4](https://www.compshare.cn/docs/modelverse/models/text_api/claude_compatible)；[供应商文档 5](https://www.compshare.cn/docs/modelverse/models/text_api/gemini_compatible)。

<a id="provider-compshare_coding"></a>

### Compshare Coding Plan — `compshare_coding`

默认模式：`coding`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1037)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `chat_completions` | `chat_completions` | `https://cp.compshare.cn/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `anthropic` | `https://cp.compshare.cn/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `responses` | `https://cp.compshare.cn/v1` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://compshare.cn/docs/modelverse/codingfaq)。

<a id="provider-crazyrouter"></a>

### CrazyRouter — `crazyrouter`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L829)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://cn.crazyrouter.com/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://cn.crazyrouter.com/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://cn.crazyrouter.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1164)；[供应商文档 2](https://docs.crazyrouter.com/en/api-endpoint)。

<a id="provider-crazyrouter_intl"></a>

### CrazyRouter International — `crazyrouter_intl`

默认模式：`payg`。来源：[供应商主要文档](https://docs.crazyrouter.com/en/api-endpoint)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.crazyrouter.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.crazyrouter.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.crazyrouter.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`gemini_to_chat`, `gemini_to_messages`, `gemini_to_responses`。

<a id="provider-cubence"></a>

### Cubence — `cubence`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L807)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.cubence.com/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.cubence.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1142)。

<a id="provider-dmxapi"></a>

### DMXAPI — `dmxapi`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L846)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://www.dmxapi.cn/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://www.dmxapi.cn/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://www.dmxapi.cn/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1180)；[供应商文档 2](https://doc.dmxapi.com/baseurl.html)。

<a id="provider-dmxapi_com"></a>

### DMXAPI (.com) — `dmxapi_com`

默认模式：`payg`。来源：[供应商主要文档](https://doc.dmxapi.com/baseurl.html)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://www.dmxapi.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

<a id="provider-dmxapi_ssvip"></a>

### DMXAPI SSVIP — `dmxapi_ssvip`

默认模式：`payg`。来源：[供应商主要文档](https://doc.dmxapi.com/baseurl.html)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://ssvip.dmxapi.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

<a id="provider-eflowcode"></a>

### E-FlowCode — `eflowcode`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1614)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://e-flowcode.cc/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://e-flowcode.cc/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3249)。

<a id="provider-fennoai"></a>

### FennoAI — `fennoai`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L263)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `anthropic` | `https://api.fenno.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.fenno.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `chat_completions` | `https://api.fenno.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_chat`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L457)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L590)。

<a id="provider-fluxa"></a>

### FluxA Token Plan — `fluxa`

默认模式：`coding`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L382)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `chat_completions` | `anthropic` | `https://api.baiduqianfan.ai/anthropic/tokenplan/team/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `chat_completions` | `https://api.baiduqianfan.ai/v2/tokenplan/team` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`responses_to_chat`, `responses_to_messages`, `gemini_to_chat`, `gemini_to_messages`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L575)。

<a id="provider-jiekou"></a>

### JieKou AI — `jiekou`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1897)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `anthropic` | `https://api.jiekou.ai/anthropic/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `chat_completions` | `https://api.jiekou.ai/openai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`responses_to_chat`, `responses_to_messages`, `gemini_to_chat`, `gemini_to_messages`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3321)。

<a id="provider-kat_coder"></a>

### KAT-Coder — `kat_coder`

默认模式：`payg`。来源：[供应商主要文档](https://www.streamlake.ai/document/DOC/mg6k6nlp8j6qxicx4c9)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://vanchin.streamlake.ai/api/gateway/v1/endpoints` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://vanchin.streamlake.ai/api/gateway/v1/endpoints/kat-coder-pro-v2.5/claude-code-proxy/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `chat_completions` | `chat_completions` | `https://vanchin.streamlake.ai/api/gateway/coding/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `anthropic` | `https://vanchin.streamlake.ai/api/gateway/coding/kat-coder-pro-v2.5/claude-code-proxy/v1` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://www.streamlake.ai/document/DOC/mh1gbfvrdn6hpbzxixv)；[供应商文档 2](https://www.streamlake.ai/document/DOC/mkaylxzkso38sqxcpa)。

Vanchin Endpoint ID 是路由模板字段，不是新供应商。固定预设中的旧模板与当前供应商路径有差别；本表采用候选合同的具体路由，不能把 ENDPOINT_ID 任意拼进 Chat URL，或用 API Key 填此字段。

<a id="provider-longcat"></a>

### LongCat — `longcat`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2516)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.longcat.chat/openai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.longcat.chat/anthropic/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.longcat.chat/openai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1483)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2698)。

<a id="provider-micu"></a>

### Micu — `micu`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L774)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://www.micuapi.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://www.micuapi.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1109)。

<a id="provider-mimo"></a>

### Xiaomi MiMo — `mimo`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2649)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.xiaomimimo.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.xiaomimimo.com/anthropic/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.xiaomimimo.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1793)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2852)。

<a id="provider-mimo_token_cn"></a>

### Xiaomi MiMo Token Plan (China) — `mimo_token_cn`

默认模式：`coding`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2700)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `chat_completions` | `chat_completions` | `https://token-plan-cn.xiaomimimo.com/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `anthropic` | `https://token-plan-cn.xiaomimimo.com/anthropic/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `responses` | `https://token-plan-cn.xiaomimimo.com/v1` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1811)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2924)。

<a id="provider-modelscope"></a>

### ModelScope — `modelscope`

默认模式：`payg`。来源：[供应商主要文档](https://community.modelscope.cn/67e3a1aa8393e26e2659c12e.html)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api-inference.modelscope.cn/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://api-inference.modelscope.cn/v1/models)；[供应商文档 2](https://modelscope.cn/docs/model-service/API-Inference/intro)。

<a id="provider-novita"></a>

### Novita AI — `novita`

默认模式：`payg`。来源：[供应商主要文档](https://docs.novita.ai/api-reference/model-apis-llm-create-chat-completion)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.novita.ai/openai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://docs.novita.ai/api-reference/model-apis-llm-list-models)。

<a id="provider-nvidia"></a>

### NVIDIA Hosted — `nvidia`

默认模式：`payg`。来源：[供应商主要文档](https://docs.api.nvidia.com/nim/reference/llm-apis)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://integrate.api.nvidia.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://integrate.api.nvidia.com/v1/models)。

<a id="provider-packycode"></a>

### PackyCode — `packycode`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L177)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://www.packyapi.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://www.packyapi.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L363)。

<a id="provider-pateway"></a>

### PatewayAI — `pateway`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L247)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.pateway.ai/v1` | `x-api-key` |
| `payg` / `anthropic` | `responses` | `https://api.pateway.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L441)。

<a id="provider-pipellm"></a>

### PIPELLM — `pipellm`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1776)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://cc-api.pipellm.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://cc-api.pipellm.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3273)。

<a id="provider-ppio"></a>

### PPIO — `ppio`

默认模式：`payg`。来源：[供应商主要文档](https://ppio.com/docs/models/reference-llm-create-chat-completion)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.ppio.com/openai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.ppio.com/anthropic/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://ppio.com/docs/model/llm-anthropic-compatibility.md)。

<a id="provider-qiniu"></a>

### Qiniu — `qiniu`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L523)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.qnaigc.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.qnaigc.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.qnaigc.com/bypass/openai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://developer.qiniu.com/aitokenapi/kb/13462/aitoken-use-faq)；[供应商文档 2](https://developer.qiniu.com/aitokenapi/13379/real-time-ai-interface-api)。

<a id="provider-qwencloud"></a>

### QwenCloud MaaS — `qwencloud`

默认模式：`payg`。来源：[供应商主要文档](https://docs.qwencloud.com/api-reference/preparation/api-key)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://maas.qwencloudapi.com/compatible-mode/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://maas.qwencloudapi.com/apps/anthropic/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://maas.qwencloudapi.com/compatible-mode/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

<a id="provider-qwencloud_maas_token"></a>

### QwenCloud MaaS Token Plan — `qwencloud_maas_token`

默认模式：`coding`。来源：[供应商主要文档](https://docs.qwencloud.com/token-plan/personal/token-plan-personal-quickstart)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `chat_completions` | `chat_completions` | `https://token-plan.maas.qwencloudapi.com/compatible-mode/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `anthropic` | `https://token-plan.maas.qwencloudapi.com/apps/anthropic/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `responses` | `https://token-plan.maas.qwencloudapi.com/compatible-mode/v1` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

<a id="provider-qwencloud_token"></a>

### QwenCloud Token Plan — `qwencloud_token`

默认模式：`coding`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1865)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `responses` | `anthropic` | `https://token-plan.ap-southeast-1.maas.aliyuncs.com/apps/anthropic/v1` | `Authorization: Bearer` |
| `coding` / `responses` | `responses` | `https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_responses`, `messages_to_responses`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1380)。

<a id="provider-relaxycode"></a>

### RelaxyCode — `relaxycode`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1601)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://www.relaxycode.com/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://www.relaxycode.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3236)。

<a id="provider-rightcode"></a>

### RightCode — `rightcode`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L791)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://www.rightapi.ai/claude/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://www.rightapi.ai/codex/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://www.rightapi.ai/codex/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1126)；[供应商文档 2](https://docs.right.codes/docs/rc_extension/cherrystudio)。

<a id="provider-runapi"></a>

### RunAPI — `runapi`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L279)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://runapi.host/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://runapi.host/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L474)。

<a id="provider-shengsuanyun"></a>

### Shengsuanyun — `shengsuanyun`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L295)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://router.shengsuanyun.com/api/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://router.shengsuanyun.com/api/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://router.shengsuanyun.com/api/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L490)；[供应商文档 2](https://lean.shengsuanyun.com/openai-api)。

<a id="provider-siliconflow_cn"></a>

### SiliconFlow CN — `siliconflow_cn`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L629)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.siliconflow.cn/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.siliconflow.cn/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://raw.githubusercontent.com/siliconflow/siliconcloud/main/openapi.yaml)；[供应商文档 2](https://raw.githubusercontent.com/siliconflow/siliconflow-open-source-tasks/main/README.md)。

<a id="provider-siliconflow_global"></a>

### SiliconFlow Global — `siliconflow_global`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L649)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.siliconflow.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.siliconflow.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[供应商文档 1](https://raw.githubusercontent.com/siliconflow/siliconflow-open-source-tasks/main/README_EN.md)；[供应商文档 2](https://docs.siliconflow.com/en/api-reference/models/get-model-list)。

<a id="provider-soleapi"></a>

### SoleAPI — `soleapi`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L758)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://soleapi.com/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://soleapi.com/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://soleapi.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1093)；[供应商文档 2](https://soleapi.com/zh/docs/config/general)。

<a id="provider-soshow"></a>

### Soshow — `soshow`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L948)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://maas.so-show.com/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://maas.so-show.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1317)。

<a id="provider-sssaicode"></a>

### SSSAiCode — `sssaicode`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L737)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://node-hk.sssaicodeapi.com/api/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://node-hk.sssaicodeapi.com/api/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1072)。

<a id="provider-stepfun_api"></a>

### StepFun API (China) — `stepfun_api`

默认模式：`payg`。来源：[供应商主要文档](https://platform.stepfun.com/docs/zh/quickstart/overview.md)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.stepfun.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.stepfun.com/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.stepfun.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2524)。

<a id="provider-stepfun_api_intl"></a>

### StepFun API (International) — `stepfun_api_intl`

默认模式：`payg`。来源：[供应商主要文档](https://platform.stepfun.ai/docs/en/quickstart/overview.md)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.stepfun.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `anthropic` | `https://api.stepfun.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://api.stepfun.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2553)。

<a id="provider-stepfun_plan"></a>

### StepFun Step Plan (China) — `stepfun_plan`

默认模式：`coding`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2580)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `chat_completions` | `chat_completions` | `https://api.stepfun.com/step_plan/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `anthropic` | `https://api.stepfun.com/step_plan/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `responses` | `https://api.stepfun.com/step_plan/v1` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1402)；[供应商文档 2](https://platform.stepfun.com/docs/zh/step-plan/integrations)。

<a id="provider-stepfun_plan_intl"></a>

### StepFun Step Plan (International) — `stepfun_plan_intl`

默认模式：`coding`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2620)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `coding` / `chat_completions` | `chat_completions` | `https://api.stepfun.ai/step_plan/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `anthropic` | `https://api.stepfun.ai/step_plan/v1` | `Authorization: Bearer` |
| `coding` / `chat_completions` | `responses` | `https://api.stepfun.ai/step_plan/v1` | `Authorization: Bearer` |

`coding`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1421)；[供应商文档 2](https://platform.stepfun.ai/docs/en/step-plan/integrations/claude-code.md)。

<a id="provider-subrouter"></a>

### SubRouter — `subrouter`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L367)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `anthropic` | `https://subrouter.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `responses` | `https://subrouter.ai/v1` | `Authorization: Bearer` |
| `payg` / `chat_completions` | `chat_completions` | `https://subrouter.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_chat`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L559)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L807)。

<a id="provider-sudocode_chat"></a>

### SudoCode.chat — `sudocode_chat`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L862)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.sudocode.chat/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.sudocode.chat/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1194)。

<a id="provider-sudocode_us"></a>

### SudoCode.us — `sudocode_us`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L882)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://sudocode.us/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://sudocode.us/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`chat_to_messages`, `chat_to_responses`, `gemini_to_messages`, `gemini_to_responses`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1221)。

<a id="provider-teamorouter"></a>

### TeamoRouter — `teamorouter`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L482)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.teamorouter.cn/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.teamorouter.cn/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://api.teamorouter.cn/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L721)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L988)。

<a id="provider-teamorouter_com"></a>

### TeamoRouter (.com) — `teamorouter_com`

默认模式：`payg`。来源：[供应商主要文档](https://teamorouter.com)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.teamorouter.com/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

<a id="provider-therouter"></a>

### TheRouter — `therouter`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1648)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.therouter.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.therouter.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://api.therouter.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3308)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2887)。

<a id="provider-together"></a>

### Together AI — `together`

默认模式：`payg`。来源：[供应商主要文档](https://docs.together.ai/docs/inference/openai-compatibility)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `chat_completions` | `chat_completions` | `https://api.together.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `openai_models`（协议 `chat_completions`）；网关转换：`messages_to_chat`, `responses_to_chat`, `gemini_to_chat`。

<a id="provider-xycai"></a>

### XycAi — `xycai`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L897)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://apicdn.xycai.us/v1` | `x-api-key` |
| `payg` / `anthropic` | `responses` | `https://apicdn.xycai.us/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://apicdn.xycai.us/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1244)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1578)。

<a id="provider-zetaapi"></a>

### ZetaAPI — `zetaapi`

默认模式：`payg`。来源：[CC Switch 固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L199)（观察日期 2026-10-03）。

| 模式 / 默认协议 | 原生协议 | 上游 base | 上游鉴权 |
| --- | --- | --- | --- |
| `payg` / `anthropic` | `anthropic` | `https://api.zetaapi.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `responses` | `https://api.zetaapi.ai/v1` | `Authorization: Bearer` |
| `payg` / `anthropic` | `chat_completions` | `https://api.zetaapi.ai/v1` | `Authorization: Bearer` |

`payg`：模型发现 `none`（未接入，不等于无模型访问权）；网关转换：`gemini_to_messages`, `gemini_to_responses`, `gemini_to_chat`。

操作级来源：[固定配置 1](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L384)；[固定配置 2](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L499)。

## 既有平台的区域、套餐和认证配置

下面是固定客户端预设中各名称对应的配置来源，不是 70 个新增 ID 的合同表。**原始客户端 base**可能由 SDK 追加 `/v1`，与上一节已规范化的网关 base 不同；不得不经核对直接覆盖已保存账号。界面中尚未出现的地区/套餐预设应视为待集成配置，不凭本表宣称当前部署已经支持。原生能力与模型授权仍以实际适配器、所选模式和同主机供应商文档为准。

| 名称 / 既有平台 | 客户端 | 原始客户端 base | 上游协议 / 本地转换 | 凭据语义 | 来源 |
| --- | --- | --- | --- | --- | --- |
| OpenRouter / `openrouter` | Claude | `https://openrouter.ai/api` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1630) |
| OpenRouter / `openrouter` | Codex | `https://openrouter.ai/api/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3294) |
| OpenRouter / `openrouter` | Gemini | `https://openrouter.ai/api` | `gemini_native` / 否 | x-goog-api-key; generic Gemini API-key mode | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/geminiProviderPresets.ts#L486) |
| OpenRouter / `openrouter` | OpenCode | `https://openrouter.ai/api/v1` | `anthropic` / 否 | x-api-key (Anthropic SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2849) |
| OpenCode Go / `opencode_go` | Claude | `https://opencode.ai/zen/go` | `anthropic` / 否 | x-api-key + anthropic-version | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1007) |
| OpenCode Go / `opencode_go` | Codex | `https://opencode.ai/zen/go/v1` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3132) |
| OpenCode Go / `opencode_go` | OpenCode | `https://opencode.ai/zen/go/v1` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2747) |
| AWS Bedrock AKSK / `bedrock` | Claude | `https://bedrock-runtime.${AWS_REGION}.amazonaws.com` | `bedrock` / 否 | AWS SigV4 access key/secret/region | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1829) |
| AWS Bedrock API Key / `bedrock` | Claude | `https://bedrock-runtime.${AWS_REGION}.amazonaws.com` | `bedrock` / 否 | Bearer AWS_BEARER_TOKEN_BEDROCK | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1868) |
| Command Code / `commandcode` | Claude | `https://api.commandcode.ai/provider` | `openai_chat` / 是 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1934) |
| Command Code / `commandcode` | Codex | `https://api.commandcode.ai/provider/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3361) |
| Gemini Native / `gemini` | Claude | `https://generativelanguage.googleapis.com` | `gemini_native` / 是 | x-goog-api-key | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L967) |
| xAI Grok / `grok` | Codex | `https://api.x.ai/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L3021) |
| Kimi / `kimi` | Claude | `https://api.moonshot.cn/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L94) |
| Kimi / `kimi` | Codex | `https://api.moonshot.cn/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L150) |
| Kimi / `kimi` | OpenCode | `https://api.moonshot.cn/v1` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L297) |
| Kimi Global / `kimi` | Claude | `https://api.moonshot.ai/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L115) |
| Kimi Global / `kimi` | Codex | `https://api.moonshot.ai/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L214) |
| Kimi Global / `kimi` | OpenCode | `https://api.moonshot.ai/v1` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L345) |
| Kimi For Coding / `kimi` | Claude | `https://api.kimi.com/coding/` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L134) |
| Kimi For Coding / `kimi` | Codex | `https://api.kimi.com/coding/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L248) |
| Kimi For Coding / `kimi` | OpenCode | `https://api.kimi.com/coding/v1` | `anthropic` / 否 | x-api-key (Anthropic SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L391) |
| Kimi For Coding Global / `kimi` | Claude | `https://api.kimi.ai/coding/` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L158) |
| Kimi For Coding Global / `kimi` | Codex | `https://api.kimi.ai/coding/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L314) |
| Kimi For Coding Global / `kimi` | OpenCode | `https://api.kimi.ai/coding/v1` | `anthropic` / 否 | x-api-key (Anthropic SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L426) |
| DeepSeek / `deepseek` | Claude | `https://api.deepseek.com/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L988) |
| DeepSeek / `deepseek` | Codex | `https://api.deepseek.com` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1360) |
| DeepSeek / `deepseek` | OpenCode | `https://api.deepseek.com/v1` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1696) |
| Zhipu GLM / `zhipu` | Claude | `https://open.bigmodel.cn/api/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1198) |
| Zhipu GLM / `zhipu` | Codex | `https://open.bigmodel.cn/api/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1401) |
| Zhipu GLM / `zhipu` | OpenCode | `https://open.bigmodel.cn/api/coding/paas/v4` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1731) |
| Zhipu GLM en / `zhipu` | Claude | `https://api.z.ai/api/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1216) |
| Zhipu GLM en / `zhipu` | Codex | `https://api.z.ai/api/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1464) |
| Zhipu GLM en / `zhipu` | OpenCode | `https://api.z.ai/api/coding/paas/v4` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1769) |
| Baidu Qianfan / `qianfan` | Codex | `https://qianfan.baidubce.com/v2` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1504) |
| Baidu Qianfan Coding Plan / `qianfan` | Claude | `https://qianfan.baidubce.com/anthropic/coding` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1234) |
| Baidu Qianfan Coding Plan / `qianfan` | Codex | `https://qianfan.baidubce.com/v2/coding` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1542) |
| Baidu Qianfan Token Plan / `qianfan` | Claude | `https://qianfan.baidubce.com/anthropic/tokenplan/personal` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1254) |
| Baidu Qianfan Token Plan / `qianfan` | Codex | `https://qianfan.baidubce.com/v2/tokenplan/personal` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1580) |
| Baidu Qianfan Token Plan / `qianfan` | OpenCode | `https://qianfan.baidubce.com/v2/tokenplan/personal` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2057) |
| 千问AI平台 / `qianwen` | Claude | `https://dashscope.aliyuncs.com/apps/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1279) |
| 千问AI平台 / `qianwen` | Codex | `https://dashscope.aliyuncs.com/compatible-mode/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1667) |
| 千问AI平台 / `qianwen` | OpenCode | `https://dashscope.aliyuncs.com/compatible-mode/v1` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2097) |
| 千问AI平台 Coding Plan / `qianwen` | Claude | `https://coding.dashscope.aliyuncs.com/apps/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1301) |
| 千问AI平台 Token Plan / `qianwen` | Claude | `https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1315) |
| 千问AI平台 Token Plan / `qianwen` | Codex | `https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1721) |
| 千问AI平台 Token Plan / `qianwen` | OpenCode | `https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic/v1` | `anthropic` / 否 | x-api-key (Anthropic SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2140) |
| QwenCloud / `qianwen` | Claude | `https://dashscope-intl.aliyuncs.com/apps/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1342) |
| QwenCloud / `qianwen` | Codex | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1764) |
| QwenCloud / `qianwen` | OpenCode | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2191) |
| QwenCloud For Coding / `qianwen` | Claude | `https://coding-intl.dashscope.aliyuncs.com/apps/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1361) |
| QwenCloud For Coding / `qianwen` | Codex | `https://coding-intl.dashscope.aliyuncs.com/v1` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1831) |
| QwenCloud For Coding / `qianwen` | OpenCode | `https://coding-intl.dashscope.aliyuncs.com/apps/anthropic/v1` | `anthropic` / 否 | x-api-key (Anthropic SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2234) |
| Tencent Hunyuan / `hunyuan` | Codex | `https://tokenhub.tencentmaas.com/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1910) |
| Tencent Token Plan / `hunyuan` | Claude | `https://api.lkeap.cloud.tencent.com/plan/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1032) |
| Tencent Token Plan / `hunyuan` | Codex | `https://api.lkeap.cloud.tencent.com/plan/v3` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L1970) |
| Tencent Token Plan / `hunyuan` | OpenCode | `https://api.lkeap.cloud.tencent.com/plan/v3` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1807) |
| Tencent Token Plan Intl / `hunyuan` | Claude | `https://tokenhub-intl.tencentcloudmaas.com/plan/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1059) |
| Tencent Token Plan Intl / `hunyuan` | Codex | `https://tokenhub-intl.tencentcloudmaas.com/plan/v3` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2072) |
| Tencent Token Plan Intl / `hunyuan` | OpenCode | `https://tokenhub-intl.tencentcloudmaas.com/plan/v3` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1853) |
| Tencent Token Plan Enterprise Pro / `hunyuan` | Claude | `https://tokenhub.tencentmaas.com/plan/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1087) |
| Tencent Token Plan Enterprise Pro / `hunyuan` | Codex | `https://tokenhub.tencentmaas.com/plan/v3` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2144) |
| Tencent Token Plan Enterprise Pro / `hunyuan` | OpenCode | `https://tokenhub.tencentmaas.com/plan/v3` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1894) |
| Tencent Token Plan Enterprise Pro Intl / `hunyuan` | Claude | `https://tokenhub-intl.tencentcloudmaas.com/plan/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1115) |
| Tencent Token Plan Enterprise Pro Intl / `hunyuan` | Codex | `https://tokenhub-intl.tencentcloudmaas.com/plan/v3` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2307) |
| Tencent Token Plan Enterprise Pro Intl / `hunyuan` | OpenCode | `https://tokenhub-intl.tencentcloudmaas.com/plan/v3` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1945) |
| Tencent Token Plan Enterprise Lite / `hunyuan` | Claude | `https://tokenhub.tencentmaas.com/plan/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1144) |
| Tencent Token Plan Enterprise Lite / `hunyuan` | Codex | `https://tokenhub.tencentmaas.com/plan/v3` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2434) |
| Tencent Token Plan Enterprise Lite / `hunyuan` | OpenCode | `https://tokenhub.tencentmaas.com/plan/v3` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1990) |
| Tencent Token Plan Enterprise Lite Intl / `hunyuan` | Claude | `https://tokenhub-intl.tencentcloudmaas.com/plan/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1170) |
| Tencent Token Plan Enterprise Lite Intl / `hunyuan` | Codex | `https://tokenhub-intl.tencentcloudmaas.com/plan/v3` | `openai_chat` / 是 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2479) |
| Tencent Token Plan Enterprise Lite Intl / `hunyuan` | OpenCode | `https://tokenhub-intl.tencentcloudmaas.com/plan/v3` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2023) |
| 火山 Agent Plan / `volcengine` | Claude | `https://ark.cn-beijing.volces.com/api/plan` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L539) |
| 火山 Agent Plan / `volcengine` | Codex | `https://ark.cn-beijing.volces.com/api/plan/v3` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L789) |
| 火山 Agent Plan / `volcengine` | OpenCode | `https://ark.cn-beijing.volces.com/api/plan/v3` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1088) |
| 火山 Coding Plan / `volcengine` | Claude | `https://ark.cn-beijing.volces.com/api/coding` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L561) |
| 火山 Coding Plan / `volcengine` | Codex | `https://ark.cn-beijing.volces.com/api/coding/v3` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L826) |
| 火山 Coding Plan / `volcengine` | OpenCode | `https://ark.cn-beijing.volces.com/api/coding/v3` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1121) |
| Volcengine Doubao / `volcengine` | Claude | `https://ark.cn-beijing.volces.com/api/compatible` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L606) |
| Volcengine Doubao / `volcengine` | Codex | `https://ark.cn-beijing.volces.com/api/v3` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L898) |
| Volcengine Doubao / `volcengine` | OpenCode | `https://ark.cn-beijing.volces.com/api/v3` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L1187) |
| MiniMax / `minimax` | Claude | `https://api.minimax.cn/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1503) |
| MiniMax / `minimax` | Codex | `https://api.minimax.cn/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2728) |
| MiniMax / `minimax` | OpenCode | `https://api.minimax.cn/v1` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2552) |
| MiniMax en / `minimax` | Claude | `https://api.minimax.io/anthropic` | `anthropic` / 否 | Authorization: Bearer | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/claudeProviderPresets.ts#L1526) |
| MiniMax en / `minimax` | Codex | `https://api.minimax.io/v1` | `openai_responses` / 否 | Bearer via auth.json OPENAI_API_KEY | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/codexProviderPresets.ts#L2766) |
| MiniMax en / `minimax` | OpenCode | `https://api.minimax.io/v1` | `openai_chat` / 否 | Bearer (OpenAI SDK) | [固定配置](https://github.com/farion1231/cc-switch/blob/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config/opencodeProviderPresets.ts#L2588) |

### 地区与套餐解释

- **OpenRouter**：已有原生 Messages、Responses 及媒体/视频适配能力，不能降为 Chat-only；每次请求仍受模型、账号和动作资格约束。参阅 [API reference](https://openrouter.ai/docs/api_reference/overview)。
- **Claude / OpenAI / Gemini**：官方 API Key 与消费者订阅分离。Codex 走既有 OAuth 接入流程；Google Native 与 OpenAI-compatible 是同一 Gemini 平台的两类协议，不把本地转换的 Claude 配置当作原生 Messages。参阅 [Claude API](https://platform.claude.com/docs/en/api/overview)、[OpenAI API](https://platform.openai.com/docs/api-reference/introduction)、[Gemini API](https://ai.google.dev/gemini-api/docs)。
- **Grok**：API-key 主机与 OAuth 会话服务保持分离。固定客户端预设不是公开第三方 OAuth 服务合同，不导入浏览器 cookie，也不承诺新的消费者订阅接入。保留已有认证流程；新增范围需单独确认。参阅 [xAI 文档](https://docs.x.ai/docs)。
- **Bedrock**：AK/SK 签名与 Bedrock API Key 属既有认证模式，区域、模型 / inference profile 权限分别核对。不能假定 API Key 替代所有 AWS 管理面签名。参阅 [Bedrock API keys](https://docs.aws.amazon.com/bedrock/latest/userguide/api-keys.html)。
- **Kimi / Zhipu / MiniMax**：国内与国际、PAYG 与 Coding 不互借 Key。Kimi Code 模型标签与 Moonshot PAYG 模型 ID 不同；MiniMax 历史 `api.minimaxi.com` 配置应保留，不能因新 `.cn` / `.io` 文档而静默重写。参阅 [Kimi Code](https://www.kimi.com/code/docs/)、[Moonshot](https://platform.moonshot.ai/docs)、[Z.ai](https://docs.z.ai/)、[MiniMax](https://platform.minimax.io/docs)。
- **Qianfan**：PAYG、Coding 和个人 Token Plan 分开；FluxA 团队转售合同使用独立 `fluxa` ID，不继承 Qianfan 的发现、额度或模型元数据。Coding 的交互式编程工具限制不因保存账号而消失。参阅 [Qianfan Coding Plan](https://cloud.baidu.com/doc/qianfan/s/imlg0beiu)。
- **千问 / QwenCloud**：固定预设的千问和 QwenCloud PAYG/Coding 分别指国内/国际 DashScope，归 `qianwen`；新 `qwencloud` MaaS、`qwencloud_maas_token`、新加坡 `qwencloud_token` 不合并。新增中国 MaaS PAYG `https://maas.qianwenaiapi.com` 与 MaaS Token `https://token-plan.maas.qianwenaiapi.com` 仍需分别匹配账号合同；资料和候选区域预设不授权自动迁移旧 DashScope Key。参阅 [Coding Plan 使用限制](https://help.aliyun.com/zh/model-studio/coding-plan)、[QwenCloud API key](https://docs.qwencloud.com/api-reference/preparation/api-key)。
- **Tencent**：个人、企业 Pro、企业 Lite 与国内/国际站分别确认。企业 Pro / Lite 可以共用 `/plan` 主机，但模型与额度由套餐决定；Lite 不能借用 Pro 目录。`/plan` 与 TokenHub PAYG `/v1` 不能交换 Key；国内站新加坡主机不等于国际站。额度管理 API 使用管理权限，不是推理 Key 的余额 API。参阅 [Tencent TokenHub](https://cloud.tencent.com/document/product/1823/130079)、[Tencent Cloud international](https://www.tencentcloud.com/document/product/1300/78940)。
- **Volcengine / BytePlus**：国内 PAYG、Coding、Agent Plan 路径分别维护；BytePlus 是独立国际合同。Agent Key 不发送到 Coding/PAYG 主机，模型名与推理端点 ID 分别核对。参阅 [Volcengine Ark](https://www.volcengine.com/docs/82379)。
- **OpenCode Go / Command Code**：原始客户端配置中的本地 Chat 转换不构成供应商原生 Responses 或 Messages 证据；同时不删除供应商文档已明确的原生能力。按所选模式和模型验证，订阅窗口与 PAYG 余额分别处理。参阅 [OpenCode Go](https://opencode.ai/docs/go/)、[Command Code](https://commandcode.ai/docs/provider)。

## 特定账号限制与未知项

这些说明记录供应商的用途边界，不新增本站本地拒绝规则，也不帮助绕过供应商限制。

| 配置 | 必须核对的限制 | 主要依据 |
| --- | --- | --- |
| Micu | `vip_1_max_enterprise` 与 `vip_1_max_ext` 面向不同客户端范围；不要复制供应商客户端 User-Agent 冒充受支持客户端。 | [供应商说明](https://docs.micuapi.ai/openclaw) |
| SSSAiCode | 2026-09-11 公告说明 OpenAI 调用限 Codex 客户端，第三方/API 使用受限；预设不能证明任意后台使用权。 | [供应商公告](https://wind.sssaicode.com/) |
| AICoding | 配置条目不建立任意通用后端调用权；确认具体账号/镜像产品授权，不冒充原客户端。 | [供应商站点](https://aicoding.inc/en/tutorial) |
| Baidu Qianfan Coding / DashScope Coding | 交互式个人编程工具限制，不能推导为脚本、后台应用、共享 Key 或非交互批量任务授权。Token Plan 要独立确认。 | [阿里云 Coding Plan](https://help.aliyun.com/zh/model-studio/coding-plan) |
| SiliconFlow CN / Global | 两站账号及 Key 不互通；保留命名空间和大小写，不借用另一站模型目录。 | [官方国际说明](https://github.com/siliconflow/siliconflow-open-source-tasks/blob/main/README_EN.md) |
| Compshare Coding | 套餐用尽/到期与 PAYG 独立；切换 PAYG 同时核对 Key 和 base，不能仅改模式。 | [Coding FAQ](https://compshare.cn/docs/modelverse/codingfaq) |
| Azure / KAT-Coder | Azure 部署名与 KAT Endpoint ID 各有用途；显示名称、模型 ID、部署 ID 不能互换。 | [Azure v1](https://learn.microsoft.com/en-us/azure/foundry/openai/latest)、[KAT 接入](https://www.streamlake.ai/document/DOC/mg6k6nlp8j6qxicx4c9) |
| Copilot / Nous / RelayCode / ETok.ai | Copilot 与 Nous 特殊鉴权待集成验证；RelayCode 身份未解决；ETok.ai 仅历史记录。这四项不作为新增已支持供应商。 | [固定配置目录](https://github.com/farion1231/cc-switch/tree/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config) |

未知余额、模型发现、价格、推理设置或缓存信息，不等于访问被拒绝。访问被拒绝需要结合实际 HTTP 状态、协议错误体、所选账号/组、模型和动作判断；401、403、404、429 与上游暂时失败分别排查。不要用一条失败请求倒推出所有模型、区域或套餐都不可用。

## 媒体与公开入口

新增兼容文本合同不创建原生媒体适配器。站点已注册的媒体路由是独立功能：

| 类别 | 本站路由示例 | 范围 |
| --- | --- | --- |
| 图片 | `POST /v1/images/generations`、`/v1/images/edits`、`/v1/images/variations` | 按模型、尺寸、输入格式和账号原生动作检查。 |
| 异步图片 | `POST /v1/images/generations/async`、`GET /v1/images/tasks/{task_id}` | 使用本站返回的任务 ID，查询归属与原 Key 保持一致。 |
| 视频 | `POST /v1/videos`、`/v1/videos/generations`、`/v1/videos/edits`、`/v1/videos/extensions` | 创建、状态、下载、取消分别有动作资格；不将 Chat 成功当成视频权限。 |
| 音频 | `POST /v1/audio/speech`、`/v1/audio/transcriptions`、`/v1/audio/translations` | TTS 二进制响应和 STT multipart 有独立合同；专用 `/v1/tts`、`/v1/stt` 不作通用替代。 |

完整请求、返回结构、流式处理、工具循环、思考/缓存、费用及故障处理见[使用手册](usage-guide.md)。本站接口以[实际公开路由](../backend/internal/server/routes/gateway.go)与[平台协议矩阵](gateway-platform-protocol-matrix.md)为准；[兼容供应商合同源码](../backend/internal/domain/data/compatible_providers)是上表维护依据。OpenRouter 文档提供组织与 API 概念参考，不导入其插件、路由优先级、免费或价格承诺。
