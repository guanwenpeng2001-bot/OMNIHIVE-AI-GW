export type ActiveCaptchaProvider = 'turnstile' | 'tencent' | 'aliyun' | 'none'

export interface CaptchaProviderConfig {
  turnstileEnabled?: boolean
  turnstileSiteKey?: string
  tencentEnabled?: boolean
  tencentAppId?: string
  aliyunEnabled?: boolean
  aliyunSceneId?: string
  aliyunPrefix?: string
}

/**
 * Resolves the single active captcha provider matching CaptchaChallenge.vue's
 * rendering precedence (Turnstile > Tencent > Aliyun).
 */
export function resolveActiveCaptchaProvider(
  config: CaptchaProviderConfig
): ActiveCaptchaProvider {
  if (config.turnstileEnabled && Boolean(config.turnstileSiteKey?.trim())) {
    return 'turnstile'
  }
  if (config.tencentEnabled && Boolean(config.tencentAppId?.trim())) {
    return 'tencent'
  }
  if (
    config.aliyunEnabled &&
    Boolean(config.aliyunSceneId?.trim()) &&
    Boolean(config.aliyunPrefix?.trim())
  ) {
    return 'aliyun'
  }
  return 'none'
}
