import type {
  AsyncErrorContext,
  CheckMixedChannelLike,
  ConnectionCheck,
  ConnectionField,
  ConnectionOperation,
  ConnectionResource,
  ConnectionSchema,
  ConnectionVerification,
  CredentialOperationMode,
  HeaderOverrideDraftRow,
  MixedChannelDetails,
  TempUnschedRuleInput,
  TempUnschedRulePayload,
  ValidationFieldIssue
} from './types'
import { normalizeApiError } from '@/utils/apiError'

export function parsePoolModeRetryStatusCodes(input: string): number[] {
  if (!input || !input.trim()) return []
  const seen = new Set<number>()
  const out: number[] = []
  for (const token of input.split(/[,\s]+/)) {
    const trimmed = token.trim()
    if (!trimmed) continue
    const parsed = Number(trimmed)
    if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) continue
    if (parsed < 100 || parsed > 599) continue
    if (seen.has(parsed)) continue
    seen.add(parsed)
    out.push(parsed)
  }
  return out.sort((left, right) => left - right)
}

export function formatPoolModeRetryStatusCodes(value: unknown): string {
  if (!Array.isArray(value)) return ''
  const out: number[] = []
  const seen = new Set<number>()
  for (const entry of value) {
    const parsed = typeof entry === 'string' ? Number(entry.trim()) : Number(entry)
    if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) continue
    if (parsed < 100 || parsed > 599) continue
    if (seen.has(parsed)) continue
    seen.add(parsed)
    out.push(parsed)
  }
  return out.sort((left, right) => left - right).join(', ')
}

export type ErrorCodeValidationResult =
  | { ok: true; requiresConfirm?: 429 | 529 }
  | { ok: false; reason: 'invalid' | 'duplicate' }

export function validateCustomErrorCode(
  code: number | null,
  existing: readonly number[]
): ErrorCodeValidationResult {
  if (code === null || !Number.isFinite(code) || !Number.isInteger(code) || code < 100 || code > 599) {
    return { ok: false, reason: 'invalid' }
  }
  if (existing.includes(code)) {
    return { ok: false, reason: 'duplicate' }
  }
  if (code === 429 || code === 529) {
    return { ok: true, requiresConfirm: code }
  }
  return { ok: true }
}

export function toggleErrorCodeInList(codes: readonly number[], code: number): number[] {
  const index = codes.indexOf(code)
  if (index === -1) {
    return [...codes, code]
  }
  return [...codes.slice(0, index), ...codes.slice(index + 1)]
}

export function splitTempUnschedKeywords(value: string): string[] {
  return value
    .split(/[,;]/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}

export function buildTempUnschedRules(rules: readonly TempUnschedRuleInput[]): TempUnschedRulePayload[] {
  const out: TempUnschedRulePayload[] = []
  for (const rule of rules) {
    const errorCode = Number(rule.error_code)
    const duration = Number(rule.duration_minutes)
    const keywords = splitTempUnschedKeywords(rule.keywords)
    if (!Number.isFinite(errorCode) || errorCode < 100 || errorCode > 599) {
      continue
    }
    if (!Number.isFinite(duration) || duration <= 0) {
      continue
    }
    if (keywords.length === 0) {
      continue
    }
    out.push({
      error_code: Math.trunc(errorCode),
      keywords,
      duration_minutes: Math.trunc(duration),
      description: rule.description.trim()
    })
  }
  return out
}

export function buildMixedChannelDetails(resp?: CheckMixedChannelLike): MixedChannelDetails | null {
  const details = resp?.details
  if (!details) {
    return null
  }
  return {
    groupName: details.group_name || 'Unknown',
    currentPlatform: details.current_platform || 'Unknown',
    otherPlatform: details.other_platform || 'Unknown'
  }
}

export const ALLOWED_CREDENTIAL_TYPES = [
  'api_key',
  'oauth',
  'setup-token',
  'service_account',
  'bedrock'
] as const

export type AllowedCredentialType = (typeof ALLOWED_CREDENTIAL_TYPES)[number]

export function isAllowedCredentialType(authType: string): boolean {
  const normalized = authType.trim().toLowerCase()
  return (ALLOWED_CREDENTIAL_TYPES as readonly string[]).includes(normalized)
}

export function filterSupportedAuthTypes(authTypes: readonly string[]): string[] {
  const filtered = authTypes
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0 && isAllowedCredentialType(entry))
  return filtered.length > 0 ? filtered : ['api_key']
}

export type ProviderCategory = 'international' | 'domestic' | 'compatible'

export interface ProviderDisplayMeta {
  id: string
  label: string
  protocolSummary: string
  defaultEndpoint: string
  category: ProviderCategory
  featured?: boolean
}

interface ProviderCatalogEntry {
  labelEn: string
  labelZh: string
  protocolEn: string
  protocolZh: string
  defaultEndpoint: string
  category: ProviderCategory
  featured?: boolean
}

export const FEATURED_PROVIDER_IDS: readonly string[] = [
  'openai',
  'anthropic',
  'gemini',
  'deepseek',
  'kimi',
  'qianwen',
  'volcengine',
  'zhipu',
  'siliconflow_cn',
  'grok',
  'openrouter',
  'minimax'
]

