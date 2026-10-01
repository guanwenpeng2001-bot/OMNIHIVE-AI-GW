export default {
  proxyGroups: {
    title: 'IP分组', description: '组合IP节点，查看账号出口绑定。',
    create: '新建分组', edit: '编辑分组', name: '名称', descriptionLabel: '描述', region: '地区标签', services: '目标服务标签',
    tagsHint: '标签仅声明用途，不代表目标服务一定可访问。',
    search: '搜索分组', allStatus: '全部状态', allServices: '全部服务', active: '启用', inactive: '停用', expired: '已过期',
    counts: '可调度 / 总数', countsHint: '可调度指启用、未过期且不在冷却中的节点，不保证业务请求成功。',
    accounts: '绑定账号', fallback: '没有可调度节点时', none: '请求失败（默认）', direct: '直连', proxy: '备用节点',
    directHint: '直连：组内没有可调度节点时，请求将不经代理直接发出。',
    inactiveHint: '手动停用的分组将明确失败，不会通过退路绕过。',
    ttl: '绑定有效期（秒）', advanced: '高级设置', members: '管理成员', bindings: '绑定观测',
    empty: '没有找到IP分组。请新建分组或调整筛选条件。', loadFailed: '无法加载IP分组，请重试。',
    saveFailed: '保存失败。', saved: '已保存。', delete: '删除分组', deleteConfirm: '确定删除此IP分组？节点将保留。',
    inUse: '请先解除账号对该分组的配置绑定，再删除分组。', deleteFailed: '删除失败，该分组可能仍有绑定账号。',
    memberSearch: '搜索节点名称、主机或地区', allRegions: '全部地区', selected: '已选（{count}）', selectVisible: '选择当前筛选节点', clearSelection: '清空选择',
    noNodes: '没有匹配的节点。', observations: '探测观测', unknown: '未探测', cooldown: '冷却至 {time}',
    membersHint: '未知健康状态仍可选择。停用、过期成员保留可见，但不参与调度。',
    membersLoadFailed: '无法加载成员，请关闭后重试。',
    account: '账号', node: '当前节点', expires: '绑定到期时间', eligible: '合格', ineligible: '不合格', unbound: '当前未绑定节点',
    noBindings: '没有账号配置绑定此分组。', reset: '全部重绑', resetConfirm: '下一次请求起重新分配节点，不会断开正在进行的流或WebSocket连接。',
    resetDone: '已清除 {count} 个绑定。', bindingsFailed: '无法加载或重置绑定。',
    egress: '账号出口', noProxy: '不使用代理', defaultEgress: '沿用本机或服务器原有默认出口，不会自动选择IP节点或IP分组。', selectionRequired: '请选择节点或分组，或选择不使用代理。', single: '单个节点', group: 'IP分组', chooseGroup: '选择IP分组', chooseNode: '选择节点',
    groupHint: '当前出口节点由分组调度。', unavailableGroup: '已选分组 #{id}（不可用）',
    groupLoadFailed: '无法加载分组，当前选择已保留。', retry: '重试',
    health: { healthy: '观测健康', warn: '观测警告', challenge: '观测到验证挑战', failed: '观测失败' }
  }
}
