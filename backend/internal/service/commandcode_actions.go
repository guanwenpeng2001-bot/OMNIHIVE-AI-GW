package service

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/tidwall/gjson"
)

// Command Code Provider API 的动作面适配。
//
// 证据（第一方，2026-09-21 抓取；原文快照在 main-work/.pi/actions-commandcode/docs-*.txt）：
//
//  1. https://commandcode.ai/docs/provider（Provider API 参考）文档化了**完整**的四个端点，
//     base 为 https://api.commandcode.ai/provider/v1：
//     POST /chat/completions  OpenAI Chat Completions
//     POST /responses         OpenAI Responses
//     POST /messages          Anthropic Messages
//     GET  /models            模型列表；每个模型带 supported_endpoints 字段，
//     该字段才是“这个模型由哪些路由服务”的运行时权威。
//
//  2. 鉴权（同页 401 条目）：任意路由接受 `Authorization: Bearer <key>`；
//     Anthropic SDK 在 /messages 上接受 `x-api-key`。流式用 `stream: true`，
//     用量固定在流尾（CC 末块 usage / Responses 终态 response.completed /
//     Messages 的 message_delta），无需 opt-in。
//
//  3. 端点与模型匹配（同页“Supported endpoints and formats”）：OpenAI 与开源模型在
//     /chat/completions 回答、其中多数同时支持 /responses；Claude 模型只在 /messages
//     回答。把 Claude 打到 /chat/completions 或 /responses、把非 Anthropic 模型打到
//     /messages 都是 400 invalid_request_error。
//
//  4. 错误契约（同页“Errors”）：400 unsupported_model（未知模型，code 只在 OpenAI 信封上）、
//     400 invalid_request_error（端点与模型不匹配或请求体非法）、401 authentication_error
//     （缺失/无效鉴权）、403 upgrade_required（Go 套餐无 API 权限）、
//     422 cmd_zdr_no_providers（请求带 `x-cmd-zdr: 1` 但该模型无 ZDR 上游）、
//     429 rate_limit_error、5xx server_error/api_error。
//
//  5. 消息内容（同页 FAQ“What can I send in a message?”）：**只有文本与图片**；
//     audio、file、document 分片被 schema 拒绝。即本平台有“图片输入”，
//     没有任何图像/视频/音频生成端点；也没有独立检索或实时端点
//     （web_search 只是 Responses 的内建工具，由 responses 格转发给上游裁决）。
//
// 平台动作矩阵（全 28 格三态见 CommandCodeActionCells，证据时间同上）：
//   implemented：chat、messages、responses 走既有 OpenAI 两主干（原生协议转换/直通），
//   count_tokens 走本地估算桥。hard_conflict：其余全部生成/媒体/检索/实时/批处理格——
//   官方端点表只有上述四个端点，且 FAQ 明确 audio/file/document 分片被拒，
//   既无原生端点也无保真桥接。unverified：无，本平台不留未核证格。
//
// 归口说明：把 /provider/v1/responses 接成“非 Claude 模型的原生 Responses 选择”，
// 以及把 /models 的 supported_endpoints 写入模型注册表（methods 字段），属于
// domain 矩阵与 model-registry-data 的所有权，本分支不自行修改，已在 worker_done
// 的集成说明中登记给 multimodal-ingress 与 model-registry-data。

// CommandCodeProviderEndpoint 是 Provider API 文档化端点的路径（相对 provider/v1 base）。
type CommandCodeProviderEndpoint string

const (
	CommandCodeEndpointChatCompletions CommandCodeProviderEndpoint = "/chat/completions"
	CommandCodeEndpointResponses       CommandCodeProviderEndpoint = "/responses"
	CommandCodeEndpointMessages        CommandCodeProviderEndpoint = "/messages"
	CommandCodeEndpointModels          CommandCodeProviderEndpoint = "/models"
)

