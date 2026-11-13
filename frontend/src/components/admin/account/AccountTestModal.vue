<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.testAccountConnection')"
    width="normal"
    @close="handleClose"
  >
    <div class="space-y-4">
      <!-- Account Info Card -->
      <div
        v-if="account"
        class="flex items-center justify-between rounded-xl border border-[var(--border)] bg-[color-mix(in_srgb,var(--surface-2)_60%,transparent)] p-3"
      >
        <div class="flex items-center gap-3">
          <div
            class="flex h-9 w-9 items-center justify-center rounded-lg border border-[var(--border)] bg-[var(--primary)] text-[var(--primary-foreground)]"
          >
            <Icon name="play" size="sm" :stroke-width="2" />
          </div>
          <div>
            <div class="text-sm font-semibold text-[var(--foreground)]">{{ account.name }}</div>
            <div class="flex items-center gap-1.5 text-xs text-[var(--muted-foreground)]">
              <span
                class="pill pill-ghost font-mono tabular-nums text-[10px] font-medium uppercase"
              >
                {{ account.type }}
              </span>
              <span>{{ t('admin.accounts.account') }}</span>
            </div>
          </div>
        </div>
        <span
          :class="[
            'rounded-full px-2.5 py-1 text-xs font-semibold',
            account.status === 'active'
              ? 'bg-green-100 text-green-700 dark:bg-green-500/20 dark:text-green-400'
              : 'bg-[var(--surface-2)] text-[var(--muted-foreground)]'
          ]"
        >
          {{ account.status }}
        </span>
      </div>

      <!-- Grok: mode first, then optional model / mode params -->
      <div v-if="isGrokAccount" class="space-y-1.5">
        <label class="text-sm font-medium text-[var(--foreground)]">
          {{ t('admin.accounts.grok.testMode') }}
        </label>
        <Select
          v-model="grokTestMode"
          :options="grokTestModeOptions"
          :disabled="status === 'connecting'"
        />
        <p class="text-xs text-[var(--muted-foreground)]">
          {{ t('admin.accounts.grok.testModeHint') }}
        </p>
      </div>

      <div v-if="showModelSelect" class="space-y-1.5">
        <label class="text-sm font-medium text-[var(--foreground)]">
          {{ t('admin.accounts.selectTestModel') }}
        </label>
        <Select
          v-model="selectedModelId"
          :options="modelOptionsForMode"
          :disabled="loadingModels || status === 'connecting'"
          value-key="id"
          label-key="display_name"
          :placeholder="loadingModels ? t('common.loading') + '...' : t('admin.accounts.selectTestModel')"
        />
        <p v-if="modelsFailed" role="status">{{ t('admin.accounts.modelCatalog.failed') }}
          <button type="button" @click="loadAvailableModels">{{ t('common.tryAgain') }}</button>
        </p>
        <p v-else-if="!loadingModels && !availableModels.length" role="status">{{ t('admin.accounts.modelCatalog.empty') }}</p>
        <p v-if="selectedModel?.catalog_source" class="mt-2 text-xs text-[var(--muted-foreground)]">
          {{ t('admin.accounts.catalogSourceLabel') }}: {{ t(`admin.accounts.catalogSource.${selectedModel.catalog_source}`) }}
          <span v-if="selectedModel.metadata_source"> · {{ t('admin.accounts.metadataSourceLabel') }}: {{ selectedModel.metadata_source }}</span>
        </p>
        <p v-if="probeUnsupported" class="mt-2 text-sm text-amber-600">{{ t('admin.accounts.nativeProbeUnsupported') }}</p>
      </div>

      <div v-if="isOpenAIAccount" class="space-y-1.5">
        <label class="text-sm font-medium text-[var(--foreground)]">
          {{ t('admin.accounts.openai.testMode') }}
        </label>
        <Select
          v-model="testMode"
          :options="openAITestModeOptions"
          :disabled="status === 'connecting'"
        />
      </div>

      <div v-if="supportsPromptInput" class="space-y-1.5">
        <TextArea
          v-model="testPrompt"
          :label="promptInputLabel"
          :placeholder="promptInputPlaceholder"
          :hint="promptInputHint"
          :disabled="status === 'connecting'"
          rows="3"
        />
      </div>
      <p
        v-else-if="isGrokAccount && promptInputHint"
        class="text-xs text-[var(--muted-foreground)]"
      >
        {{ promptInputHint }}
      </p>

      <!-- Optional media uploads for real generation / transcription -->
      <div v-if="supportsImageUpload" class="space-y-1.5">
        <label class="text-sm font-medium text-[var(--foreground)]">
          {{ imageUploadLabel }}
        </label>
        <div class="flex items-center gap-3">
          <button
            type="button"
            class="btn btn-secondary btn-sm shrink-0"
            :disabled="status === 'connecting'"
            @click="imageFileInput?.click()"
          >
            {{ t('admin.accounts.grok.chooseImageFile') }}
          </button>
          <span class="min-w-0 truncate text-xs text-[var(--muted-foreground)]">
            {{
              uploadImageName
                ? t('common.selectedFile', { name: uploadImageName })
                : t('common.noFileSelected')
            }}
          </span>
          <input
            ref="imageFileInput"
            type="file"
            accept="image/png,image/jpeg,image/webp,image/gif"
            class="hidden"
            :disabled="status === 'connecting'"
            @change="onImageFileChange"
          />
        </div>
        <p class="text-xs text-[var(--muted-foreground)]">{{ imageUploadHint }}</p>
        <div v-if="uploadImagePreview" class="overflow-hidden rounded-lg border border-[var(--border)]">
          <img
            :src="uploadImagePreview"
            :alt="t('admin.accounts.grok.uploadPreviewAlt')"
            class="max-h-40 w-full object-contain bg-[var(--surface-2)]"
          />
        </div>
      </div>

      <div v-if="supportsAudioUpload" class="space-y-1.5">
        <label class="text-sm font-medium text-[var(--foreground)]">
          {{ t('admin.accounts.grok.audioUploadLabel') }}
        </label>
        <div class="flex items-center gap-3">
          <button
            type="button"
            class="btn btn-secondary btn-sm shrink-0"
            :disabled="status === 'connecting'"
            @click="audioFileInput?.click()"
          >
            {{ t('admin.accounts.grok.chooseAudioFile') }}
          </button>
          <span class="min-w-0 truncate text-xs text-[var(--muted-foreground)]">
            {{
              uploadAudioName
                ? t('common.selectedFile', { name: uploadAudioName })
                : t('common.noFileSelected')
            }}
          </span>
          <input
            ref="audioFileInput"
            type="file"
            accept="audio/*,.wav,.mp3,.m4a,.ogg,.webm"
            class="hidden"
            :disabled="status === 'connecting'"
            @change="onAudioFileChange"
          />
        </div>
        <p class="text-xs text-[var(--muted-foreground)]">{{ t('admin.accounts.grok.audioUploadHint') }}</p>
      </div>

      <!-- Terminal Output -->
      <div class="group relative">
        <div
          ref="terminalRef"
          class="max-h-[240px] min-h-[120px] overflow-y-auto rounded-xl border border-[var(--border)] bg-[var(--surface-2)] p-4 font-mono tabular-nums text-sm"
        >
          <!-- Status Line -->
          <div v-if="status === 'idle'" class="flex items-center gap-2 text-[var(--muted-foreground)]">
            <Icon name="play" size="sm" :stroke-width="2" />
            <span>{{ t('admin.accounts.readyToTest') }}</span>
          </div>
          <div v-else-if="status === 'connecting'" class="flex items-center gap-2 text-[var(--warn-text)]">
            <Icon name="refresh" size="sm" class="animate-spin" :stroke-width="2" />
            <span>{{ t('admin.accounts.connectingToApi') }}</span>
          </div>

          <!-- Output Lines -->
          <div v-for="(line, index) in outputLines" :key="index" :class="line.class">
            {{ line.text }}
          </div>

          <!-- Streaming Content -->
          <div v-if="streamingContent" class="text-[var(--ok-text)]">
            {{ streamingContent }}<span class="animate-pulse">_</span>
          </div>

          <!-- Result Status -->
          <div
            v-if="status === 'success'"
            class="mt-3 flex items-center gap-2 border-t border-[var(--border)] pt-3 text-[var(--ok-text)]"
          >
            <Icon name="check" size="sm" :stroke-width="2" />
            <span>{{ t('admin.accounts.testCompleted') }}</span>
          </div>
          <div
            v-else-if="status === 'error'"
            class="mt-3 flex items-center gap-2 border-t border-[var(--border)] pt-3 text-[var(--bad-text)]"
          >
            <Icon name="x" size="sm" :stroke-width="2" />
            <span>{{ errorMessage }}</span>
          </div>
        </div>

        <!-- Copy Button -->
        <button
          v-if="outputLines.length > 0"
          @click="copyOutput"
          class="absolute right-2 top-2 rounded-lg border border-[var(--border)] bg-[var(--popover)] p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--secondary)] hover:text-[var(--foreground)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--ring)]"
          :title="t('admin.accounts.copyOutput')"
        >
          <Icon name="link" size="sm" :stroke-width="2" />
        </button>
      </div>

      <div v-if="generatedImages.length > 0" class="space-y-2">
        <div class="text-xs font-medium text-[var(--muted-foreground)]">
          {{ t('admin.accounts.imagePreview') }}
        </div>
        <div class="flex flex-wrap justify-center gap-3">
          <div
            v-for="(image, index) in generatedImages"
            :key="`${image.url}-${index}`"
            class="group/img relative cursor-pointer overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--card)] shadow-sm transition hover:border-[var(--brand)] hover:shadow-md"
            @click="previewImageUrl = image.url"
          >
            <img
              :src="image.url"
              :alt="t('admin.accounts.imagePreviewAlt', { index: index + 1 })"
              class="max-h-[360px] w-full object-contain"
            />
            <div class="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors group-hover/img:bg-black/20">
              <Icon name="eye" size="lg" class="text-white opacity-0 drop-shadow-lg transition-opacity group-hover/img:opacity-100" :stroke-width="2" />
            </div>
            <div class="border-t border-[var(--border)] px-3 py-1.5 text-xs text-[var(--muted-foreground)]">
              {{ image.mimeType || 'image/*' }}
            </div>
          </div>
        </div>
      </div>

      <div v-if="generatedAudios.length > 0" class="space-y-2">
        <div class="text-xs font-medium text-[var(--muted-foreground)]">
          {{ t('admin.accounts.audioPreview') }}
        </div>
        <div
          v-for="(audio, index) in generatedAudios"
          :key="`audio-${index}`"
          class="rounded-xl border border-[var(--border)] bg-[var(--card)] p-3"
        >
          <audio :src="audio.url" controls class="w-full" :type="audio.mimeType" />
          <div class="mt-1 text-xs text-[var(--muted-foreground)]">{{ audio.mimeType || 'audio/*' }}</div>
        </div>
      </div>

      <div v-if="generatedVideos.length > 0" class="space-y-2">
        <div class="text-xs font-medium text-[var(--muted-foreground)]">
          {{ t('admin.accounts.videoPreview') }}
        </div>
        <div
          v-for="(video, index) in generatedVideos"
          :key="`video-${index}`"
          class="overflow-hidden rounded-xl border border-[var(--border)] bg-black"
        >
          <video :src="video.url" controls class="max-h-[360px] w-full" :type="video.mimeType" />
          <div class="border-t border-[var(--border)] bg-[var(--card)] px-3 py-1.5 text-xs text-[var(--muted-foreground)]">
            {{ video.mimeType || 'video/*' }}
          </div>
        </div>
      </div>

      <!-- Image Lightbox -->
      <Teleport to="body">
        <Transition name="fade">
          <div
            v-if="previewImageUrl"
            class="fixed inset-0 z-[100] flex items-center justify-center bg-black/80 p-4"
            @click.self="previewImageUrl = ''"
          >
            <button
              class="absolute right-4 top-4 rounded-full bg-black/50 p-2 text-white transition-colors hover:bg-black/70"
              @click="previewImageUrl = ''"
            >
              <Icon name="x" size="lg" :stroke-width="2" />
            </button>
            <img
              :src="previewImageUrl"
              :alt="t('admin.accounts.imageLightboxAlt')"
              class="max-h-[90vh] max-w-[90vw] rounded-lg object-contain shadow-2xl"
            />
          </div>
        </Transition>
      </Teleport>

      <!-- Test Info -->
      <div class="flex items-center justify-between px-1 text-xs text-[var(--muted-foreground)]">
        <div class="flex items-center gap-3">
          <span class="flex items-center gap-1">
            <Icon name="grid" size="sm" :stroke-width="2" />
            {{ t('admin.accounts.testModel') }}
          </span>
        </div>
        <span class="flex items-center gap-1">
          <Icon name="chat" size="sm" :stroke-width="2" />
          {{ testModeSummary }}
        </span>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button
          @click="handleClose"
          class="rounded-lg bg-[var(--surface-2)] px-4 py-2 text-sm font-medium text-[var(--foreground)] transition-colors hover:bg-[var(--surface-2)]"
        >
          {{ t('common.close') }}
        </button>
        <button
          @click="startTest"
          :disabled="!canStartTest"
          :class="[
            'flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium transition-all',
            !canStartTest
              ? 'cursor-not-allowed bg-[var(--primary)] text-[var(--primary-foreground)]'
              : status === 'success'
                ? 'bg-green-500 text-white hover:bg-green-600'
                : status === 'error'
                  ? 'bg-orange-500 text-white hover:bg-orange-600'
                  : 'bg-[var(--primary)] text-[var(--primary-foreground)] hover:opacity-90'
          ]"
        >
          <Icon
            v-if="status === 'connecting'"
            name="refresh"
            size="sm"
            class="animate-spin"
            :stroke-width="2"
          />
          <Icon v-else-if="status === 'idle'" name="play" size="sm" :stroke-width="2" />
          <Icon v-else name="refresh" size="sm" :stroke-width="2" />
          <span>
            {{
              status === 'connecting'
                ? t('admin.accounts.testing')
                : status === 'idle'
                  ? t('admin.accounts.startTest')
                  : t('admin.accounts.retry')
            }}
          </span>
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch, nextTick, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import { Icon } from '@/components/icons'
import { useClipboard } from '@/composables/useClipboard'
import { streamAccountTest, type AccountTestEvent, type AccountTestRequest } from '@/api/admin/accountTest'
import { adminAPI } from '@/api/admin'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AccountListItem } from '@/types/admin'
import type { AvailableAccountModel } from '@/types/model'

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

