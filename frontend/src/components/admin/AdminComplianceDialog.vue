<template>
  <BaseDialog
    :show="visible"
    :title="t('adminCompliance.title')"
    width="wide"
    :close-on-escape="false"
    :close-on-click-outside="false"
    :show-close-button="false"
    :z-index="80"
    @close="noop"
  >
    <div class="space-y-5">
      <div class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100">
        <div class="flex gap-3">
          <Icon name="exclamationTriangle" size="md" class="mt-0.5 flex-shrink-0" />
          <div class="space-y-2">
            <p class="font-semibold">{{ t('adminCompliance.blockingNotice') }}</p>
            <p class="leading-6">{{ t('adminCompliance.riskNotice') }}</p>
          </div>
        </div>
      </div>

      <div class="grid gap-4 md:grid-cols-[minmax(0,1fr)_240px]">
        <section class="min-h-[320px] max-h-[46vh] overflow-y-auto rounded-lg border border-[var(--border)] bg-[var(--card)] p-5">
          <div class="legal-document-content" v-html="renderedDocument"></div>
        </section>

        <aside class="space-y-3 rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-4 text-sm">
          <div>
            <p class="text-xs font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
              {{ t('adminCompliance.version') }}
            </p>
            <p class="mt-1 break-all font-mono tabular-nums text-[var(--foreground)]">
              {{ complianceStore.status?.version || 'v2026.06.10' }}
            </p>
          </div>
          <a
            :href="documentUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex items-center gap-2 text-[var(--brand-text)] underline underline-offset-4 hover:text-[var(--brand-text)]"
          >
            <Icon name="externalLink" size="sm" />
            {{ t('adminCompliance.openDocument') }}
          </a>
          <p class="leading-6 text-[var(--muted-foreground)]">
            {{ t('adminCompliance.documentSource') }}
          </p>
        </aside>
      </div>

      <div class="space-y-3">
        <label for="admin-compliance-phrase" class="block text-sm font-semibold text-[var(--foreground)]">
          {{ t('adminCompliance.inputLabel') }}
        </label>
        <div class="rounded-lg bg-[var(--surface-2)] px-3 py-2 font-mono tabular-nums text-sm text-[var(--foreground)]">
          {{ expectedPhrase }}
        </div>
        <Input
          id="admin-compliance-phrase"
          v-model="typedPhrase"
          :placeholder="t('adminCompliance.inputPlaceholder')"
          autocomplete="off"
          :disabled="complianceStore.submitting"
          :error="inputError"
          @enter="submit"
        />
      </div>

      <p class="text-xs leading-5 text-[var(--muted-foreground)]">
        {{ t('adminCompliance.legalNote') }}
      </p>
    </div>

    <template #footer>
      <div class="flex flex-col gap-3 sm:flex-row sm:justify-end">
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="complianceStore.submitting"
          @click="logout"
        >
          {{ t('adminCompliance.logout') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!canSubmit || complianceStore.submitting"
          @click="submit"
        >
          <span v-if="complianceStore.submitting">{{ t('common.submitting') }}</span>
          <span v-else>{{ t('adminCompliance.accept') }}</span>
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Input from '@/components/common/Input.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAdminComplianceStore, useAppStore, useAuthStore } from '@/stores'
import { getLocale } from '@/i18n'
import zhDocument from '../../../../docs/legal/admin-compliance.zh.md?raw'
import enDocument from '../../../../docs/legal/admin-compliance.en.md?raw'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const complianceStore = useAdminComplianceStore()
const authStore = useAuthStore()
const appStore = useAppStore()
const typedPhrase = ref('')
const attemptedSubmit = ref(false)

marked.setOptions({
  breaks: true,
  gfm: true,
})

const visible = computed(() => authStore.isAuthenticated && authStore.isAdmin &&
  (complianceStore.shouldShow || (!complianceStore.initialized && !complianceStore.loading)))
const expectedPhrase = computed(() => complianceStore.expectedPhrase)
const canSubmit = computed(() => typedPhrase.value.trim() === expectedPhrase.value)
const currentDocument = computed(() => getLocale() === 'zh' ? zhDocument : enDocument)
const documentUrl = computed(() => {
  if (getLocale() === 'zh') {
    return complianceStore.status?.document_url_zh || 'https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/blob/main/docs/legal/admin-compliance.zh.md'
  }
  return complianceStore.status?.document_url_en || 'https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/blob/main/docs/legal/admin-compliance.en.md'
})
const inputError = computed(() => {
  if (!attemptedSubmit.value || canSubmit.value) {
    return ''
  }
  return t('adminCompliance.inputMismatch')
})
const renderedDocument = computed(() => {
  const html = marked.parse(currentDocument.value.replace(/\bSub2API\b/g, 'OmniHive')) as string
  return DOMPurify.sanitize(html)
})

watch(() => [expectedPhrase.value, authStore.user?.id, authStore.sessionRevision], () => {
  typedPhrase.value = ''
  attemptedSubmit.value = false
})

watch(visible, (isVisible) => {
  if (isVisible) {
    typedPhrase.value = ''
    attemptedSubmit.value = false
  }
})

// A 423 received by an already mounted admin page must also leave that page.
watch(() => [visible.value, route.fullPath], () => {
  if (visible.value && route.meta.requiresAdmin && route.path !== '/admin/compliance') {
    void router.replace({ path: '/admin/compliance', query: { redirect: route.fullPath } })
  }
}, { immediate: true })

function noop(): void {
  // 强制确认弹窗不允许通过关闭按钮绕过。
}

async function submit(): Promise<void> {
  attemptedSubmit.value = true
  if (!visible.value || !canSubmit.value || complianceStore.submitting) {
    return
  }

  const request = complianceStore.accept(typedPhrase.value.trim())
  const revision = complianceStore.getRevision()
  try {
    const status = await request
    if (revision !== complianceStore.getRevision()) return
    if (!status.required) {
      appStore.showSuccess(t('adminCompliance.accepted'))
      typedPhrase.value = ''
      attemptedSubmit.value = false
      if (route.path === '/admin/compliance') {
        const redirect = route.query.redirect
        const target = typeof redirect === 'string' && redirect.startsWith('/admin/') &&
          !redirect.startsWith('/admin/compliance') ? redirect : '/admin/dashboard'
        await router.replace(target)
      }
    }
  } catch (error) {
    if (revision !== complianceStore.getRevision()) return
    const message = (error as { message?: string })?.message || t('adminCompliance.acceptFailed')
    appStore.showError(message)
  }
}

async function logout(): Promise<void> {
  await authStore.logout()
  await router.replace('/login')
}
</script>

<style scoped>
.legal-document-content {
  line-height: 1.75;
  overflow-wrap: anywhere;
  color: inherit;
}

.legal-document-content :deep(h1) {
  @apply mb-4 text-2xl font-bold text-[var(--foreground)];
}

.legal-document-content :deep(h2) {
  @apply mb-3 mt-6 text-xl font-semibold text-[var(--foreground)];
}

.legal-document-content :deep(p) {
  @apply mb-4 text-sm text-[var(--foreground)];
}

.legal-document-content :deep(ul),
.legal-document-content :deep(ol) {
  @apply mb-4 pl-6 text-sm text-[var(--foreground)];
}

.legal-document-content :deep(ul) {
  @apply list-disc;
}

.legal-document-content :deep(ol) {
  @apply list-decimal;
}

.legal-document-content :deep(li) {
  @apply mb-1;
}

.legal-document-content :deep(strong) {
  @apply font-semibold text-[var(--foreground)];
}
</style>
