<template>
  <BaseDialog :show="show" :title="t('keys.rotateKey')" width="normal" @close="emit('close')">
    <div class="space-y-4">
      <p class="text-sm leading-6 text-[var(--muted-foreground)]">
        {{ t('keys.rotation.description', { name: apiKey?.name || '' }) }}
      </p>

      <label class="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-4">
        <input v-model="revokePrevious" type="checkbox" class="mt-0.5 h-4 w-4" />
        <span class="text-sm text-[var(--foreground)]">
          {{ t('keys.rotation.revokePrevious') }}
        </span>
      </label>

      <div v-if="!revokePrevious" class="space-y-1">
        <label for="rotate-grace-hours" class="block text-sm font-medium text-[var(--oh-ink,var(--foreground))]">
          {{ t('keys.rotation.graceHours') }}
        </label>
        <input
          id="rotate-grace-hours"
          v-model.number="graceHours"
          type="number"
          min="0"
          max="720"
          class="input w-full font-mono tabular-nums"
          :placeholder="t('keys.rotation.graceDefault')"
        />
        <p class="text-xs text-[var(--oh-ink-secondary,var(--muted-foreground))]">
          {{ t('keys.rotation.graceHint') }}
        </p>
      </div>

      <p v-if="errorMessage" role="alert" class="text-sm text-[var(--oh-danger,#dc2626)]">{{ errorMessage }}</p>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">
        {{ t('common.cancel') }}
      </button>
      <button type="button" class="btn btn-primary" :disabled="submitting" @click="submit">
        {{ submitting ? t('common.loading') : t('keys.rotateKey') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { keysAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types/user'

const props = defineProps<{
  show: boolean
  apiKey: ApiKey | null
  // Admin surfaces pass their own rotate call; the user surface uses the default one.
  rotateFn?: (id: number, options: { grace_ttl_hours?: number; revoke_previous?: boolean }) => Promise<{ secret: string; grace_until?: string | null }>
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'issued', payload: { secret: string; graceUntil: string | null }): void
}>()

const { t } = useI18n()
const appStore = useAppStore()

const revokePrevious = ref(false)
const graceHours = ref<number | undefined>(undefined)
const submitting = ref(false)
const errorMessage = ref('')

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      revokePrevious.value = false
      graceHours.value = undefined
      errorMessage.value = ''
    }
  }
)

async function submit() {
  if (!props.apiKey || submitting.value) {
    return
  }
  submitting.value = true
  errorMessage.value = ''
  try {
    const rotate = props.rotateFn ?? keysAPI.rotate
    const issue = await rotate(props.apiKey.id, {
      grace_ttl_hours: revokePrevious.value ? undefined : graceHours.value,
      revoke_previous: revokePrevious.value
    })
    appStore.showSuccess(t('keys.rotation.success'))
    emit('issued', { secret: issue.secret, graceUntil: issue.grace_until ?? null })
    emit('close')
  } catch (error: unknown) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    submitting.value = false
  }
}
</script>