interface OutputLine {
  text: string
  class: string
}

interface PreviewMedia {
  url: string
  mimeType?: string
}

const props = defineProps<{
  show: boolean
  account: AccountListItem | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const terminalRef = ref<HTMLElement | null>(null)
const status = ref<'idle' | 'connecting' | 'success' | 'error'>('idle')
const outputLines = ref<OutputLine[]>([])
const streamingContent = ref('')
const errorMessage = ref('')
const availableModels = ref<AvailableAccountModel[]>([])
const selectedModelId = ref('')
const testPrompt = ref('')
const loadingModels = ref(false)
const modelsFailed = ref(false)
let modelGeneration = 0
onUnmounted(() => { modelGeneration++; abortStream() })
let abortController: AbortController | null = null
const generatedImages = ref<PreviewMedia[]>([])
const generatedAudios = ref<PreviewMedia[]>([])
const generatedVideos = ref<PreviewMedia[]>([])
const previewImageUrl = ref('')
const testMode = ref<'default' | 'compact'>('default')
const grokTestMode = ref<'text' | 'image' | 'video' | 'search' | 'tts' | 'stt' | 'realtime'>('text')
const uploadImageDataURL = ref('')
const uploadImagePreview = ref('')
const uploadImageName = ref('')
const uploadAudioDataURL = ref('')
const uploadAudioName = ref('')
const imageFileInput = ref<HTMLInputElement | null>(null)
const audioFileInput = ref<HTMLInputElement | null>(null)
const isOpenAIAccount = computed(() => props.account?.platform === 'openai')
const isGrokAccount = computed(() => props.account?.platform === 'grok')
const openAITestModeOptions = computed(() => [
  { value: 'default', label: t('admin.accounts.openai.testModeDefault') },
  { value: 'compact', label: t('admin.accounts.openai.testModeCompact') }
])
const grokTestModeOptions = computed(() => [
  { value: 'text', label: t('admin.accounts.grok.testModeText') },
  { value: 'image', label: t('admin.accounts.grok.testModeImage') },
  { value: 'video', label: t('admin.accounts.grok.testModeVideo') },
  { value: 'search', label: t('admin.accounts.grok.testModeSearch') },
  { value: 'tts', label: t('admin.accounts.grok.testModeTTS') },
  { value: 'stt', label: t('admin.accounts.grok.testModeSTT') },
  { value: 'realtime', label: t('admin.accounts.grok.testModeRealtime') }
])
const prioritizedGeminiModels = ['gemini-3.1-flash-image', 'gemini-2.5-flash-image', 'gemini-3.5-flash', 'gemini-2.5-flash', 'gemini-2.5-pro', 'gemini-3-flash-preview', 'gemini-3-pro-preview', 'gemini-2.0-flash']
const supportsGeminiImageTest = computed(() => {
  const modelID = selectedModelId.value.toLowerCase()
  if (!modelID.startsWith('gemini-') || !modelID.includes('-image')) return false

  return props.account?.platform === 'gemini' || (props.account?.platform === 'antigravity' && props.account?.type === 'apikey')
})

const supportsOpenAIImageTest = computed(() => {
  const modelID = selectedModelId.value.toLowerCase()
  if (!modelID.startsWith('gpt-image-')) return false
  return props.account?.platform === 'openai'
})

const isGrokImageModel = (id: string) => {
  const modelID = id.toLowerCase()
  return (
    modelID === 'grok-imagine' ||
    modelID === 'grok-imagine-edit' ||
    modelID.startsWith('grok-imagine-image')
  )
}
const isGrokVideoModel = (id: string) => {
  const modelID = id.toLowerCase()
  return modelID.startsWith('grok-imagine-video') || modelID.startsWith('grok-video')
}
const isGrokTextModel = (id: string) => !isGrokImageModel(id) && !isGrokVideoModel(id)

const supportsGrokImageTest = computed(
  () => isGrokAccount.value && grokTestMode.value === 'image'
)
const supportsGrokVideoTest = computed(
  () => isGrokAccount.value && grokTestMode.value === 'video'
)

const supportsImageTest = computed(
  () => supportsGeminiImageTest.value || supportsOpenAIImageTest.value || supportsGrokImageTest.value
)

// Model select only when the mode needs a model.
const showModelSelect = computed(() => {
  if (!isGrokAccount.value) return true
  return grokTestMode.value === 'text' || grokTestMode.value === 'image' || grokTestMode.value === 'video'
})

const modelOptionsForMode = computed(() => {
  if (!isGrokAccount.value) return availableModels.value
  if (grokTestMode.value === 'image') {
    return availableModels.value.filter((m) => isGrokImageModel(m.id))
  }
  if (grokTestMode.value === 'video') {
    return availableModels.value.filter((m) => isGrokVideoModel(m.id))
  }
  if (grokTestMode.value === 'text') {
    return availableModels.value.filter((m) => isGrokTextModel(m.id))
  }
  return []
})

const supportsPromptInput = computed(() => {
  if (!isGrokAccount.value) {
    return supportsImageTest.value
  }
  return (
    grokTestMode.value === 'image' ||
    grokTestMode.value === 'video' ||
    grokTestMode.value === 'search' ||
    grokTestMode.value === 'tts'
  )
})

const supportsImageUpload = computed(
  () => isGrokAccount.value && (grokTestMode.value === 'image' || grokTestMode.value === 'video')
)
const supportsAudioUpload = computed(() => isGrokAccount.value && grokTestMode.value === 'stt')
const imageUploadLabel = computed(() =>
  grokTestMode.value === 'video'
    ? t('admin.accounts.grok.videoFirstFrameLabel')
    : t('admin.accounts.grok.imageUploadLabel')
)

const imageUploadHint = computed(() =>
  grokTestMode.value === 'video'
    ? t('admin.accounts.grok.videoFirstFrameHint')
    : t('admin.accounts.grok.imageUploadHint')
)

const readFileAsDataURL = (file: File): Promise<string> =>
  new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(new Error(t('admin.accounts.grok.fileReadFailed')))
    reader.readAsDataURL(file)
  })

