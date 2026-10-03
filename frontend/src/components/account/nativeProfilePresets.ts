import type { CnBaseUrlPreset, CnBaseUrlPresetPlatform } from './credentialsBuilder'

// Pinned CC Switch 793e67 plus vendor RESEARCH-C; no model or quota claims.
export const NATIVE_PROFILE_PRESETS: Partial<Record<CnBaseUrlPresetPlatform, CnBaseUrlPreset[]>> = {
  kimi: [
    { mode: 'payg', protocol: 'chat_completions', label: 'Moonshot CN chat_completions', url: 'https://api.moonshot.cn/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'Moonshot CN anthropic', url: 'https://api.moonshot.cn/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'Moonshot CN responses', url: 'https://api.moonshot.cn/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'Moonshot Global chat_completions', url: 'https://api.moonshot.ai/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'Moonshot Global anthropic', url: 'https://api.moonshot.ai/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'Moonshot Global responses', url: 'https://api.moonshot.ai/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Kimi Coding CN chat_completions', url: 'https://api.kimi.com/coding/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'Kimi Coding CN anthropic', url: 'https://api.kimi.com/coding' },
    { mode: 'coding', protocol: 'responses', label: 'Kimi Coding CN responses', url: 'https://api.kimi.com/coding/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Kimi Coding Global chat_completions', url: 'https://api.kimi.ai/coding/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'Kimi Coding Global anthropic', url: 'https://api.kimi.ai/coding' },
    { mode: 'coding', protocol: 'responses', label: 'Kimi Coding Global responses', url: 'https://api.kimi.ai/coding/v1' },
  ],
  zhipu: [
    { mode: 'payg', protocol: 'chat_completions', label: 'GLM CN payg chat_completions', url: 'https://open.bigmodel.cn/api/paas/v4' },
    { mode: 'payg', protocol: 'anthropic', label: 'GLM CN payg anthropic', url: 'https://open.bigmodel.cn/api/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'GLM CN payg responses', url: 'https://open.bigmodel.cn/api/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'GLM CN coding chat_completions', url: 'https://open.bigmodel.cn/api/coding/paas/v4' },
    { mode: 'coding', protocol: 'anthropic', label: 'GLM CN coding anthropic', url: 'https://open.bigmodel.cn/api/anthropic' },
    { mode: 'coding', protocol: 'responses', label: 'GLM CN coding responses', url: 'https://open.bigmodel.cn/api/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'GLM Global payg chat_completions', url: 'https://api.z.ai/api/paas/v4' },
    { mode: 'payg', protocol: 'anthropic', label: 'GLM Global payg anthropic', url: 'https://api.z.ai/api/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'GLM Global payg responses', url: 'https://api.z.ai/api/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'GLM Global coding chat_completions', url: 'https://api.z.ai/api/coding/paas/v4' },
    { mode: 'coding', protocol: 'anthropic', label: 'GLM Global coding anthropic', url: 'https://api.z.ai/api/anthropic' },
    { mode: 'coding', protocol: 'responses', label: 'GLM Global coding responses', url: 'https://api.z.ai/api/v1' },
  ],
  deepseek: [
    { mode: 'payg', protocol: 'chat_completions', label: 'DeepSeek chat_completions', url: 'https://api.deepseek.com/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'DeepSeek anthropic', url: 'https://api.deepseek.com/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'DeepSeek responses', url: 'https://api.deepseek.com' },
  ],
  minimax: [
    { mode: 'payg', protocol: 'chat_completions', label: 'MiniMax Legacy CN payg chat_completions', url: 'https://api.minimaxi.com/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'MiniMax Legacy CN payg anthropic', url: 'https://api.minimaxi.com/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'MiniMax Legacy CN payg responses', url: 'https://api.minimaxi.com/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'MiniMax Legacy CN coding chat_completions', url: 'https://api.minimaxi.com/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'MiniMax Legacy CN coding anthropic', url: 'https://api.minimaxi.com/anthropic' },
    { mode: 'coding', protocol: 'responses', label: 'MiniMax Legacy CN coding responses', url: 'https://api.minimaxi.com/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'MiniMax CN payg chat_completions', url: 'https://api.minimax.cn/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'MiniMax CN payg anthropic', url: 'https://api.minimax.cn/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'MiniMax CN payg responses', url: 'https://api.minimax.cn/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'MiniMax CN coding chat_completions', url: 'https://api.minimax.cn/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'MiniMax CN coding anthropic', url: 'https://api.minimax.cn/anthropic' },
    { mode: 'coding', protocol: 'responses', label: 'MiniMax CN coding responses', url: 'https://api.minimax.cn/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'MiniMax Global payg chat_completions', url: 'https://api.minimax.io/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'MiniMax Global payg anthropic', url: 'https://api.minimax.io/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'MiniMax Global payg responses', url: 'https://api.minimax.io/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'MiniMax Global coding chat_completions', url: 'https://api.minimax.io/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'MiniMax Global coding anthropic', url: 'https://api.minimax.io/anthropic' },
    { mode: 'coding', protocol: 'responses', label: 'MiniMax Global coding responses', url: 'https://api.minimax.io/v1' },
  ],
  qianfan: [
    { mode: 'payg', protocol: 'chat_completions', label: 'Qianfan PAYG chat_completions', url: 'https://qianfan.baidubce.com/v2' },
    { mode: 'payg', protocol: 'responses', label: 'Qianfan PAYG responses', url: 'https://qianfan.baidubce.com/v2' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Qianfan Coding chat_completions', url: 'https://qianfan.baidubce.com/v2/coding' },
    { mode: 'coding', protocol: 'anthropic', label: 'Qianfan Coding anthropic', url: 'https://qianfan.baidubce.com/anthropic/coding' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Qianfan Token Personal chat_completions', url: 'https://qianfan.baidubce.com/v2/tokenplan/personal' },
    { mode: 'coding', protocol: 'anthropic', label: 'Qianfan Token Personal anthropic', url: 'https://qianfan.baidubce.com/anthropic/tokenplan/personal' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Qianfan Token Enterprise chat_completions', url: 'https://qianfan.baidubce.com/v2/tokenplan/team' },
    { mode: 'coding', protocol: 'anthropic', label: 'Qianfan Token Enterprise anthropic', url: 'https://qianfan.baidubce.com/anthropic/tokenplan/team' },
  ],
  qianwen: [
    { mode: 'payg', protocol: 'chat_completions', label: 'DashScope CN chat_completions', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'DashScope CN anthropic', url: 'https://dashscope.aliyuncs.com/apps/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'DashScope CN responses', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'DashScope Intl (QwenCloud) chat_completions', url: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'DashScope Intl (QwenCloud) anthropic', url: 'https://dashscope-intl.aliyuncs.com/apps/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'DashScope Intl (QwenCloud) responses', url: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'DashScope Coding CN chat_completions', url: 'https://coding.dashscope.aliyuncs.com/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'DashScope Coding CN anthropic', url: 'https://coding.dashscope.aliyuncs.com/apps/anthropic' },
    { mode: 'coding', protocol: 'chat_completions', label: 'DashScope Coding Intl (QwenCloud) chat_completions', url: 'https://coding-intl.dashscope.aliyuncs.com/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'DashScope Coding Intl (QwenCloud) anthropic', url: 'https://coding-intl.dashscope.aliyuncs.com/apps/anthropic' },
    { mode: 'coding', protocol: 'anthropic', label: 'Qianwen Token CN Beijing anthropic', url: 'https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic' },
    { mode: 'coding', protocol: 'responses', label: 'Qianwen Token CN Beijing responses', url: 'https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'Qianwen MaaS CN PAYG chat_completions', url: 'https://maas.qianwenaiapi.com/compatible-mode/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'Qianwen MaaS CN PAYG anthropic', url: 'https://maas.qianwenaiapi.com/apps/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'Qianwen MaaS CN PAYG responses', url: 'https://maas.qianwenaiapi.com/compatible-mode/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Qianwen MaaS CN Token chat_completions', url: 'https://token-plan.maas.qianwenaiapi.com/compatible-mode/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'Qianwen MaaS CN Token anthropic', url: 'https://token-plan.maas.qianwenaiapi.com/apps/anthropic' },
    { mode: 'coding', protocol: 'responses', label: 'Qianwen MaaS CN Token responses', url: 'https://token-plan.maas.qianwenaiapi.com/compatible-mode/v1' },
  ],
  hunyuan: [
    { mode: 'coding', protocol: 'responses', label: 'Token Enterprise Pro CN responses', url: 'https://tokenhub.tencentmaas.com/plan/v3' },
    { mode: 'coding', protocol: 'responses', label: 'Token Enterprise Lite CN responses', url: 'https://tokenhub.tencentmaas.com/plan/v3' },
    { mode: 'coding', protocol: 'responses', label: 'Token Enterprise Pro Intl responses', url: 'https://tokenhub-intl.tencentcloudmaas.com/plan/v3' },
    { mode: 'coding', protocol: 'responses', label: 'Token Enterprise Lite Intl responses', url: 'https://tokenhub-intl.tencentcloudmaas.com/plan/v3' },
    { mode: 'payg', protocol: 'chat_completions', label: 'TokenHub CN chat_completions', url: 'https://tokenhub.tencentmaas.com/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'TokenHub CN anthropic', url: 'https://tokenhub.tencentmaas.com/v1' },
    { mode: 'payg', protocol: 'responses', label: 'TokenHub CN responses', url: 'https://tokenhub.tencentmaas.com/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'TokenHub CN alternate chat_completions', url: 'https://tokenhub.tencentmaas.cn/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'TokenHub CN alternate anthropic', url: 'https://tokenhub.tencentmaas.cn/v1' },
    { mode: 'payg', protocol: 'responses', label: 'TokenHub CN alternate responses', url: 'https://tokenhub.tencentmaas.cn/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'TokenHub legacy Intl chat_completions', url: 'https://tokenhub-intl.tencentmaas.com/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'TokenHub legacy Intl anthropic', url: 'https://tokenhub-intl.tencentmaas.com/v1' },
    { mode: 'payg', protocol: 'responses', label: 'TokenHub legacy Intl responses', url: 'https://tokenhub-intl.tencentmaas.com/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'TokenHub legacy Intl CN chat_completions', url: 'https://tokenhub-intl.tencentmaas.cn/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'TokenHub legacy Intl CN anthropic', url: 'https://tokenhub-intl.tencentmaas.cn/v1' },
    { mode: 'payg', protocol: 'responses', label: 'TokenHub legacy Intl CN responses', url: 'https://tokenhub-intl.tencentmaas.cn/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'TokenHub Intl chat_completions', url: 'https://tokenhub-intl.tencentcloudmaas.com/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'TokenHub Intl anthropic', url: 'https://tokenhub-intl.tencentcloudmaas.com/v1' },
    { mode: 'payg', protocol: 'responses', label: 'TokenHub Intl responses', url: 'https://tokenhub-intl.tencentcloudmaas.com/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'Hunyuan Legacy chat_completions', url: 'https://api.hunyuan.cloud.tencent.com/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'Hunyuan Legacy anthropic', url: 'https://api.hunyuan.cloud.tencent.com/v1' },
    { mode: 'payg', protocol: 'responses', label: 'Hunyuan Legacy responses', url: 'https://api.hunyuan.cloud.tencent.com/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'TokenHub Legacy Coding chat_completions', url: 'https://api.lkeap.cloud.tencent.com/coding/v3' },
    { mode: 'coding', protocol: 'anthropic', label: 'TokenHub Legacy Coding anthropic', url: 'https://api.lkeap.cloud.tencent.com/coding/anthropic' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Token Personal CN chat_completions', url: 'https://api.lkeap.cloud.tencent.com/plan/v3' },
    { mode: 'coding', protocol: 'anthropic', label: 'Token Personal CN anthropic', url: 'https://api.lkeap.cloud.tencent.com/plan/anthropic' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Token Enterprise Pro CN chat_completions', url: 'https://tokenhub.tencentmaas.com/plan/v3' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Token Enterprise Lite CN chat_completions', url: 'https://tokenhub.tencentmaas.com/plan/v3' },
    { mode: 'coding', protocol: 'anthropic', label: 'Token Enterprise Pro CN anthropic', url: 'https://tokenhub.tencentmaas.com/plan/anthropic' },
    { mode: 'coding', protocol: 'anthropic', label: 'Token Enterprise Lite CN anthropic', url: 'https://tokenhub.tencentmaas.com/plan/anthropic' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Token Personal Intl chat_completions', url: 'https://tokenhub-intl.tencentcloudmaas.com/plan/v3' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Token Enterprise Pro Intl chat_completions', url: 'https://tokenhub-intl.tencentcloudmaas.com/plan/v3' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Token Enterprise Lite Intl chat_completions', url: 'https://tokenhub-intl.tencentcloudmaas.com/plan/v3' },
    { mode: 'coding', protocol: 'anthropic', label: 'Token Personal Intl anthropic', url: 'https://tokenhub-intl.tencentcloudmaas.com/plan/anthropic' },
    { mode: 'coding', protocol: 'anthropic', label: 'Token Enterprise Pro Intl anthropic', url: 'https://tokenhub-intl.tencentcloudmaas.com/plan/anthropic' },
    { mode: 'coding', protocol: 'anthropic', label: 'Token Enterprise Lite Intl anthropic', url: 'https://tokenhub-intl.tencentcloudmaas.com/plan/anthropic' },
  ],
  volcengine: [
    { mode: 'payg', protocol: 'chat_completions', label: 'Ark PAYG chat_completions', url: 'https://ark.cn-beijing.volces.com/api/v3' },
    { mode: 'payg', protocol: 'anthropic', label: 'Ark PAYG anthropic', url: 'https://ark.cn-beijing.volces.com/api/compatible' },
    { mode: 'payg', protocol: 'responses', label: 'Ark PAYG responses', url: 'https://ark.cn-beijing.volces.com/api/v3' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Ark Coding chat_completions', url: 'https://ark.cn-beijing.volces.com/api/coding/v3' },
    { mode: 'coding', protocol: 'anthropic', label: 'Ark Coding anthropic', url: 'https://ark.cn-beijing.volces.com/api/coding' },
    { mode: 'coding', protocol: 'responses', label: 'Ark Coding responses', url: 'https://ark.cn-beijing.volces.com/api/coding/v3' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Ark Agent chat_completions', url: 'https://ark.cn-beijing.volces.com/api/plan/v3' },
    { mode: 'coding', protocol: 'anthropic', label: 'Ark Agent anthropic', url: 'https://ark.cn-beijing.volces.com/api/plan' },
    { mode: 'coding', protocol: 'responses', label: 'Ark Agent responses', url: 'https://ark.cn-beijing.volces.com/api/plan/v3' },
  ],
}

/** Resolve only exact preset roots within the selected mode. No cross-site fallback. */
export function nativeProfileBases(platform: string, mode: string, url: string, selectedLabel?: string): Record<string, string> | undefined {
  const presets = NATIVE_PROFILE_PRESETS[platform as CnBaseUrlPresetPlatform]
  const normalize = (value: string) => value.trim().replace(/\/+$/, '')
  const matches = presets?.filter(p => p.mode === mode && normalize(p.url) === normalize(url))
  const selected = matches?.find(p => p.label === selectedLabel) ?? matches?.find(p => p.protocol === 'chat_completions') ?? matches?.[0]
  if (!selected) return undefined
  const label = selected.label.slice(0, selected.label.lastIndexOf(' '))
  return Object.fromEntries(presets!.filter(p => p.mode === mode && p.label.slice(0, p.label.lastIndexOf(' ')) === label).map(p => [p.protocol, p.url]))
}

/** Build native adaptive credentials without replacing stored, invisible overrides. */
export function buildNativeProfileBases(
  platform: string, mode: string, base: string, draft: Record<string, string>,
  defaults: Record<string, string>, existing: Record<string, unknown> = {}
): Record<string, string> {
  if (!(platform in NATIVE_PROFILE_PRESETS)) return { ...defaults, ...Object.fromEntries(Object.entries(existing).filter((entry): entry is [string, string] => typeof entry[1] === 'string')), ...draft }
  const customDraft = Object.values(draft).some(value => value.trim() && !Object.values(defaults).includes(value) && !nativeProfileBases(platform, mode, value))
  const profileBase = base.trim() || Object.values(draft).find(value => value.trim() && !Object.values(defaults).includes(value) && nativeProfileBases(platform, mode, value)) || ''
  const profile = customDraft ? undefined : nativeProfileBases(platform, mode, profileBase)
  const result: Record<string, string> = {}
  for (const [protocol, value] of Object.entries(existing)) {
    if (typeof value === 'string') result[protocol] = value
  }
  for (const protocol of ['chat_completions', 'anthropic', 'responses']) {
    const value = draft[protocol]?.trim() || ''
    // Untouched UI defaults must not accompany a different regional/custom key.
    const untouchedDefault = value === defaults[protocol] && existing[protocol] !== value
    if (value && !untouchedDefault) result[protocol] = value
    else if (!result[protocol] && profile?.[protocol]) result[protocol] = profile[protocol]!
    else if (!result[protocol] && !profileBase && !customDraft && defaults[protocol]) result[protocol] = defaults[protocol]!
  }
  if (!result.chat_completions && base.trim() && !profile && !customDraft) result.chat_completions = base.trim()
  return result
}
