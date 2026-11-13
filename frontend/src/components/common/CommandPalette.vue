<template>
  <Teleport to="body">
    <div
      v-if="cmdkOpen"
      id="cmdk"
      class="cmdk-backdrop"
      role="dialog"
      aria-modal="true"
      aria-label="OmniHive 全局命令面板"
      @click.self="close"
    >
      <div class="cmd-card">
        <div class="cmd-header">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            class="shrink-0 opacity-50"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <input
            ref="inputRef"
            v-model="query"
            type="text"
            autocomplete="off"
            placeholder="搜索页面、模型广场、主题方向与快捷动作…  ⌘K"
            aria-label="搜索命令或页面"
            @keydown="handleInputKeydown"
          />
          <button
            type="button"
            class="kbd cursor-pointer hover:opacity-80"
            @click="close"
          >
            ESC
          </button>
        </div>

        <div ref="listRef" class="cmd-list" role="listbox">
          <template v-if="filteredEntries.length > 0">
            <template v-for="(item, idx) in filteredEntries" :key="item.id">
              <div
                v-if="idx === 0 || filteredEntries[idx - 1]?.section !== item.section"
                class="cmd-section-title"
              >
                {{ item.section }}
              </div>
              <button
                type="button"
                role="option"
                :aria-selected="idx === selectedIndex"
                class="cmd-item w-full text-left"
                :class="{ sel: idx === selectedIndex }"
                @mouseenter="selectedIndex = idx"
                @click="executeItem(item)"
              >
                <span class="cmd-badge" aria-hidden="true">{{ item.badge }}</span>
                <span class="min-w-0 flex-1 truncate font-medium">{{ item.title }}</span>
                <span v-if="item.hint" class="cmd-hint">{{ item.hint }}</span>
                <span class="cmd-sub">{{ item.group }}</span>
              </button>
            </template>
          </template>
          <div v-else class="px-5 py-8 text-center text-sm opacity-55">
            没有匹配的页面、模型或命令
          </div>
        </div>

        <div class="cmd-footer">
          <div class="flex items-center gap-2">
            <span class="kbd">↑↓</span>
            <span>选择</span>
            <span class="kbd">↵</span>
            <span>跳转</span>
            <span class="kbd">ESC</span>
            <span>关闭</span>
          </div>
          <span class="font-mono">OmniHive Command</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore, useAppStore } from '@/stores'
import { useThemeDirection, THEME_DIRECTIONS } from '@/composables/useThemeDirection'

interface CommandEntry {
  id: string
  badge: string
  section: string
  title: string
  group: string
  hint?: string
  keywords: string
  run: () => void
}

const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()
const {
  cmdkOpen,
  closeCommandPalette,
  setThemeDirection,
  toggleDarkMode,
  toggleDenseMode,
  openThemePickerModal
} = useThemeDirection()

const query = ref('')
const selectedIndex = ref(0)
const inputRef = ref<HTMLInputElement | null>(null)
const listRef = ref<HTMLElement | null>(null)

function navigateTo(path: string, title: string) {
  void router.push(path)
  appStore.showInfo?.(`已前往 · ${title}`)
}