const PROVIDER_CATALOG: Record<string, ProviderCatalogEntry> = {
  openai: {
    labelEn: 'OpenAI',
    labelZh: 'OpenAI',
    protocolEn: 'Responses · Chat Completions',
    protocolZh: 'Responses · Chat Completions',
    defaultEndpoint: 'https://api.openai.com',
    category: 'international',
    featured: true
  },
  anthropic: {
    labelEn: 'Anthropic',
    labelZh: 'Anthropic Claude',
    protocolEn: 'Messages API',
    protocolZh: 'Messages 原生协议',
    defaultEndpoint: 'https://api.anthropic.com',
    category: 'international',
    featured: true
  },
  gemini: {
    labelEn: 'Google Gemini',
    labelZh: 'Google Gemini',
    protocolEn: 'GenerateContent · Vertex AI',
    protocolZh: 'GenerateContent · Vertex AI',
    defaultEndpoint: 'https://generativelanguage.googleapis.com',
    category: 'international',
    featured: true
  },
  deepseek: {
    labelEn: 'DeepSeek',
    labelZh: 'DeepSeek 深度求索',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: 'OpenAI · Anthropic 双协议兼容',
    defaultEndpoint: 'https://api.deepseek.com',
    category: 'domestic',
    featured: true
  },
  azure: {
    labelEn: 'Azure OpenAI',
    labelZh: 'Azure OpenAI',
    protocolEn: 'Azure Deployments',
    protocolZh: 'Azure 企业部署端点',
    defaultEndpoint: '',
    category: 'international'
  },
  azure_openai: {
    labelEn: 'Azure OpenAI',
    labelZh: 'Azure OpenAI',
    protocolEn: 'Azure Deployments',
    protocolZh: 'Azure 企业部署端点',
    defaultEndpoint: '',
    category: 'international'
  },
  bedrock: {
    labelEn: 'AWS Bedrock',
    labelZh: 'AWS Bedrock',
    protocolEn: 'Converse · InvokeModel',
    protocolZh: 'Converse · InvokeModel',
    defaultEndpoint: '',
    category: 'international'
  },
  grok: {
    labelEn: 'xAI Grok',
    labelZh: 'xAI Grok',
    protocolEn: 'Responses · OpenAI-Compatible',
    protocolZh: 'Responses · OpenAI 兼容协议',
    defaultEndpoint: 'https://api.x.ai',
    category: 'international',
    featured: true
  },
  kimi: {
    labelEn: 'Moonshot Kimi',
    labelZh: 'Moonshot Kimi 月之暗面',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: 'OpenAI · Anthropic 双协议兼容',
    defaultEndpoint: 'https://api.moonshot.cn',
    category: 'domestic',
    featured: true
  },
  zhipu: {
    labelEn: 'Zhipu GLM',
    labelZh: 'Zhipu 智谱清言 (GLM)',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: 'OpenAI · Anthropic 双协议兼容',
    defaultEndpoint: 'https://open.bigmodel.cn',
    category: 'domestic',
    featured: true
  },
  minimax: {
    labelEn: 'MiniMax',
    labelZh: 'MiniMax 稀宇科技',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: 'OpenAI · Anthropic 双协议兼容',
    defaultEndpoint: 'https://api.minimax.chat',
    category: 'domestic',
    featured: true
  },
  qianwen: {
    labelEn: 'Alibaba Qwen',
    labelZh: 'DashScope 通义千问',
    protocolEn: 'DashScope · OpenAI-Compatible',
    protocolZh: 'DashScope · OpenAI 兼容协议',
    defaultEndpoint: 'https://dashscope.aliyuncs.com/compatible-mode',
    category: 'domestic',
    featured: true
  },
  qwencloud: {
    labelEn: 'QwenCloud MaaS',
    labelZh: '通义千问 QwenCloud MaaS',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://maas.qwencloudapi.com/compatible-mode/v1',
    category: 'domestic'
  },
  qwencloud_maas_token: {
    labelEn: 'QwenCloud MaaS Token',
    labelZh: '通义千问 QwenCloud (Token Plan)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://token-plan.maas.qwencloudapi.com/compatible-mode/v1',
    category: 'domestic'
  },
  qwencloud_token: {
    labelEn: 'QwenCloud Token (Intl)',
    labelZh: '通义千问 QwenCloud (国际 Token)',
    protocolEn: 'Anthropic-Compatible',
    protocolZh: 'Anthropic 兼容协议',
    defaultEndpoint: 'https://token-plan.ap-southeast-1.maas.aliyuncs.com/apps/anthropic/v1',
    category: 'domestic'
  },
  openrouter: {
    labelEn: 'OpenRouter',
    labelZh: 'OpenRouter',
    protocolEn: 'Multi-Model Router · OpenAI-Compatible',
    protocolZh: '全球模型聚合路由 · OpenAI 兼容',
    defaultEndpoint: 'https://openrouter.ai/api',
    category: 'compatible',
    featured: true
  },
  mistral: {
    labelEn: 'Mistral AI',
    labelZh: 'Mistral AI',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.mistral.ai',
    category: 'international'
  },
  volcengine: {
    labelEn: 'Volcengine Ark',
    labelZh: 'Volcengine 火山方舟 (豆包)',
    protocolEn: 'Ark · OpenAI-Compatible',
    protocolZh: '豆包大模型 · OpenAI 兼容协议',
    defaultEndpoint: 'https://ark.cn-beijing.volces.com/api/v3',
    category: 'domestic',
    featured: true
  },
  byteplus: {
    labelEn: 'BytePlus Ark',
    labelZh: 'BytePlus 火山国际版',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://ark.ap-southeast.bytepluses.com/api/coding/v3',
    category: 'international'
  },
  siliconflow: {
    labelEn: 'SiliconFlow',
    labelZh: 'SiliconFlow 硅基流动',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: 'OpenAI · Anthropic 兼容协议',
    defaultEndpoint: 'https://api.siliconflow.cn/v1',
    category: 'domestic',
    featured: true
  },
  siliconflow_cn: {
    labelEn: 'SiliconFlow (CN)',
    labelZh: 'SiliconFlow 硅基流动 (中国)',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: 'OpenAI · Anthropic 兼容协议',
    defaultEndpoint: 'https://api.siliconflow.cn/v1',
    category: 'domestic',
    featured: true
  },
  siliconflow_global: {
    labelEn: 'SiliconFlow (Global)',
    labelZh: 'SiliconFlow 硅基流动 (国际)',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: 'OpenAI · Anthropic 兼容协议',
    defaultEndpoint: 'https://api.siliconflow.com/v1',
    category: 'compatible'
  },
  stepfun: {
    labelEn: 'StepFun',
    labelZh: 'StepFun 阶跃星辰',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.stepfun.com/v1',
    category: 'domestic'
  },
  stepfun_api: {
    labelEn: 'StepFun (CN)',
    labelZh: 'StepFun 阶跃星辰 (中国)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.stepfun.com/v1',
    category: 'domestic'
  },
  stepfun_api_intl: {
    labelEn: 'StepFun (Intl)',
    labelZh: 'StepFun 阶跃星辰 (国际)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.stepfun.ai/v1',
    category: 'domestic'
  },
  stepfun_plan: {
    labelEn: 'StepFun Step Plan (CN)',
    labelZh: 'StepFun 阶跃星辰 (Step Plan)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.stepfun.com/step_plan/v1',
    category: 'domestic'
  },
  stepfun_plan_intl: {
    labelEn: 'StepFun Step Plan (Intl)',
    labelZh: 'StepFun 阶跃星辰 (Step Plan 国际)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.stepfun.ai/step_plan/v1',
    category: 'domestic'
  },
  qianfan: {
    labelEn: 'Baidu Qianfan',
    labelZh: '百度智能云千帆',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://qianfan.baidubce.com/v2',
    category: 'domestic'
  },
  hunyuan: {
    labelEn: 'Tencent Hunyuan',
    labelZh: '腾讯混元 (Hunyuan)',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: 'OpenAI · Anthropic 兼容协议',
    defaultEndpoint: 'https://api.hunyuan.cloud.tencent.com',
    category: 'domestic'
  },
  mimo: {
    labelEn: 'Xiaomi MiMo',
    labelZh: '小米 MiMo',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.xiaomimimo.com/v1',
    category: 'domestic'
  },
  mimo_token_cn: {
    labelEn: 'Xiaomi MiMo (CN)',
    labelZh: '小米 MiMo (中国)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://token-plan-cn.xiaomimimo.com/v1',
    category: 'domestic'
  },
  mimo_token_eu: {
    labelEn: 'Xiaomi MiMo (EU)',
    labelZh: '小米 MiMo (欧洲)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://token-plan-ams.xiaomimimo.com/v1',
    category: 'domestic'
  },
  mimo_token_sg: {
    labelEn: 'Xiaomi MiMo (SG)',
    labelZh: '小米 MiMo (新加坡)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://token-plan-sgp.xiaomimimo.com/v1',
    category: 'domestic'
  },
  modelscope: {
    labelEn: 'ModelScope',
    labelZh: '魔搭 ModelScope',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api-inference.modelscope.cn/v1',
    category: 'domestic'
  },
  bailing: {
    labelEn: 'BaiLing',
    labelZh: '蚂蚁百灵 (BaiLing)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.ant-ling.com/v1',
    category: 'domestic'
  },
  astron_coding: {
    labelEn: 'Astron Coding',
    labelZh: '讯飞星辰 Astron Coding',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://maas-coding-api.cn-huabei-1.xf-yun.com/v2',
    category: 'domestic'
  },
  astron_token: {
    labelEn: 'Astron Token Plan',
    labelZh: '讯飞星辰 Astron Token',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://maas-token-api.cn-huabei-1.xf-yun.com/v2',
    category: 'domestic'
  },
  ppio: {
    labelEn: 'PPIO Cloud',
    labelZh: 'PPIO 派欧云',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.ppio.com/openai/v1',
    category: 'domestic'
  },
  qiniu: {
    labelEn: 'Qiniu AI',
    labelZh: '七牛云 AI',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.qnaigc.com/v1',
    category: 'domestic'
  },
  shengsuanyun: {
    labelEn: 'Shengsuanyun',
    labelZh: '胜算云',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://router.shengsuanyun.com/api/v1',
    category: 'domestic'
  },
  compshare: {
    labelEn: 'Compshare',
    labelZh: '优云智算 (Compshare)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.modelverse.cn/v1',
    category: 'domestic'
  },
  compshare_coding: {
    labelEn: 'Compshare Coding',
    labelZh: '优云智算 (Coding Plan)',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://cp.compshare.cn/v1',
    category: 'domestic'
  },
  kat_coder: {
    labelEn: 'KAT-Coder',
    labelZh: '快手 KAT-Coder',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://vanchin.streamlake.ai/api/gateway/v1/endpoints',
    category: 'domestic'
  },
  longcat: {
    labelEn: 'LongCat',
    labelZh: 'LongCat',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.longcat.chat/openai/v1',
    category: 'domestic'
  },
  together: {
    labelEn: 'Together AI',
    labelZh: 'Together AI',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容推理网关',
    defaultEndpoint: 'https://api.together.ai/v1',
    category: 'compatible'
  },
  fireworks: {
    labelEn: 'Fireworks AI',
    labelZh: 'Fireworks AI',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容推理网关',
    defaultEndpoint: 'https://api.fireworks.ai/inference/v1',
    category: 'compatible'
  },
  groq: {
    labelEn: 'Groq',
    labelZh: 'Groq',
    protocolEn: 'LPU Inference · OpenAI-Compatible',
    protocolZh: 'LPU 高速推理 · OpenAI 兼容',
    defaultEndpoint: 'https://api.groq.com/openai/v1',
    category: 'compatible'
  },
  novita: {
    labelEn: 'Novita AI',
    labelZh: 'Novita AI',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容推理网关',
    defaultEndpoint: 'https://api.novita.ai/openai/v1',
    category: 'compatible'
  },
  nvidia: {
    labelEn: 'NVIDIA NIM',
    labelZh: 'NVIDIA NIM 托管',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://integrate.api.nvidia.com/v1',
    category: 'international'
  },
  github_copilot: {
    labelEn: 'GitHub Copilot',
    labelZh: 'GitHub Copilot',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://api.githubcopilot.com/v1',
    category: 'international'
  },
  nous_research: {
    labelEn: 'Nous Research',
    labelZh: 'Nous Research',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: 'https://inference-api.nousresearch.com/v1',
    category: 'international'
  },
  kaggle: {
    labelEn: 'Kaggle',
    labelZh: 'Kaggle',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: '',
    category: 'international'
  },
  atlascloud: {
    labelEn: 'AtlasCloud',
    labelZh: 'AtlasCloud',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容聚合网关',
    defaultEndpoint: 'https://api.atlascloud.ai/v1',
    category: 'compatible'
  },
  aihubmix: {
    labelEn: 'AiHubMix',
    labelZh: 'AiHubMix',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: '多模型聚合 · OpenAI 兼容',
    defaultEndpoint: 'https://aihubmix.com/v1',
    category: 'compatible'
  },
  dmxapi: {
    labelEn: 'DMXAPI (CN)',
    labelZh: 'DMXAPI (中国)',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: '多模型聚合 · OpenAI 兼容',
    defaultEndpoint: 'https://www.dmxapi.cn/v1',
    category: 'compatible'
  },
  dmxapi_com: {
    labelEn: 'DMXAPI (.com)',
    labelZh: 'DMXAPI (.com)',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: '多模型聚合 · OpenAI 兼容',
    defaultEndpoint: 'https://www.dmxapi.com/v1',
    category: 'compatible'
  },
  dmxapi_ssvip: {
    labelEn: 'DMXAPI (SSVIP)',
    labelZh: 'DMXAPI (SSVIP)',
    protocolEn: 'OpenAI & Anthropic Compatible',
    protocolZh: '多模型聚合 · OpenAI 兼容',
    defaultEndpoint: 'https://ssvip.dmxapi.com/v1',
    category: 'compatible'
  },
  antigravity: {
    labelEn: 'Antigravity',
    labelZh: 'Antigravity',
    protocolEn: 'Managed Upstream Protocol',
    protocolZh: '托管上游协议',
    defaultEndpoint: '',
    category: 'international'
  },
  cursor: {
    labelEn: 'Cursor',
    labelZh: 'Cursor',
    protocolEn: 'OAuth · Relay Protocol',
    protocolZh: 'OAuth · 托管中继协议',
    defaultEndpoint: '',
    category: 'international'
  },
  opencode_go: {
    labelEn: 'OpenCode',
    labelZh: 'OpenCode',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: '',
    category: 'compatible'
  },
  commandcode: {
    labelEn: 'Command Code',
    labelZh: 'Command Code',
    protocolEn: 'OpenAI-Compatible',
    protocolZh: 'OpenAI 兼容协议',
    defaultEndpoint: '',
    category: 'compatible'
  },
  compatible: {
    labelEn: 'OpenAI / Claude Compatible',
    labelZh: 'OpenAI / Claude 兼容网关',
    protocolEn: 'Custom Gateway Endpoint',
    protocolZh: '自定义兼容网关端点',
    defaultEndpoint: '',
    category: 'compatible'
  }
}

