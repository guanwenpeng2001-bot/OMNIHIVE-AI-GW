/** User domain contracts. */
import type { NotifyEmailEntry } from './common'
import type { ProxyNode } from './admin'
import type {
  GroupPlatform,
  VideoModelPrices,
  OpenAIMessagesDispatchModelConfig,
  ReasoningEffortMapping,
} from './model'

export type UserAuthProvider = 'email' | 'linuxdo' | 'oidc' | 'wechat' | 'github' | 'google' | 'dingtalk'

export interface UserAuthBindingStatus {
  bound?: boolean
  bound_count?: number
  provider?: UserAuthProvider | string
  provider_key?: string | null
  provider_subject?: string | null
  issuer?: string | null
  label?: string | null
  provider_label?: string | null
  display_name?: string | null
  subject_hint?: string | null
  verified_at?: string | null
  bind_start_path?: string | null
  can_bind?: boolean
  can_unbind?: boolean
  note_key?: string | null
  note?: string | null
  metadata?: Record<string, unknown>
}

export interface UserProfileSourceContext {
  provider?: UserAuthProvider | string
  source?: string | null
  label?: string | null
  provider_label?: string | null
}

export interface User {
  id: number
  username: string
  email: string
  avatar_url?: string | null
  avatar_source?: string | UserProfileSourceContext | null
  username_source?: string | UserProfileSourceContext | null
  display_name_source?: string | UserProfileSourceContext | null
  nickname_source?: string | UserProfileSourceContext | null
  profile_sources?: {
    avatar?: string | UserProfileSourceContext | null
    username?: string | UserProfileSourceContext | null
    display_name?: string | UserProfileSourceContext | null
    nickname?: string | UserProfileSourceContext | null
  }
  auth_bindings?: Partial<Record<UserAuthProvider, boolean | UserAuthBindingStatus>>
  identity_bindings?: Partial<Record<UserAuthProvider, boolean | UserAuthBindingStatus>>
  email_bound?: boolean
  linuxdo_bound?: boolean
  oidc_bound?: boolean
  wechat_bound?: boolean
  role: 'admin' | 'user' // User role for authorization
  balance: number // User balance for API usage
  frozen_balance?: number // Balance currently held by async batch jobs
  concurrency: number // Allowed concurrent requests
  rpm_limit?: number // User-level RPM cap (0 = unlimited); effective as fallback when group has no rpm_limit
  status: 'active' | 'disabled' // Account status
  allowed_groups: number[] | null // Allowed group IDs (null = all non-exclusive groups)
  balance_notify_enabled: boolean
  balance_notify_threshold: number | null
  balance_notify_extra_emails: NotifyEmailEntry[]
  subscriptions?: UserSubscription[] // User's active subscriptions
  last_active_at?: string | null
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface LoginRequest {
  email: string
  password: string
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
}

export interface TencentCaptchaRequestProof {
  tencent_captcha_ticket: string
  tencent_captcha_randstr: string
}

// 动作触发式验证码（OAuth 启动、passkey 等入口）的请求凭据：
// 腾讯填 tencent_captcha_*，阿里云的 captchaVerifyParam 复用 turnstile_token 字段

export interface ActionCaptchaRequestProof extends Partial<TencentCaptchaRequestProof> {
  turnstile_token?: string
}

export interface RegisterRequest {
  email: string
  password: string
  verify_code?: string
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
  promo_code?: string
  invitation_code?: string
  aff_code?: string
}

export interface AffiliateInvitee {
  user_id: number
  email: string
  username: string
  created_at?: string
  total_rebate: number
}

export interface UserAffiliateDetail {
  user_id: number
  aff_code: string
  inviter_id?: number | null
  aff_count: number
  aff_quota: number
  aff_frozen_quota: number
  aff_history_quota: number
  /** 当前用户作为邀请人时实际生效的返利比例（专属覆盖全局）。0-100。 */
  effective_rebate_rate_percent: number
  invitees: AffiliateInvitee[]
}

export interface AffiliateTransferResponse {
  transferred_quota: number
  balance: number
}

export interface SendVerifyCodeRequest {
  email: string
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
  pending_auth_token?: string
  pending_oauth_token?: string
}

export interface SendVerifyCodeResponse {
  message: string
  countdown: number
}

export interface CustomMenuItem {
  id: string
  label: string
  icon_svg: string
  url: string
  page_slug?: string
  hide_open_button?: boolean
  visibility: 'user' | 'admin'
  sort_order: number
}

export interface CustomEndpoint {
  name: string
  endpoint: string
  description: string
}

export interface LoginAgreementDocument {
  id: string
  title: string
  content_md: string
}

export interface PublicSettings {
  registration_enabled: boolean
  email_verify_enabled: boolean
  force_email_on_third_party_signup: boolean
  registration_email_suffix_whitelist: string[]
  registration_email_domain_quota_enabled?: boolean
  promo_code_enabled: boolean
  password_reset_enabled: boolean
  invitation_code_enabled: boolean
  login_agreement_enabled?: boolean
  login_agreement_mode?: 'modal' | 'checkbox' | string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
  turnstile_enabled: boolean
  tencent_captcha_enabled?: boolean
  tencent_captcha_app_id?: string
  tencent_captcha_region?: string
  passkey_enabled?: boolean
  turnstile_site_key: string
  aliyun_captcha_enabled?: boolean
  aliyun_captcha_scene_id?: string
  aliyun_captcha_prefix?: string
  aliyun_captcha_region?: string
  site_name: string
  site_logo: string
  site_subtitle: string
  api_base_url: string
  contact_info: string
  doc_url: string
  home_content: string
  compact_home_enabled: boolean
  hide_ccs_import_button: boolean
  payment_enabled: boolean
  risk_control_enabled: boolean
  table_default_page_size: number
  table_page_size_options: number[]
  custom_menu_items: CustomMenuItem[]
  custom_endpoints: CustomEndpoint[]
  linuxdo_oauth_enabled: boolean
  dingtalk_oauth_enabled?: boolean
  wechat_oauth_enabled: boolean
  wechat_oauth_open_enabled?: boolean
  wechat_oauth_mp_enabled?: boolean
  wechat_oauth_mobile_enabled?: boolean
  oidc_oauth_enabled: boolean
  oidc_oauth_provider_name: string
  github_oauth_enabled: boolean
  google_oauth_enabled: boolean
  backend_mode_enabled: boolean
  version: string
  // 服务器全局时区（IANA 名称与当前 UTC 偏移），高峰时段等服务端本地时间窗口的展示标注用；
  // 可选：注入的 __APP_CONFIG__ 旧缓存可能缺失
  server_timezone?: string
  server_utc_offset?: string
  balance_low_notify_enabled: boolean
  account_quota_notify_enabled: boolean
  balance_low_notify_threshold: number
  channel_monitor_enabled: boolean
  /** Exclusive mode: v1 active probes or v2 passive aggregation. Default v2. */
  channel_monitor_mode?: 'v1' | 'v2'
  channel_monitor_default_interval_seconds: number
  /** When true, user monitor hides RPM/TPM so scale cannot be reverse-estimated. */
  channel_monitor_hide_throughput?: boolean
  /** When true, user monitor shows account quota/balance snapshots (default off). */
  channel_monitor_show_quota?: boolean
  /** When true, user monitor hides the user ranking tab and /users payload. */
  channel_monitor_hide_user_ranking?: boolean
  available_channels_enabled: boolean
  /** When false, the whole user-facing subscription surface is hidden. Default true. */
  subscription_enabled: boolean
  /** Mirrors payment config BALANCE_PAYMENT_DISABLED; true = balance top-up closed (subscription-only site). */
  payment_balance_disabled: boolean
  model_plaza_enabled: boolean
  model_plaza_require_auth: boolean
  plugin_management_enabled: boolean
  service_quota_enabled: boolean
  affiliate_enabled: boolean
  allow_user_view_error_requests?: boolean
}

export interface AuthResponse {
  access_token: string
  refresh_token?: string  // New: Refresh Token for token renewal
  expires_in?: number     // New: Access Token expiry time in seconds
  token_type: string
  user: User & { run_mode?: 'standard' | 'simple' }
}

export interface CurrentUserResponse extends User {
  run_mode?: 'standard' | 'simple'
}

export interface Subscription {
  id: number
  user_id: number
  name: string
  url: string
  type: 'clash' | 'v2ray' | 'surge' | 'quantumult' | 'shadowrocket'
  update_interval: number // in hours
  last_updated: string | null
  node_count: number
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface CreateSubscriptionRequest {
  name: string
  url: string
  type: Subscription['type']
  update_interval?: number
}

export interface UpdateSubscriptionRequest {
  name?: string
  url?: string
  type?: Subscription['type']
  update_interval?: number
  is_active?: boolean
}

export interface SubscriptionStats {
  subscription_id: number
  total_nodes: number
  available_nodes: number
  avg_latency: number | null
  by_type: Record<ProxyNode['type'], number>
  last_update: string
}

export interface UserStats {
  total_subscriptions: number
  total_nodes: number
  active_subscriptions: number
  total_conversions: number
  last_conversion: string | null
}

export type SubscriptionType = 'standard' | 'subscription'

export interface Group {
  account_type?: '' | 'bedrock' | 'service_account'
  id: number
  name: string
  description: string | null
  platform: GroupPlatform
  rate_multiplier: number
  rpm_limit?: number // Group-level RPM cap (0 = unlimited); overrides user-level rpm_limit when set
  max_reasoning_effort?: string // Anthropic/OpenAI reasoning ceiling; empty means unlimited
  max_reasoning_effort_over_limit?: string // downgrade (default) or deny when over the ceiling
  reasoning_effort_mappings?: ReasoningEffortMapping[]
  is_exclusive: boolean
  status: 'active' | 'inactive'
  subscription_type: SubscriptionType
  daily_limit_usd: number | null
  weekly_limit_usd: number | null
  monthly_limit_usd: number | null
  long_context_pricing_enabled: boolean
  // 图片生成计费配置
  allow_image_generation: boolean
  allow_batch_image_generation: boolean
  image_rate_independent: boolean
  image_rate_multiplier: number
  batch_image_discount_multiplier: number
  batch_image_hold_multiplier: number
  image_price_1k: number | null
  image_price_2k: number | null
  image_price_4k: number | null
  video_rate_independent: boolean
  video_rate_multiplier: number
  video_price_480p: number | null
  video_price_720p: number | null
  video_price_1080p: number | null
  // Optional model-family x resolution overrides for Grok video pricing.
  video_model_prices?: VideoModelPrices
  // Codex 网页搜索单次价格（USD/次）；null 表示使用默认价 0.01
  web_search_price_per_call: number | null
  // Grok Voice 显式定价（分组级）
  search_price_per_1k: number | null
  audio_realtime_price_per_min: number | null
  audio_tts_price_per_million_chars: number | null
  audio_stt_price_per_hour: number | null
  // 高峰时段倍率配置
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
  // Claude Code 客户端限制
  claude_code_only: boolean
  fallback_group_id: number | null
  fallback_group_id_on_invalid_request: number | null
  // OpenAI Messages 调度开关（用户侧需要此字段判断是否展示 Claude Code 教程）
  allow_messages_dispatch?: boolean
  // OpenAI Live 接口开关
  allow_live: boolean
  default_mapped_model?: string
  messages_dispatch_model_config?: OpenAIMessagesDispatchModelConfig
  require_oauth_only: boolean
  require_privacy_set: boolean
  created_at: string
  updated_at: string
}

export interface ApiKey {
  id: number
  user_id: number
  key: string
  name: string
  group_id: number | null
  // disabled = 管理员/风控停用（不可在面板自助恢复）；inactive = 用户自行停用
  status: 'active' | 'inactive' | 'disabled' | 'quota_exhausted' | 'expired'
  ip_whitelist: string[]
  ip_blacklist: string[]
  last_used_at: string | null
  last_used_ip: string | null
  quota: number // Quota limit in USD (0 = unlimited)
  quota_used: number // Used quota amount in USD
  expires_at: string | null // Expiration time (null = never expires)
  created_at: string
  updated_at: string
  current_concurrency: number
  group?: Group
  rate_limit_5h: number
  rate_limit_1d: number
  rate_limit_7d: number
  usage_5h: number
  usage_1d: number
  usage_7d: number
  window_5h_start: string | null
  window_1d_start: string | null
  window_7d_start: string | null
  reset_5h_at: string | null
  reset_1d_at: string | null
  reset_7d_at: string | null
  /**
   * Period budget in USD. This is a spend limit over one explicit period and is
   * deliberately separate from RPM/TPM and from the legacy USD windows above:
   * budget_* describes money, not request rates.
   */
  budget_period_mode: 'off' | 'duration' | 'calendar'
  budget_limit: number
  budget_used: number
  budget_reserved: number
  budget_remaining: number
  budget_overrun: number
  budget_period_start: string | null
  budget_period_end: string | null
  budget_period_display: string
  /** Historical 5h/1d/7d anchor when the period is one of the compatible presets. */
  budget_legacy_anchor: string
  budget_duration_seconds: number
  budget_calendar_unit: '' | 'day' | 'week' | 'month'
  budget_anchor: '' | 'first_use' | 'day'
  budget_timezone: string
  budget_enforced: boolean
  /**
   * Effective RPM explained to the user: which dimension owns each limit, what
   * the limit is and when the current minute window ends. Only user/group scope
   * facts appear here; upstream account quota is never exposed.
   */
  effective_rpm?: ApiKeyEffectiveRPM
}

/** One effective RPM limit that actually applies to a key. */
export interface ApiKeyEffectiveRPMLimit {
  source: 'user' | 'group' | 'user_group_override'
  limit: number
  scope_id: number
  window_seconds: number
}

/** User-visible explanation of the effective RPM limits of one key. */
export interface ApiKeyEffectiveRPM {
  limits: ApiKeyEffectiveRPMLimit[]
  /** Real window end; absent when no trustworthy reset time is available. */
  window_resets_at?: string | null
  /** Machine-readable precedence contract: override, then group, then the user ceiling. */
  priority: string
}

/**
 * Period budget request fields (USD). Shared by the create and update contracts;
 * `clear_budget` removes a self-set budget and is rejected for enforced ones.
 */
export interface ApiKeyPeriodBudgetRequest {
  legacy_budget_anchor?: string
  budget_period_mode?: string
  budget_limit?: number
  budget_duration_seconds?: number
  budget_anchor?: string
  budget_calendar_unit?: string
  budget_timezone?: string
  clear_budget?: boolean
}

export interface CreateApiKeyRequest extends ApiKeyPeriodBudgetRequest {
  name: string
  group_id?: number | null
  custom_key?: string // Optional custom API Key
  ip_whitelist?: string[]
  ip_blacklist?: string[]
  quota?: number // Quota limit in USD (0 = unlimited)
  expires_in_days?: number // Days until expiry (null = never expires)
  rate_limit_5h?: number
  rate_limit_1d?: number
  rate_limit_7d?: number
}

export interface UpdateApiKeyRequest extends ApiKeyPeriodBudgetRequest {
  name?: string
  group_id?: number | null
  status?: 'active' | 'inactive'
  ip_whitelist?: string[]
  ip_blacklist?: string[]
  quota?: number // Quota limit in USD (null = no change, 0 = unlimited)
  expires_at?: string | null // Expiration time (null = no change)
  reset_quota?: boolean // Reset quota_used to 0
  rate_limit_5h?: number
  rate_limit_1d?: number
  rate_limit_7d?: number
  /** Owner-facing usage resets are rejected by the server; use an audited adjustment instead. */
  reset_rate_limit_usage?: boolean
}

export type RedeemCodeType = 'balance' | 'concurrency' | 'subscription' | 'invitation'

export type UsageRequestType = 'unknown' | 'sync' | 'stream' | 'ws_v2' | 'cyber' | 'live'

export type ImageSizeSource = 'output' | 'input' | 'default' | 'legacy'

export type ImageSizeBreakdown = Record<string, number>

export interface UsagePricingSnapshot {
  pricing_state: string
  model: string
  provider?: string
  mode: string
  source: string
  price_card_version?: string
  tokens?: Record<string, number | boolean>
  token_prices?: Record<string, number | boolean | string | null>
  unit_price?: number
  units?: number
  size_tier?: string
  service_tier?: string
  thinking_enabled?: boolean
  long_context_applied?: boolean
  rate_multiplier: number
  time_multiplier: number
  reasoning_multiplier: number
  adjustment_multiplier?: number
  fx?: Record<string, unknown>
  charged_amount: number
}

export interface UsageLog {
  billing_explanation?: {
    price_source?: string | null
    price_card_version?: string | null
    pricing_state: string | null
    settlement_state: string | null
    completion_state: string | null
    snapshot: UsagePricingSnapshot | null
    family?: string
    job_id?: string
    session_id?: string
    turn_id?: string
  } | null
  session_id?: string | null
  id: number
  user_id: number
  api_key_id: number
  account_id: number | null
  request_id: string
  model: string
  service_tier?: string | null
  reasoning_effort?: string | null
  inbound_endpoint?: string | null
  upstream_endpoint?: string | null

