<template>
  <div>
    <!-- 铃铛按钮 -->
    <button
      @click="openModal"
      class="relative flex h-8 w-8 items-center justify-center rounded-lg transition-colors hover:bg-[var(--secondary)]"
      :style="unreadCount > 0 ? 'color: var(--brand-text)' : 'color: var(--foreground)'"
      :aria-label="t('announcements.title')"
    >
      <Icon name="bell" size="sm" />
      <!-- 未读脉冲点 -->
      <span
        v-if="unreadCount > 0"
        class="absolute right-1 top-1 flex h-2 w-2"
      >
        <span class="pulse-dot pulse-warn !h-2 !w-2"></span>
      </span>
    </button>

    <!-- 公告列表 Modal (OmniHive / OpenRouter Inbox 风格) -->
    <Teleport to="body">
      <Transition name="modal-fade">
        <div
          v-if="isModalOpen"
          class="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-black/45 p-4 pt-[8vh] backdrop-blur-sm"
          @click="closeModal"
        >
          <div
            class="w-full max-w-[580px] overflow-hidden rounded-2xl border shadow-2xl"
            style="background: var(--popover); color: var(--popover-foreground); border-color: var(--border)"
            @click.stop
          >
            <!-- Clean Hairline Header -->
            <div
              class="flex items-center justify-between border-b px-5 py-3.5"
              style="border-color: var(--border); background: var(--card)"
            >
              <div class="flex items-center gap-2.5">
                <span
                  class="flex h-7 w-7 items-center justify-center rounded-lg border"
                  style="border-color: var(--border); background: var(--surface-2); color: var(--brand-text)"
                >
                  <Icon name="bell" size="sm" />
                </span>
                <div>
                  <h2 class="text-sm font-semibold tracking-tight">
                    {{ t('announcements.title') }}
                  </h2>
                  <p v-if="unreadCount > 0" class="font-mono text-[11px] opacity-65">
                    <span class="font-semibold" style="color: var(--brand-text)">{{ unreadCount }}</span>
                    {{ t('announcements.unread') }}
                  </p>
                </div>
              </div>
              <div class="flex items-center gap-2">
                <button
                  v-if="unreadCount > 0"
                  @click="markAllAsRead"
                  :disabled="loading"
                  class="btn btn-secondary btn-sm"
                >
                  {{ t('announcements.markAllRead') }}
                </button>
                <button
                  @click="closeModal"
                  class="btn-ghost btn-icon rounded-lg"
                  :aria-label="t('common.close')"
                >
                  <Icon name="x" size="sm" />
                </button>
              </div>
            </div>

            <!-- Body -->
            <div class="max-h-[65vh] overflow-y-auto">
              <!-- Loading -->
              <div v-if="loading" class="flex items-center justify-center py-16">
                <div class="spinner" style="color: var(--brand)"></div>
              </div>

              <!-- Announcements List -->
              <div v-else-if="announcements.length > 0">
                <div
                  v-for="item in announcements"
                  :key="item.id"
                  class="group relative flex cursor-pointer items-center gap-3.5 border-b px-5 py-3.5 transition-colors hover:bg-[var(--secondary)]"
                  :style="{
                    borderColor: 'var(--border)',
                    background: !item.read_at ? 'color-mix(in srgb, var(--brand) 6%, var(--card))' : 'transparent'
                  }"
                  @click="openDetail(item)"
                >
                  <!-- Status Indicator -->
                  <div class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg border" style="border-color: var(--border); background: var(--surface-2)">
                    <span v-if="!item.read_at" class="pulse-dot pulse-warn"></span>
                    <svg
                      v-else
                      class="h-4 w-4 opacity-45"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                  </div>

                  <!-- Content -->
                  <div class="flex min-w-0 flex-1 items-center justify-between gap-4">
                    <div class="min-w-0 flex-1">
                      <h3 class="truncate text-sm font-medium">
                        {{ item.title }}
                      </h3>
                      <div class="mt-0.5 flex items-center gap-2">
                        <time class="font-mono text-[11px] opacity-55">
                          {{ formatRelativeTime(item.created_at) }}
                        </time>
                        <span
                          v-if="!item.read_at"
                          class="pill"
                          style="background: color-mix(in srgb, var(--brand) 14%, transparent); color: var(--brand-text)"
                        >
                          {{ t('announcements.unread') }}
                        </span>
                      </div>
                    </div>

                    <!-- Arrow -->
                    <div class="flex-shrink-0">
                      <svg
                        class="h-4 w-4 opacity-40 transition-transform group-hover:translate-x-0.5"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
                      </svg>
                    </div>
                  </div>

                  <!-- Unread indicator bar -->
                  <div
                    v-if="!item.read_at"
                    class="absolute left-0 top-2 bottom-2 w-0.5 rounded-r"
                    style="background: var(--brand)"
                  ></div>
                </div>
              </div>

              <!-- Data-Shape Empty State -->
              <div v-else class="px-6 py-12 text-center">
                <div class="mx-auto mb-4 max-w-xs space-y-2 opacity-40" aria-hidden="true">
                  <div class="h-2.5 w-3/4 mx-auto rounded" style="background: var(--border)"></div>
                  <div class="h-2 w-1/2 mx-auto rounded" style="background: var(--border)"></div>
                </div>
                <p class="text-sm font-medium">{{ t('announcements.empty') }}</p>
                <p class="mt-1 text-xs opacity-60">{{ t('announcements.emptyDescription') }}</p>
              </div>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- 公告详情 Modal -->
    <Teleport to="body">
      <Transition name="modal-fade">
        <div
          v-if="detailModalOpen && selectedAnnouncement"
          class="fixed inset-0 z-[110] flex items-start justify-center overflow-y-auto bg-black/50 p-4 pt-[6vh] backdrop-blur-sm"
          @click="closeDetail"
        >
          <div
            class="w-full max-w-[680px] overflow-hidden rounded-2xl border shadow-2xl"
            style="background: var(--popover); color: var(--popover-foreground); border-color: var(--border)"
            @click.stop
          >
            <!-- Header -->
            <div class="flex items-start justify-between gap-4 border-b px-6 py-5" style="border-color: var(--border); background: var(--card)">
              <div class="min-w-0 flex-1">
                <div class="mb-2 flex items-center gap-2">
                  <span class="pill pill-ghost font-mono uppercase">{{ t('announcements.title') }}</span>
                  <span
                    v-if="!selectedAnnouncement.read_at"
                    class="pill"
                    style="background: color-mix(in srgb, var(--brand) 15%, transparent); color: var(--brand-text)"
                  >
                    {{ t('announcements.unread') }}
                  </span>
                </div>
                <h2 class="text-lg font-bold leading-snug tracking-tight">
                  {{ selectedAnnouncement.title }}
                </h2>
                <div class="mt-2 flex items-center gap-4 font-mono text-xs opacity-60">
                  <time>{{ formatRelativeWithDateTime(selectedAnnouncement.created_at) }}</time>
                  <span>·</span>
                  <span>{{ selectedAnnouncement.read_at ? t('announcements.read') : t('announcements.unread') }}</span>
                </div>
              </div>
              <button
                @click="closeDetail"
                class="btn-ghost btn-icon shrink-0 rounded-lg"
                :aria-label="t('common.close')"
              >
                <Icon name="x" size="sm" />
              </button>
            </div>

            <!-- Body -->
            <div class="max-h-[60vh] overflow-y-auto px-6 py-6" style="background: var(--card)">
              <div
                class="markdown-body prose prose-sm max-w-none dark:prose-invert"
                v-html="renderMarkdown(selectedAnnouncement.content)"
              ></div>
            </div>

            <!-- Footer -->
            <div
              class="flex items-center justify-between border-t px-6 py-3.5"
              style="border-color: var(--border); background: var(--surface-2)"
            >
              <span class="text-xs opacity-60">
                {{ selectedAnnouncement.read_at ? t('announcements.readStatus') : t('announcements.markReadHint') }}
              </span>
              <div class="flex items-center gap-2">
                <button
                  @click="closeDetail"
                  class="btn btn-secondary btn-sm"
                >
                  {{ t('common.close') }}
                </button>
                <button
                  v-if="!selectedAnnouncement.read_at"
                  @click="markAsReadAndClose(selectedAnnouncement.id)"
                  class="btn btn-primary btn-sm"
                >
                  {{ t('announcements.markRead') }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { storeToRefs } from 'pinia'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAppStore } from '@/stores/app'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeTime, formatRelativeWithDateTime } from '@/utils/format'