const PROVIDER_REGION_SUFFIXES: Record<string, string> = {
  cn: '(CN)',
  us: '(US)',
  eu: '(EU)',
  sg: '(SG)',
  hk: '(HK)',
  jp: '(JP)',
  uk: '(UK)',
  intl: '(Intl)',
  global: '(Global)',
  com: '(.com)'
}

const PROVIDER_WORD_OVERRIDES: Record<string, string> = {
  ai: 'AI',
  api: 'API',
  gw: 'GW',
  llm: 'LLM',
  vip: 'VIP',
  ssvip: 'SSVIP',
  maas: 'MaaS',
  openai: 'OpenAI',
  openrouter: 'OpenRouter',
  siliconflow: 'SiliconFlow',
  stepfun: 'StepFun',
  sudocode: 'SudoCode',
  aicode: 'AICode',
  aicodemirror: 'AICodeMirror',
  aicodewith: 'AiCodeWith',
  aicoding: 'AICoding',
  aigocode: 'AIGoCode',
  aihubmix: 'AiHubMix',
  apikey: 'APIKey',
  apinebula: 'APINebula',
  atlascloud: 'AtlasCloud',
  claudeapi: 'ClaudeAPI',
  claudecn: 'ClaudeCN',
  ccsub: 'CCSub',
  cherryin: 'CherryIN',
  crazyrouter: 'CrazyRouter',
  dmxapi: 'DMXAPI',
  eflowcode: 'E-FlowCode',
  fennoai: 'FennoAI',
  fluxa: 'FluxA',
  jiekou: 'JieKou',
  packycode: 'PackyCode',
  pateway: 'PatewayAI',
  pipellm: 'PipeLLM',
  qwencloud: 'QwenCloud',
  relaxycode: 'RelaxyCode',
  rightcode: 'RightCode',
  runapi: 'RunAPI',
  soleapi: 'SoleAPI',
  sssaicode: 'SSSAiCode',
  subrouter: 'SubRouter',
  teamorouter: 'TeamoRouter',
  therouter: 'TheRouter',
  xycai: 'XycAI',
  zetaapi: 'ZetaAPI',
  a6api: 'A6API'
}

export function formatProviderIdLabel(rawProvider: string): string {
  const cleaned = rawProvider.trim()
  if (!cleaned) return ''
  const parts = cleaned.split(/[_-]+/).filter((part) => part.length > 0)
  if (parts.length === 0) return cleaned

  let suffix = ''
  if (parts.length > 1) {
    const lastLower = parts[parts.length - 1].toLowerCase()
    if (PROVIDER_REGION_SUFFIXES[lastLower]) {
      suffix = PROVIDER_REGION_SUFFIXES[lastLower]
      parts.pop()
    }
  }

  const formattedWords = parts.map((part) => {
    const lower = part.toLowerCase()
    if (PROVIDER_WORD_OVERRIDES[lower]) {
      return PROVIDER_WORD_OVERRIDES[lower]
    }
    if (/[a-z]/.test(part) && /[A-Z]/.test(part)) {
      return part
    }
    return lower.charAt(0).toUpperCase() + lower.slice(1)
  })

  return suffix ? `${formattedWords.join(' ')} ${suffix}` : formattedWords.join(' ')
}