  group_id: number | null
  subscription_id: number | null

  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  cache_creation_5m_tokens: number
  cache_creation_1h_tokens: number

  input_cost: number
  output_cost: number
  cache_creation_cost: number
  cache_read_cost: number
  total_cost: number
  actual_cost: number
  rate_multiplier: number
  long_context_billing_applied: boolean
  billing_type: number

  request_type?: UsageRequestType
  stream: boolean
  openai_ws_mode?: boolean
  native_compaction_v2: boolean
  duration_ms: number | null
  first_token_ms: number | null

  // 图片生成字段
  media_type?: string | null
  image_count: number
  image_size: string | null
  image_input_size: string | null
  image_output_size: string | null
  image_size_source: ImageSizeSource | null
  image_size_breakdown: ImageSizeBreakdown | null
  image_input_tokens: number
  image_input_cost: number
  image_output_tokens: number
  image_output_cost: number

  // User-Agent
  user_agent: string | null
  ip_address?: string | null

  // Cache TTL Override
  cache_ttl_overridden: boolean

  // 计费模式
  billing_mode?: string | null

  created_at: string

  user?: User
  api_key?: ApiKey
  group?: Group
  subscription?: UserSubscription
}

export interface RedeemCode {
  id: number
  code: string
  type: RedeemCodeType
  value: number
  status: 'active' | 'used' | 'expired' | 'unused' | 'disabled'
  used_by: number | null
  used_at: string | null
  created_at: string
  expires_at?: string | null
  updated_at?: string
  notes?: string
  group_id?: number | null // 订阅类型专用
  validity_days?: number // 订阅类型专用
  user?: User
  group?: Group // 关联的分组
}

export interface GenerateRedeemCodesRequest {
  count: number
  type: RedeemCodeType
  value: number
  group_id?: number | null // 订阅类型专用
  validity_days?: number // 订阅类型专用
  expires_at?: string | null
  expires_in_days?: number
}

export interface BatchUpdateRedeemCodeFields {
  status?: 'unused' | 'disabled'
  expires_at?: string | null
  notes?: string
  group_id?: number | null
}

export interface BatchUpdateRedeemCodesRequest {
  ids: number[]
  fields: BatchUpdateRedeemCodeFields
}

export interface RedeemCodeRequest {
  code: string
}

// Matches usagestats.Coverage; timestamps are RFC3339 strings.

export interface UsageCoverage {
  complete: boolean
  reason?: string
  raw_retained_from: string
  hourly_retained_from?: string
  daily_retained_from?: string
  published_before: string
  dirty_from?: string
  timezone: string
  projection_version: number
}

export interface UsageStatsResponse {
  period?: string
  total_requests: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_tokens: number
  total_cache_read_tokens: number
  total_cache_creation_tokens: number
  total_tokens: number
  total_cost: number // 标准计费
  total_actual_cost: number // 实际扣除
  average_duration_ms: number
  models?: Record<string, number>
  endpoints?: EndpointStat[]
  upstream_endpoints?: EndpointStat[]
  endpoint_paths?: EndpointStat[]
}

export interface TrendDataPoint {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number // 标准计费
  actual_cost: number // 实际扣除
}

export interface ModelStat {
  model: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number // 标准计费
  actual_cost: number // 实际扣除
  account_cost?: number // 账号成本（仅管理员接口返回）
}

export interface EndpointStat {
  endpoint: string
  requests: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface GroupStat {
  group_id: number
  group_name: string
  requests: number
  total_tokens: number
  cost: number // 标准计费
  actual_cost: number // 实际扣除
  account_cost?: number // 账号成本（仅管理员接口返回）
}

export interface UserBreakdownItem {
  user_id: number
  email: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
  account_cost: number
}

export interface UserUsageTrendPoint {
  date: string
  user_id: number
  email: string
  username: string
  requests: number
  tokens: number
  cost: number // 标准计费
  actual_cost: number // 实际扣除
}

export interface UserSpendingRankingItem {
  user_id: number
  email: string
  username: string
  actual_cost: number
  requests: number
  tokens: number
}

export interface UserSpendingRankingResponse {
  ranking: UserSpendingRankingItem[]
  total_actual_cost: number
  total_requests: number
  total_tokens: number
  start_date: string
  end_date: string
}

export interface ApiKeyUsageTrendPoint {
  date: string
  api_key_id: number
  key_name: string
  requests: number
  tokens: number
}

export interface ChangePasswordRequest {
  old_password: string
  new_password: string
}

export interface UserSubscription {
  id: number
  user_id: number
  group_id: number
  status: 'active' | 'expired' | 'revoked' | 'suspended'
  starts_at: string
  daily_usage_usd: number
  weekly_usage_usd: number
  monthly_usage_usd: number
  daily_window_start: string | null
  weekly_window_start: string | null
  monthly_window_start: string | null
  created_at: string
  updated_at: string
  revoked_at?: string | null
  expires_at: string | null
  user?: User
  group?: Group
}

export interface SubscriptionProgress {
  subscription_id: number
  daily: {
    used: number
    limit: number | null
    percentage: number
    reset_in_seconds: number | null
  } | null
  weekly: {
    used: number
    limit: number | null
    percentage: number
    reset_in_seconds: number | null
  } | null
  monthly: {
    used: number
    limit: number | null
    percentage: number
    reset_in_seconds: number | null
  } | null
  expires_at: string | null
  days_remaining: number | null
}

export interface AssignSubscriptionRequest {
  user_id: number
  group_id: number
  validity_days?: number
}

export interface BulkAssignSubscriptionRequest {
  user_ids: number[]
  group_id: number
  validity_days?: number
}

export interface ExtendSubscriptionRequest {
  days: number
}

export interface UserErrorRequest {
  id: number
  created_at: string
  model: string
  inbound_endpoint: string
  status_code: number
  category: string
  platform: string
  message: string
  key_name: string
  key_deleted: boolean
  client_ip?: string
  group_name?: string
  request_type?: number
  stream?: boolean
  user_agent?: string
}

export interface UserErrorRequestDetail extends UserErrorRequest {
  error_body: string
  upstream_status_code?: number
}

export interface UserErrorListParams {
  page?: number
  page_size?: number
  start_date?: string
  end_date?: string
  timezone?: string
  model?: string
  status_code?: number
  category?: string
  api_key_id?: number
  // 服务端排序,列白名单见后端 opsErrorLogsOrderBy(created_at/model/status_code)
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export interface UsageQueryParams {
  page?: number
  page_size?: number
  api_key_id?: number
  user_id?: number
  account_id?: number
  group_id?: number
  model?: string
  request_type?: UsageRequestType
  stream?: boolean
  native_compaction_v2?: boolean | null
  billing_type?: number | null
  billing_mode?: string | null
  start_date?: string
  end_date?: string
  timezone?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export interface TotpStatus {
  enabled: boolean
  enabled_at: number | null  // Unix timestamp in seconds
  feature_enabled: boolean
}

export interface TotpSetupRequest {
  email_code?: string
  password?: string
}

export interface TotpSetupResponse {
  secret: string
  qr_code_url: string
  setup_token: string
  countdown: number
}

export interface TotpEnableRequest {
  totp_code: string
  setup_token: string
}

export interface TotpEnableResponse {
  success: boolean
}

export interface TotpDisableRequest {
  email_code?: string
  password?: string
}

export interface TotpVerificationMethod {
  method: 'email' | 'password'
}

export interface TotpLoginResponse {
  requires_2fa: boolean
  temp_token?: string
  user_email_masked?: string
}

export interface TotpLogin2FARequest {
  temp_token: string
  totp_code: string
}

/** Public /v1/usage response, with the two server billing modes kept distinct. */
export interface KeyUsageTotals {
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface KeyDailyUsage {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost: number
  actual_cost: number
}

export interface KeyUsageSummary {
  isValid: boolean
  remaining?: number
  unit?: string
  usage?: {
    today: KeyUsageTotals
    total: KeyUsageTotals
    average_duration_ms: number
    rpm: number
    tpm: number
  }
  daily_usage?: KeyDailyUsage[]
  model_stats?: ModelStat[]
}

export type KeyUsageResponse = KeyUsageSummary & (
  | {
      mode: 'quota_limited'
      status: ApiKey['status']
      quota?: { limit: number; used: number; remaining: number; unit: string }
      rate_limits?: Array<{
        window: '5h' | '1d' | '7d'
        limit: number
        used: number
        remaining: number
        window_start: string | null
        reset_at?: string
      }>
      expires_at?: string
      days_until_expiry?: number
    }
  | {
      mode: 'unrestricted'
      planName: string
      balance?: number
      subscription?: {
        daily_usage_usd: number
        weekly_usage_usd: number
        monthly_usage_usd: number
        daily_limit_usd: number | null
        weekly_limit_usd: number | null
        monthly_limit_usd: number | null
        weekly_window_start: string | null
        expires_at: string
      }
    }
)
