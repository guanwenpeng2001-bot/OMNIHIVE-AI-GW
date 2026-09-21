package service

import "time"

// DeepSeek 动作面证据锚点（本文件只登记经核证的第一方事实，不含路由与暴露开关）。
//
// 证据来源（记录证据时间）：
//   - 官方 Models & Pricing 页 https://api-docs.deepseek.com/quick_start/pricing ，
//     页面快照保存于协调侧 model-registry-data/deepseek-inventory.{html,txt}，
//     保存时间 2026-09-20 22:50 UTC：
//     在售模型 deepseek-flash（DeepSeek-V4.1-Flash）与 deepseek-v4-pro
//     （DeepSeek-V4-Pro-0813）；旧名 deepseek-v4-flash / deepseek-v4-flash-vision-exp
//     已退役但仍被接受、按 Flash 档计价；OpenAI 格式 Base
//     https://api.deepseek.com ，Anthropic 格式 Base https://api.deepseek.com/anthropic ；
//     特性表：JSON Output / Tool Calls / Responses API / Anthropic API /
//     Chat Prefix Completion(Beta) / FIM Completion(Beta，仅非思考模式) 两模型均支持，
//     Vision 仅 deepseek-flash 支持（deepseek-v4-pro 不支持）；
//     峰谷价：高峰价 = 2× 低谷价，高峰时段 01:00–04:00 与 06:00–10:00 UTC
//     周一至周五、中国法定节假日除外，周末与节假日全天低谷。
//   - 同一快照的官方文档整站导航枚举了全部 API 能力面：Vision、Thinking Mode、
//     Multi-round Conversation、Chat Prefix(Beta)、FIM(Beta)、JSON Output、
//     Tool Calls、Files API、Context Caching、Responses API、Anthropic API。
//     导航中不存在 embeddings / rerank / 图像生成 / 视频 / 音频(TTS/STT/翻译) /
//     音色 / realtime / search 任何条目——这些动作格按硬冲突连本证据上报协调者，
//     不自动写 unsupported、也不虚构实现。
//   - Anthropic 兼容层无 /v1/messages/count_tokens（2026-08 核证，见
//     openai_gateway_count_tokens.go 注释），count_tokens 走本地 tiktoken 估算桥，
//     永不转发上游。
//   - 2026-09-10 官方降价公告 https://api-docs.deepseek.com/news/news260910
//     （billing_service.go 头注引据，2026-09-13 复核价页）。
//
// 平台动作矩阵结论（actions-deepseek 交付，逐格明细见 worker_done 三态表）：
//   已实现：chat、messages、responses（openai 主干原生协议/零转换直通）、
//   count_tokens（本地估算桥）。
//   硬冲突：embeddings、rerank、image.generate/edit/variation/async/batch、
//   video.generate/edit/extend/status/content、audio.tts/stt/translate、voice、
//   realtime、search、alpha_search、live、responses.compact、responses.websocket、
//   generate_content、native——官方资料无原生端点，对话接口亦不可保真桥接。
//   Files API 有官方文档但动作归属 gateway-resource-actions（标准 files 资源），
//   已在派发回报的集成说明中登记，不在本平台分支接线。

// deepseekChinesePublicHolidays 登记中国法定节假日（北京时间日历日，yyyy-mm-dd），
// 供 deepseekPeakMultiplierAt 做节假日低谷判定。官方价页口径："Peak hours are
// 01:00-04:00 and 06:00-10:00 UTC, Monday through Friday, excluding Chinese
// public holidays. All other hours are off-peak, including weekends and Chinese
// public holidays in full."（证据见文件头，2026-09-20 保存）。
//
// 2026 年日期按国务院办公厅节假日安排的通常口径登记（春节 8 天、国庆 7 天等，
// 周末日期一并列出仅作文档完整性，判定只在工作日起作用）；国务院发布新安排或
// 官方价页变更时须刷新本表（一手公告核对已列为协调者遗留项）。
var deepseekChinesePublicHolidays = map[string]bool{
	// 元旦：1 月 1 日至 3 日
	"2026-01-01": true, "2026-01-02": true, "2026-01-03": true,
	// 春节：2 月 16 日（除夕）至 23 日
	"2026-02-16": true, "2026-02-17": true, "2026-02-18": true, "2026-02-19": true,
	"2026-02-20": true, "2026-02-21": true, "2026-02-22": true, "2026-02-23": true,
	// 清明节：4 月 4 日至 6 日
	"2026-04-04": true, "2026-04-05": true, "2026-04-06": true,
	// 劳动节：5 月 1 日至 5 日
	"2026-05-01": true, "2026-05-02": true, "2026-05-03": true, "2026-05-04": true, "2026-05-05": true,
	// 端午节：6 月 19 日至 21 日
	"2026-06-19": true, "2026-06-20": true, "2026-06-21": true,
	// 中秋节：9 月 25 日至 27 日
	"2026-09-25": true, "2026-09-26": true, "2026-09-27": true,
	// 国庆节：10 月 1 日至 7 日
	"2026-10-01": true, "2026-10-02": true, "2026-10-03": true, "2026-10-04": true,
	"2026-10-05": true, "2026-10-06": true, "2026-10-07": true,
	// 2027 元旦（国务院通常在 2026 年底发布全年安排，届时刷新）
	"2027-01-01": true,
}

// isDeepSeekChinesePublicHoliday 判定北京时间日历日是否为中国法定节假日。
// 高峰窗口（01:00–04:00、06:00–10:00 UTC）折算北京时间为 09:00–12:00、
// 14:00–18:00，与 UTC 同一日历日，因此直接取北京时间日期查表即可。
func isDeepSeekChinesePublicHoliday(beijing time.Time) bool {
	return deepseekChinesePublicHolidays[beijing.Format("2006-01-02")]
}
