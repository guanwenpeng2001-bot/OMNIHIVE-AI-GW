<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, toRef, watch } from 'vue'
import type { UiCommandGroup, UiCommandItem } from './types'
import { cn, useOverlayDialog, useUiI18n, useUiId } from './utils'

export interface CommandPaletteProps {
  open: boolean
  groups: UiCommandGroup[]
  placeholder?: string
  emptyText?: string
  loading?: boolean
  registerShortcut?: boolean
  teleportDisabled?: boolean
}

const props = withDefaults(defineProps<CommandPaletteProps>(), {
  placeholder: undefined,
  emptyText: undefined,
  loading: false,
  registerShortcut: false,
  teleportDisabled: false
})

const { t } = useUiI18n()
const resolvedPlaceholder = computed(
  () => props.placeholder ?? t('ui.commandPalette.placeholder')
)
const resolvedEmptyText = computed(
  () => props.emptyText ?? t('ui.commandPalette.emptyText')
)

const emit = defineEmits<{
  'update:open': [open: boolean]
  select: [item: UiCommandItem]
  queryChange: [query: string]
}>()

const panelRef = ref<HTMLElement | null>(null)
const searchInputRef = ref<HTMLInputElement | null>(null)
const query = ref('')
const activeIndex = ref(0)
const listboxId = useUiId('oh-cmd-list')

function closePalette(): void {
  emit('update:open', false)
}

useOverlayDialog({
  open: toRef(props, 'open'),
  panelRef,
  initialFocusRef: searchInputRef,
  onClose: closePalette
})

const filteredGroups = computed(() => {
  const normalized = query.value.trim().toLowerCase()
  if (!normalized) return props.groups.filter((group) => group.items.length > 0)

  return props.groups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => {
        const haystack = [
          item.label,
          item.description ?? '',
          group.label,
          ...(item.keywords ?? [])
        ]
          .join(' ')
          .toLowerCase()
        return haystack.includes(normalized)
      })
    }))
    .filter((group) => group.items.length > 0)
})

const flatEnabledItems = computed(() =>
  filteredGroups.value.flatMap((group) => group.items.filter((item) => !item.disabled))
)

const activeItem = computed(() => flatEnabledItems.value[activeIndex.value] ?? null)

watch(query, (nextQuery) => {
  activeIndex.value = 0
  emit('queryChange', nextQuery)
})

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      query.value = ''
      activeIndex.value = 0
    }
  }
)

function activateItem(item: UiCommandItem): void {
  if (item.disabled) return
  item.onSelect?.()
  emit('select', item)
  closePalette()
}

function handleInputKeydown(event: KeyboardEvent): void {
  const total = flatEnabledItems.value.length
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    if (total > 0) {
      activeIndex.value = (activeIndex.value + 1) % total
    }
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    if (total > 0) {
      activeIndex.value = (activeIndex.value - 1 + total) % total
    }
  } else if (event.key === 'Enter') {
    if (activeItem.value) {
      event.preventDefault()
      activateItem(activeItem.value)
    }
  }
}

function handleGlobalShortcut(event: KeyboardEvent): void {
  if (!props.registerShortcut) return
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault()
    emit('update:open', !props.open)
  }
}

onMounted(() => {
  if (typeof window !== 'undefined') {
    window.addEventListener('keydown', handleGlobalShortcut)
  }
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') {
    window.removeEventListener('keydown', handleGlobalShortcut)
  }
})
</script>