export function getProviderDisplayMeta(provider: string, isZh = false): ProviderDisplayMeta {
  const key = provider.trim().toLowerCase()
  const entry = PROVIDER_CATALOG[key]
  if (entry) {
    return {
      id: key,
      label: isZh ? entry.labelZh : entry.labelEn,
      protocolSummary: isZh ? entry.protocolZh : entry.protocolEn,
      defaultEndpoint: entry.defaultEndpoint,
      category: entry.category,
      featured: entry.featured
    }
  }
  return {
    id: key,
    label: formatProviderIdLabel(provider),
    protocolSummary: isZh ? '自定义兼容协议' : 'Custom Compatible Schema',
    defaultEndpoint: '',
    category: 'compatible'
  }
}

export function getAuthModeLabel(authType: string, isZh = false): string {
  const normalized = authType.trim().toLowerCase()
  if (normalized === 'api_key') return 'API Key'
  if (normalized === 'oauth') return 'OAuth 2.0'
  if (normalized === 'setup-token') {
    return isZh ? '部署令牌 (Setup Token)' : 'Setup Token'
  }
  if (normalized === 'service_account') {
    return isZh ? '服务账号 (Service Account)' : 'Service Account'
  }
  if (normalized === 'bedrock') return 'Amazon Bedrock'
  return authType
}

export function resolveSchemaFields(schema: ConnectionSchema | null | undefined): ConnectionField[] {
  if (!schema || !Array.isArray(schema.fields)) return []
  return schema.fields
}

export function isFieldRequired(schema: ConnectionSchema | null | undefined, path: string): boolean {
  return resolveSchemaFields(schema).some((field) => field.path === path && field.required)
}

export function isOAuthWorkflowSupported(schema: ConnectionSchema | null | undefined, authType: string): boolean {
  if (!schema) return false
  if (authType.trim().toLowerCase() === 'oauth') return true
  const workflows = schema.authorization_workflows
  return Boolean(workflows && Object.keys(workflows).length > 0 && workflows[authType])
}

export function isVerificationFresh(
  verification: ConnectionVerification | null | undefined,
  nowMs: number = Date.now()
): boolean {
  if (!verification || !verification.expires_at) return false
  const expiresMs = Date.parse(verification.expires_at)
  if (!Number.isFinite(expiresMs)) return false
  return expiresMs > nowMs
}

export function hasUnavailableVerificationEvidence(
  verification: ConnectionVerification | null | undefined
): boolean {
  if (!verification || !Array.isArray(verification.checks)) return false
  return verification.checks.some(
    (check) =>
      check.reason === 'EVIDENCE_UNAVAILABLE' ||
      check.state === 'unknown' ||
      (check.name === 'discover_models' && check.state === 'unsupported') ||
      ((check.name === 'network' || check.name === 'authorization') &&
        check.state !== 'passed' &&
        check.state !== 'failed')
  )
}

export function isConnectionRedacted(result: unknown): result is { version: string; redactions: string[] } {
  if (!result || typeof result !== 'object') return false
  const record = result as Record<string, unknown>
  return Array.isArray(record.redactions) && !('id' in record)
}

export function isConnectionResource(result: unknown): result is ConnectionResource {
  if (!result || typeof result !== 'object') return false
  const record = result as Record<string, unknown>
  return typeof record.id === 'string' && Array.isArray(record.redactions)
}

export function canPerformConnectionAction(
  connection: Pick<ConnectionResource, 'allowed_actions'> | null | undefined,
  action: string
): boolean {
  if (!connection || !Array.isArray(connection.allowed_actions)) return false
  return connection.allowed_actions.includes(action)
}

export function credentialOperationRequiresSecret(mode: CredentialOperationMode): boolean {
  return mode === 'set'
}

const ENDPOINT_SUFFIX_PATTERNS: readonly RegExp[] = [
  /\/v1\/chat\/completions\/?$/i,
  /\/chat\/completions\/?$/i,
  /\/v1\/responses\/?$/i,
  /\/responses\/?$/i,
  /\/v1\/messages\/?$/i,
  /\/messages\/?$/i,
  /\/v1\/embeddings\/?$/i,
  /\/embeddings\/?$/i
]

export function normalizeConnectionEndpointInput(endpoint: string): {
  normalized: string
  strippedSuffix?: string
} {
  let cleaned = endpoint.trim()
  if (!cleaned) {
    return { normalized: '' }
  }
  let strippedSuffix: string | undefined
  for (const pattern of ENDPOINT_SUFFIX_PATTERNS) {
    const match = cleaned.match(pattern)
    if (match && match[0]) {
      strippedSuffix = match[0].replace(/\/+$/, '')
      cleaned = cleaned.slice(0, cleaned.length - match[0].length)
      break
    }
  }
  cleaned = cleaned.replace(/\/+$/, '')
  return { normalized: cleaned, strippedSuffix }
}

export function validateConnectionEndpoint(
  endpoint: string,
  required: boolean,
  isZh = false
): string | null {
  const trimmed = endpoint.trim()
  if (!trimmed) {
    if (!required) return null
    return isZh ? '当前协议要求填写上游服务地址。' : 'Endpoint URL is required by provider schema.'
  }
  if (trimmed.length > 2048) {
    return isZh ? '上游服务地址长度不能超过 2048 个字符。' : 'Endpoint URL must not exceed 2048 characters.'
  }
  if (/[?#\\]/.test(trimmed)) {
    return isZh
      ? '上游服务地址不能包含查询参数 (?)、锚点 (#) 或反斜杠 (\\)。'
      : 'Endpoint URL must not contain query parameters (?), fragments (#), or backslashes (\\).'
  }
  try {
    const parsed = new URL(trimmed)
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
      return isZh ? '上游服务地址必须以 https:// 或 http:// 开头' : 'Endpoint must use https:// or http://'
    }
    if (!parsed.hostname) {
      return isZh
        ? '请填写包含有效主机名的服务地址（例如 https://api.openai.com）'
        : 'Enter a valid URL (for example https://api.openai.com)'
    }
    if (parsed.username || parsed.password) {
      return isZh
        ? '上游服务地址中禁止内嵌用户名或密码，请在下一步配置认证凭据。'
        : 'Do not embed credentials in the endpoint URL; configure secrets in the next step.'
    }
    for (const pattern of ENDPOINT_SUFFIX_PATTERNS) {
      if (pattern.test(trimmed)) {
        return isZh
          ? '请填写基础服务地址，无需包含 /chat/completions、/responses 或 /messages 后缀。'
          : 'Enter the base endpoint URL without /chat/completions, /responses, or /messages suffixes.'
      }
    }
    return null
  } catch {
    return isZh
      ? '请填写合法的 http:// 或 https:// 地址（例如 https://api.openai.com 或 http://127.0.0.1:3425）'
      : 'Enter a valid URL (for example https://api.openai.com)'
  }
}

export function validateNetworkPolicyId(policyId: string, isZh = false): string | null {
  const trimmed = policyId.trim()
  if (!trimmed) return null
  if (!/^[1-9]\d*$/.test(trimmed)) {
    return isZh
      ? '出站代理策略需留空（使用默认直连）或填写有效的正整数代理 ID。'
      : 'Leave Egress Proxy ID blank for direct egress or enter a valid numeric proxy ID.'
  }
  return null
}