import type { UserAnnouncement } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import '@/styles/announcement-markdown.css'

const { t } = useI18n()
const appStore = useAppStore()
const announcementStore = useAnnouncementStore()

// Configure marked
marked.setOptions({
  breaks: true,
  gfm: true,
})

// Use store state (storeToRefs for reactivity)
const { announcements, loading } = storeToRefs(announcementStore)
const unreadCount = computed(() => announcementStore.unreadCount)

// Local modal state
const isModalOpen = ref(false)
const detailModalOpen = ref(false)
const selectedAnnouncement = ref<UserAnnouncement | null>(null)

// Methods
function renderMarkdown(content: string): string {
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
}

function openModal() {
  isModalOpen.value = true
}

function closeModal() {
  isModalOpen.value = false
}

function openDetail(announcement: UserAnnouncement) {
  selectedAnnouncement.value = announcement
  detailModalOpen.value = true
  if (!announcement.read_at) {
    markAsRead(announcement.id)
  }
}

function closeDetail() {
  detailModalOpen.value = false
  selectedAnnouncement.value = null
}

async function markAsRead(id: number) {
  try {
    await announcementStore.markAsRead(id)
  } catch (err: any) {
    appStore.showError(err?.message || t('common.unknownError'))
  }
}

async function markAsReadAndClose(id: number) {
  await markAsRead(id)
  appStore.showSuccess(t('announcements.markedAsRead'))
  closeDetail()
}