const onImageFileChange = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) {
    uploadImageDataURL.value = ''
    uploadImagePreview.value = ''
    uploadImageName.value = ''
    return
  }
  if (file.size > 6 * 1024 * 1024) {
    errorMessage.value = t('admin.accounts.grok.mediaTooLarge')
    status.value = 'error'
    input.value = ''
    return
  }
  try {
    const dataURL = await readFileAsDataURL(file)
    uploadImageDataURL.value = dataURL
    uploadImagePreview.value = dataURL
    uploadImageName.value = file.name
  } catch {
    uploadImageDataURL.value = ''
    uploadImagePreview.value = ''
    uploadImageName.value = ''
    errorMessage.value = t('admin.accounts.grok.fileReadFailed')
    status.value = 'error'
  }
}

const onAudioFileChange = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) {
    uploadAudioDataURL.value = ''
    uploadAudioName.value = ''
    return
  }
  if (file.size > 6 * 1024 * 1024) {
    errorMessage.value = t('admin.accounts.grok.mediaTooLarge')
    status.value = 'error'
    input.value = ''
    return
  }
  try {
    uploadAudioDataURL.value = await readFileAsDataURL(file)
    uploadAudioName.value = file.name
  } catch {
    uploadAudioDataURL.value = ''
    uploadAudioName.value = ''
    errorMessage.value = t('admin.accounts.grok.fileReadFailed')
    status.value = 'error'
  }
}