const allEntries = computed<CommandEntry[]>(() => {
  const entries: CommandEntry[] = [
    // 热门模型直达（OpenRouter 风格）
    {
      id: 'mdl-claude-sonnet',
      badge: 'AI',
      section: '热门模型 · TRENDING MODELS',
      title: 'anthropic/claude-sonnet-4-6',
      hint: '200K · Agents',
      group: '模型目录',
      keywords: 'claude sonnet 4.6 anthropic coding agent 模型',
      run: () => navigateTo('/model-plaza?q=claude-sonnet', '模型广场 · Claude Sonnet')
    },
    {
      id: 'mdl-gpt-5',
      badge: 'AI',
      section: '热门模型 · TRENDING MODELS',
      title: 'openai/gpt-5.4',
      hint: 'Reasoning',
      group: '模型目录',
      keywords: 'gpt 5.4 openai codex responses 模型',
      run: () => navigateTo('/model-plaza?q=gpt-5', '模型广场 · GPT-5.4')
    },
    {
      id: 'mdl-gemini-3',
      badge: 'AI',
      section: '热门模型 · TRENDING MODELS',
      title: 'google/gemini-3.1-pro',
      hint: '1M Context',
      group: '模型目录',
      keywords: 'gemini 3.1 pro google multimodal 视觉 模型',
      run: () => navigateTo('/model-plaza?q=gemini-3', '模型广场 · Gemini 3.1 Pro')
    },
    {
      id: 'mdl-deepseek-r1',
      badge: 'AI',
      section: '热门模型 · TRENDING MODELS',
      title: 'deepseek/deepseek-r1',
      hint: '$0.14/M',
      group: '模型目录',
      keywords: 'deepseek r1 v3 推理 性价比 模型',
      run: () => navigateTo('/model-plaza?q=deepseek', '模型广场 · DeepSeek')
    },
    // 公开门户
    {
      id: 'pub-home',
      badge: '01',
      section: '公开门户 · PUBLIC',
      title: '落地页 / 首页 (Home)',
      group: '公开门户',
      keywords: 'home 首页 落地页 主页 omnihive',
      run: () => navigateTo('/home', '首页')
    },
    {
      id: 'pub-plaza',
      badge: '02',
      section: '公开门户 · PUBLIC',
      title: '模型广场 (Model Plaza)',
      group: '公开门户',
      keywords: 'model plaza 模型广场 定价 目录 claude gpt gemini deepseek',
      run: () => navigateTo('/model-plaza', '模型广场')
    },
    {
      id: 'pub-key-usage',
      badge: '03',
      section: '公开门户 · PUBLIC',
      title: '凭 Key 公开查询用量',
      group: '公开门户',
      keywords: 'key usage 公开 用量 查询 免登录',
      run: () => navigateTo('/key-usage', '公开用量查询')
    }
  ]

  if (!authStore.isAuthenticated) {
    entries.push(
      {
        id: 'pub-login',
        badge: '→',
        section: '认证入口 · AUTH',
        title: '登录控制台 (Sign In)',
        group: '账户认证',
        keywords: 'login signin 登录 控制台',
        run: () => navigateTo('/login', '登录')
      },
      {
        id: 'pub-register',
        badge: '+',
        section: '认证入口 · AUTH',
        title: '注册新账户 (Create Account)',
        group: '账户认证',
        keywords: 'register signup 注册 账户',
        run: () => navigateTo('/register', '注册')
      }
    )
  } else {
    entries.push(
      {
        id: 'user-dashboard',
        badge: '⌘1',
        section: '用户控制台 · CONSOLE',
        title: '控制台首页 (Dashboard)',
        group: '概览',
        keywords: 'dashboard 控制台 概览 仪表盘 余额 趋势',
        run: () => navigateTo('/dashboard', '控制台首页')
      },
      {
        id: 'user-workspaces',
        badge: 'WS',
        section: '用户控制台 · CONSOLE',
        title: '工作空间与路由策略 (Workspaces)',
        group: '概览',
        keywords: 'workspaces 工作空间 团队 路由策略 zdr 合规',
        run: () => navigateTo('/workspaces', '工作空间')
      },
      {
        id: 'user-keys',
        badge: 'KEY',
        section: '用户控制台 · CONSOLE',
        title: 'API 密钥管理 (API Keys)',
        group: '密钥与用量',
        keywords: 'keys api 密钥 令牌 token 创建 测速',
        run: () => navigateTo('/keys', 'API 密钥')
      },
      {
        id: 'user-usage',
        badge: 'LOG',
        section: '用户控制台 · CONSOLE',
        title: '用量记录与明细 (Usage Logs)',
        group: '密钥与用量',
        keywords: 'usage 用量 记录 明细 日志 导出',
        run: () => navigateTo('/usage', '用量记录')
      },
      {
        id: 'user-batch-image',
        badge: 'IMG',
        section: '用户控制台 · CONSOLE',
        title: '批量生图工作台 (Batch Image)',
        group: '资源',
        keywords: 'batch image 批量生图 绘图 图片',
        run: () => navigateTo('/batch-image', '批量生图')
      },
      {
        id: 'user-channels',
        badge: 'CH',
        section: '用户控制台 · CONSOLE',
        title: '可用渠道与模型定价 (Available Channels)',
        group: '资源',
        keywords: 'available channels 可用渠道 定价 模型',
        run: () => navigateTo('/available-channels', '可用渠道')
      },
      {
        id: 'user-monitor',
        badge: 'SLA',
        section: '用户控制台 · CONSOLE',
        title: '渠道状态看板 (Channel Status)',
        group: '资源',
        keywords: 'monitor 渠道状态 监控 健康 延迟',
        run: () => navigateTo('/monitor', '渠道状态')
      },
      {
        id: 'user-subscriptions',
        badge: 'SUB',
        section: '用户控制台 · CONSOLE',
        title: '我的订阅 (My Subscriptions)',
        group: '账户',
        keywords: 'subscriptions 我的订阅 套餐 配额',
        run: () => navigateTo('/subscriptions', '我的订阅')
      },
      {
        id: 'user-purchase',
        badge: 'PAY',
        section: '用户控制台 · CONSOLE',
        title: '充值与购买订阅 (Purchase)',
        group: '账户',
        keywords: 'purchase 充值 订阅 支付 购买 钱包',
        run: () => navigateTo('/purchase', '充值 / 订阅')
      },
      {
        id: 'user-orders',
        badge: 'ORD',
        section: '用户控制台 · CONSOLE',
        title: '我的订单 (My Orders)',
        group: '账户',
        keywords: 'orders 我的订单 账单 支付记录',
        run: () => navigateTo('/orders', '我的订单')
      },
      {
        id: 'user-redeem',
        badge: 'RDM',
        section: '用户控制台 · CONSOLE',
        title: '兑换码中心 (Redeem Code)',
        group: '账户',
        keywords: 'redeem 兑换码 卡密 充值码',
        run: () => navigateTo('/redeem', '兑换码')
      },
      {
        id: 'user-affiliate',
        badge: 'AFF',
        section: '用户控制台 · CONSOLE',
        title: '邀请返利 (Affiliate)',
        group: '账户',
        keywords: 'affiliate 邀请 返利 佣金 推广',
        run: () => navigateTo('/affiliate', '邀请返利')
      },
      {
        id: 'user-help',
        badge: 'DOC',
        section: '用户控制台 · CONSOLE',
        title: '快速接入与使用指南 (Help Guide)',
        group: '概览',
        keywords: 'help guide 帮助 教程 指南 快速接入 文档 opencode claude codex',
        run: () => navigateTo('/help', '使用指南')
      },
      {
        id: 'user-profile',
        badge: 'SEC',
        section: '用户控制台 · CONSOLE',
        title: '个人资料与安全设置 (Profile)',
        group: '账户',
        keywords: 'profile 个人资料 密码 2fa totp passkey 绑定',
        run: () => navigateTo('/profile', '个人资料')
      },
      {
        id: 'user-mcp',
        badge: 'MCP',
        section: '用户控制台 · CONSOLE',
        title: 'MCP 授权管理 (MCP OAuth Grants)',
        group: '账户',
        keywords: 'mcp oauth 授权 客户端 claude desktop cursor',
        run: () => navigateTo('/settings/mcp', 'MCP 授权')
      }
    )
  }

  if (authStore.isAdmin) {
    entries.push(
      {
        id: 'admin-dashboard',
        badge: 'ADM',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '管理总览仪表盘 (Admin Overview)',
        group: '运行',
        keywords: 'admin dashboard 管理总览 仪表盘 运行总览',
        run: () => navigateTo('/admin/dashboard', '管理总览')
      },
      {
        id: 'admin-ops',
        badge: 'OPS',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '运维实时监控 (Real-time Ops)',
        group: '运行',
        keywords: 'admin ops 运维 实时监控 延迟 错误率 告警 脉搏',
        run: () => navigateTo('/admin/ops', '运维实时监控')
      },
      {
        id: 'admin-chmonitor',
        badge: 'MON',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '渠道监控管理 (Channel Monitor)',
        group: '运行',
        keywords: 'admin channel monitor 渠道监控 探活',
        run: () => navigateTo('/admin/channels/monitor', '渠道监控')
      },
      {
        id: 'admin-usage',
        badge: 'USG',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '全站用量记录 (Admin Usage)',
        group: '运行',
        keywords: 'admin usage 全站用量 记录 排行 错误',
        run: () => navigateTo('/admin/usage', '全站用量记录')
      },
      {
        id: 'admin-audit',
        badge: 'AUD',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '审计日志 (Audit Logs)',
        group: '运行',
        keywords: 'admin audit logs 审计日志 操作记录',
        run: () => navigateTo('/admin/audit-logs', '审计日志')
      },
      {
        id: 'admin-channels',
        badge: 'PRC',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '渠道定价管理 (Channel Pricing)',
        group: '渠道与账户',
        keywords: 'admin channels pricing 渠道定价 倍率 模型价格',
        run: () => navigateTo('/admin/channels/pricing', '渠道定价')
      },
      {
        id: 'admin-accounts',
        badge: 'ACC',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '上游账号池管理 (Upstream Accounts)',
        group: '渠道与账户',
        keywords: 'admin accounts 上游账号 凭证 配额 调度 测速',
        run: () => navigateTo('/admin/accounts', '上游账号')
      },
      {
        id: 'admin-groups',
        badge: 'GRP',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '分组管理 (Groups)',
        group: '渠道与账户',
        keywords: 'admin groups 分组管理 白名单 利润 推理强度',
        run: () => navigateTo('/admin/groups', '分组管理')
      },
      {
        id: 'admin-proxies',
        badge: 'PRX',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: 'IP 代理节点 (IP Proxies)',
        group: '渠道与账户',
        keywords: 'admin proxies ip 代理 节点',
        run: () => navigateTo('/admin/proxies', 'IP 代理')
      },
      {
        id: 'admin-plugins',
        badge: 'PLG',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '插件中心 (Plugins)',
        group: '渠道与账户',
        keywords: 'admin plugins 插件 扩展',
        run: () => navigateTo('/admin/plugins', '插件中心')
      },
      {
        id: 'admin-orders-dash',
        badge: 'REV',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '支付大盘 (Payment Dashboard)',
        group: '结算',
        keywords: 'admin orders dashboard 支付大盘 营收',
        run: () => navigateTo('/admin/orders/dashboard', '支付大盘')
      },
      {
        id: 'admin-orders',
        badge: 'BIL',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '订单管理 (Admin Orders)',
        group: '结算',
        keywords: 'admin orders 订单管理 退款',
        run: () => navigateTo('/admin/orders', '订单管理')
      },
      {
        id: 'admin-plans',
        badge: 'PLN',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '订阅套餐配置 (Payment Plans)',
        group: '结算',
        keywords: 'admin plans 订阅套餐 价格',
        run: () => navigateTo('/admin/orders/plans', '订阅套餐')
      },
      {
        id: 'admin-subscriptions',
        badge: 'SUB',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '订阅分配管理 (Subscriptions)',
        group: '结算',
        keywords: 'admin subscriptions 订阅分配 续期',
        run: () => navigateTo('/admin/subscriptions', '订阅分配')
      },
      {
        id: 'admin-redeem',
        badge: 'RDM',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '兑换码生成与管理 (Admin Redeem)',
        group: '结算',
        keywords: 'admin redeem 兑换码 批量生成',
        run: () => navigateTo('/admin/redeem', '兑换码管理')
      },
      {
        id: 'admin-promo',
        badge: 'PRM',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '优惠码管理 (Promo Codes)',
        group: '结算',
        keywords: 'admin promo codes 优惠码 折扣',
        run: () => navigateTo('/admin/promo-codes', '优惠码管理')
      },
      {
        id: 'admin-users',
        badge: 'USR',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '用户管理 (Users)',
        group: '用户',
        keywords: 'admin users 用户管理 余额 配额 角色',
        run: () => navigateTo('/admin/users', '用户管理')
      },
      {
        id: 'admin-announcements',
        badge: 'MSG',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '系统公告 (Announcements)',
        group: '用户',
        keywords: 'admin announcements 公告 通知',
        run: () => navigateTo('/admin/announcements', '系统公告')
      },
      {
        id: 'admin-risk',
        badge: 'RSK',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '风控中心 (Risk Control)',
        group: '用户',
        keywords: 'admin risk control 风控 审核 拦截',
        run: () => navigateTo('/admin/risk-control', '风控中心')
      },
      {
        id: 'admin-prompt-audit',
        badge: 'PMT',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '提示词审计 (Prompt Audit)',
        group: '用户',
        keywords: 'admin prompt audit 提示词审计 敏感词',
        run: () => navigateTo('/admin/prompt-audit', '提示词审计')
      },
      {
        id: 'admin-settings',
        badge: 'CFG',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '系统设置 · 9 大配置域 (System Settings)',
        group: '系统',
        keywords: 'admin settings 系统设置 通用 安全 网关 支付 邮件 备份',
        run: () => navigateTo('/admin/settings', '系统设置')
      },
      {
        id: 'admin-backup',
        badge: 'BAK',
        section: '管理工作台 · ADMIN WORKBENCH',
        title: '数据备份与归档 (Backup & Archive)',
        group: '系统',
        keywords: 'admin backup 备份 归档 s3 恢复 导出',
        run: () => navigateTo('/admin/backup', '备份与归档')
      }
    )
  }

  // 主题与外观快捷动作
  entries.push(
    {
      id: 'act-dark',
      badge: 'UI',
      section: '外观与工作台 · APPEARANCE',
      title: '切换深色 / 浅色模式 (Toggle Dark Mode)',
      group: '外观',
      keywords: 'dark light 深色 浅色 暗黑 明暗 主题',
      run: () => {
        const dark = toggleDarkMode()
        appStore.showInfo?.(dark ? '已切换为深色模式' : '已切换为浅色模式')
      }
    },
    {
      id: 'act-dense',
      badge: 'UI',
      section: '外观与工作台 · APPEARANCE',
      title: '切换紧凑 / 舒适信息密度 (Toggle Density)',
      group: '外观',
      keywords: 'dense 密度 紧凑 舒适 表格',
      run: () => {
        const dense = toggleDenseMode()
        appStore.showInfo?.(dense ? '已开启高密度紧凑视图' : '已恢复舒适留白视图')
      }
    },
    {
      id: 'act-theme-modal',
      badge: 'UI',
      section: '外观与工作台 · APPEARANCE',
      title: '打开设计方向与外观面板 (Theme Picker)',
      group: '外观',
      keywords: 'theme picker 设计方向 主题 面板 配色',
      run: () => {
        openThemePickerModal()
      }
    },
    ...THEME_DIRECTIONS.map((t) => ({
      id: `theme-${t.key}`,
      badge: 'THM',
      section: '外观与工作台 · APPEARANCE',
      title: `切换设计方向：${t.name}`,
      group: '5 套设计方向',
      keywords: `theme 主题 设计方向 ${t.key} ${t.name} ${t.desc}`.toLowerCase(),
      run: () => {
        setThemeDirection(t.key)
        appStore.showInfo?.(`已切换设计方向 · ${t.name}`)
      }
    }))
  )

  return entries
})

