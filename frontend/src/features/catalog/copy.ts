export interface CatalogUiCopy {
  origins: {
    reviewedRegistry: string
    builtinSupplement: string
    platformBuiltin: string
    communityPi: string
    accountDiscovery: string
    platformPreset: string
    channelPublished: string
    heuristic: string
    unknown: string
  }
  availability: {
    live: string
    listedUnrouted: string
    requiresScope: string
    catalogOnly: string
    unavailable: string
    sentences: {
      live: string
      listedUnrouted: string
      requiresScope: string
      catalogOnlyPlatform: string
      catalogOnlyDiscovered: string
      catalogOnlyDefault: string
      unavailable: string
      unknown: string
    }
  }
  billing: {
    perToken: string
    tiered: string
    perRequest: string
    priced: string
    free: string
    varies: string
    variesFull: string
    conflict: string
    conditional: string
    partial: string
    unpriced: string
    referenceOnly: string
    error: string
    errorFull: string
    unknown: string
    retryPricing: string
    seeDetail: string
    seeDetailTooltip: string
    referencePrefix: string
    referenceSourcePrefix: string
    nonSettlementShort: string
    nonSettlementFull: string
    observedAtPrefix: string
    unpricedHint: string
    reasons: {
      no_sale_tariff: string
      missing_price: string
      billing_provider_undetermined: string
      required_dimension_missing: string
      offer_price_missing: string
      partial_price: string
      offers_disagree: string
      conflicting_prices: string
      scenario_required: string
      price_read_failed: string
    }
  }
  filters: {
    searchPlaceholder: string
    availabilityLabel: string
    availabilityAll: string
    availabilityLive: string
    availabilityUnavailable: string
    availabilityCatalogOnly: string
    billingLabel: string
    billingAll: string
    billingPriced: string
    billingFree: string
    billingVaries: string
    billingUnpriced: string
    originLabel: string
    originAll: string
    regionLabel: string
    regionUs: string
    regionEu: string
    regionApac: string
    regionGlobal: string
    retentionLabel: string
    retentionZero: string
    retentionNone: string
    retentionLimited: string
    retentionStandard: string
    protocolLabel: string
    modalityInputLabel: string
    providerLabel: string
    resetFilters: string
    favoritesOnly: string
  }
  empty: {
    liveNoMatchesTitle: string
    liveNoMatchesDesc: string
    viewCatalogOnly: string
    contactSupport: string
    catalogOnlyQuickstartBanner: string
  }
}