// CommandCodeZDRHeader 是 Provider API 的零数据保留开关（`x-cmd-zdr: 1`）。
// 网关不向客户端透传该头：需要 ZDR 的账号用账号级 HeaderOverride 配置，
// 这是本仓库既有的、可审计的头部配置通道。
const CommandCodeZDRHeader = "x-cmd-zdr"

// CommandCodeZDRUnavailableCode 是 422 的机器码：请求开了 ZDR 但该模型无 ZDR 上游。
const CommandCodeZDRUnavailableCode = "cmd_zdr_no_providers"

// CommandCodeEndpointFact 登记官方端点表的一行。它是路由选择的证据锚点，
// 不是运行时可达性授权（账号模式、模型证据与端点匹配仍逐请求校验）。
type CommandCodeEndpointFact struct {
	Endpoint CommandCodeProviderEndpoint
	Method   string
	Format   string
	Evidence string
}

// CommandCodeDocumentedEndpoints 返回 Provider API 参考文档化的全部端点。
// 官方只公布这四个，因此任何其它动作都不存在原生端点。
func CommandCodeDocumentedEndpoints() []CommandCodeEndpointFact {
	const evidence = "https://commandcode.ai/docs/provider (official Provider API reference, fetched 2026-09-21)"
	return []CommandCodeEndpointFact{
		{Endpoint: CommandCodeEndpointChatCompletions, Method: http.MethodPost, Format: "OpenAI Chat Completions", Evidence: evidence},
		{Endpoint: CommandCodeEndpointResponses, Method: http.MethodPost, Format: "OpenAI Responses (OpenAI and open models; Claude stays on /messages)", Evidence: evidence},
		{Endpoint: CommandCodeEndpointMessages, Method: http.MethodPost, Format: "Anthropic Messages (Claude models only)", Evidence: evidence},
		{Endpoint: CommandCodeEndpointModels, Method: http.MethodGet, Format: "Model list carrying supported_endpoints per model", Evidence: evidence},
	}
}

// commandCodeOpenAIModelFamily 报告模型是否属于官方口径里的 “OpenAI models”：
// 这些模型在 /responses 上被服务。目录里的这些 ID 形如 `gpt-5.6-sol`，
// 供应商前缀形式是 `openai/...`。运行时权威仍是 /models 的 supported_endpoints，
// 这里只给出证据可追溯的静态兜底。
func commandCodeOpenAIModelFamily(model string) bool {
	id := normalizeCommandCodeModelID(model)
	if strings.HasPrefix(id, "openai/") {
		return true
	}
	return strings.HasPrefix(id, "gpt-")
}

// CommandCodeModelEndpoint 返回官方端点表下服务该模型的端点。
// Claude 模型只在 /messages 回答；OpenAI 家族在 /responses；
// 其余目录模型由 /chat/completions 服务（其中多数也支持 /responses，
// 但只有该模型的 supported_endpoints 能证明，故静态兜底取 /chat/completions）。
func CommandCodeModelEndpoint(model string) CommandCodeProviderEndpoint {
	if matchModelProtocolRules(normalizeCommandCodeModelID(model), DefaultCommandCodeProtocolRules()) == APIProtocolAnthropic {
		return CommandCodeEndpointMessages
	}
	if commandCodeOpenAIModelFamily(model) {
		return CommandCodeEndpointResponses
	}
	return CommandCodeEndpointChatCompletions
}

// CommandCodeErrorKind 描述官方错误契约的一格。
// AccountFault 表示凭据/权限层面的账号问题（可停止调度）；Retryable 表示同账号可退避重试。
type CommandCodeErrorKind struct {
	Code          string
	Type          string
	AccountFault  bool
	Retryable     bool
	ClientMessage string
}

// commandCodeUpstreamErrorType 读取上游错误信封的类型字段：
// OpenAI 信封是 error.type，Anthropic 信封是 error.type（外层 type=error）。
func commandCodeUpstreamErrorType(body []byte) string {
	if !gjson.ValidBytes(body) {
		return ""
	}
	if value := strings.TrimSpace(gjson.GetBytes(body, "error.type").String()); value != "" {
		return strings.ToLower(value)
	}
	return ""
}