const clearMediaUploads = () => {
  uploadImageDataURL.value = ''
  uploadImagePreview.value = ''
  uploadImageName.value = ''
  uploadAudioDataURL.value = ''
  uploadAudioName.value = ''
  if (imageFileInput.value) imageFileInput.value.value = ''
  if (audioFileInput.value) audioFileInput.value.value = ''
}

const promptInputLabel = computed(() => {
  if (supportsGrokVideoTest.value || grokTestMode.value === 'video') {
    return t('admin.accounts.videoPromptLabel')
  }
  if (supportsImageTest.value || grokTestMode.value === 'image') {
    return t('admin.accounts.imagePromptLabel')
  }
  if (grokTestMode.value === 'search') {
    return t('admin.accounts.grok.searchQueryLabel')
  }
  if (grokTestMode.value === 'tts') {
    return t('admin.accounts.grok.ttsTextLabel')
  }
  return t('admin.accounts.imagePromptLabel')
})

const promptInputPlaceholder = computed(() => {
  if (grokTestMode.value === 'video') {
    return t('admin.accounts.videoPromptPlaceholder')
  }
  if (grokTestMode.value === 'image' || supportsImageTest.value) {
    return t('admin.accounts.imagePromptPlaceholder')
  }
  if (grokTestMode.value === 'search') {
    return t('admin.accounts.grok.searchQueryPlaceholder')
  }
  if (grokTestMode.value === 'tts') {
    return t('admin.accounts.grok.ttsTextPlaceholder')
  }
  return ''
})