const ZH_COPY: CatalogUiCopy = {
  origins: {
    reviewedRegistry: '内置审阅',
    builtinSupplement: '内置补充',
    platformBuiltin: '平台内置目录',
    communityPi: '社区目录（pi）',
    accountDiscovery: '上游实时发现',
    platformPreset: '平台候选',
    channelPublished: '渠道已发布',
    heuristic: '启发式补充',
    unknown: '来源未知'
  },
  availability: {
    live: '可调用',
    listedUnrouted: '已上架·暂不可用',
    requiresScope: '需项目授权',
    catalogOnly: '仅目录收录',
    unavailable: '已停用',
    sentences: {
      live: '该模型已在网关上线，可使用有效个人或项目 API 密钥直接调用。',
      listedUnrouted: '已上架·暂不可用 · 渠道已发布该名称，但当前无可用上游路由绑定',
      requiresScope: '该模型已发布至平台目录，登录后使用有效个人或项目 API 密钥即可调用。',
      catalogOnlyPlatform: '仅目录收录 · 本平台尚未接入该模型路由，规格与参考价仅供资料查阅',
      catalogOnlyDiscovered: '仅目录收录 · 上游连接已原生发现该模型，但管理员尚未加入有效模型',
      catalogOnlyDefault: '仅目录收录 · 本平台尚未接入该模型路由，规格与参考价仅供资料查阅。',
      unavailable: '当前作用域下没有可用的上游路由绑定。',
      unknown: '当前响应未包含完整的可用性状态，系统按未知处理，不作推测。'
    }
  },
  billing: {
    perToken: '按量计费',
    tiered: '阶梯定价',
    perRequest: '按次计费',
    priced: '已定价',
    free: '免费',
    varies: '按分组定价',
    variesFull: '按分组定价（不同路由分组费率或倍率存在差异，点击查看分组明细）',
    conflict: '多方案价格差异',
    conditional: '按条件计费',
    partial: '部分已定价',
    unpriced: '未定价',
    referenceOnly: '仅参考价',
    error: '价格加载失败',
    errorFull: '价格加载失败（计费快照暂不可用，请点击重试，切勿当作免费或未定价）',
    unknown: '价格未知',
    retryPricing: '重试',
    seeDetail: '详见模型页',
    seeDetailTooltip: '该模型使用分段或按次计费，点击查看模型详情中的完整费率表',
    referencePrefix: '参考价',
    referenceSourcePrefix: '参考来源',
    nonSettlementShort: '非网关结算价',
    nonSettlementFull: '仅供参考，非网关结算价',
    observedAtPrefix: '观测于',
    unpricedHint: '当前未配置销售价，实际调用将按网关无定价策略拒绝或由管理员规则处理',
    reasons: {
      no_sale_tariff: '尚未配置销售定价',
      missing_price: '尚未配置销售定价',
      billing_provider_undetermined: '计费归属供应商未确定',
      required_dimension_missing: '缺少必要的计费维度',
      offer_price_missing: '部分分组或动作缺少定价',
      partial_price: '部分分组或动作缺少定价',
      offers_disagree: '不同分组或渠道报价不一致',
      conflicting_prices: '不同分组或渠道报价不一致',
      scenario_required: '需要指定上下文/时长/规格参数以计算定价',
      price_read_failed: '价格读取失败，可重试'
    }
  },
  filters: {
    searchPlaceholder: '搜索模型名称、ID 或供应商（按 / 聚焦）',
    availabilityLabel: '可用性',
    availabilityAll: '全部',
    availabilityLive: '可调用',
    availabilityUnavailable: '暂不可用',
    availabilityCatalogOnly: '仅目录收录',
    billingLabel: '价格状态',
    billingAll: '全部',
    billingPriced: '有销售价',
    billingFree: '免费',
    billingVaries: '按分组定价',
    billingUnpriced: '未定价',
    originLabel: '资料来源',
    originAll: '全部来源',
    regionLabel: '部署区域',
    regionUs: '北美 US',
    regionEu: '欧洲 EU',
    regionApac: '亚太 APAC',
    regionGlobal: '全球 Global',
    retentionLabel: '留存合规',
    retentionZero: '零留存 Zero',
    retentionNone: '不落盘 None',
    retentionLimited: '受限审计留存',
    retentionStandard: '标准策略',
    protocolLabel: '兼容协议',
    modalityInputLabel: '输入模态',
    providerLabel: '供应商',
    resetFilters: '重置筛选条件',
    favoritesOnly: '仅看收藏'
  },
  empty: {
    liveNoMatchesTitle: '当前筛选下没有可直接调用的模型',
    liveNoMatchesDesc: '当前筛选下没有可直接调用的模型。可清除筛选或查看「仅目录收录」模型。',
    viewCatalogOnly: '查看「仅目录收录」模型',
    contactSupport: '联系支持',
    catalogOnlyQuickstartBanner: '本平台尚未启用该模型；以下规格与调用示例仅作参考，不构成接入或报价承诺。'
  }
}

