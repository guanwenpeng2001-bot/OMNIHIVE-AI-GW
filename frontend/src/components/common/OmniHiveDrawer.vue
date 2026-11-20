<template>
  <Teleport to="body">
    <!-- 全局右侧抽屉遮罩 -->
    <div
      id="drawerMask"
      class="oh-drawer-mask"
      :class="{ open: drawerState.open }"
      @click="closeDrawer"
    ></div>

    <!-- 全局右侧抽屉 (#drawer) -->
    <aside
      id="drawer"
      class="oh-drawer"
      :class="{ open: drawerState.open }"
      role="dialog"
      aria-modal="true"
      aria-labelledby="drawerTitle"
    >
      <div
        class="flex items-center justify-between border-b border-oh-default p-4"
      >
        <b id="drawerTitle" class="text-sm font-semibold">{{ drawerState.title }}</b>
        <button
          type="button"
          class="btn btn-ghost btn-icon btn-sm rounded-md"
          :aria-label="isZh ? '关闭详情' : 'Close details'"
          @click="closeDrawer"
        >
          ✕
        </button>
      </div>
      <div
        id="drawerBody"
        class="flex-1 overflow-y-auto p-5 text-sm"
        @click="handleDrawerBodyClick"
        v-html="sanitizedBodyHtml"
      ></div>
      <div
        id="drawerFoot"
        class="flex justify-end gap-2 border-t border-oh-default p-4"
      >
        <div v-if="drawerState.footerHtml" v-html="sanitizedFooterHtml" @click="handleDrawerBodyClick"></div>
        <button
          v-else
          type="button"
          class="btn btn-secondary btn-sm"
          @click="closeDrawer"
        >
          {{ isZh ? '关闭' : 'Close' }}
        </button>
      </div>
    </aside>

    <!-- 设计稿规格标注浮动面板 (Spec Mode Panel) -->
    <div
      v-if="isSpecMode"
      class="spec-panel card p-3.5 text-xs shadow-xl"
      role="region"
      :aria-label="isZh ? '设计稿规格标注面板' : 'Design system spec panel'"
    >
      <div class="mb-2 flex items-center justify-between">
        <span class="font-semibold text-oh-accent-text">
          📐 {{ isZh ? '设计系统规格 (SPEC)' : 'Design System Spec' }}
        </span>
        <button
          type="button"
          class="text-xs opacity-60 hover:opacity-100"
          @click="toggleSpecMode"
        >
          {{ isZh ? '关闭 ✕' : 'Close ✕' }}
        </button>
      </div>
      <div class="spec-row"><span>{{ isZh ? '当前方向' : 'Direction' }}</span><b>{{ currentThemeMeta.shortName }}</b></div>
      <div class="spec-row"><span>{{ isZh ? '明暗模式' : 'Theme Mode' }}</span><b>{{ isDark ? 'Dark (#0E1015)' : 'Light (#F7F8FA)' }}</b></div>
      <div class="spec-row"><span>{{ isZh ? '信息密度' : 'Density' }}</span><b>{{ isDense ? (isZh ? 'Compact (紧凑)' : 'Compact') : (isZh ? 'Comfort (舒适)' : 'Comfort') }}</b></div>
      <div class="spec-row"><span>{{ isZh ? '圆角 Token' : 'Radius Token' }}</span><b>{{ currentThemeMeta.radius }}</b></div>
      <div class="spec-row"><span>{{ isZh ? '主品牌色' : 'Brand Accent' }}</span><b>{{ currentThemeMeta.c2 }}</b></div>
      <div class="spec-row"><span>{{ isZh ? '最大容器宽' : 'Max Container' }}</span><b>Public 1560 / Console 1600</b></div>
      <div class="mt-2 text-[11px] opacity-60">
        {{
          isZh
            ? '双击任意表格行可呼出右侧「行详情」抽屉；按 ⌘K 唤起全局命令面板。'
            : 'Double-click any table row to inspect row details; press ⌘K for Command Palette.'
        }}
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import DOMPurify from 'dompurify'
import { i18n as globalI18n } from '@/i18n'
import { useAppStore } from '@/stores'
import { useThemeDirection } from '@/composables/useThemeDirection'

const router = useRouter()
const appStore = useAppStore()

let i18nLocale: { value?: string } | string | undefined
try {
  const composer = useI18n()
  i18nLocale = composer?.locale
} catch {
  i18nLocale = undefined
}

const isZh = computed(() => {
  const raw =
    (typeof i18nLocale === 'string' ? i18nLocale : i18nLocale?.value) ||
    globalI18n?.global?.locale?.value ||
    (typeof document !== 'undefined' ? document.documentElement?.lang : '') ||
    'zh'
  return String(raw).toLowerCase().startsWith('zh')
})

const {
  drawerState,
  closeDrawer,
  openDrawer,
  isSpecMode,
  toggleSpecMode,
  currentThemeMeta,
  isDark,
  isDense
} = useThemeDirection()

const DRAWER_SANITIZE_CONFIG = {
  ADD_ATTR: ['data-drawer-close', 'data-drawer-copy', 'data-drawer-nav', 'target', 'rel', 'aria-label', 'role']
}
const sanitizedBodyHtml = computed(() => DOMPurify.sanitize(drawerState.value.html || '', DRAWER_SANITIZE_CONFIG))
const sanitizedFooterHtml = computed(() => DOMPurify.sanitize(drawerState.value.footerHtml || '', DRAWER_SANITIZE_CONFIG))

function escHtml(v: unknown): string {
  return String(v ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
  }[c] || c))
}

