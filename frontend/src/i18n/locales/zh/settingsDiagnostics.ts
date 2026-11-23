export default {
  eyebrow: '配置权威与溯源',
  title: '配置来源诊断',
  description:
    '核查各项配置的当前生效值、解析来源、部署硬门控与数据库软开关关系、初始播种记录及可重置覆盖项。敏感密钥明文永不回显。',
  syncBadges: {
    loading: '加载中…',
    synced: '已同步',
    notSynced: '未同步',
    stale: '同步异常',
    issuesCount: '{count} 项异常'
  },
  actions: {
    refresh: '刷新',
    clearFilters: '清除筛选',
    loadMore: '加载更多',
    loadingMore: '加载中…',
    resetToBaseline: '恢复部署基线',
    confirmReset: '确认重置',
    cancel: '取消',
    retry: '重试'
  },
  meta: {
    configFileLabel: '配置文件 (CONFIG_FILE)',
    configFileUnset: '未显式指定（仅环境变量与默认配置）',
    generatedAtLabel: '快照时间',
    showingCount: '已加载 {count} 项',
    moreAvailable: '仍有更多配置项可加载'
  },
  filters: {
    searchPlaceholder: '搜索配置键名或所属分组…',
    searchAriaLabel: '搜索配置键名或所属分组',
    groupLabel: '配置分组',
    allGroups: '全部分组',
    sourceLabel: '当前生效来源',
    allSources: '全部来源',
    stateLabel: '健康与门控状态',
    allStates: '全部状态'
  },
  groups: {
    system: '系统基础',
    security: '安全与认证',
    onboarding: '新用户默认配额',
    gateway: '网关运行时',
    email: '邮件与 SMTP',
    plaza: '模型广场',
    ops: '运维监控',
    catalog: '模型目录',
    logging: '日志格式',
    image_storage: '图片对象存储',
    backup: '备份与恢复',
    linuxdo_connect: 'LinuxDo 登录',
    dingtalk_connect: '钉钉企业登录',
    wechat_connect: '微信登录',
    oidc_connect: 'OIDC 登录',
    github_oauth: 'GitHub OAuth',
    google_oauth: 'Google OAuth'
  },
  sources: {
    database: '数据库',
    environment: '环境变量',
    yaml: 'YAML 配置文件',
    default: '注册表默认',
    derived: '派生门控',
    artifact: '版本化制品'
  },
  authorities: {
    deployment: '部署静态权威',
    database_override: '数据库可覆盖',
    database_only: '仅数据库权威',
    versioned_artifact: '版本化制品权威'
  },
  states: {
    effective: '正常生效',
    invalid: '配置损坏',
    unavailable: '存储不可用',
    blocked_by_hard_gate: '受硬门控阻断'
  },
  seedSources: {
    install_seed: '新装初始化播种',
    environment: '环境变量播种',
    yaml: 'YAML 播种',
    default: '注册表默认播种',
    admin: '管理员写入',
    unknown: '存量升级 / 未知来源'
  },
  columns: {
    setting: '配置键与权威边界',
    effectiveValue: '当前生效值',
    sourceAndChain: '生效来源与回退链',
    stateAndGate: '门控与健康解释',
    actions: '覆盖重置'
  },
  values: {
    secretConfigured: '已配置（密钥不回显）',
    secretNotConfigured: '未配置（密钥不回显）',
    secretUnknown: '状态未知（密钥不回显）',
    emptyString: '""（空字符串）',
    unavailableValue: '不可用（故障安全关闭）',
    seedProvenanceValue: '初始播种：{source}',
    restartRequired: '需重启生效',
    overrideFrozenHint: '数据库覆盖已生效 — 重置前环境变量与 YAML 变更将被冻结屏蔽。'
  },
  seed: {
    badge: '初始播种来源：{source}',
    newUsersOnlyHint:
      '当前生效值以数据库为准；部署配置仅用于首次安装播种，修改此值仅影响之后新注册的用户。',
    provenanceRowHint: '仅记录迁移或首次安装的初始播种来源元数据，并非当前运行期配置源。',
    plazaSeedHint:
      '模型广场由数据库唯一权威控制（缺键或读取异常时按安全关闭处理；全新安装默认播种开启，升级保留既有值）。'
  },
  upgradeNotes: {
    catalogRefreshOptIn: 'v0.3.4 部署默认改为 false（模型目录后台刷新需显式开启）。',
    logFormatJson: 'v0.3.4 部署默认统一为 json；运行期运维日志策略不覆盖 log.format。'
  },
  gates: {
    guideTitle: '部署硬门控与数据库软开关规则',
    guideBody:
      '部署硬门控（环境变量/YAML，如 webauthn.enabled、ops.enabled 及生产模式 turnstile.required）决定运行期能力边界：硬门关闭时数据库软开关被阻断，移除硬门后数据库残留 true 不会重新开启功能；生产环境强制要求开启但数据库软开关或密钥不完整时，相关流程按故障安全关闭拒绝。',
    blockedByKey: '受硬门控阻断：{blockedBy}',
    turnstileRequiredIncomplete:
      '生产环境已开启强制校验 (turnstile.required)，但数据库人机验证开关或密钥未完整配置 — 登录校验按故障安全关闭拒绝。',
    passkeyGateHint:
      '最终生效需同时满足部署硬门控 webauthn.enabled（含 RP 配置）与数据库软开关 passkey_enabled。',
    opsGateHint: '最终生效需同时满足部署硬门控 ops.enabled 与数据库软开关 ops_monitoring_enabled。',
    healthyHint: '已按权威来源正常解析生效。',
    invalidHint: '存储值或部署配置校验失败 ({code})；运行期按故障安全策略处理，不会静默伪装为正常默认。',
    unavailableHint: '底层配置存储暂不可读 ({code})；运行期按故障安全关闭处理，不以默认值掩盖故障。'
  },
  alerts: {
    unhealthyBannerTitle: '检测到 {count} 项配置校验失败或存储不可用',
    unhealthyBannerBody:
      '损坏或不可读的配置项已显式标记告警，系统绝不将其伪装为正常默认值，请核查下方高亮条目。',
    forbiddenTitle: '需要管理员权限 (403)',
    loadFailedTitle: '加载生效配置诊断失败',
    staleTitle: '刷新失败 — 当前展示快照可能已过期',
    emptyTitle: '未返回已注册配置项',
    filteredEmptyTitle: '没有匹配当前筛选条件的配置项',
    filteredEmptyDescription: '请清除搜索关键词或筛选条件以查看全部注册配置项。'
  },
  reset: {
    notResettableDeployment: '部署静态项',
    notResettableDbOnly: '仅数据库项',
    notResettableDerived: '派生门控',
    notResettableArtifact: '版本化制品',
    modalTitle: '恢复部署基线（移除数据库覆盖）',
    modalDescriptionSingle:
      '将删除「{key}」的数据库覆盖记录，并立即按部署配置优先级（环境变量 → YAML → 注册表默认）重新解析生效值。',
    modalDescriptionGroup:
      '「{key}」属于「{group}」原子覆盖组。确认重置后将在同一事务内删除「{group}」组的全部数据库覆盖项，并按当前部署配置重新解析。',
    groupScopeBadge: '整组原子重置：{group}',
    previewHeading: '重置后基线预览（敏感密钥永不回显明文）',
    currentLabel: '当前生效值 ({source})',
    baselineLabel: '将恢复的基线值',
    secretBaselineRedacted: '已脱敏 — 密钥基线由服务端按部署配置重解析，不展示明文',
    baselineUnavailable: '由服务端按部署基线重解析',
    restartNotice: '涉及需重启生效的配置项，部分静态组件需重启服务后完全生效。',
    stepUpNotice: '涉及凭据或登录提供商覆盖重置时，可能需要完成 TOTP 二次验证。',
    failedTitle: '重置失败 — 数据库覆盖未清除',
    unknownOutcomeTitle: '重置结果待确认 — 请刷新核实当前状态',
    successToastTitle: '已恢复部署基线',
    successToastBody: '已更新 {count} 项生效配置状态。'
  },
  workbench: {
    loadingBadge: '加载中…',
    notSyncedBadge: '未同步',
    syncFailedBadge: '同步异常',
    refreshFailedTitle: '刷新失败 — 当前显示上次已加载配置',
    saveFailedTitle: '分组配置保存失败'
  }
}