const BLOCKED_HEADER_OVERRIDE_NAMES = new Set([
  'host',
  'content-length',
  'content-type',
  'transfer-encoding',
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'proxy-connection',
  'te',
  'trailer',
  'upgrade',
  'authorization',
  'api-key',
  'x-api-key',
  'x-goog-api-key',
  'cookie',
  'accept-encoding',
  'sec-websocket-key',
  'sec-websocket-version',
  'sec-websocket-extensions',
  'sec-websocket-protocol',
  'sec-websocket-accept',
  'session_id',
  'conversation_id',
  'x-codex-turn-state',
  'x-codex-turn-metadata',
  'chatgpt-account-id',
  'x-claude-code-session-id',
  'x-client-request-id',
  'x-grok-conv-id'
])

const API_KEY_HEADER_OVERRIDE_PROVIDERS = new Set([
  'anthropic',
  'openai',
  'kimi',
  'zhipu',
  'deepseek',
  'minimax',
  'opencode_go',
  'qianwen',
  'commandcode'
])

export function isHeaderOverrideSupported(provider: string, authType: string): boolean {
  const normalizedProvider = provider.trim().toLowerCase()
  const normalizedAuth = authType.trim().toLowerCase()
  if (API_KEY_HEADER_OVERRIDE_PROVIDERS.has(normalizedProvider)) {
    return normalizedAuth === 'api_key' || normalizedAuth === 'apikey'
  }
  if (normalizedProvider === 'grok') {
    return normalizedAuth === 'api_key' || normalizedAuth === 'apikey' || normalizedAuth === 'oauth'
  }
  return false
}

export function validateHeaderOverrideRows(
  rows: readonly HeaderOverrideDraftRow[],
  provider: string,
  authType: string,
  isZh = false
): string | null {
  const activeRows = rows.filter((row) => row.key.trim() !== '' || row.value.trim() !== '')
  if (activeRows.length === 0) return null
  if (!isHeaderOverrideSupported(provider, authType)) {
    return isZh
      ? '当前提供商或认证模式不支持自定义请求头覆写，请清空请求头或切换为兼容的 API Key 模式。'
      : 'Custom header overrides are not supported for the selected provider and authentication mode.'
  }
  if (activeRows.length > 64) {
    return isZh ? '自定义请求头最多支持 64 项。' : 'At most 64 custom headers are allowed.'
  }
  const seen = new Set<string>()
  for (const row of activeRows) {
    const name = row.key.trim()
    const value = row.value.trim()
    if (!name) {
      return isZh ? '自定义请求头名称不能为空。' : 'Custom header name cannot be empty.'
    }
    if (name.length > 200 || !/^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/.test(name)) {
      return isZh
        ? `请求头名称 "${name}" 格式不合法，仅允许标准 HTTP Header 字符。`
        : `Invalid HTTP header name "${name}".`
    }
    const lower = name.toLowerCase()
    if (BLOCKED_HEADER_OVERRIDE_NAMES.has(lower)) {
      return isZh
        ? `禁止覆写受保护的请求头 "${name}"（如认证头、Host 或传输控制头）。`
        : `Header "${name}" is protected and cannot be overridden.`
    }
    if (seen.has(lower)) {
      return isZh
        ? `存在重复的请求头名称 "${name}"（不区分大小写）。`
        : `Duplicate header name "${name}" (case-insensitive).`
    }
    if (value.length > 8192) {
      return isZh
        ? `请求头 "${name}" 的值超过最大长度限制 (8192)。`
        : `Header "${name}" value exceeds 8192 characters.`
    }
    seen.add(lower)
  }
  return null
}

export function buildHeaderOverridesMap(
  rows: readonly HeaderOverrideDraftRow[]
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of rows) {
    const key = row.key.trim()
    const value = row.value.trim()
    if (!key || !value) continue
    out[key] = value
  }
  return out
}

export function extractOperationVerification(
  operation: ConnectionOperation | null | undefined
): ConnectionVerification | null {
  const result = operation?.result
  if (!result || typeof result !== 'object') return null
  const record = result as Record<string, unknown>
  if (!Array.isArray(record.checks)) return null
  const checks = record.checks as ConnectionCheck[]
  const models = Array.isArray(record.models)
    ? record.models.filter((modelId): modelId is string => typeof modelId === 'string')
    : []
  const nowIso = new Date().toISOString()
  const defaultExpiresIso = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString()
  return {
    verification_id:
      typeof record.verification_id === 'string' && record.verification_id
        ? record.verification_id
        : operation?.operation_id || 'op_verification',
    checks,
    models,
    verified_at: typeof record.verified_at === 'string' ? record.verified_at : nowIso,
    expires_at: typeof record.expires_at === 'string' ? record.expires_at : defaultExpiresIso
  }
}

export function formatVerificationCheckReason(
  checkName: string,
  reason: string | null | undefined,
  isZh = false
): string {
  if (!reason) return ''
  if (reason === 'URL_NOT_ALLOWED') {
    return isZh
      ? '自定义端点域名未在服务端出站域名白名单中，已被 SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS 策略拦截。请在服务端 .env 中将目标域名加入 SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS，或清空/禁用主机名白名单后重试。'
      : 'Custom endpoint domain is blocked by the server outbound allowlist policy (SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS). Add this domain to SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS in .env or disable the hostname allowlist policy, then retry.'
  }
  if (reason === 'INVALID_ENDPOINT') {
    return isZh
      ? '上游服务地址格式无效或协议不受支持，请返回第 1 步核对完整的 http(s):// 服务地址。'
      : 'The upstream endpoint URL is invalid or uses an unsupported scheme. Return to Step 1 and verify the http(s):// endpoint URL.'
  }
  if (reason === 'UPSTREAM_RATE_LIMITED') {
    return isZh
      ? '上游服务返回限流响应（HTTP 429），验证探针暂时受限，请稍候重试或检查上游配额与并发限制。'
      : 'Upstream returned HTTP 429 (rate limited). Wait briefly before retrying the probe or check upstream quota and concurrency limits.'
  }
  if (reason === 'AUTH_REJECTED') {
    return isZh
      ? '上游服务端拒绝了当前认证凭据（HTTP 401/403），请返回第 2 步核对 API Key 或 OAuth 授权状态。'
      : 'Upstream rejected the credential (HTTP 401/403). Return to Step 2 to verify the API key or re-authorize.'
  }
  if (reason === 'NETWORK_UNREACHABLE') {
    return isZh
      ? '无法建立到上游服务的网络连接（DNS 解析失败、连接超时或网络不可达），请检查服务地址、端口与出站网络策略。'
      : 'Network unreachable when connecting to upstream (DNS failure, timeout, or egress block). Verify the endpoint host, port, and outbound network policy.'
  }
  if (reason === 'EVIDENCE_UNAVAILABLE') {
    return isZh
      ? '上游端点未能返回有效的连通或鉴权证据（探针不可达、鉴权被拒或当前协议无内置主动探针）。请检查配置后重试验证探针，或先保存为草稿并退出。'
      : 'Verification evidence is unavailable (upstream probe unreachable, auth rejected, or active probe unsupported). Retry the verification probe or save as draft and exit.'
  }
  if (reason === 'CHECK_UNSUPPORTED') {
    return isZh
      ? '当前提供商协议暂不支持此项自动检查。'
      : 'This check is not supported by the selected provider protocol.'
  }
  if (reason === 'ACTIVE_MODEL_MISSING') {
    return isZh
      ? '替换凭据缺少当前已启用连接正在服务的模型权限。'
      : 'Replacement credential is missing access to one or more active models.'
  }
  if (reason === 'PROBE_FAILED') {
    if (checkName === 'network') {
      return isZh
        ? '无法连接至上游服务端点，请检查 Endpoint 地址、TLS 证书或出站代理策略。'
        : 'Failed to reach upstream endpoint. Verify the Endpoint URL, TLS certificate, or egress proxy.'
    }
    if (checkName === 'authorization') {
      return isZh
        ? '上游服务端拒绝了当前凭据（401/403），请返回第 2 步核对密钥或重新授权。'
        : 'Upstream rejected the credential (401/403). Return to Step 2 to verify the secret or re-authorize.'
    }
    if (checkName === 'discover_models') {
      return isZh
        ? '上游模型目录探测未通过，请确认服务端点支持模型发现接口后重试。'
        : 'Upstream model discovery probe failed. Verify endpoint compatibility and retry.'
    }
    return isZh ? '上游验证探针执行未通过。' : 'Upstream verification probe failed.'
  }
  return reason
}