function handleDrawerBodyClick(e: MouseEvent) {
  const target = (e.target as HTMLElement | null)?.closest('[data-drawer-action],[data-drawer-nav],[data-drawer-copy]') as HTMLElement | null
  if (!target) return
  const nav = target.getAttribute('data-drawer-nav')
  if (nav) {
    closeDrawer()
    void router.push(nav)
    return
  }
  const copyText = target.getAttribute('data-drawer-copy')
  if (copyText) {
    navigator.clipboard?.writeText(copyText).then(() => {
      appStore.showSuccess?.(isZh.value ? '已复制到剪贴板' : 'Copied to clipboard')
    }).catch(() => {
      appStore.showWarning?.(isZh.value ? '复制失败，请手动复制' : 'Copy failed, please copy manually')
    })
    return
  }
  const action = target.getAttribute('data-drawer-action')
  if (action === 'close') {
    closeDrawer()
  }
}

// 全局表格双击打开「行详情」抽屉
function handleGlobalDblClick(e: MouseEvent) {
  const target = e.target as HTMLElement | null
  if (!target || target.closest('button,input,label,a,select,textarea')) return
  const row = target.closest('tbody tr') as HTMLTableRowElement | null
  if (!row) return
  const table = row.closest('table')
  if (!table) return
  const cells = [...row.querySelectorAll('td')].map((td) => (td.textContent || '').trim())
  if (cells.length <= 1) return
  const heads = [...table.querySelectorAll('thead th')].map((th) => (th.textContent || '').trim())
  const fallbackRecord = isZh.value ? '记录' : 'Record'
  const fieldPrefix = isZh.value ? '字段 ' : 'Field '
  const tipText = isZh.value
    ? '提示：双击任意表格行可快速检视完整字段详情。'
    : 'Tip: Double-click any table row to inspect full field values.'
  const titleHint = cells.find((c) => c && c !== '—' && c.length < 48) || fallbackRecord
  const rowsHtml = heads
    .map((h, i) =>
      cells[i] !== undefined
        ? `<div class="flex justify-between gap-4 border-b pb-2" style="border-color:var(--oh-border-subtle)"><span class="opacity-60">${escHtml(h || fieldPrefix + (i + 1))}</span><b class="text-right font-mono break-all">${escHtml(cells[i] || '—')}</b></div>`
        : ''
    )
    .join('')
  openDrawer(
    isZh.value ? `行详情 · ${titleHint}` : `Row Details · ${titleHint}`,
    `<div class="space-y-2.5 text-sm">${rowsHtml}<div class="text-xs opacity-50 pt-2">${escHtml(tipText)}</div></div>`
  )
}

function handleGlobalKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && drawerState.value.open) {
    closeDrawer()
  }
}

onMounted(() => {
  document.addEventListener('dblclick', handleGlobalDblClick)
  window.addEventListener('keydown', handleGlobalKeydown)
  if (typeof window !== 'undefined') {
    const win = window as unknown as Record<string, unknown>
    win.openOmniHiveDrawer = openDrawer
    win.closeOmniHiveDrawer = closeDrawer
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('dblclick', handleGlobalDblClick)
  window.removeEventListener('keydown', handleGlobalKeydown)
})
</script>

<style scoped>
.oh-drawer-mask {
  position: fixed;
  inset: 0;
  z-index: 88;
  background: var(--oh-backdrop);
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.28s cubic-bezier(0.2, 0.8, 0.2, 1);
}
.oh-drawer-mask.open {
  opacity: 1;
  pointer-events: auto;
}
.oh-drawer {
  position: fixed;
  top: 0;
  right: 0;
  height: 100vh;
  width: min(460px, 94vw);
  z-index: 89;
  background: var(--oh-bg-surface);
  color: var(--oh-fg-primary);
  border-left: 1px solid var(--oh-border-default);
  box-shadow: var(--oh-shadow-xl);
  transform: translateX(105%);
  transition: transform 0.34s cubic-bezier(0.2, 0.8, 0.2, 1);
  display: flex;
  flex-direction: column;
}
.oh-drawer.open {
  transform: none;
}
.spec-panel {
  position: fixed;
  left: 16px;
  bottom: 16px;
  z-index: 85;
  width: 280px;
  max-height: 70vh;
  overflow: auto;
  border-color: var(--oh-accent-border);
}
.spec-row {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  padding: 4px 0;
  border-bottom: 1px solid var(--oh-border-subtle);
  font-family: var(--oh-font-mono);
  font-size: 11px;
}
.spec-row b {
  color: var(--oh-accent-text);
}
</style>