const promptInputHint = computed(() => {
  if (grokTestMode.value === 'video') {
    return t('admin.accounts.videoTestHint')
  }
  if (grokTestMode.value === 'image' || supportsImageTest.value) {
    return t('admin.accounts.imageTestHint')
  }
  if (grokTestMode.value === 'search') {
    return t('admin.accounts.grok.searchTestHint')
  }
  if (grokTestMode.value === 'tts') {
    return t('admin.accounts.grok.ttsTestHint')
  }
  if (grokTestMode.value === 'stt') {
    return t('admin.accounts.grok.sttTestHint')
  }
  if (grokTestMode.value === 'realtime') {
    return t('admin.accounts.grok.realtimeTestHint')
  }
  return ''
})

const testModeSummary = computed(() => {
  if (isGrokAccount.value) {
    switch (grokTestMode.value) {
      case 'video':
        return t('admin.accounts.videoTestMode')
      case 'image':
        return t('admin.accounts.imageTestMode')
      case 'search':
        return t('admin.accounts.grok.searchTestMode')
      case 'tts':
        return t('admin.accounts.grok.ttsTestMode')
      case 'stt':
        return t('admin.accounts.grok.sttTestMode')
      case 'realtime':
        return t('admin.accounts.grok.realtimeTestMode')
      default:
        return t('admin.accounts.grok.textTestMode')
    }
  }
  if (supportsImageTest.value) return t('admin.accounts.imageTestMode')
  return t('admin.accounts.testPrompt')
})