const filteredEntries = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return allEntries.value.slice(0, 18)
  return allEntries.value
    .filter(
      (item) =>
        item.title.toLowerCase().includes(q) ||
        item.group.toLowerCase().includes(q) ||
        item.section.toLowerCase().includes(q) ||
        item.keywords.toLowerCase().includes(q)
    )
    .slice(0, 18)
})

watch(query, () => {
  selectedIndex.value = 0
})

watch(cmdkOpen, (open) => {
  if (open) {
    query.value = ''
    selectedIndex.value = 0
    void nextTick(() => {
      inputRef.value?.focus()
    })
  }
})

function close() {
  closeCommandPalette()
  query.value = ''
}

function executeItem(item: CommandEntry) {
  close()
  item.run()
}

function handleInputKeydown(e: KeyboardEvent) {
  const len = filteredEntries.value.length
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (len > 0) selectedIndex.value = (selectedIndex.value + 1) % len
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (len > 0) selectedIndex.value = (selectedIndex.value - 1 + len) % len
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const item = filteredEntries.value[selectedIndex.value]
    if (item) executeItem(item)
  } else if (e.key === 'Escape') {
    e.preventDefault()
    close()
  }
}

function handleGlobalKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    cmdkOpen.value = !cmdkOpen.value
  } else if (e.key === 'Escape' && cmdkOpen.value) {
    e.preventDefault()
    close()
  }
}