export interface PrimaryVerificationDiagnostic {
  checkName: string
  reason: string
  message: string
  detail: string | null
}

const SPECIFIC_DIAGNOSTIC_PRIORITY = [
  'URL_NOT_ALLOWED',
  'INVALID_ENDPOINT',
  'AUTH_REJECTED',
  'UPSTREAM_RATE_LIMITED',
  'NETWORK_UNREACHABLE',
  'ACTIVE_MODEL_MISSING',
  'PROBE_FAILED',
  'CHECK_UNSUPPORTED',
  'EVIDENCE_UNAVAILABLE'
] as const

export function getPrimaryVerificationDiagnostic(
  verification: ConnectionVerification | null | undefined,
  isZh = false
): PrimaryVerificationDiagnostic | null {
  if (!verification || !Array.isArray(verification.checks)) return null
  const candidates = verification.checks.filter(
    (check) => check.state !== 'passed' && Boolean(check.reason || check.detail)
  )
  if (candidates.length === 0) return null

  const sorted = [...candidates].sort((a, b) => {
    const idxA = a.reason
      ? SPECIFIC_DIAGNOSTIC_PRIORITY.indexOf(
          a.reason as (typeof SPECIFIC_DIAGNOSTIC_PRIORITY)[number]
        )
      : -1
    const idxB = b.reason
      ? SPECIFIC_DIAGNOSTIC_PRIORITY.indexOf(
          b.reason as (typeof SPECIFIC_DIAGNOSTIC_PRIORITY)[number]
        )
      : -1
    const rankA = idxA === -1 ? 99 : idxA
    const rankB = idxB === -1 ? 99 : idxB
    return rankA - rankB
  })

  const chosen = sorted[0]
  const reason =
    chosen.reason || (chosen.state === 'unsupported' ? 'CHECK_UNSUPPORTED' : 'EVIDENCE_UNAVAILABLE')
  return {
    checkName: chosen.name,
    reason,
    message: formatVerificationCheckReason(chosen.name, reason, isZh),
    detail: typeof chosen.detail === 'string' && chosen.detail.trim() ? chosen.detail.trim() : null
  }
}

const FIELD_ERROR_DESCRIPTORS: Record<
  string,
  { labelZh: string; labelEn: string; hintZh: string; hintEn: string }
> = {
  '/endpoint': {
    labelZh: '上游服务地址 (/endpoint)',
    labelEn: 'Upstream Endpoint (/endpoint)',
    hintZh: '请填写合法的 http:// 或 https:// 基础地址（不含查询参数或接口路径后缀）',
    hintEn: 'Enter a valid http:// or https:// base URL without query strings or route suffixes'
  },
  '/name': {
    labelZh: '连接名称 (/name)',
    labelEn: 'Connection Name (/name)',
    hintZh: '请填写 1–100 字节（约 33 个汉字）的名称，首尾不含多余空白或控制字符',
    hintEn: 'Enter a non-empty name within 100 UTF-8 bytes without leading/trailing whitespace or control characters'
  },
  '/secret': {
    labelZh: '认证密钥 (/secret)',
    labelEn: 'Credential Secret (/secret)',
    hintZh: '请在第 2 步填写非空且不超过 256KB 的有效 API Key 或访问令牌',
    hintEn: 'Provide a non-empty API key or access token (up to 256 KB) in Step 2'
  },
  '/type': {
    labelZh: '凭据类型 (/type)',
    labelEn: 'Credential Type (/type)',
    hintZh: '请选择受支持的凭据类型（api_key、oauth、setup-token、service_account 或 bedrock）',
    hintEn: 'Use a supported credential type (api_key, oauth, setup-token, service_account, or bedrock)'
  },
  '/operation': {
    labelZh: '凭据变更操作 (/operation)',
    labelEn: 'Credential Operation (/operation)',
    hintZh: '凭据操作模式须为 keep、set 或 clear，且保留/清空模式下不可附带密钥字段',
    hintEn: 'Credential operation must be keep, set, or clear without extraneous secret fields'
  },
  '/replacement/operation': {
    labelZh: '轮换凭据参数 (/replacement/operation)',
    labelEn: 'Rotation Credential Input (/replacement/operation)',
    hintZh: '轮换接口会自动设置替换模式，请仅提交新密钥内容',
    hintEn: 'Omit operation inside replacement payload when rotating credentials'
  },
  '/refresh_token': {
    labelZh: '刷新令牌 (/refresh_token)',
    labelEn: 'Refresh Token (/refresh_token)',
    hintZh: '仅 OAuth 2.0 认证模式支持提交 Refresh Token，请核对认证方式',
    hintEn: 'Refresh tokens are only accepted when authentication mode is oauth'
  },
  '/checks': {
    labelZh: '验证检查项 (/checks)',
    labelEn: 'Verification Checks (/checks)',
    hintZh: '连通性验证需同时包含 network 与 authorization 检查项且不可重复',
    hintEn: 'Verification must include non-duplicate network and authorization checks'
  },
  '/header_overrides': {
    labelZh: '自定义请求头 (/header_overrides)',
    labelEn: 'Custom Headers (/header_overrides)',
    hintZh: '请确认当前平台与认证模式支持请求头覆写，且未包含 Authorization / Host / Content-Type 等受保护请求头',
    hintEn: 'Verify provider support and avoid protected headers such as Authorization, Host, or Content-Type'
  },
  '/network_policy_id': {
    labelZh: '出站代理策略 (/network_policy_id)',
    labelEn: 'Egress Proxy Policy (/network_policy_id)',
    hintZh: '请留空使用默认直连，或填写当前工作空间内已启用的数字代理 ID',
    hintEn: 'Leave blank for direct connection or enter an active numeric proxy ID in this workspace'
  },
  '/schema_version': {
    labelZh: '协议模板版本 (/schema_version)',
    labelEn: 'Schema Version (/schema_version)',
    hintZh: '协议模板版本不受支持，请重新选择提供商协议模板（当前版本为 "1"）',
    hintEn: 'Unsupported provider schema version; re-select the provider template (expected "1")'
  },
  '/auth_type': {
    labelZh: '认证方式 (/auth_type)',
    labelEn: 'Authentication Mode (/auth_type)',
    hintZh: '当前提供商不支持所选认证方式，请重新选择受支持的认证模式',
    hintEn: 'Selected authentication mode is not supported by this provider schema'
  },
  '/provider': {
    labelZh: '提供商与认证方式 (/provider)',
    labelEn: 'Provider & Auth Mode (/provider)',
    hintZh: '请选择受支持的上游提供商与认证模式组合',
    hintEn: 'Select a supported upstream provider and authentication mode combination'
  },
  '/limits/concurrency': {
    labelZh: '并发上限 (/limits/concurrency)',
    labelEn: 'Concurrency Cap (/limits/concurrency)',
    hintZh: '并发上限需设置在 1–10000 之间',
    hintEn: 'Concurrency cap must be an integer between 1 and 10000'
  },
  '/pool_ids': {
    labelZh: '出站资源池 (/pool_ids)',
    labelEn: 'Resource Pools (/pool_ids)',
    hintZh: '请选择有效的资源池 ID，避免重复或无效条目',
    hintEn: 'Select valid numeric resource pool IDs without duplicates'
  },
  '/assignments': {
    labelZh: '模型与资源池分配 (/assignments)',
    labelEn: 'Model & Pool Assignments (/assignments)',
    hintZh: '请检查模型列表与资源池绑定格式，避免空白或重复项',
    hintEn: 'Verify model IDs and pool assignments have valid non-duplicate values'
  },
  '/credential_draft': {
    labelZh: '授权草稿 (/credential_draft)',
    labelEn: 'Authorization Draft (/credential_draft)',
    hintZh: '授权会话已过期或与当前提供商不匹配，请重新发起授权',
    hintEn: 'Authorization draft expired or mismatched; restart the OAuth flow'
  },
  '/If-Match': {
    labelZh: '配置修订号 (/If-Match)',
    labelEn: 'Configuration Revision (/If-Match)',
    hintZh: '配置修订号无效或缺失，请重新加载最新状态后重试',
    hintEn: 'Missing or invalid configuration revision; reload latest state and retry'
  },
  '/Idempotency-Key': {
    labelZh: '请求幂等键 (/Idempotency-Key)',
    labelEn: 'Idempotency Key (/Idempotency-Key)',
    hintZh: '请求幂等标识无效或缺失，请重新提交操作',
    hintEn: 'Missing or invalid Idempotency-Key header; retry the action'
  },
  '/id': {
    labelZh: '连接标识 (/id)',
    labelEn: 'Connection ID (/id)',
    hintZh: '连接资源 ID 格式无效，请返回连接列表重新打开',
    hintEn: 'Invalid connection identifier; reopen the connection from the list'
  },
  '/patch': {
    labelZh: '变更载荷 (/patch)',
    labelEn: 'Patch Payload (/patch)',
    hintZh: '提交的变更内容为空或包含当前步骤不允许修改的字段',
    hintEn: 'Patch payload is empty or includes fields not permitted in this operation'
  },
  '/ids': {
    labelZh: '目标连接列表 (/ids)',
    labelEn: 'Target Connection IDs (/ids)',
    hintZh: '请至少选择 1 个且不超过 100 个不重复的有效连接 ID',
    hintEn: 'Select between 1 and 100 unique valid connection IDs'
  },
  '/preview_id': {
    labelZh: '变更预览标识 (/preview_id)',
    labelEn: 'Preview Identifier (/preview_id)',
    hintZh: '分配预览标识无效或缺失，请先在第 4 步重新生成变更预览',
    hintEn: 'Preview identifier is missing or invalid; generate a fresh preview in Step 4'
  },
  '/selected_ids': {
    labelZh: '批量应用目标 (/selected_ids)',
    labelEn: 'Selected Connections (/selected_ids)',
    hintZh: '所选连接不在可应用预览集合中，请重新生成批量预览',
    hintEn: 'Selected connection IDs do not match applicable preview items'
  }
}