async function markAllAsRead() {
  try {
    await announcementStore.markAllAsRead()
    appStore.showSuccess(t('announcements.allMarkedAsRead'))
  } catch (err: any) {
    appStore.showError(err?.message || t('common.unknownError'))
  }
}

function handleEscape(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (detailModalOpen.value) {
      closeDetail()
    } else if (isModalOpen.value) {
      closeModal()
    }
  }
}

onMounted(() => {
  document.addEventListener('keydown', handleEscape)
})

onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleEscape)
  document.body.style.overflow = ''
})

watch(
  [isModalOpen, detailModalOpen, () => announcementStore.currentPopup],
  ([modal, detail, popup]) => {
    document.body.style.overflow = (modal || detail || popup) ? 'hidden' : ''
  }
)
</script>

<style scoped>
/* Modal Animations */
.modal-fade-enter-active {
  transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

.modal-fade-leave-active {
  transition: all 0.2s cubic-bezier(0.4, 0, 1, 1);
}

.modal-fade-enter-from,
.modal-fade-leave-to {
  opacity: 0;
}

.modal-fade-enter-from > div {
  transform: scale(0.94) translateY(-12px);
  opacity: 0;
}

.modal-fade-leave-to > div {
  transform: scale(0.96) translateY(-8px);
  opacity: 0;
}

/* Scrollbar Styling */
.overflow-y-auto::-webkit-scrollbar {
  width: 8px;
}

.overflow-y-auto::-webkit-scrollbar-track {
  background: transparent;
}

.overflow-y-auto::-webkit-scrollbar-thumb {
  background: linear-gradient(to bottom, #cbd5e1, #94a3b8);
  border-radius: 4px;
}

.dark .overflow-y-auto::-webkit-scrollbar-thumb {
  background: linear-gradient(to bottom, #4b5563, #374151);
}

.overflow-y-auto::-webkit-scrollbar-thumb:hover {
  background: linear-gradient(to bottom, #94a3b8, #64748b);
}

.dark .overflow-y-auto::-webkit-scrollbar-thumb:hover {
  background: linear-gradient(to bottom, #6b7280, #4b5563);
}
</style>