<template>
  <Teleport to="body" :disabled="teleportDisabled">
    <div
      v-if="open"
      class="fixed inset-0 z-50 flex items-start justify-center p-4 pt-[12vh]"
    >
      <div
        class="fixed inset-0 bg-[var(--oh-backdrop)]"
        aria-hidden="true"
        @click="closePalette"
      />

      <div
        ref="panelRef"
        role="dialog"
        aria-modal="true"
        :aria-label="t('ui.commandPalette.ariaLabel')"
        tabindex="-1"
        class="relative z-10 flex max-h-[72vh] w-full max-w-xl flex-col overflow-hidden rounded-oh-xl border border-oh-default bg-oh-elevated text-oh-primary shadow-oh-xl focus:outline-none"
      >
        <div class="flex items-center gap-2.5 border-b border-oh-subtle px-4 py-3">
          <svg
            viewBox="0 0 18 18"
            class="h-4 w-4 shrink-0 text-oh-muted"
            fill="none"
            stroke="currentColor"
            stroke-width="1.75"
            aria-hidden="true"
          >
            <circle cx="8" cy="8" r="5.25" />
            <path stroke-linecap="round" d="M12 12l3.5 3.5" />
          </svg>

          <input
            ref="searchInputRef"
            v-model="query"
            type="search"
            role="combobox"
            aria-expanded="true"
            :aria-controls="listboxId"
            :aria-activedescendant="activeItem ? `${listboxId}-${activeItem.id}` : undefined"
            :placeholder="resolvedPlaceholder"
            class="w-full bg-transparent text-sm text-oh-primary placeholder:text-oh-muted focus:outline-none"
            @keydown="handleInputKeydown"
          />

          <kbd class="rounded-oh-xs border border-oh-default bg-oh-subtle px-1.5 py-0.5 font-mono text-[10px] text-oh-muted">
            ESC
          </kbd>
        </div>

        <div
          :id="listboxId"
          role="listbox"
          :aria-label="t('ui.commandPalette.availableCommands')"
          class="flex-1 overflow-y-auto p-2"
        >
          <div v-if="loading" class="px-3 py-6 text-center font-mono text-xs text-oh-muted">
            {{ t('ui.commandPalette.searching') }}
          </div>

          <div
            v-else-if="filteredGroups.length === 0"
            class="px-3 py-8 text-center text-xs text-oh-muted"
          >
            {{ resolvedEmptyText }}
          </div>

          <template v-else>
            <div
              v-for="group in filteredGroups"
              :key="group.id"
              class="mb-2 last:mb-0"
            >
            <p class="oh-eyebrow px-2.5 py-1.5">{{ group.label }}</p>
            <div class="space-y-0.5">
              <button
                v-for="item in group.items"
                :id="`${listboxId}-${item.id}`"
                :key="item.id"
                type="button"
                role="option"
                :disabled="item.disabled"
                :aria-selected="activeItem?.id === item.id ? 'true' : 'false'"
                :class="
                  cn(
                    'flex w-full items-center justify-between gap-3 rounded-oh-md px-2.5 py-2 text-left text-xs transition-colors duration-100',
                    activeItem?.id === item.id
                      ? 'bg-oh-selected text-oh-primary'
                      : 'text-oh-secondary hover:bg-oh-hover hover:text-oh-primary',
                    item.disabled && 'cursor-not-allowed opacity-45'
                  )
                "
                @mouseenter="
                  !item.disabled &&
                    (activeIndex = flatEnabledItems.findIndex((candidate) => candidate.id === item.id))
                "
                @click="activateItem(item)"
              >
                <span class="min-w-0 flex-1">
                  <span class="flex items-center gap-2">
                    <span class="truncate font-medium text-oh-primary">{{ item.label }}</span>
                    <span
                      v-if="item.badge"
                      class="rounded-oh-xs border border-oh-default bg-oh-subtle px-1.5 py-px font-mono text-[10px] text-oh-muted"
                    >
                      {{ item.badge }}
                    </span>
                  </span>
                  <span v-if="item.description" class="mt-0.5 block truncate text-[11px] text-oh-muted">
                    {{ item.description }}
                  </span>
                </span>

                <kbd
                  v-if="item.shortcut"
                  class="shrink-0 rounded-oh-xs border border-oh-default bg-oh-subtle px-1.5 py-0.5 font-mono text-[10px] text-oh-muted"
                >
                  {{ item.shortcut }}
                </kbd>
              </button>
            </div>
            </div>
          </template>
        </div>

        <footer class="flex items-center justify-between border-t border-oh-subtle bg-oh-subtle/60 px-4 py-2 text-[11px] text-oh-muted">
          <div class="flex items-center gap-3 font-mono">
            <span>{{ t('ui.commandPalette.navigateHint') }}</span>
            <span>{{ t('ui.commandPalette.selectHint') }}</span>
            <span>{{ t('ui.commandPalette.closeHint') }}</span>
          </div>
          <span class="font-mono text-[10px] uppercase tracking-wider">{{ 'OmniHive' }} Command</span>
        </footer>
      </div>
    </div>
  </Teleport>
</template>