const selectedModel = computed(() => availableModels.value.find(model => model.id === selectedModelId.value))
const probeUnsupported = computed(() => selectedModel.value?.probe_supported === false)

const canStartTest = computed(() => {
  if (status.value === 'connecting' || probeUnsupported.value) return false
  if (isGrokAccount.value) {
    if (
      grokTestMode.value === 'search' ||
      grokTestMode.value === 'tts' ||
      grokTestMode.value === 'stt' ||
      grokTestMode.value === 'realtime'
    ) {
      return true // standalone modes (prompt/model optional)
    }
    return Boolean(selectedModelId.value)
  }
  return Boolean(selectedModelId.value)
})

const sortTestModels = (models: AvailableAccountModel[]) => {
  const priorityMap = new Map(prioritizedGeminiModels.map((id, index) => [id, index]))

  return [...models].sort((a, b) => {
    const aPriority = priorityMap.get(a.id) ?? Number.MAX_SAFE_INTEGER
    const bPriority = priorityMap.get(b.id) ?? Number.MAX_SAFE_INTEGER
    if (aPriority !== bPriority) return aPriority - bPriority
    return 0
  })
}

// Load available models when modal opens
const applyDefaultPromptForMode = () => {
  if (!supportsPromptInput.value) return
  if (testPrompt.value.trim()) return
  if (grokTestMode.value === 'video') {
    testPrompt.value = t('admin.accounts.videoPromptDefault')
  } else if (grokTestMode.value === 'image' || supportsImageTest.value) {
    testPrompt.value = t('admin.accounts.imagePromptDefault')
  } else if (grokTestMode.value === 'search') {
    testPrompt.value = t('admin.accounts.grok.searchQueryDefault')
  } else if (grokTestMode.value === 'tts') {
    testPrompt.value = t('admin.accounts.grok.ttsTextDefault')
  }
}

