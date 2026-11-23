export default {
  models: {
    title: '模型目录',
    subtitle:
      '按规范模型聚合核验四段漏斗（1. 平台候选 -> 2. 账号原生发现 -> 3. 账号有效模型 -> 4. 分组对外可见）与四层价格（参考价（非结算价） / 销售价 / 内部成本 / 实收结算价）。',
    actions: {
      openVerificationWorkbench: '目录核验工作台',
      exportUnpricedCsv: '导出缺价 CSV',
      batchActions: '批量操作 ({count})',
      refreshCatalog: '刷新目录快照',
      loadMoreModels: '加载更多模型',
      loadMoreAccounts: '加载更多发现账号',
      loadMoreOffers: '加载更多路由绑定',
      clearFilters: '清除筛选',
      inspectDetail: '查看详情'
    },
    scope: {
      workspaceLabel: '工作空间 ID',
      projectLabel: '项目 ID（留空为全部项目）',
      allProjects: '全部项目（工作空间总览）',
      catalogVersion: '目录版本',
      asOf: '快照时间',
      funnelLegend: '1. 平台候选 -> 2. 账号原生发现 -> 3. 账号有效模型 -> 4. 分组对外可见',
      priceLayerLegend: '1. 参考价（非结算价） -> 2. 销售价 -> 3. 内部成本 -> 4. 实收结算价'
    },
    quickFilters: {
      all: '全部模型',
      routable_but_unpriced: '可路由但未定价 (P0)',
      discovered_not_enabled: '已发现但未启用',
      listed_unrouted: '已上架无路由',
      group_price_conflict: '分组价格冲突',
      pi_only: '仅社区目录（pi）',
      stale: '资料或发现过期'
    },
    filters: {
      searchPlaceholder: '搜索规范 ID、公开模型名或别名…',
      providerPlaceholder: '全部厂商',
      sourceAll: '全部来源',
      availabilityAll: '全部可用性',
      billingAll: '全部价格层级',
      publicationAll: '全部发布状态'
    },
    sources: {
      builtin_reviewed: '内置审阅',
      platform_preset: '平台候选',
      builtin_supplement: '内置补充',
      pi_ai_reference: '社区目录（pi）',
      account_discovery: '账号原生发现',
      account_declaration: '连接声明',
      external_reference: '外部参考',
      heuristic: '启发式估算',
      unknown: '未知来源'
    },
    availability: {
      live: '可调用',
      listed_unrouted: '已上架·无路由',
      catalog_only: '仅目录收录',
      requires_scope: '需项目作用域',
      unavailable: '不可用',
      unknown: '状态未知'
    },
    publication: {
      published: '分组对外可见',
      unpublished: '未公开',
      unknown: '未记录发布'
    },
    billing: {
      priced: '渠道已定价（销售价）',
      free: '免费调用',
      conditional: '按条件定价',
      partial: '部分维度定价',
      conflict: '分组价格冲突',
      varies: '按分组定价',
      unpriced: '未配置销售价',
      error: '价格加载失败',
      unknown: '价格状态未知',
      p0Alert: '缺销售价 P0',
      referenceSecondary: '参考价（非结算价）：{rate} · 社区目录（pi）'
    },
    columns: {
      model: '规范模型 / 厂商',
      sources: '来源徽标组 (Axis 1)',
      availability: '可用性 (Axis 2)',
      bindings: '连接 / 分组绑定',
      pricing: '价格层级 / 告警 (Axis 3)',
      actions: '操作'
    },
    states: {
      loading: '正在读取模型目录快照…',
      empty: '当前筛选条件下没有匹配的模型记录。',
      snapshotExpired: '目录快照游标已过期（410 SNAPSHOT_EXPIRED），请刷新目录快照后继续翻页。',
      errorTitle: '读取模型目录失败'
    },
    drawer: {
      title: '模型目录详情与证据链',
      tabs: {
        inspector: '规格来源 · 漏斗 · 价格瀑布',
        verification: '目录核验与发布工作台'
      },
      provenanceTitle: '1. 字段级来源合并表（优先级：内置审阅 > 内置补充 > 社区目录（pi））',
      provenanceHint:
        '账号原生发现按连接作用域独立记录；不同来源规格不一致时以琥珀色高亮冲突，绝不静默丢弃或用 0/false 猜测未知值。',
      conflictBadge: '规格来源冲突',
      staleBadge: '证据已过期',
      funnelTitle: '2. 四段漏斗绑定链（1. 平台候选 -> 2. 账号原生发现 -> 3. 账号有效模型 -> 4. 分组对外可见）',
      stage1: '1. 平台候选',
      stage2: '2. 账号原生发现',
      stage3: '3. 账号有效模型',
      stage4: '4. 分组对外可见',
      waterfallTitle: '3. 四层价格瀑布链（1. 参考价（非结算价） -> 2. 销售价 -> 3. 内部成本 -> 4. 实收结算价）',
      layer1: '1. 参考价（非结算价）',
      layer1Hint: '来源：社区目录（pi）· 仅供目录对比，绝不作为网关扣费或场景结算依据',
      layer2: '2. 销售价',
      layer2Hint: '渠道与路由分组生效费率（group_default）',
      layer3: '3. 内部成本',
      layer3Hint: '上游连接凭据核算成本（与对外销售价严格隔离）',
      layer4: '4. 实收结算价',
      layer4Hint: '按路由分组倍率与项目/个人额度结算的最终实收金额',
      groupOnlyChannel: '仅分组证据 (channel #0)',
      unboundOfferAccount: '无具体连接绑定 (group/channel 级)'
    },
    provenanceFields: {
      context_window: '上下文窗口',
      max_output_tokens: '最大输出 Token',
      modalities: '支持模态',
      tools: '工具调用 (Tools)',
      supports_vision: '视觉输入 (Vision)',
      reasoning: '深度推理 (Reasoning)',
      prompt_caching: '提示词缓存 (Caching)',
      embedding_dimensions: '向量维度',
      pdf: 'PDF 文档输入',
      reference_price: '参考价（非结算价）'
    },
    batch: {
      title: '批量变更预览与 CAS 确认',
      subtitle: '所有批量写操作必须先核对变更前后差异（Preview），再携带版本令牌（If-Match / rules_revision）显式提交。',
      operationLabel: '批量操作类型',
      opEnableDiscovered: '加入账号有效模型（2. 账号原生发现 -> 3. 账号有效模型）',
      opDraftChannelPricing: '以参考价起草渠道销售价（1. 参考价（非结算价） -> 2. 销售价草稿）',
      opTogglePublication: '批量更新分组对外可见（3. 账号有效模型 -> 4. 分组对外可见）',
      beforeColumn: '变更前状态',
      afterColumn: '变更后预览',
      casTokenLabel: 'CAS 版本令牌 (If-Match / rules_revision)',
      confirmSubmit: '确认并携带 CAS 提交',
      conflict409Title: '检测到并发版本冲突（409 / 412 CAS Conflict）',
      conflict409Message: '目标连接或渠道配置已被其他管理员修改，草稿已保留。请刷新快照重新生成预览后再提交。',
      refreshAndRepreview: '刷新快照重新预览'
    }
  }
}
