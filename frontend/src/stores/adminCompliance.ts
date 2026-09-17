import { defineStore } from 'pinia'
import { computed, ref, watch, onScopeDispose } from 'vue'
import { useAuthStore } from './auth'
import adminComplianceAPI, { type AdminComplianceStatus } from '@/api/admin/compliance'
import { getLocale } from '@/i18n'

const FALLBACK_ZH_PHRASE = '我已阅读、理解并同意 Sub2API 部署与运营合规承诺'
const FALLBACK_EN_PHRASE = 'I have read, understood, and agree to the Sub2API Deployment and Operation Compliance Commitment'

export const useAdminComplianceStore = defineStore('adminCompliance', () => {
  const authStore = useAuthStore()
  const status = ref<AdminComplianceStatus | null>(null)
  const loading = ref(false)
  const submitting = ref(false)
  const initialized = ref(false)
  const forceVisible = ref(false)

  const required = computed(() => status.value?.required === true)
  const shouldShow = computed(() => required.value || forceVisible.value)
  const currentLocale = computed(() => getLocale())
  const expectedPhrase = computed(() => {
    if (currentLocale.value === 'zh') {
      return status.value?.ack_phrase_zh || FALLBACK_ZH_PHRASE
    }
    return status.value?.ack_phrase_en || FALLBACK_EN_PHRASE
  })

  let pending: Promise<AdminComplianceStatus> | null = null
  let revision = 0
  let identityRevision = 0

  // Synchronous invalidation also covers logout/relogin in the same tick.
  // Session changes invalidate acknowledgements; ordinary token rotation does not.
  watch(() => [authStore.user?.id, authStore.user?.role, authStore.sessionRevision, authStore.isAuthenticated], (identity, previous) => {
    if (identity.every((value, index) => value === previous[index])) return
    identityRevision += 1
    reset()
  }, { flush: 'sync' })

  onScopeDispose(reset)

  function fetchStatus(force = false): Promise<AdminComplianceStatus> {
    if (pending) return pending
    if (initialized.value && status.value && !force) return Promise.resolve(status.value)
    const requestRevision = revision
    loading.value = true
    const request = adminComplianceAPI.getStatus().then((nextStatus) => {
      if (requestRevision === revision) {
        status.value = nextStatus
        initialized.value = true
        forceVisible.value = nextStatus.required
      }
      return status.value ?? nextStatus
    }).finally(() => {
      if (pending === request) {
        pending = null
        loading.value = false
      }
    })
    pending = request
    return request
  }

  async function accept(phrase: string): Promise<AdminComplianceStatus> {
    revision += 1
    pending = null
    loading.value = false
    const acceptRevision = revision
    submitting.value = true
    try {
      const nextStatus = await adminComplianceAPI.accept({
        phrase,
        language: currentLocale.value
      })
      if (acceptRevision === revision) {
        status.value = nextStatus
        initialized.value = true
        forceVisible.value = nextStatus.required
      }
      return nextStatus
    } finally {
      if (acceptRevision === revision) submitting.value = false
    }
  }

  function requireAcknowledgement(partialStatus?: Partial<AdminComplianceStatus>): void {
    revision += 1
    pending = null
    loading.value = false
    submitting.value = false
    status.value = {
      required: true,
      version: partialStatus?.version || status.value?.version || 'v2026.06.10',
      document_path_zh: partialStatus?.document_path_zh || status.value?.document_path_zh || 'docs/legal/admin-compliance.zh.md',
      document_path_en: partialStatus?.document_path_en || status.value?.document_path_en || 'docs/legal/admin-compliance.en.md',
      document_url_zh: partialStatus?.document_url_zh || status.value?.document_url_zh || 'https://github.com/Wei-Shaw/sub2api/blob/main/docs/legal/admin-compliance.zh.md',
      document_url_en: partialStatus?.document_url_en || status.value?.document_url_en || 'https://github.com/Wei-Shaw/sub2api/blob/main/docs/legal/admin-compliance.en.md',
      ack_phrase_zh: partialStatus?.ack_phrase_zh || status.value?.ack_phrase_zh || FALLBACK_ZH_PHRASE,
      ack_phrase_en: partialStatus?.ack_phrase_en || status.value?.ack_phrase_en || FALLBACK_EN_PHRASE,
      acknowledgement: status.value?.acknowledgement
    }
    initialized.value = true
    forceVisible.value = true
  }

  function reset(): void {
    revision += 1
    pending = null
    status.value = null
    loading.value = false
    submitting.value = false
    initialized.value = false
    forceVisible.value = false
  }

  return {
    status,
    loading,
    submitting,
    initialized,
    required,
    shouldShow,
    expectedPhrase,
    getRevision: () => revision,
    getIdentityRevision: () => identityRevision,
    fetchStatus,
    accept,
    requireAcknowledgement,
    reset
  }
})