const EN_COPY: CatalogUiCopy = {
  origins: {
    reviewedRegistry: 'Reviewed Built-in',
    builtinSupplement: 'Built-in Supplement',
    platformBuiltin: 'Platform Built-in',
    communityPi: 'Community Catalog (pi)',
    accountDiscovery: 'Upstream Discovered',
    platformPreset: 'Platform Candidate',
    channelPublished: 'Channel Published',
    heuristic: 'Heuristic',
    unknown: 'Unknown Origin'
  },
  availability: {
    live: 'Available',
    listedUnrouted: 'Listed · Unrouted',
    requiresScope: 'Requires Project Grant',
    catalogOnly: 'Catalog Only',
    unavailable: 'Unavailable',
    sentences: {
      live: 'This model is live on the gateway and ready to call with a valid personal or project API key.',
      listedUnrouted: 'Listed · Temporarily Unavailable · Zero active upstream routes',
      requiresScope: 'This model is published on the platform; sign in with an eligible personal or project API key to call it.',
      catalogOnlyPlatform: 'Catalog Only · Not routed on platform; specs for reference',
      catalogOnlyDiscovered: 'Catalog Only · Discovered upstream, not enabled in effective',
      catalogOnlyDefault: 'Catalog Only · Not routed on platform; specs for reference.',
      unavailable: 'No eligible upstream routing binding is currently available in this perspective.',
      unknown: 'Availability status was omitted from the payload; rendered as unknown without guessing.'
    }
  },
  billing: {
    perToken: 'Per-Token',
    tiered: 'Tiered',
    perRequest: 'Per-Request',
    priced: 'Priced',
    free: 'Free',
    varies: 'Group-Specific',
    variesFull: 'Group-Specific Pricing (Rates vary by group; click for breakdown)',
    conflict: 'Group / Offer Variance',
    conditional: 'Conditional',
    partial: 'Partially Priced',
    unpriced: 'Unpriced',
    referenceOnly: 'Reference Only',
    error: 'Pricing Unavailable',
    errorFull: 'Pricing Unavailable (Snapshot error; retry—not free/unpriced)',
    unknown: 'Pricing Unknown',
    retryPricing: 'Retry',
    seeDetail: 'See detail',
    seeDetailTooltip: 'Open model details for tiered or per-request pricing bands',
    referencePrefix: 'Ref',
    referenceSourcePrefix: 'Reference source',
    nonSettlementShort: 'Non-settlement',
    nonSettlementFull: 'Indicative reference only · Non-settlement rate',
    observedAtPrefix: 'Observed',
    unpricedHint: 'No platform sale tariff is configured; runtime calls fail closed or follow operator policy',
    reasons: {
      no_sale_tariff: 'No sale tariff configured',
      missing_price: 'No sale tariff configured',
      billing_provider_undetermined: 'Billing provider undetermined',
      required_dimension_missing: 'Required billing dimension missing',
      offer_price_missing: 'Missing price on one or more offers',
      partial_price: 'Missing price on one or more offers',
      offers_disagree: 'Rates vary across groups or offers',
      conflicting_prices: 'Rates vary across groups or offers',
      scenario_required: 'Scenario dimensions required to compute price',
      price_read_failed: 'Failed to read pricing data; retry'
    }
  },
  filters: {
    searchPlaceholder: 'Search model name, ID, or provider (press /)',
    availabilityLabel: 'Availability',
    availabilityAll: 'All',
    availabilityLive: 'Available',
    availabilityUnavailable: 'Unavailable',
    availabilityCatalogOnly: 'Catalog Only',
    billingLabel: 'Pricing',
    billingAll: 'All',
    billingPriced: 'Priced',
    billingFree: 'Free',
    billingVaries: 'Group-Specific',
    billingUnpriced: 'Unpriced',
    originLabel: 'Source',
    originAll: 'All sources',
    regionLabel: 'Region',
    regionUs: 'US',
    regionEu: 'EU',
    regionApac: 'APAC',
    regionGlobal: 'Global',
    retentionLabel: 'Retention',
    retentionZero: 'Zero Retention',
    retentionNone: 'No Retention',
    retentionLimited: 'Limited Retention',
    retentionStandard: 'Standard',
    protocolLabel: 'Protocol',
    modalityInputLabel: 'Modality',
    providerLabel: 'Provider',
    resetFilters: 'Clear filters',
    favoritesOnly: 'Favorites'
  },
  empty: {
    liveNoMatchesTitle: 'No directly callable models match',
    liveNoMatchesDesc: 'No live models match filters. Clear filters or view Catalog-Only.',
    viewCatalogOnly: 'View Catalog-Only models',
    contactSupport: 'Contact Support',
    catalogOnlyQuickstartBanner: 'Platform has not enabled this model (catalog-only); specification and code examples below are for reference only.'
  }
}

export function getCatalogCopy(isZh: boolean): CatalogUiCopy {
  return isZh ? ZH_COPY : EN_COPY
}

export function catalogLocaleMessages(isZh: boolean) {
  const copy = getCatalogCopy(isZh)
  const { sentences } = copy.availability
  return {
    reason: {
      platform: sentences.catalogOnlyPlatform,
      discovered: sentences.catalogOnlyDiscovered,
      unrouted: sentences.listedUnrouted
    },
    price: { varies: copy.billing.variesFull, error: copy.billing.errorFull },
    empty: { liveFilter: copy.empty.liveNoMatchesDesc }
  }
}