const pickDefaultModelForMode = () => {
  const opts = modelOptionsForMode.value
  if (!opts.length) {
    selectedModelId.value = ''
    return
  }
  if (opts.some((m) => m.id === selectedModelId.value)) return
  if (grokTestMode.value === 'text') {
    const preferred =
      opts.find((m) => m.id.includes('grok-4.5')) ||
      opts.find((m) => m.id === 'grok') ||
      opts[0]
    selectedModelId.value = preferred.id
    return
  }
  selectedModelId.value = opts[0].id
}

watch(
  () => [props.show, props.account?.id, props.account?.platform] as const,
  async ([newVal]) => {
    modelGeneration++
    abortStream()
    availableModels.value = []
    selectedModelId.value = ''
    modelsFailed.value = false
    loadingModels.value = false
    const contextGeneration = modelGeneration
    if (newVal && props.account) {
      testPrompt.value = ''
      testMode.value = 'default'
      grokTestMode.value = 'text'
      resetState()
      await loadAvailableModels()
      if (!props.show || contextGeneration + 1 !== modelGeneration) return
      if (isGrokAccount.value) {
        pickDefaultModelForMode()
        applyDefaultPromptForMode()
      }
    } else {
      abortStream()
    }
  },
  { flush: 'sync' }
)

watch(grokTestMode, () => {
  if (!isGrokAccount.value) return
  testPrompt.value = ''
  clearMediaUploads()
  pickDefaultModelForMode()
  applyDefaultPromptForMode()
})

const loadAvailableModels = async () => {
  if (!props.account) return

  const request = ++modelGeneration
  const account = props.account
  loadingModels.value = true
  modelsFailed.value = false
  selectedModelId.value = '' // Reset selection before loading
  try {
    const models = await adminAPI.accounts.getAvailableModels(account.id)
    if (request !== modelGeneration || !props.show) return
    availableModels.value = props.account.platform === 'gemini' || props.account.platform === 'antigravity'
      ? sortTestModels(models)
      : models
    // Default selection by platform
    if (availableModels.value.length > 0) {
      if (props.account.platform === 'gemini') {
        selectedModelId.value = availableModels.value[0].id
      } else {
        // Try to select Sonnet as default, otherwise use first model
        const sonnetModel = availableModels.value.find((m) => m.id.includes('sonnet'))
        selectedModelId.value = sonnetModel?.id || availableModels.value[0].id
      }
    }
  } catch (error) {
    if (request !== modelGeneration || !props.show) return
    modelsFailed.value = true
    console.error('Failed to load available models:', error)
    // Fallback to empty list
    availableModels.value = []
    selectedModelId.value = ''
  } finally {
    if (request === modelGeneration) loadingModels.value = false
  }
}

const resetState = () => {
  status.value = 'idle'
  outputLines.value = []
  streamingContent.value = ''
  errorMessage.value = ''
  generatedImages.value = []
  generatedAudios.value = []
  generatedVideos.value = []
  previewImageUrl.value = ''
}

const handleClose = () => {
  modelGeneration++
  abortStream()
  emit('close')
}

const abortStream = () => {
  if (abortController) {
    abortController.abort()
    abortController = null
  }
}

const addLine = (text: string, className: string = 'text-[var(--muted-foreground)]') => {
  outputLines.value.push({ text, class: className })
  scrollToBottom()
}

const scrollToBottom = async () => {
  await nextTick()
  if (terminalRef.value) {
    terminalRef.value.scrollTop = terminalRef.value.scrollHeight
  }
}