function handleOpenEvent() {
  cmdkOpen.value = true
}

onMounted(() => {
  window.addEventListener('keydown', handleGlobalKeydown)
  window.addEventListener('omnihive:open-cmdk', handleOpenEvent)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
  window.removeEventListener('omnihive:open-cmdk', handleOpenEvent)
})
</script>

<style scoped>
.cmdk-backdrop {
  position: fixed;
  inset: 0;
  z-index: 95;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 13vh;
  background: rgba(10, 10, 14, 0.45);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}

.cmd-card {
  width: min(640px, 92vw);
  background: var(--popover);
  color: var(--popover-foreground);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.28);
  overflow: hidden;
}

.cmd-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 16px;
  border-bottom: 1px solid var(--border);
}

.cmd-header input {
  width: 100%;
  border: none;
  background: transparent;
  padding: 15px 4px;
  font-size: 14.5px;
  color: var(--foreground);
  outline: none;
}

.cmd-list {
  max-height: 400px;
  overflow-y: auto;
  padding: 6px 0;
}

.cmd-section-title {
  padding: 8px 16px 4px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.08em;
  color: var(--muted-foreground);
  opacity: 0.78;
}

.cmd-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 16px;
  cursor: pointer;
  font-size: 13.5px;
  color: var(--foreground);
  transition: background-color 0.12s ease;
}

.cmd-item:hover,
.cmd-item.sel {
  background: var(--secondary);
}

.cmd-badge {
  min-width: 30px;
  padding: 1.5px 5px;
  border-radius: 5px;
  border: 1px solid var(--border);
  background: var(--surface-2);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  text-align: center;
  color: var(--brand-text);
  flex-shrink: 0;
}

.cmd-hint {
  font-family: var(--font-mono);
  font-size: 10.5px;
  padding: 1px 6px;
  border-radius: 9999px;
  background: color-mix(in srgb, var(--brand) 10%, transparent);
  color: var(--brand-text);
  white-space: nowrap;
}

.cmd-sub {
  margin-left: auto;
  font-size: 11px;
  opacity: 0.55;
  font-family: var(--font-mono);
  white-space: nowrap;
}

.cmd-footer {
  padding: 9px 16px;
  font-size: 11.5px;
  opacity: 0.6;
  border-top: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: space-between;
}
</style>