export function parseValidationFieldIssues(rawFields: unknown): ValidationFieldIssue[] {
  if (!Array.isArray(rawFields)) return []
  const issues: ValidationFieldIssue[] = []
  for (const entry of rawFields) {
    if (!entry || typeof entry !== 'object') continue
    const rec = entry as Record<string, unknown>
    const path = typeof rec.path === 'string' && rec.path.trim() ? rec.path.trim() : ''
    if (!path) continue
    const reason = typeof rec.reason === 'string' && rec.reason.trim() ? rec.reason.trim() : 'INVALID_INPUT'
    const descriptor = FIELD_ERROR_DESCRIPTORS[path] ?? {
      labelZh: `字段 (${path})`,
      labelEn: `Field (${path})`,
      hintZh: '请检查该字段的填写格式是否符合要求',
      hintEn: 'Check that this field has a valid value'
    }
    issues.push({
      path,
      reason,
      ...descriptor
    })
  }
  return issues
}

export function formatValidationIssuesMessage(
  issues: readonly ValidationFieldIssue[],
  isZh: boolean
): string {
  if (issues.length === 0) return ''
  if (issues.length === 1) {
    const first = issues[0]
    return isZh
      ? `字段校验未通过：${first.labelZh}，${first.hintZh}。`
      : `Field validation failed: ${first.labelEn} — ${first.hintEn}.`
  }
  const summary = issues
    .map((item) => (isZh ? `${item.labelZh}（${item.hintZh}）` : `${item.labelEn} (${item.hintEn})`))
    .join(isZh ? '；' : '; ')
  return isZh ? `多处字段校验未通过：${summary}。` : `Multiple fields failed validation: ${summary}.`
}

const REASON_ERROR_MESSAGES: Record<string, { zh: string; en: string }> = {
  URL_NOT_ALLOWED: {
    zh: '自定义端点域名已被服务端出站域名白名单（SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS）拦截，请在 .env 中添加该域名或禁用主机名白名单后重试。',
    en: 'Custom endpoint domain is blocked by SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS. Allowlist the domain in .env or disable hostname allowlisting, then retry.'
  },
  INVALID_ENDPOINT: {
    zh: '上游服务地址格式无效或协议不受支持，请检查 Endpoint 配置后重试。',
    en: 'The upstream endpoint URL is invalid or uses an unsupported scheme.'
  },
  UPSTREAM_RATE_LIMITED: {
    zh: '上游服务触发限流（HTTP 429），请稍候重试或调整并发配置。',
    en: 'Upstream returned HTTP 429 (rate limited). Wait briefly before retrying.'
  },
  AUTH_REJECTED: {
    zh: '上游服务拒绝了当前认证凭据（HTTP 401/403），请核对密钥或 OAuth 授权状态。',
    en: 'Upstream rejected the credential (HTTP 401/403). Verify your secret or OAuth authorization.'
  },
  NETWORK_UNREACHABLE: {
    zh: '无法连接至上游服务（DNS 解析失败、超时或网络不可达），请检查服务地址与出站网络策略。',
    en: 'Upstream network unreachable (DNS failure, timeout, or egress block). Verify endpoint and egress policy.'
  },
  CREDENTIAL_UNAVAILABLE: {
    zh: '尚未配置有效凭据，请返回第 2 步填写密钥或完成 OAuth 授权。',
    en: 'No valid credential is configured yet. Return to Step 2 to set a secret or complete OAuth authorization.'
  },
  CREDENTIAL_CHANGED: {
    zh: '认证凭据修订已更新，请重新执行连通性验证后再继续。',
    en: 'Credential revision changed since last check. Re-run verification to continue.'
  },
  VERSION_CONFLICT: {
    zh: '连接配置已被其他会话更新，请重新加载最新修订后再保存。',
    en: 'Connection configuration was updated in another session. Reload the latest revision before saving.'
  },
  IDEMPOTENCY_CONFLICT: {
    zh: '检测到相同幂等键的冲突提交，请确认最新状态后重试。',
    en: 'A conflicting submission with the same idempotency key was rejected. Review current state and retry.'
  },
  STALE_VERIFICATION: {
    zh: '连通性验证结果已过期或配置已变更，请重新运行验证探针。',
    en: 'Verification evidence is stale because configuration changed. Re-run verification.'
  },
  STALE_PREVIEW: {
    zh: '分配预览已过期或配置已变更，请重新生成变更预览后再启用。',
    en: 'Assignment preview is stale or expired. Generate a fresh preview before activating.'
  },
  VERIFICATION_REQUIRED: {
    zh: '尚未通过连通性验证，请先在第 3 步执行验证探针（若兼容协议无内置探针可保存草稿）。',
    en: 'Active connection verification is required before previewing or activating.'
  },
  MODELS_REQUIRED: {
    zh: '请至少填写一个要暴露的模型 ID 后再生成预览。',
    en: 'Specify at least one model ID before generating an assignment preview.'
  },
  INCOMPATIBLE_PROVIDER: {
    zh: '所填模型不在本次探测发现的上游可用模型列表中，请核对模型 ID。',
    en: 'One or more model IDs were not returned by upstream discovery.'
  },
  ROTATION_REQUIRED: {
    zh: '已启用的连接不支持直接覆盖密钥，请使用凭据平滑轮换流程更新密钥。',
    en: 'Active connections require credential rotation instead of direct overwrite.'
  },
  CLEAR_CREDENTIAL_BEFORE_EGRESS_CHANGE: {
    zh: '为防止密钥泄露至非预期端点，修改服务地址或出站代理前请先清空现有凭据。',
    en: 'Clear existing credentials before changing the upstream endpoint or egress proxy.'
  },
  ACCESS_CHANGED: {
    zh: '您的工作空间权限已发生变更，请刷新页面后重试。',
    en: 'Workspace access permissions changed during the operation. Reload and retry.'
  },
  ACTION_DENIED: {
    zh: '当前账号角色无权执行此连接管理操作。',
    en: 'Your current role does not have permission to perform this connection action.'
  },
  FORBIDDEN: {
    zh: '当前账号角色无权执行此连接管理操作。',
    en: 'Your current role does not have permission to perform this connection action.'
  },
  UNAUTHORIZED: {
    zh: '登录会话已失效或未授权，请重新登录后重试。',
    en: 'Authentication required or session expired. Please sign in again.'
  },
  INVALID_INPUT: {
    zh: '提交的配置字段未通过校验，请检查填写内容后重试。',
    en: 'One or more configuration fields failed validation. Check your inputs and retry.'
  },
  INVALID_JSON: {
    zh: '请求数据格式无效，请检查表单内容后重试。',
    en: 'Invalid request payload format. Check your inputs and retry.'
  },
  TIMEOUT: {
    zh: '上游操作响应超时，结果正在等待服务端确认。',
    en: 'Operation timed out before confirmation. Check operation status before retrying.'
  },
  OUTCOME_UNKNOWN: {
    zh: '操作结果正在等待服务端确认，请查询最新状态避免重复提交。',
    en: 'Operation outcome is pending verification.'
  },
  DEPENDENCY_UNAVAILABLE: {
    zh: '连接管理服务暂时不可用，请稍后重试。',
    en: 'Connection management service is temporarily unavailable. Retry shortly.'
  }
}