// commandCodeUpstreamErrorCode 返回上游信封里的机器码原文（官方给的是小写，
// 例如 unsupported_model / upgrade_required / cmd_zdr_no_providers）。
func commandCodeUpstreamErrorCode(body []byte) string {
	if !gjson.ValidBytes(body) {
		return ""
	}
	return strings.TrimSpace(gjson.GetBytes(body, "error.code").String())
}

// commandCodeProviderError 按官方错误契约给上游响应归类。
// 返回 false 表示该状态码/错误体不在文档化契约内（调用方保留既有兜底）。
func commandCodeProviderError(status int, body []byte) (CommandCodeErrorKind, bool) {
	code := commandCodeUpstreamErrorCode(body)
	upperCode := strings.ToUpper(code)
	errorType := commandCodeUpstreamErrorType(body)
	switch status {
	case http.StatusBadRequest:
		if upperCode == "UNSUPPORTED_MODEL" {
			return CommandCodeErrorKind{Code: code, Type: errorType, ClientMessage: "The requested model is not in the Command Code catalog"}, true
		}
		return CommandCodeErrorKind{Type: errorType, ClientMessage: "The requested model and endpoint do not match, or the request body is invalid"}, true
	case http.StatusUnauthorized:
		return CommandCodeErrorKind{Code: code, Type: errorType, AccountFault: true, ClientMessage: "Command Code authentication is missing or invalid"}, true
	case http.StatusForbidden:
		if upperCode == "UPGRADE_REQUIRED" || upperCode == "MODEL_NOT_IN_PLAN" {
			return CommandCodeErrorKind{Code: code, Type: errorType, ClientMessage: "The Command Code plan does not grant API access or this model"}, true
		}
		// 同一份凭据证据：handleCommandCodeError 命中它就会停调账号，
		// 因此这里的分类必须同口径，否则运维看到的诊断与账号状态互相矛盾。
		if isCommandCodeInvalidCredential(status, body) {
			return CommandCodeErrorKind{Code: code, Type: errorType, AccountFault: true, ClientMessage: "Command Code reported an invalid or revoked API key"}, true
		}
		return CommandCodeErrorKind{Code: code, Type: errorType, ClientMessage: "Command Code refused the request without establishing an account credential failure"}, true
	case http.StatusUnprocessableEntity:
		if strings.EqualFold(code, CommandCodeZDRUnavailableCode) {
			return CommandCodeErrorKind{Code: code, Type: errorType, ClientMessage: "The model has no zero-data-retention upstream; remove the " + CommandCodeZDRHeader + " request header or pick another model"}, true
		}
		return CommandCodeErrorKind{Code: code, Type: errorType, ClientMessage: "Command Code rejected the request conditions"}, true
	case http.StatusTooManyRequests:
		return CommandCodeErrorKind{Code: code, Type: errorType, Retryable: true, ClientMessage: "Command Code rate limit reached"}, true
	}
	if status >= 500 {
		return CommandCodeErrorKind{Code: code, Type: errorType, Retryable: true, ClientMessage: "Command Code upstream failure"}, true
	}
	return CommandCodeErrorKind{}, false
}

// CommandCodeActionCells 给出本平台 × 全部网关动作的逐格三态与证据。
// 本分支不修改 domain 权威矩阵（见文件头归口说明）；该表供
// multimodal-ingress 接线时取用，并由 commandcode_actions_test.go 逐格校验。
func CommandCodeActionCells() []domain.ActionDescriptor {
	// The cells are the authoritative platform matrix bound by
	// multimodal-ingress (internal/domain/commandcode_actions.go), so the
	// adapter's own register and Composite target selection cannot diverge.
	return domain.PlatformActionMatrix(domain.ActionIdentity{Platform: PlatformCommandCode})
}
