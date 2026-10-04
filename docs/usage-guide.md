# API 使用手册

本手册适用于通过本站网关调用模型的开发者，以及配置编程助手、聊天客户端和自动化工作流的用户。先用自己的 API Key 获取模型列表，再选择与客户端一致的协议。登录后的 `/help` 提供配置示例；模型、密钥、用量与账单以当前站点展示为准。

核对日期：**2026-10-04**。本文描述发行版本的路由与请求合同；示例经过离线语法检查，未执行真实模型调用。本文不代表每个模型、供应商账号、客户端版本或部署均已实测通过。

## 目录

- [开始调用：地址、密钥与模型](#开始调用地址密钥与模型)
- [供应商、区域与套餐](#供应商区域与套餐)
- [第一次请求：Bash 与 PowerShell](#第一次请求bash-与-powershell)
- [四种对话协议](#四种对话协议)
- [流式输出、工具、推理与缓存](#流式输出工具推理与缓存)
- [图片与异步图片](#图片与异步图片)
- [视频任务](#视频任务)
- [音频](#音频)
- [向量与重排](#向量与重排)
- [客户端与工作流](#客户端与工作流)
- [用量与价格](#用量与价格)
- [错误与重试](#错误与重试)
- [相关资料与维护依据](#相关资料与维护依据)

## 开始调用：地址、密钥与模型

### 地址填写规则

本站公开 OpenAI 兼容 Base URL 是 **`https://omnihive2.com/v1`**，根地址为 `https://omnihive2.com`。不要用后台管理接口或供应商上游地址代替；以下示例使用这一公开入口。其他私有部署请以其管理员公布的地址为准，并保留部署前缀。

| 名称 | 示例 | 用途 |
| --- | --- | --- |
| 网关根地址（本文 `GATEWAY_ROOT`） | `https://omnihive2.com` | 保存完整部署前缀，不含末尾 `/v1` 或 `/v1beta` |
| OpenAI 兼容 Base URL | `https://omnihive2.com/v1` | 客户端或 SDK 在后面追加 `/chat/completions`、`/responses` 等 |
| 完整请求 URL | `https://omnihive2.com/v1/chat/completions` | curl、HTTP 工作流节点等直接请求 |
| Gemini 完整请求 URL | `https://omnihive2.com/v1beta/models/MODEL_ID:generateContent` | Gemini 协议请求，模型 ID 按单个路径参数编码 |

如果站点提供的是 `https://omnihive2.com/v1`，本文根地址应填 `https://omnihive2.com`；只移除末尾版本段。私有部署若带 `/proxy` 等前缀，仍须保留前缀。不要重复追加 `/v1`，也不要把完整 Chat URL 填进会自行追加路径的客户端 Base URL。不同客户端的字段即使都叫“API 地址”，也可能要求不同形式。

### 密钥、分组和权限

创建本站 API Key，并为它选择正确分组。保存完整密钥，包括已签发的 `sk-` 前缀；登录密码、面板会话令牌和供应商自己的 API Key 不能代替本站 Key。示例从进程环境变量 `SUB2API_API_KEY` 读取密钥，不把它写进 JSON 或代码仓库。

能否调用取决于该 Key 的有效状态、用户及分组权限、模型范围、具体协议或动作、可用账号、额度与限流。**模型目录是当前 Key 的可见集合，能力元数据是选择提示，二者都不保证某个具体请求一定成功。** 一次 Chat 成功不能证明同一模型的 Messages、工具、图片或视频也可用。换 Key、换组或调整权限后，重新查询模型列表。

| 协议 | 推荐请求头 |
| --- | --- |
| Chat、Responses、图片、视频、音频、检索和普通模型列表 | `Authorization: Bearer <本站 API Key>` |
| Anthropic Messages | `x-api-key: <本站 API Key>`，另加 `anthropic-version: 2023-06-01` |
| Gemini | `x-goog-api-key: <本站 API Key>` |

请使用请求头传密钥。`/v1` 不支持 URL query key；当前 `/v1beta` 保留原生 `?key=` 兼容，但本文不使用它，避免凭据进入地址历史或代理日志。旧 `api_key` 查询参数不能作为替代。

### 用同一把 Key 查看模型

设置变量后执行以下只读请求（设置方式见下一节）：

```bash
curl --fail-with-body "$GATEWAY_ROOT/v1/models" \
  -H "Authorization: Bearer $SUB2API_API_KEY"
curl --fail-with-body "$GATEWAY_ROOT/v1/models/catalog" \
  -H "Authorization: Bearer $SUB2API_API_KEY"
```

| 接口 | 返回用途 |
| --- | --- |
| `GET /v1/models` | 默认 OpenAI 格式的极简列表，从 `data[].id` 取请求模型 ID |
| `GET /v1/models/{id}` | 一个可见模型的协议信息；不可见与不存在均可能为 404 |
| `GET /v1/models/catalog` | 富目录：能力、别名、来源与不确定性等；缺少字段表示未知，不能当作零或“不支持” |
| `GET /v1/models/catalog/{id}` | 某个模型的富目录详情 |
| `GET /v1beta/models` | Gemini 原生列表；名称和方法用于 Gemini 调用 |

普通列表还可提供已知的顶层 `context_window`、`max_input_tokens`、`max_output_tokens`、`supports_reasoning`、`supported_reasoning_levels`、`supports_function_calling` 和输入/输出模态等字段；未知值省略。富目录的能力事实区分 `guaranteed`、`available`、`unknown`，`available` 不能解释成每条路径都保证的值；`endpoints` 是当前授权视图的端点提示，不保证此刻有可用账号。

普通列表的响应形状示意：

```json
{"object":"list","data":[{"id":"MODEL_ID_FROM_LIST","object":"model","owned_by":"provider"}]}
```

这只是字段示意，不是站点实际模型清单。不要把显示名称当作 ID，也不要删除 `provider/` 等 ID 前缀。路径中的 ID 要整体 URL 编码，例如 `provider/model` 写成 `provider%2Fmodel`；JSON 中仍用原始 `provider/model`。没有可信创建时间时，`created` 或 `created_at` 可以省略。

带 `anthropic-version` 的 `/v1/models` 使用 Anthropic 列表格式与分页，不要再按 OpenAI 的外层字段解析。目录游标失效后应从第一页重查，不要复用另一把 Key 的游标。Codex 专用模型清单属于客户端兼容入口，不能用它代替普通列表的权限判断。

## 供应商、区域与套餐

用户只需配置本站公开地址、本站 API Key，以及该 Key 可见的精确模型 ID。**上游账号配置由管理员完成**：供应商、区域、套餐、上游 URL、协议和供应商凭据属于另一层。后台添加供应商不会自动改变你的分组权限；供应商 Key 也不能替代本站 Key。

供应商系列、地区、产品与套餐以当前后台目录为准，保留原有平台及账号路由。完整名称归一化、区域和套餐对应、上游根地址与鉴权见[管理员供应商接入参考](provider-configuration.md)。该文档中的供应商地址仅用于管理员配置；调用本站时仍使用 `https://omnihive2.com/v1`。Copilot / Nous 已实现手工凭据子集：Copilot fine-grained PAT、Nous invocation token 或官方代理；无自动 OAuth / refresh。它们的上游仅 Chat，本站 Messages / Responses / Gemini 是离线合同测试覆盖的桥接，真实可用性仍需运营方使用授权 key 验证。内部 `payg` 路由不表示供应商计费方式。RelayCode 身份未解决，不能与已实现的 RelaxyCode 合并；ETok.ai 预设历史移除不代表供应商停服。

管理员配置 Copilot 使用自己的 `Copilot-Integration-Id: omnihive`；模型发现是主机根 `/models`，与 `/v1/chat/completions` 分开。Nous invocation 到期手工换 token；官方代理需显式配置网关可访问的运营方 URL 前缀（不自动加 `/v1`），入口 key 可选。通过单账号编辑切换认证模式，必须明确换 key 或在切到无 key 代理时清空；同模式编辑保留密钥引用。不得读取客户端 token 存储或共享 refresh grants。

后台账号列表提供已接入供应商的手工查询：Novita、PPIO、SiliconFlow CN / Global、SoleAPI 和 AtlasCloud PAYG 钱包余额，胜算云普通钱包或企业项目预算，以及七牛 PAYG 的时间窗口已消耗用量。币种、Credits、kToken、预算周期与无限预算分别展示；精确条件见[余额与用量合同](provider-configuration.md#手工余额查询)。未接入或查询失败不是零余额，管理 key API 不等于推理 key API；地区、套餐元数据未知时不从另一站点补齐。Tencent Intl 个人套餐不能从共享主机继承企业 Responses，企业配置需显式选择对应 Responses 根。

| 核对项 | 应如何理解 |
| --- | --- |
| 区域与套餐 | 国内 / 国际、PAYG / Coding / Token Plan 可能使用不同地址和密钥；同名模型、同一公司或相似域名不证明密钥互通。 |
| 发现与权限 | 没有自动模型发现接口不等于没有模型访问权；管理员可维护已确认的模型映射。公开模型列表、预设示例和模型目录均不证明某个账号享有全部模型。 |
| 额度与价格 | 未接入余额查询不等于余额为零或无限；未展示价格不等于免费。本站销售价格、上游成本、套餐窗口与账户余额分别核对。 |
| 工具、流式、思考与缓存 | 根据实际协议、模型及目录能力元数据选参数。转换路径不保证所有供应商扩展字段、工具组合、缓存命中或折扣。 |
| 媒体 | 新增文本兼容供应商不自动新增原生图片、视频、音频适配器。单独核对对应媒体路由、模型与动作授权。 |
| 订阅 | Claude、ChatGPT、Google 等消费者订阅不自动授予 API 权限；Coding / Token Plan 还可能限制交互式使用、客户端、共享或后台自动化。 |

CC Switch 固定提交 `793e67d9b3210eb527e7d36f2c866a56fae9dec7` 提供的是客户端配置证据，供应商文档提供相应合同说明；二者均不等于账号真实调用认证。本文没有执行带供应商凭据的在线推理测试。

### 套餐配置与读数的区别

管理员先选供应商系列及地区/产品，再核对认证方式、套餐类型和协议。有套餐目录的账号可以保存、重新编辑或清除产品选择；例如 AICodeMirror 的 PRO / MAX / ULTRA / 按量付费，CCSub 的月度订阅 / 按量付费。选择只记录账号产品，不等于购买成功、获得额度或修改模型权限。独立接入模式（如 AtlasCloud Coding、七牛订阅）还须匹配专用 Key；同一 URL 不证明权限互通。套餐明细及清除规则见[套餐配置](provider-configuration.md#套餐选择保存与清除)。

| 页面读数 | 代表什么 | 不能推导什么 |
| --- | --- | --- |
| 上游钱包余额 | 供应商账号可用资金或 Credits，附币种和查询时间 | 不等于本站用户余额或订阅剩余次数 |
| 上游套餐/项目额度 | 指定产品、项目和周期的额度/消耗，须有对应查询合同 | 没有查询数据不表示零或无限，不能拿 PAYG 钱包替代 |
| 七牛时间窗口用量 | 所选今天/近 7 天/近 30 天各模型计费项的已消耗量 | 不是剩余额度，也不是现金；不同单位不能相加 |
| 本站账本消耗 | 本站对该用户、Key 和分组记下的请求用量与费用 | 不等于上游账号全量消耗，不包含绕过本站的请求 |

保存订阅/套餐后，未验证的 PAYG 查询会被阻止；AtlasCloud Coding、七牛订阅不能用普通余额或用量推测剩余额度。权限不足、查询失败、尚未接入均有独立含义；核对时间与错误状态，不把旧快照当实时读数。本站购买的订阅是本站分组权益，与管理员上游供应商套餐分别管理。

直接配置客户端时只需本站 URL + 本站 Key + 可见模型 ID；CC Switch 是可选的配置管理工具。它的供应商预设、本地协议转换和模型名称映射需要单独核对，不能替代网关的分组权限或证明上游支持某个协议。没有主动模型发现接口时，管理员仍可维护已确认的模型映射；模型同步与推理调用分别验证。

账号查询现已包含 Atlas 日模型消耗（余额按钮仍保留）、Azure 订阅区域配额、GitHub 个人账单、Together 组织月账单、腾讯国内企业 Token 套餐和 MiniMax 新国内 Token Plan。Azure/GitHub 需单独管理令牌；腾讯需独立云 Secret ID/Key、地域与团队，缺配置不妨碍保存推理设置。TheRouter 显示总 credits/总使用量，不推算余额；不同账单/周期/额度不可相加。腾讯和 Together 的精确十进制数量保留原样，`call_count` 不是 tokens。账户身份变化清旧数据；同身份失败保留有错误标记的历史读数。范围和来源见[新增查询说明](provider-configuration.md#独立管理查询与新增套餐)。

AiHubMix、DMX 中国站和 Qiniu PAYG 已接入模型发现；其他地区/订阅模式不继承此能力。候选模型不等于账号权益，失败不会清空已保存映射。

### 火山套餐读数与模型同步提示

国内 Ark Coding 的个人/团队套餐用量由管理员在账号表单另配查询 AK/SK，和推理 API Key 分开；查询密钥不回显，编辑留空保留，明确清除才删除。个人/团队选择只匹配已有产品，团队可指定席位或查询当前身份绑定席位。缺查询凭据不影响原推理配置，普通用户无需把这些管理凭据填入客户端。

5 小时、周、月显示的是已用百分比和重置时间，不是 Token 总量、现金或本站余额；重置时间不等于套餐到期时间。缺窗口、未订阅、无席位、认证/权限失败、上游错误都不能显示为零用量。失败保留此前成功读数，须结合更新时间判断。BytePlus 与个人 Ark Agent 不在已核实范围；国内团队 Agent 需明确席位，可显示每日及 5h/周/月真实窗口；详见[管理员配置与查询状态](provider-configuration.md#火山-ark-coding-套餐用量)。

管理员“同步上游模型”失败时，已有模型 ID、别名映射、白名单和上次成功目录保留。平台候选、手工选择与上游真实发现分别标识；候选可以用于手工配置，但不能当作该账号已授权。尚未确认发现接口不等于官方确认没有接口，也不等于该模型无法调用。404 路径、405 方法、403 权限、429 限流和网络失败应分别排查；不要为修复同步错误清空当前模型配置。完整原因码见[模型同步说明](provider-configuration.md#模型同步候选与失败处理)。

## 第一次请求：Bash 与 PowerShell

先将以下内容保存为 **UTF-8** 文件 `chat.json`，把 `MODEL_ID_FROM_LIST` 换成刚才那把 Key 列出的、允许 Chat Completions 的模型 ID：

```json
{
  "model": "MODEL_ID_FROM_LIST",
  "messages": [{"role": "user", "content": "Hello"}]
}
```

### Bash

适用于 Bash 和已安装 curl 的环境。将根地址换成本站公开地址；密钥通过不回显输入进入当前进程环境。

```bash
export GATEWAY_ROOT='https://omnihive2.com'
GATEWAY_ROOT="${GATEWAY_ROOT%/}"
read -r -s -p 'API key: ' SUB2API_API_KEY
printf '\n'
export SUB2API_API_KEY

curl --fail-with-body "$GATEWAY_ROOT/v1/chat/completions" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @chat.json
```

### Windows PowerShell

不需要 WSL。以下用 `Invoke-RestMethod` 发送相同 JSON，避免 PowerShell 的 `curl` 别名与 Bash 换行符差异。

```powershell
$env:GATEWAY_ROOT = 'https://omnihive2.com'
$env:GATEWAY_ROOT = $env:GATEWAY_ROOT.TrimEnd('/')
$secret = Read-Host 'API key' -AsSecureString
$env:SUB2API_API_KEY = [System.Net.NetworkCredential]::new('', $secret).Password
$request = @{
    Uri = "$env:GATEWAY_ROOT/v1/chat/completions"
    Method = 'Post'
    Headers = @{ Authorization = "Bearer $env:SUB2API_API_KEY" }
    ContentType = 'application/json; charset=utf-8'
    Body = [System.IO.File]::ReadAllBytes((Join-Path $PWD 'chat.json'))
}
Invoke-RestMethod @request | ConvertTo-Json -Depth 30
```

环境变量中的密钥在运行时仍是明文，仅向可信子进程提供。用完可关闭终端，或分别执行 `unset SUB2API_API_KEY` / `Remove-Item Env:SUB2API_API_KEY`。

### Python 与 Node.js

以下同样读取 `chat.json` 和已设置的两个环境变量。Python 使用标准库；Node.js 需要具有原生 `fetch` 的版本。这些是直接 HTTP 示例，避免依赖某个 SDK 的默认协议。

```python
import json
import os
import urllib.request
from pathlib import Path

body = json.loads(Path("chat.json").read_text(encoding="utf-8-sig"))
url = os.environ["GATEWAY_ROOT"].rstrip("/") + "/v1/chat/completions"
request = urllib.request.Request(
    url,
    data=json.dumps(body).encode("utf-8"),
    headers={
        "Authorization": "Bearer " + os.environ["SUB2API_API_KEY"],
        "Content-Type": "application/json",
    },
    method="POST",
)
with urllib.request.urlopen(request, timeout=120) as response:
    print(json.load(response))
```

```javascript
// 保存为 request.mjs，使用 node request.mjs 运行。
import { readFile } from 'node:fs/promises';
const root = process.env.GATEWAY_ROOT?.replace(/\/+$/, '');
const key = process.env.SUB2API_API_KEY;
if (!root || !key) throw new Error('Set GATEWAY_ROOT and SUB2API_API_KEY');
const body = JSON.parse((await readFile('chat.json', 'utf8')).replace(/^\uFEFF/, ''));
const response = await fetch(`${root}/v1/chat/completions`, {
  method: 'POST',
  headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});
if (!response.ok) throw new Error(`HTTP ${response.status}: ${await response.text()}`);
console.log(await response.json());
```

## 四种对话协议

选择与客户端发出的协议一致的入口。以下请求是最小文本示例；响应只展示读取结果所需的片段，不是完整字段清单。各协议中的模型 ID 均需独立确认可调用。

| 协议与方法 | 最小请求要点 | 成功结果主要位置 |
| --- | --- | --- |
| `POST /v1/chat/completions` | `model`、`messages` | `choices[].message`、`finish_reason`、`usage`（如提供） |
| `POST /v1/messages` | `model`、`max_tokens`、`messages`；版本请求头 | `content[]`、`stop_reason`、`usage` |
| `POST /v1/responses` | `model`、`input` | `output[]`、`status`、`usage`（如提供） |
| `POST /v1beta/models/{id}:generateContent` | 模型在路径；`contents[].parts` | `candidates[].content.parts`、`finishReason`、`usageMetadata`（如提供） |

### Chat Completions

使用上一节 `chat.json` 和完整请求。响应片段：

```json
{"choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}]}
```

不要假定 `message.content` 永远是最终文本：支持工具的模型也会返回 `tool_calls`。`finish_reason: "length"` 表示达到长度边界，应向用户显示输出可能不完整。

### Anthropic Messages

保存为 `messages.json`：

```json
{"model":"MODEL_ID_FROM_LIST","max_tokens":256,"messages":[{"role":"user","content":"Hello"}]}
```

```bash
curl --fail-with-body "$GATEWAY_ROOT/v1/messages" \
  -H "x-api-key: $SUB2API_API_KEY" \
  -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  --data-binary @messages.json
```

响应片段：

```json
{"type":"message","role":"assistant","content":[{"type":"text","text":"Hello!"}],"stop_reason":"end_turn"}
```

系统提示使用协议的顶层 `system`，不要把 Chat 的 `role: "system"` 原样套入 Messages 历史。`max_tokens` 是输出预算，不能超过所选模型与路径允许的范围；工具返回时可能出现 `stop_reason: "tool_use"`。

### Responses

保存为 `responses.json`：

```json
{"model":"MODEL_ID_FROM_LIST","input":"Hello"}
```

```bash
curl --fail-with-body "$GATEWAY_ROOT/v1/responses" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @responses.json
```

响应片段：

```json
{"object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello!"}]}]}
```

原始 HTTP 返回的文本在 `output[]` 的内容块中；不要假定它带 SDK 才提供的便捷 `output_text` 属性。对话历史应由客户端保存并重发，不把 `previous_response_id` 当作所有路由都支持的会话恢复方式。Responses WebSocket、compact 和其他子路径有单独限制，能调用 POST 不代表这些功能已开放。

### Gemini generateContent

保存为 `gemini.json`：

```json
{"contents":[{"role":"user","parts":[{"text":"Hello"}]}]}
```

下面使用 Python 对路径模型 ID 编码，再由 curl 发送。`MODEL_ID` 填请求 ID，不重复加资源名里的 `models/`。

```bash
export MODEL_ID='MODEL_ID_FROM_LIST'
MODEL_PATH=$(python -c 'import os, urllib.parse; print(urllib.parse.quote(os.environ["MODEL_ID"], safe=""))')
curl --fail-with-body "$GATEWAY_ROOT/v1beta/models/$MODEL_PATH:generateContent" \
  -H "x-goog-api-key: $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @gemini.json
```

响应片段：

```json
{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello!"}]},"finishReason":"STOP"}]}
```

Gemini 使用 `model` 表示模型回复角色；系统指令和工具字段也不同于 Chat。`countTokens`、`embedContent` 等方法是否可用，应按模型和动作单独确认，不能从 `generateContent` 成功推导。

## 流式输出、工具、推理与缓存

### SSE 的完成与中断

对支持流式的 Chat、Messages、Responses 请求，在 JSON 中加入 `"stream": true`，curl 加 `-N` 以减少客户端缓冲。例如保存为 `chat-stream.json`：

```json
{"model":"MODEL_ID_FROM_LIST","messages":[{"role":"user","content":"Hello"}],"stream":true}
```

```bash
curl -N --fail-with-body "$GATEWAY_ROOT/v1/chat/completions" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @chat-stream.json
```

Gemini 流式使用 `:streamGenerateContent?alt=sse` 和同样的 `contents` 请求体，不添加 Chat 的 `stream` 字段。是否支持流式仍取决于分组、模型及转发路径。

| 流 | 客户端应处理的语义 |
| --- | --- |
| Chat | 累积 `choices[].delta`；检查结束原因、错误帧与 `[DONE]`；`[DONE]` 本身不能抹去此前错误 |
| Messages | 按事件顺序处理内容块和增量，正常结束为 `message_stop`；`event: error` 是失败边界 |
| Responses | 读取类型化事件；`response.completed` 表示完成，`response.failed` 表示失败，`response.incomplete` 表示未完整完成 |
| Gemini | 按 SSE `data` 中的候选增量、结束原因与错误处理，不等待 OpenAI 的 `[DONE]` |

HTTP 200 只说明响应已开始，流内仍可能失败。心跳不是答案，连接关闭也不是完成证明。断网时错误帧可能无法送达；保留已有内容并标记未完成，检查用量后再决定是否发起新请求。不存在跨供应商通用断点续传，也不要把重连当作恢复原生成。上面的 PowerShell 示例适用于非流式 JSON；流式应用应使用能逐帧读取 SSE 的客户端。

### 工具调用与无状态历史

工具定义、选择策略和并行调用能力由模型与协议决定。网关转发模型的工具请求；应用负责检查参数、执行自己的工具，并将结果送回。多轮请求中保留原始调用 ID 和关联关系：

| 协议 | 下一轮应保留的内容 |
| --- | --- |
| Chat | 原 assistant 的 `tool_calls`，随后每个 `role: "tool"` 消息及匹配的 `tool_call_id` |
| Messages | assistant 的 `tool_use` 内容块，随后 user 的 `tool_result` 及匹配的 `tool_use_id` |
| Responses | 原 `function_call` 项及对应 `function_call_output`，以 `call_id` 关联，并携带所需历史 |
| Gemini | 原模型的 `functionCall` 与对应 `functionResponse`，保留协议要求的内容块和签名 |

本站不会替应用记住通用对话历史。不要只发工具结果、丢掉先前工具调用，或任意删除返回的推理/签名块。设置最大轮数、调用超时和工具权限；外部动作执行成功后，不因模型流中断而重复执行该动作。工作流中的子代理与主代理也要分别确认模型权限。

### 推理设置

某些路径可接收 Chat 的 `reasoning_effort`、Responses 的 `reasoning.effort`、Messages 的 `thinking` / `output_config.effort` 或 Gemini 的 `generationConfig.thinkingConfig`。这些字段的取值、预算、开关方式和与工具的组合依模型而异，不能把一个协议的字段直接复制到另一个协议。先运行该模型的最小请求，再按其已确认能力增加设置；某次响应没有可见推理文本，不足以证明推理关闭。目录里的推理能力和客户端“高/低”选项都不构成实际生效证明。

### 提示缓存

支持的路径可能接受 Messages 内容块的 `cache_control`，或使用上游的隐式缓存。命中取决于模型、前缀、请求格式与上游规则，不能通过任意添加 `cache_control` 强制命中。不要把缓存当作服务端保存会话的替代。

以响应与本站用量中实际提供的缓存读取、缓存创建等数据为准。缺少缓存字段表示未知，不自动算零；缓存命中也不等于本站承诺折扣，具体费用按本站销售报价计算。

## 图片与异步图片

### 同步生成与编辑

| 方法 | 请求形式 | 结果 |
| --- | --- | --- |
| `POST /v1/images/generations` | 通常为 JSON：`model`、`prompt`；可选参数按模型确认 | `data[]`，支持路径返回 `url` 或 `b64_json` |
| `POST /v1/images/edits` | 支持文件编辑的路径使用 multipart：`model`、`prompt`、`image`；部分路径另有 JSON 契约 | 编辑结果；蒙版、多图、尺寸等不是通用能力 |
| `POST /v1/images/variations` | 仅已开放该动作的模型；文件和参数依模型 | 不因可生成图片就保证支持变体 |

生成请求保存为 `image.json`：

```json
{"model":"IMAGE_MODEL_ID_FROM_LIST","prompt":"A small blue circle","n":1}
```

```bash
curl --fail-with-body "$GATEWAY_ROOT/v1/images/generations" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @image.json
```

文件编辑示例只用于已确认支持 multipart 的模型。`input.png` 是本地真实文件，模型 ID 按需替换：

```bash
curl --fail-with-body "$GATEWAY_ROOT/v1/images/edits" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  --form-string 'model=IMAGE_EDIT_MODEL_ID_FROM_LIST' \
  --form-string 'prompt=Make the background blue' \
  -F 'image=@input.png;type=image/png'
```

让 curl 生成 multipart boundary，不要手动添加 `Content-Type: application/json` 或一个没有 boundary 的 multipart 请求头。`url` 与 `b64_json` 不保证同时出现；图片链接可能过期，请及时保存。响应格式、张数、分辨率、质量和流式图片能力需按具体模型确认。

例如 OpenRouter 图片仅支持同步位图和 `b64_json` 输出，不能要求 URL、SVG、SSE、编辑或变体；千问编辑采用 multipart，不接受蒙版，结果格式仅 `b64_json`；千帆编辑则有自己的 JSON 格式。上面的单文件编辑示例适用于已允许相应动作的 OpenAI/千问模型，不能推广到所有图片模型。

### 异步提交与轮询

异步图片是站点可选功能。当前通用异步提交处理 OpenAI/Grok 目标路径（包括分组实际选中的相应目标），不是所有同步图片模型的通用包装；关闭时提交被拒绝且不创建任务。请求体沿用对应同步入口，不能提交流式图片请求。

| 方法 | 用途 |
| --- | --- |
| `POST /v1/images/generations/async` | 提交生成任务 |
| `POST /v1/images/edits/async` | 提交编辑任务，沿用对应请求类型 |
| `GET /v1/images/tasks/{task_id}` | 用提交时同一把 Key 查询任务 |

```bash
curl -i --fail-with-body "$GATEWAY_ROOT/v1/images/generations/async" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @image.json
```

接受时为 **202**，返回 `id`、`task_id`、`status`、`created_at`、`expires_at` 和 `poll_url`，并带 `Location` 与 `Retry-After`。202 不是图片已生成。保存实际 `task_id`，按 `Retry-After` 间隔查询：

```bash
TASK_ID='TASK_ID_FROM_RESPONSE'
curl --fail-with-body "$GATEWAY_ROOT/v1/images/tasks/$TASK_ID" \
  -H "Authorization: Bearer $SUB2API_API_KEY"
```

`poll_url` 可能是 `/v1/images/tasks/...` 这样的网关相对路径。部署有 `/proxy` 前缀时，应保留该前缀；上面的根地址拼接方式不会丢失它。不要将 Key 发往未经确认的外部地址。

| `status` | 下一步 |
| --- | --- |
| `processing` | 继续等候并轮询；GET 查询不新建生成任务 |
| `completed` | 读取 `result` 中的同步图片结果；若有 `image_url` 可用于取图 |
| `failed` | 读取 `error` 和可用的 `http_status`；停止轮询并排查 |

查询 HTTP 200 不等于任务成功。任务有 `expires_at`，到期后不能保证继续查询；及时保存结果。任务与提交者身份绑定，换另一把 Key 不能接管。提交超时且未取得任务 ID 时，不能保证上游未开始生成，避免盲目重发。单图异步任务没有公共取消或删除入口。批量图片用户可使用站点 `/batch-image`；批量任务另有契约，不能与单图异步任务混用 ID。

## 视频任务

视频不是对话接口。只有已开放视频动作的分组和模型才能创建；编辑、延长、取消分别有自己的能力限制。

| 方法 | 用途与边界 |
| --- | --- |
| `POST /v1/videos` 或 `/v1/videos/generations` | 创建视频；JSON 或 multipart 取决于模型路径 |
| `POST /v1/videos/edits` | 编辑视频，仅限支持此动作的路径 |
| `POST /v1/videos/extensions` | 延长视频，仅限支持此动作的路径 |
| `GET /v1/videos/{request_id}` | 用创建时的 Key 查询状态 |
| `GET /v1/videos/{request_id}/content` | 在结果可用后读取内容；可能返回媒体字节或相应交付结果 |
| `POST /v1/videos/{request_id}/cancel` | 当前 Qianwen/Qianfan 对应路径有取消实现；其他路径不作保证 |

以下以 OpenRouter 视频的 JSON 文本生成路径为例；时长、尺寸、参考媒体等附加项按选定模型填写，不作为其他视频路径的通用模板：

```json
{"model":"VIDEO_MODEL_ID_FROM_LIST","prompt":"A blue paper boat floating on calm water"}
```

保存为 `video.json` 后，调用完整 `/v1/videos/generations` URL，使用 Bearer 和 JSON 请求头，发送 `--data-binary @video.json`。保存实际返回的任务标识（路径可能返回 `id` 或 `request_id`），不要用模型响应 ID、图片任务 ID 或外部上游任务 ID 替代。

```bash
VIDEO_ID='VIDEO_ID_FROM_RESPONSE'
curl --fail-with-body "$GATEWAY_ROOT/v1/videos/$VIDEO_ID" \
  -H "Authorization: Bearer $SUB2API_API_KEY"
```

OpenAI/OpenRouter 视频对象包含 `id`、`object: "video"`、`status`、`created_at` 和 `model`，处理中通常是 `queued` 或 `in_progress`，成功是 `completed`；失败或超时也需要停止等待并处理。OpenRouter 不支持该标准视频接口的编辑、延长和取消。OpenAI 编辑/延长需要 `video.id` 指向同一 Key 已完成的本站视频，不能用任意远程 URL 代替。

确认任务成功且有内容可取后再请求 `/content`，检查响应状态与 `Content-Type` 后保存；不要把错误 JSON 当成视频。其他路径的状态字段及完成/失败词汇可能不同，不能把图片任务的三种状态硬套给所有视频。保留创建 Key 与所属权限；查询、下载、取消均校验任务归属，不能靠猜到任务 ID 读取别人的视频。千问取消确认 `cancellation_acknowledged: true` 仍不是最终取消，应继续查询原任务；取消请求成功不承诺立即停止或免除已发生费用。

## 音频

| 方法 | 输入 | 输出与限制 |
| --- | --- | --- |
| `POST /v1/audio/speech` | JSON，常见 `model`、`input`、`voice` | 按模型返回音频字节或相应结果；音色、编码格式各自有限制 |
| `POST /v1/audio/transcriptions` | multipart：`model`、`file` | 通常 JSON 的 `text`；可选格式不是所有模型都支持 |
| `POST /v1/audio/translations` | multipart：`model`、`file` | 独立翻译动作，不能由转写成功推导支持 |

以下 `speech.json` 仅用于接受这三个字段的语音合成模型，音色 ID 应取该模型支持值：

```json
{"model":"SPEECH_MODEL_ID_FROM_LIST","input":"Hello","voice":"VOICE_ID_FOR_MODEL"}
```

使用 Bearer 和 JSON 请求头调用 `/v1/audio/speech`；先检查响应状态、`Content-Type` 和模型格式再保存音频。不要用 JSON 解析器读取二进制结果。

转写示例：

```bash
curl --fail-with-body "$GATEWAY_ROOT/v1/audio/transcriptions" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  --form-string 'model=TRANSCRIPTION_MODEL_ID_FROM_LIST' \
  -F 'file=@sample.wav;type=audio/wav'
```

文件大小、时长、语言和输出格式取决于模型与站点限制。千问合成需要合法 `voice`，格式仅 WAV；OpenRouter 合成格式限 MP3/PCM 且不支持流式，转写仅接受 JSON/verbose JSON；Mistral 合成还可能返回 `audio_data` base64 JSON，而不是直接音频字节。不要凭文件扩展名推断服务会转换格式。

标准路径与供应商专用 `/v1/tts`、`/v1/stt` 的字段不同。当前 OpenAI 标准 speech/transcriptions 没有动作绑定，不能由“OpenAI 兼容”推导可用。HTTP 音频接口也不证明 Realtime WebSocket、语音克隆或某个供应商原生语音接口可用。

## 向量与重排

检索任务要使用专门的模型，不能把对话模型 ID 原样套用。

| 方法 | 必需请求字段 | 读取结果 |
| --- | --- | --- |
| `POST /v1/embeddings` | `model`、`input`；文本或文本数组是否允许按模型确认 | `data[].embedding`、`data[].index`；维度不能跨模型混用 |
| `POST /v1/rerank` | `model`、`query`、非空 `documents` 文本数组 | `results[].index`、`relevance_score`，按原文档索引关联 |

保存为 `embeddings.json`：

```json
{"model":"EMBEDDING_MODEL_ID_FROM_LIST","input":"A blue paper boat"}
```

保存为 `rerank.json`：

```json
{"model":"RERANK_MODEL_ID_FROM_LIST","query":"blue boat","documents":["A blue paper boat","A red bicycle"]}
```

```bash
curl --fail-with-body "$GATEWAY_ROOT/v1/embeddings" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @embeddings.json
curl --fail-with-body "$GATEWAY_ROOT/v1/rerank" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @rerank.json
```

`dimensions`、编码、批量长度、`top_n` 等参数仅在对应模型支持时使用。重排分数是相关性分值，不是统一概率。缺少 usage 不等于免费，按次收费的动作也不一定返回 token 数。

### 其他资源的定位

以下是已注册的专项入口，使用前应确认站点开放、模型/动作资格和对应资源契约。它们不属于所有客户端默认可用的通用能力。

| 资源 | 入口与用途 | 使用边界 |
| --- | --- | --- |
| 图片批次 | `/v1/images/batches` 的提交/列表、任务详情/下载/取消等 | 当前为获授权 Gemini 分组；用 `GET /v1/images/batches/models` 选择模型，参见站点 `/batch-image` |
| 文件 | `POST/GET /v1/files`，`GET/DELETE /v1/files/{file_id}`，`GET /v1/files/{file_id}/content` | 使用上传资源所属身份，文件 ID 不能跨业务接口随意复用 |
| 文本批次 | `POST/GET /v1/batches`，详情与 `POST /v1/batches/{batch_id}/cancel` | 独立批处理契约，不是单请求后台生成开关 |
| Messages 批次 | `/v1/messages/batches` 的提交/列表、详情/取消/删除/结果 | 与图片批次和文本批次的 ID、结果格式不同 |
| 内容审核 | `POST /v1/moderations` | 需相应模型与动作支持，不是所有文本模型都有审核功能 |
| 搜索 | `/v1/tools/search`、`/v1/tools/search_pro`、`/v1/web_search`、`/v1/x_search`、`/v1/alpha/search` | 供应商专用动作；普通 function tool 不会自动获得这些能力 |
| 实时、Live、自定义声音 | `GET /v1/realtime`、`POST /v1/live`、`/v1/custom-voices` 等 | 专项音频/会话契约；WebSocket 注册不代表采用同一种事件协议 |

强制平台前缀、Codex 专用前缀及 DashScope 原生入口服务于对应客户端或平台协议，不是任意供应商 API 的通配代理。一般应用优先使用本文的 `/v1`、`/v1beta` 规范入口，专项需求参阅[协议矩阵](gateway-platform-protocol-matrix.md)。

## 客户端与工作流

### 直接配置优先

客户端需要能够设置**自定义地址、本站 Key、准确模型 ID 和调用协议**。把这四项与本手册对应起来即可开始接入；客户端自己的登录订阅不自动变成本站额度。

| 客户端或方式 | 配置方向 | 兼容边界及官方依据 |
| --- | --- | --- |
| Claude Code | `ANTHROPIC_BASE_URL` 使用网关根地址；本站 Key 放在支持的 `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN`；使用 Messages | [LLM gateway](https://code.claude.com/docs/en/llm-gateway)；Agent Teams 仍是客户端工作流，每个子模型均需授权 |
| Codex | 自定义 provider 的 `base_url` 使用根地址加 `/v1`，`env_key` 指向 Key 环境变量，HTTP `wire_api = "responses"` | [配置参考](https://developers.openai.com/codex/config-reference)；不把旧 `wire_api = "chat"` 样例视作当前配置，WS 能力另行确认 |
| OpenCode | 选择匹配协议的 provider 包，配置其 `baseURL`、凭据和模型 ID | [Providers](https://opencode.ai/docs/providers/)；不同包可能发送不同协议，OpenCode 2 需按其独立文档配置 |
| DeepSeek Harness（DSH） | 使用对应 provider 的地址、凭据与模型配置；按模式选择 Chat、Responses 或 Messages | [Providers](https://deepseek-harness.github.io/deepseek-harness/en/guide/providers)；配置入口证据不等于每条模式均实测 |
| Pi | 自定义模型配置中的 `baseUrl` 使用相应 base；例如 `api: "openai-completions"` 对应 Chat | [Models](https://pi.dev/docs/latest/models)；不要把 Chat 的 base 与 Messages 协议混用 |
| HTTP 工作流 / 自写程序 | 完整 URL、协议请求头、JSON 或 multipart 请求体；凭据放在平台的秘密变量 | 能发送 HTTP 不代表自动维护工具历史、处理 SSE 或轮询媒体任务，需要明确实现这些步骤 |

以上依据客户端官方配置资料与既有客户端调查，并不表示本次对这些客户端做了真实调用。IDE、CLI、SDK、云端产品和同名新版本可能采用不同协议与 URL 拼接方式。Cursor 的官方 BYOK 说明不等于任意网关地址已验证；Kiro、Gemini CLI、Qoder CLI、Windsurf 等也不能只凭品牌名或社区代理说明承诺直连。代理转换同样要求客户端有可配置的接入点。

工作流先完成一次非流式文本请求，再逐项增加流式、工具或媒体任务。把主模型、子代理、摘要模型和向量模型分别设为当前 Key 允许的 ID；并发子任务共享额度与速率限制。重试节点不要无条件重放图片、视频或已经执行外部工具的步骤。

### CC Switch 是可选配置工具

可以手动配置地址和 Key，也可以使用站点提供的 CC Switch 导入流程。CC Switch 管理客户端配置，不负责新增本站模型权限或把不支持自定义地址的客户端变成可接入客户端。它也可以运行本地协议转换代理：例如客户端说 Responses，而代理向供应商发送 Chat。`wire_api = "responses"` 只说明客户端侧协议；必须同时核对上游协议和本地转换设置，不能据此推断供应商原生实现 `/responses`。

本站网关的协议转换是另一层：符合分组、账号模式及模型条件时，将公开的 Messages、Chat、Responses 或 Gemini 请求路由到对应上游。使用本站时，CC Switch 里填写本站地址和本站 Key；不要把管理员供应商地址混入导入项。先确认直连公开入口的协议，再决定是否需要本地转换。

导入前核对应用类型、模型及生成地址。不同分组或目标应用可能使用根地址、`/v1` 或专用前缀，不能统一替换成 Chat 完整 URL。没有配置公开网关地址时，不要猜测导入地址。密钥通常仅在创建或轮换时显示一次，妥善保存；分享配置前移除密钥。

## 用量与价格

使用面板“用量”核对调用记录、状态、模型、token/媒体单位和费用；“账单”用于余额、订阅与支付信息。API 可用当前 Key 查询：

```bash
curl --fail-with-body "$GATEWAY_ROOT/v1/usage" \
  -H "Authorization: Bearer $SUB2API_API_KEY"
```

余额、订阅周期额度、Key 自身预算和速率/并发限制是不同约束。提高 Key 限额不会给账户充值，支付跳转成功也不等于余额已经到账，应回到本站核对入账状态。

**客户支付的是本站销售价格。** 供应商成本、免费上游、带 `:free` 等字样的模型 ID、免费试用或缓存命中，均不自动承诺本站免费。图片、视频、音频和检索可能按张数、时长、字符、调用次数或组合单位计费，不能一律按文本 token 估算。未展示价格应先确认，不能当作零。

响应中的 `usage` 是调用证据的一部分，不一定是最终账单；字段缺失表示未知。失败、超时、取消或只收到部分内容时，上游可能已经发生用量，不能由 HTTP 状态推断“未收费”。以本站实际用量与结算记录核对，保留请求关联 ID 便于查询。

## 错误与重试

### 先看 HTTP，再看协议体和任务状态

| 情况 | 常见原因 | 处理方式 |
| --- | --- | --- |
| 400 | JSON/字段格式、模型参数、协议或 query key 用法不正确 | 先修正请求，不循环重试相同错误 |
| 401 | 缺少、错误、禁用或失效凭据 | 核对本站 Key 及请求头，不发送面板密码 |
| 403 | 分组、模型/动作权限、过期或其他访问限制 | 查看错误内容与 Key 配置；换协议不会增加权限 |
| 404 | 路径错误、模型不可见、功能未开放、任务不存在或归属不符 | 检查 Base/full URL、前缀、动作及原创建 Key |
| 413 | 请求或上传体积过大 | 缩小输入并检查模型/站点限制 |
| 429 | 速率、并发或额度限制 | 区分限流与额度耗尽；额度问题不能靠短延迟解决 |
| 5xx / 超时 | 上游或服务暂不可用、执行结果不明 | 对可安全重试的操作退避；生成任务先查结果与用量 |

具体错误码随路径而异，不要仅凭状态码识别所有原因。Chat/Responses 常用 `error` 对象；Messages 常为 `type: "error"` 加 `error`；Gemini 使用含 `code`、`message`、`status` 的 `error`。流式请求还需检查终止事件，异步任务需检查任务本身的 `status` 与 `error`。

### 安全地决定是否重试

只读列表和任务查询可以在暂时故障后使用有限次数、带抖动的指数退避，并遵循提供的 `Retry-After`。生成 POST 超时可能已被接受；先用已取得的任务 ID 查询，或检查用量及关联记录。没有结果证据时，由应用明确决定是否承担再次生成和计费的可能。

不要依赖给普通生成请求添加 `Idempotency-Key` 就能防止重复生成或重复计费；当前公开路由没有通用保证。重复相同正文、相同请求 ID 或重连 SSE 都不构成去重。单独资源的特定幂等契约不能推广到全部 API。

记录响应头中的 `X-Client-Request-ID`、可用的其他 request ID，以及发生时间、模型 ID、方法/路径、HTTP 状态和脱敏后的错误内容。该关联 ID 不是任务 ID，也不是幂等键。报障时提交这些信息；不要附带完整 Key、Authorization 请求头或含敏感内容的整段输入。

## 相关资料与维护依据

- [网关平台与协议矩阵](gateway-platform-protocol-matrix.md)：更细的路由和平台限制；路由注册不等于已开放或实测。
- [模型注册表合同](model-registry-contract.md) 与 [账号/模型访问合同](account-model-access.md)：目录、权限和来源语义，其中历史实施要求不应误读为当前功能保证。
- [公开路由定义](../backend/internal/server/routes/gateway.go)、[Google 鉴权](../backend/internal/server/middleware/api_key_auth_google.go)、[异步图片请求](../backend/internal/handler/image_task_handler.go)、[图片任务状态](../backend/internal/service/image_task.go)：本文端点、鉴权与任务语义的维护依据。
- [OpenRouter Quickstart](https://openrouter.ai/docs/quickstart) 与 [API Overview](https://openrouter.ai/docs/api_reference/overview)：仅参考先快速开始、再协议详情的组织方式，不移植其 SDK、参数、路由、收费或功能承诺。

更新本手册时同时核对路由、请求解析、权限限制及示例，注明核对日期与版本。离线解析证明的是示例语法；客户端文档证明的是配置方式；真实兼容性需要在明确的客户端版本、Key 权限、模型和具体动作范围内分别验证。
