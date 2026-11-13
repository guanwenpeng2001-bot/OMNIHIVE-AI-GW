<template>
  <Teleport to="body">
    <!-- 右下角轻量悬浮外观胶囊（默认收起，绝不遮挡底部保存/分页操作栏） -->
    <div v-if="floating" id="dock" class="dock-root" aria-label="OmniHive 外观与主题切换">
      <div id="dockPanel" class="dock-panel" :class="{ 'is-collapsed': dockCollapsed }">
        <div v-show="!dockCollapsed" class="flex items-center gap-1.5">
          <span class="mr-1 hidden font-mono text-[10.5px] uppercase tracking-wider opacity-55 sm:inline">Theme</span>
          <button
            v-for="t in themes"
            :key="t.key"
            type="button"
            class="dock-dot"
            :class="{ active: currentTheme === t.key }"
            :title="t.name"
            :aria-label="`切换主题 ${t.name}`"
            :style="{ background: t.c2 }"
            @click="selectTheme(t.key)"
          ></button>
          <span class="mx-0.5 h-4 w-px" style="background: var(--border)"></span>
          <button
            type="button"
            class="dock-action-btn"
            title="选择设计方向与外观"
            aria-label="打开主题选择面板"
            @click="themePickerOpen = true"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true">
              <circle cx="13.5" cy="6.5" r="1" fill="currentColor" />
              <circle cx="17.5" cy="10.5" r="1" fill="currentColor" />
              <circle cx="8.5" cy="7.5" r="1" fill="currentColor" />
              <circle cx="6.5" cy="12.5" r="1" fill="currentColor" />
              <path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.93 0 1.65-.75 1.65-1.69 0-.43-.18-.84-.44-1.13-.29-.29-.44-.65-.44-1.12a1.64 1.64 0 0 1 1.67-1.67h2c3.05 0 5.56-2.5 5.56-5.55C21.97 5.95 17.46 2 12 2Z" />
            </svg>
          </button>
          <button
            type="button"
            class="dock-action-btn"
            :title="isDark ? '切换为浅色模式' : '切换为深色模式'"
            aria-label="切换深浅色"
            @click="toggleDarkMode()"
          >
            <svg v-if="isDark" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true">
              <circle cx="12" cy="12" r="4" />
              <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
            </svg>
            <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true">
              <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
            </svg>
          </button>
          <button
            type="button"
            class="dock-action-btn"
            :class="{ 'dock-action-active': isDense }"
            title="切换信息密度（舒适 / 紧凑）"
            aria-label="切换信息密度"
            @click="toggleDenseMode()"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true">
              <path d="M3 6h18M3 10h18M3 14h18M3 18h18" />
            </svg>
          </button>
          <button
            type="button"
            class="dock-action-btn"
            title="打开全局命令面板 (⌘K)"
            aria-label="打开全局命令面板"
            @click="openCommandPalette()"
          >
            ⌘K
          </button>
        </div>
        <button
          type="button"
          class="dock-action-btn"
          :title="dockCollapsed ? `展开外观切换坞 (${currentThemeMeta.shortName})` : '收起外观切换坞'"
          :aria-label="dockCollapsed ? '展开外观切换坞' : '收起外观切换坞'"
          @click="dockCollapsed = !dockCollapsed"
        >
          <span v-if="dockCollapsed" class="inline-flex items-center gap-1.5 px-0.5">
            <span
              class="inline-block h-2.5 w-2.5 rounded-full"
              :style="{ background: currentThemeMeta.c2 }"
            ></span>
            <span class="hidden font-mono text-[10.5px] opacity-75 sm:inline">{{ currentThemeMeta.shortName }}</span>
          </span>
          <span v-else class="text-[11px] opacity-60">›</span>
        </button>
      </div>
    </div>

    <!-- 主题选择弹窗 -->
    <div
      v-if="themePickerOpen"
      class="theme-modal-mask"
      role="dialog"
      aria-modal="true"
      aria-label="选择设计方向与外观"
      @click.self="closeThemePickerModal()"
    >
      <div class="theme-modal-card">
        <div class="mb-1 flex items-center justify-between">
          <h3 class="text-lg font-semibold">选择设计方向</h3>
          <button
            type="button"
            class="dock-action-btn"
            aria-label="关闭主题选择"
            @click="closeThemePickerModal()"
          >
            ✕
          </button>
        </div>
        <p class="mb-4 text-xs opacity-65">
          OmniHive 内置 5 套设计方向，切换后实时应用到全站并自动记住你的偏好。
        </p>

        <div class="grid gap-2.5 sm:grid-cols-2">
          <button
            v-for="t in themes"
            :key="t.key"
            type="button"
            class="card p-3 text-left transition-all"
            :class="{ 'ring-2': currentTheme === t.key }"
            :style="currentTheme === t.key ? { borderColor: 'var(--brand)', '--tw-ring-color': 'var(--brand)' } : {}"
            @click="selectTheme(t.key)"
          >
            <div class="mb-2 flex items-center gap-2">
              <span class="h-3.5 w-3.5 rounded-full" :style="{ background: t.c2 }"></span>
              <b class="text-sm">{{ t.shortName }}</b>
              <span
                v-if="currentTheme === t.key"
                class="ml-auto text-xs font-semibold"
                style="color: var(--brand-text)"
              >
                ✓ 当前
              </span>
            </div>
            <div class="mb-2 flex gap-1">
              <span
                v-for="c in [t.c1, t.c2, t.c3]"
                :key="c"
                class="h-4 flex-1 rounded-sm border"
                :style="{ background: c, borderColor: 'var(--border)' }"
              ></span>
            </div>
            <div class="text-xs leading-snug opacity-65">{{ t.desc }}</div>
            <div class="mt-1 font-mono text-[10px] opacity-45">radius {{ t.radius }}</div>
          </button>
        </div>

        <div
          class="mt-4 space-y-2.5 border-t pt-3 text-xs"
          style="border-color: var(--border)"
        >
          <div class="flex items-center justify-between">
            <span class="opacity-70">深浅外观模式</span>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              @click="toggleDarkMode()"
            >
              {{ isDark ? '当前：深色模式' : '当前：浅色模式' }}
            </button>
          </div>
          <div class="flex items-center justify-between">
            <span class="opacity-70">工作台信息密度</span>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              @click="toggleDenseMode()"
            >
              {{ isDense ? '当前：紧凑高密度' : '当前：舒适留白' }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useThemeDirection, type ThemeDirectionKey } from '@/composables/useThemeDirection'

