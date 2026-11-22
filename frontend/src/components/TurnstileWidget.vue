<template>
  <div v-if="siteKey" class="turnstile-wrapper relative">
    <div
      v-if="loading"
      role="status"
      class="absolute inset-0 flex items-center justify-center rounded-lg border border-[var(--border)] bg-[var(--surface-2)] text-sm text-[var(--muted-foreground)]"
    >
      {{ t('auth.captchaLoading') }}
    </div>
    <div ref="containerRef" class="turnstile-container"></div>
  </div>
</template>

<script lang="ts">
interface TurnstileRenderOptions {
  sitekey: string
  callback: (token: string) => void
  'expired-callback'?: () => void
  'error-callback'?: () => void
  theme?: 'light' | 'dark' | 'auto'
  size?: 'normal' | 'compact' | 'flexible'
}

interface TurnstileAPI {
  render: (container: HTMLElement, options: TurnstileRenderOptions) => string
  reset: (widgetId?: string) => void
  remove: (widgetId?: string) => void
}

declare global {
  interface Window {
    turnstile?: TurnstileAPI
    onTurnstileLoad?: () => void
  }
}

let sharedTurnstileScriptPromise: Promise<void> | null = null
</script>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    siteKey: string
    theme?: 'light' | 'dark' | 'auto'
    size?: 'normal' | 'compact' | 'flexible'
  }>(),
  {
    theme: 'auto',
    size: 'flexible'
  }
)

const emit = defineEmits<{
  (e: 'verify', token: string): void
  (e: 'expire'): void
  (e: 'error'): void
}>()

const { t } = useI18n()
const loading = ref(true)
const containerRef = ref<HTMLElement | null>(null)
const widgetId = ref<string | null>(null)
const scriptLoaded = ref(false)
let isMounted = true

const loadScript = (): Promise<void> => {
  if (window.turnstile) {
    scriptLoaded.value = true
    return Promise.resolve()
  }

  if (sharedTurnstileScriptPromise) {
    return sharedTurnstileScriptPromise.then(() => {
      scriptLoaded.value = true
    })
  }

  sharedTurnstileScriptPromise = new Promise<void>((resolve, reject) => {
    const existingScript = document.querySelector<HTMLScriptElement>(
      'script[src*="challenges.cloudflare.com/turnstile"], script[src*="turnstile"]'
    )

    const cleanupPromise = () => {
      sharedTurnstileScriptPromise = null
    }

    const previousOnLoad = window.onTurnstileLoad
    const handleReady = () => {
      cleanupPromise()
      previousOnLoad?.()
      resolve()
    }

    if (existingScript) {
      window.onTurnstileLoad = handleReady
      const handleExistingError = () => {
        existingScript.removeEventListener('error', handleExistingError)
        existingScript.remove()
        cleanupPromise()
        reject(new Error('Failed to load Turnstile script'))
      }
      existingScript.addEventListener('error', handleExistingError, { once: true })
      return
    }

    const script = document.createElement('script')
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?onload=onTurnstileLoad'
    script.async = true
    script.defer = true

    window.onTurnstileLoad = handleReady

    script.onerror = () => {
      script.remove()
      cleanupPromise()
      reject(new Error('Failed to load Turnstile script'))
    }

    document.head.appendChild(script)
  })

  return sharedTurnstileScriptPromise.then(() => {
    scriptLoaded.value = true
  })
}

const renderWidget = () => {
  if (!window.turnstile || !containerRef.value || !props.siteKey) {
    return
  }

  // Remove existing widget if any
  if (widgetId.value) {
    try {
      window.turnstile.remove(widgetId.value)
    } catch {
      // Ignore errors when removing
    }
    widgetId.value = null
  }

  // Clear container
  containerRef.value.innerHTML = ''

  widgetId.value = window.turnstile.render(containerRef.value, {
    sitekey: props.siteKey,
    callback: (token: string) => {
      emit('verify', token)
    },
    'expired-callback': () => {
      emit('expire')
    },
    'error-callback': () => {
      emit('error')
    },
    theme: props.theme,
    size: props.size
  })
}

const reset = () => {
  if (window.turnstile && widgetId.value) {
    window.turnstile.reset(widgetId.value)
  }
}

// Expose reset method to parent
defineExpose({ reset })

onMounted(async () => {
  isMounted = true
  if (!props.siteKey) {
    loading.value = false
    return
  }

  try {
    await loadScript()
    if (!isMounted) return
    renderWidget()
  } catch (error) {
    if (!isMounted) return
    console.error('Failed to initialize Turnstile:', error)
    emit('error')
  } finally {
    if (isMounted) {
      loading.value = false
    }
  }
})

onUnmounted(() => {
  isMounted = false
  if (window.turnstile && widgetId.value) {
    try {
      window.turnstile.remove(widgetId.value)
    } catch {
      // Ignore errors when removing
    }
    widgetId.value = null
  }
})

// Re-render when siteKey changes
watch(
  () => props.siteKey,
  (newKey) => {
    if (newKey && scriptLoaded.value) {
      renderWidget()
    }
  }
)
</script>

<style scoped>
.turnstile-wrapper {
  width: 100%;
}

.turnstile-container {
  width: 100%;
  min-height: 65px;
}

/* Make the Turnstile iframe fill the container width */
.turnstile-container :deep(iframe) {
  width: 100% !important;
}
</style>