const GENERIC_BACKEND_ENGLISH_MESSAGES = new Set([
  'invalid field value.',
  'invalid request body.',
  'reload the connection and review its current state.',
  'connection service unavailable.'
])

function containsCjkCharacters(text: string): boolean {
  return /[\u4e00-\u9fff]/.test(text)
}

function resolveLocalizedErrorMessage(
  rawMessage: string,
  reason: string | undefined,
  fallbackMessage: string,
  isZh: boolean
): string {
  const reasonKey = reason?.trim().toUpperCase()
  const mapped = reasonKey ? REASON_ERROR_MESSAGES[reasonKey] : undefined
  const trimmedRaw = rawMessage.trim()
  const isGenericEnglish = GENERIC_BACKEND_ENGLISH_MESSAGES.has(trimmedRaw.toLowerCase())

  if (isZh) {
    if (mapped) return mapped.zh
    if (trimmedRaw && containsCjkCharacters(trimmedRaw)) return trimmedRaw
    if (fallbackMessage && containsCjkCharacters(fallbackMessage)) return fallbackMessage
    return '请求未通过校验或处理失败，请检查配置后重试。'
  }

  if (mapped && (!trimmedRaw || isGenericEnglish)) {
    return mapped.en
  }
  return trimmedRaw || mapped?.en || fallbackMessage
}

const NON_REVISION_CONFLICT_REASONS = new Set([
  'CREDENTIAL_UNAVAILABLE',
  'VERIFICATION_REQUIRED',
  'MODELS_REQUIRED',
  'INCOMPATIBLE_PROVIDER',
  'ROTATION_REQUIRED',
  'CLEAR_CREDENTIAL_BEFORE_EGRESS_CHANGE'
])

export function mapApiErrorToAsyncContext(
  error: unknown,
  fallbackMessage = 'Request failed',
  isZh = false
): AsyncErrorContext {
  const normalized = normalizeApiError(error)
  const reasonKey = normalized.reason?.trim().toUpperCase()
  const requestId =
    typeof normalized.metadata?.request_id === 'string'
      ? normalized.metadata.request_id
      : typeof normalized.metadata?.requestId === 'string'
        ? normalized.metadata.requestId
        : undefined
  const serverVersion =
    typeof normalized.metadata?.current_version === 'string'
      ? normalized.metadata.current_version
      : typeof normalized.metadata?.version === 'string'
        ? normalized.metadata.version
        : undefined
  const requiredRole =
    typeof normalized.metadata?.required_role === 'string'
      ? normalized.metadata.required_role
      : isZh
        ? '工作空间管理员'
        : 'Workspace Admin'
  const statusUrl =
    typeof normalized.metadata?.status_url === 'string' ? normalized.metadata.status_url : undefined
  const fields = parseValidationFieldIssues(normalized.metadata?.fields)

  if (
    normalized.status === 403 ||
    reasonKey === 'ACTION_DENIED' ||
    reasonKey === 'FORBIDDEN' ||
    reasonKey === 'ACCESS_CHANGED'
  ) {
    return {
      state: 'permission_denied',
      status: normalized.status || 403,
      reason: normalized.reason,
      message: resolveLocalizedErrorMessage(
        normalized.message,
        normalized.reason,
        isZh ? '当前角色无权执行此连接操作。' : fallbackMessage,
        isZh
      ),
      requestId,
      requiredRole
    }
  }

  if (normalized.status === 504 || reasonKey === 'TIMEOUT' || reasonKey === 'OUTCOME_UNKNOWN') {
    return {
      state: 'unknown_outcome',
      status: normalized.status || 504,
      reason: normalized.reason,
      message: resolveLocalizedErrorMessage(
        normalized.message,
        normalized.reason,
        isZh ? '操作结果正在等待服务端确认。' : 'Operation outcome is pending verification.',
        isZh
      ),
      requestId,
      statusUrl
    }
  }

  if (
    reasonKey === 'STALE_VERIFICATION' ||
    reasonKey === 'STALE_PREVIEW' ||
    reasonKey === 'CREDENTIAL_CHANGED'
  ) {
    return {
      state: 'stale',
      status: normalized.status || 412,
      reason: normalized.reason,
      message: resolveLocalizedErrorMessage(
        normalized.message,
        normalized.reason,
        isZh ? '验证或预览凭据已过期，请重新执行检查。' : 'Evidence is stale and must be refreshed.',
        isZh
      ),
      requestId
    }
  }

  if (reasonKey && NON_REVISION_CONFLICT_REASONS.has(reasonKey)) {
    return {
      state: 'partial_failure',
      status: normalized.status,
      reason: normalized.reason,
      message: resolveLocalizedErrorMessage(normalized.message, normalized.reason, fallbackMessage, isZh),
      requestId
    }
  }

  if (
    normalized.status === 409 ||
    normalized.status === 412 ||
    reasonKey === 'VERSION_CONFLICT' ||
    reasonKey === 'IDEMPOTENCY_CONFLICT'
  ) {
    return {
      state: 'version_conflict',
      status: normalized.status || 409,
      reason: normalized.reason,
      message: resolveLocalizedErrorMessage(
        normalized.message,
        normalized.reason,
        isZh ? '服务端配置修订已变更，请重新加载最新状态。' : 'Resource revision changed on the server.',
        isZh
      ),
      requestId,
      serverVersion
    }
  }

  const formattedFieldMessage = fields.length > 0 ? formatValidationIssuesMessage(fields, isZh) : ''
  return {
    state: 'partial_failure',
    status: normalized.status,
    reason: normalized.reason,
    message:
      formattedFieldMessage ||
      resolveLocalizedErrorMessage(normalized.message, normalized.reason, fallbackMessage, isZh),
    requestId,
    ...(fields.length > 0 ? { fields } : {})
  }
}