withDefaults(defineProps<{ floating?: boolean }>(), {
  floating: true
})

const {
  themes,
  currentTheme,
  currentThemeMeta,
  isDark,
  isDense,
  themePickerOpen,
  setThemeDirection,
  toggleDarkMode,
  toggleDenseMode,
  openCommandPalette,
  closeThemePickerModal
} = useThemeDirection()

const dockCollapsed = ref(true)

function selectTheme(key: ThemeDirectionKey) {
  setThemeDirection(key)
}

function handleOpenModalEvent() {
  themePickerOpen.value = true
}

onMounted(() => {
  window.addEventListener('omnihive:open-theme-picker', handleOpenModalEvent)
})

onBeforeUnmount(() => {
  window.removeEventListener('omnihive:open-theme-picker', handleOpenModalEvent)
})
</script>

<style scoped>
.dock-root {
  position: fixed;
  bottom: 14px;
  right: 14px;
  z-index: 45;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
  pointer-events: none;
}

.dock-panel {
  pointer-events: auto;
  display: flex;
  align-items: center;
  gap: 6px;
  border-radius: 9999px;
  padding: 4px 10px;
  background: color-mix(in srgb, var(--card) 90%, transparent);
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
  box-shadow:
    0 10px 30px -6px rgba(0, 0, 0, 0.16),
    0 0 0 1px var(--border);
  color: var(--foreground);
}

.dock-panel.is-collapsed {
  padding: 3px 7px;
  opacity: 0.85;
  transition: opacity 0.18s ease, transform 0.18s ease;
}

.dock-panel.is-collapsed:hover {
  opacity: 1;
}

.dock-dot {
  width: 16px;
  height: 16px;
  border-radius: 9999px;
  border: 2px solid var(--border);
  cursor: pointer;
  transition: transform 0.15s ease;
}

.dock-dot:hover {
  transform: scale(1.2);
}

.dock-dot.active {
  border-color: var(--foreground);
  box-shadow: 0 0 0 2px var(--card);
}

.dock-action-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 26px;
  height: 26px;
  padding: 0 5px;
  border-radius: 7px;
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--foreground);
  background: transparent;
  transition: background-color 0.15s ease;
  cursor: pointer;
}

.dock-action-btn:hover,
.dock-action-active {
  background: var(--secondary);
}

.theme-modal-mask {
  position: fixed;
  inset: 0;
  z-index: 92;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  background: rgba(2, 6, 12, 0.5);
  backdrop-filter: blur(4px);
}

.theme-modal-card {
  width: 100%;
  max-width: 540px;
  border-radius: 16px;
  border: 1px solid var(--border);
  background: var(--card);
  color: var(--card-foreground);
  padding: 20px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.28);
}
</style>