const startTest = async () => {
  if (!props.account || !canStartTest.value) return

  resetState()
  status.value = 'connecting'
  addLine(t('admin.accounts.startingTestForAccount', { name: props.account.name }), 'text-[var(--info-text)]')
  addLine(t('admin.accounts.testAccountTypeLabel', { type: props.account.type }), 'text-[var(--muted-foreground)]')
  if (isGrokAccount.value) {
    const modeLabel =
      grokTestModeOptions.value.find((o) => o.value === grokTestMode.value)?.label || grokTestMode.value
    addLine(t('admin.accounts.grok.selectedTestMode', { mode: modeLabel }), 'text-[var(--muted-foreground)]')
  }
  addLine('', 'text-[var(--muted-foreground)]')

  abortStream()

  abortController = new AbortController()

  try {
    const requestBody: AccountTestRequest = {
      model_id: showModelSelect.value ? selectedModelId.value : '',
      prompt: supportsPromptInput.value ? testPrompt.value.trim() : ''
    }
    if (isOpenAIAccount.value) {
      requestBody.mode = testMode.value
    }
    if (isGrokAccount.value) {
      // Always send explicit Grok mode. search/tts/stt/realtime are standalone
      // endpoints (no free-form model select). text/image/video use optional model.
      requestBody.mode = grokTestMode.value
      if (
        grokTestMode.value === 'search' ||
        grokTestMode.value === 'tts' ||
        grokTestMode.value === 'stt' ||
        grokTestMode.value === 'realtime'
      ) {
        requestBody.model_id = ''
      }
      if (uploadImageDataURL.value && (grokTestMode.value === 'image' || grokTestMode.value === 'video')) {
        requestBody.image_data_url = uploadImageDataURL.value
      }
      if (uploadAudioDataURL.value && grokTestMode.value === 'stt') {
        requestBody.audio_data_url = uploadAudioDataURL.value
      }
    }

    await streamAccountTest(props.account.id, requestBody, handleEvent, abortController.signal)
    if (status.value === 'connecting') throw new Error(t('admin.accounts.incompleteProbeStream'))
  } catch (error: unknown) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      status.value = 'idle'
      return
    }
    status.value = 'error'
    const message = extractApiErrorMessage(error, t('common.unknownError'))
    errorMessage.value = message
    addLine(t('admin.accounts.errorPrefix', { message }), 'text-[var(--bad-text)]')
  }
}

const handleEvent = (event: AccountTestEvent) => {
  switch (event.type) {
    case 'test_start':
      addLine(t('admin.accounts.connectedToApi'), 'text-[var(--ok-text)]')
      if (event.model) {
        addLine(t('admin.accounts.usingModel', { model: event.model }), 'text-[var(--info-text)]')
      }
      addLine(
        isGrokAccount.value
          ? grokTestMode.value === 'video'
            ? t('admin.accounts.sendingVideoRequest')
            : grokTestMode.value === 'image'
              ? t('admin.accounts.sendingImageRequest')
              : grokTestMode.value === 'search'
                ? t('admin.accounts.grok.sendingSearchRequest')
                : grokTestMode.value === 'tts'
                  ? t('admin.accounts.grok.sendingTTSRequest')
                  : grokTestMode.value === 'stt'
                    ? t('admin.accounts.grok.sendingSTTRequest')
                    : grokTestMode.value === 'realtime'
                      ? t('admin.accounts.grok.sendingRealtimeRequest')
                      : t('admin.accounts.sendingTestMessage')
          : supportsImageTest.value
            ? t('admin.accounts.sendingImageRequest')
            : t('admin.accounts.sendingTestMessage'),
        'text-[var(--muted-foreground)]'
      )
      addLine('', 'text-[var(--muted-foreground)]')
      addLine(t('admin.accounts.response'), 'text-[var(--warn-text)]')
      break

    case 'content':
      if (event.text) {
        streamingContent.value += event.text
        scrollToBottom()
      }
      break

    case 'image':
      if (event.image_url) {
        generatedImages.value.push({
          url: event.image_url,
          mimeType: event.mime_type
        })
        addLine(t('admin.accounts.imageReceived', { count: generatedImages.value.length }), 'text-purple-300')
      }
      break

    case 'audio':
      if (event.audio_url) {
        generatedAudios.value.push({
          url: event.audio_url,
          mimeType: event.mime_type
        })
        addLine(t('admin.accounts.audioReceived', { count: generatedAudios.value.length }), 'text-purple-300')
      }
      break

    case 'video':
      if (event.video_url) {
        generatedVideos.value.push({
          url: event.video_url,
          mimeType: event.mime_type
        })
        addLine(t('admin.accounts.videoReceived', { count: generatedVideos.value.length }), 'text-purple-300')
      }
      break

    case 'status':
      if (event.text) {
        addLine(event.text, 'text-[var(--info-text)]')
      }
      break

    case 'test_complete':
      // Move streaming content to output lines
      if (streamingContent.value) {
        addLine(streamingContent.value, 'text-[var(--ok-text)]')
        streamingContent.value = ''
      }
      if (event.success) {
        status.value = 'success'
      } else {
        status.value = 'error'
        errorMessage.value = event.error || t('admin.accounts.testFailed')
      }
      break

    case 'error':
      status.value = 'error'
      errorMessage.value = event.error || t('common.unknownError')
      if (streamingContent.value) {
        addLine(streamingContent.value, 'text-[var(--ok-text)]')
        streamingContent.value = ''
      }
      break
  }
}

const copyOutput = () => {
  const text = outputLines.value.map((l) => l.text).join('\n')
  copyToClipboard(text, t('admin.accounts.outputCopied'))
}
</script>

<style>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
