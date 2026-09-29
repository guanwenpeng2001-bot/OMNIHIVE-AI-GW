export default {
  capabilityDefaults: {
  title: '补充私有模型能力', input: '账号模型能力声明',
  hint: '由部署持有人按原始模型 ID 声明实际输入/输出上限、模态、推理控制和函数调用能力，并填写依据。原生有效字段优先；声明不会增加授权或路由。预览检查具体采用字段和缺口，保存账号后重新预览并应用目录。清空表示移除声明。',
  invalid: '请输入有效声明：schema_version 为 1，reason 为核验依据，models 为按原始模型 ID 索引的非空对象。',
  placeholder: '留空表示不提供账号部署规格', example: '查看格式示例（数值仅演示，请替换为真实部署规格）',
  binding: 'confirmed_by、confirmed_at 和身份信息由服务端生成。账号身份变化后，核验部署规格并移除旧 identity_fingerprint，再预览和保存以重新确认。reasoning_control.kind 支持 none / effort / toggle / budget / adaptive / fixed / unknown；仅填写实际支持的控制形式。'
},
  capabilityCatalog: {
  countSummary: "上游发现 {discovered} 个模型 · 映射目标 {mapped} 个 · 合并目录 {merged} 个 · 符合路由条件 {eligible} 个",
  eligibilityHint: "能力资料完整不等于符合路由条件，符合条件也不保证实际调用成功。",
  progress: "显示 {shown} 个模型，共 {matched} 个匹配结果",
  candidateProgress: "显示 {shown} 个候选，共 {matched} 个匹配结果",
  coverageSummary: "资料完整 {complete} 个模型，共 {total} 个；仅有估计依据 {estimated} 个；资料不完整或未提供 {incomplete} 个模型",
  missingFields: "尚缺的信息",

  selectFiltered: '全选筛选结果', resolutions: '分辨率', duration: '最长时长（秒）', dimensions: '维度', width: '最大宽度', height: '最大高度',
  sets: '模型集合', setsHint: 'D 上游发现 · M 映射目标 · U 能力目录（D ∪ M）· E 可路由资格；能力完整不等于有路由资格，有资格也不保证实际调用成功。', coverage: '能力完整', noDiscovery: '未发现模型', noEligibility: '已发现模型，但无路由资格', partial: '部分模型能力缺失或仅有估计依据', complete: '能力完整', unknownDiscovery: '发现状态未知', search: '筛选模型 ID 或名称', filter: '筛选能力状态', all: '全部状态', more: '显示更多',
  title: '能力资料', missing: '资料不完整或未提供', estimated: '仅有估计依据', conflict: '冲突',
  unknown: '状态未提供', unavailable: '上游未返回逐字段完整性结论，请重新预览；不能视为能力完整。',
  source: '来源级别', version: '来源版本', match: '匹配依据', noSources: '尚无逐字段来源证据。', stale: '规格或账号配置已修改，以下结果来自旧预览。'
},
  quickstartGuide: {
  title: "按 Key 生成接入示例",
  hint: "通过面板会话读取我的 Key 与可见模型，不向网关发送密钥，也不执行生成请求；示例从环境变量读取。",
  loadKeys: "读取我的 Key",
  loading: "正在读取元数据…",
  noKeys: "本页没有已绑定分组的可用 Key；可翻页查看，或创建、启用 Key 并绑定分组。",
  gatewayUrl: "网关地址：",
  trustGateway: "我确认此网关地址可信；示例中的地址取自站点配置。",
  keyGroup: "我的 Key / 分组",
  selectKey: "请选择",
  previous: "上一页",
  next: "下一页",
  loadModels: "读取该 Key 的可见模型",
  loaded: "模型读取成功（面板会话读取）；账务回执：不适用（未执行生成）。",
  noModels: "此分组当前没有可见模型。",
  filter: "筛选模型",
  noMatches: "筛选无匹配；清除筛选后重试。",
  visibleModel: "可见模型",
  unknownEndpoints: "面板只给出可见模型名，未声明端点能力。请按分组文档选择协议；此示例不代表可用性已验证。",
  unsupportedEndpoints: "未声明本指引支持的端点，暂不生成示例。",
  protocol: "请求协议",
  selectProtocol: "请选择协议",
  billingHint: "手动运行示例可能产生费用。HTTP 200 仅表示传输成功；生成后的账务回执未确认时为 pending receipt，请在我的用量核对。",
  keysFailed: "无法读取 Key 列表或站点公开设置，请重试。",
  missingApiBaseUrl: "本站尚未配置公开 API 地址（api_base_url），无法生成接入示例。请联系管理员配置后重试。",
  accessDenied: "无权读取该 Key 的模型目录，请核对 Key、分组和访问权限。",
  catalogFailed: "模型读取失败，请核对分组与访问权限后重试。"
},
  capabilityMessages: {
  "invalid": "模型部署规格无效，请核对声明格式、核验依据与账号身份后重新预览。",
  "missing": "缺失字段",
  "estimated": "仅有估计依据",
  "conflicts": "冲突"
}
}
