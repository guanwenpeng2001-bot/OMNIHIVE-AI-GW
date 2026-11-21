<template>
  <div class="legal-portal min-h-screen bg-[var(--background)] text-[var(--foreground)]">
    <header class="glass-bar sticky top-0 z-20 border-b border-[var(--border)] ">
      <div class="mx-auto flex max-w-5xl items-center justify-between gap-4 px-4 py-4 sm:px-6">
        <RouterLink to="/home" class="flex min-w-0 items-center gap-2.5" :data-logo="Boolean(siteLogo)" :title="siteName">
          <template v-if="settings">
            <span class="flex h-9 w-9 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl border border-amber-500/25 bg-[var(--card)] shadow-sm ">
              <img v-if="siteLogo" :src="siteLogo" alt="Logo" class="h-full w-full object-contain" />
              <svg v-else width="22" height="22" viewBox="0 0 32 32" fill="none" aria-hidden="true">
                <path d="M16 2 28.1 9v14L16 30 3.9 23V9Z" fill="var(--primary, #16161a)" />
                <path d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z" stroke="var(--brand, #f59e0b)" stroke-width="1.8" />
                <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="var(--brand, #f59e0b)" />
                <circle cx="16" cy="16" r="2.1" fill="var(--primary, #16161a)" />
              </svg>
            </span>
            <span class="truncate text-base font-semibold text-[var(--foreground)] ">
              {{ siteName }}
            </span>
          </template>
          <template v-else>
            <span class="h-10 w-10 flex-shrink-0 animate-pulse rounded-xl bg-[var(--surface-2)] " aria-hidden="true"></span>
            <span class="h-5 w-28 animate-pulse rounded bg-[var(--surface-2)] " aria-hidden="true"></span>
          </template>
        </RouterLink>
        <RouterLink
          to="/login"
          class="btn btn-primary flex-shrink-0"
        >
          {{ t('home.login') }}
        </RouterLink>
      </div>
    </header>

    <main class="mx-auto max-w-4xl px-4 py-8 sm:px-6 lg:py-10">
      <nav
        v-if="!isAdminComplianceDocument"
        :aria-label="t('publicPortal.legal.documentsNavAria')"
        class="mb-6 flex flex-wrap items-center gap-2"
      >
        <RouterLink
          v-for="item in navDocuments"
          :key="item.routeId"
          :to="{ name: 'LegalDocument', params: { documentId: item.routeId } }"
          class="btn btn-outline text-xs"
          :class="isActiveNavDoc(item.routeId) ? 'border-[var(--brand)] text-[var(--brand-text)]' : ''"
        >
          {{ item.title }}
        </RouterLink>
        <RouterLink to="/home" class="btn btn-outline ml-auto text-xs">
          {{ t('publicPortal.legal.backToHome') }}
        </RouterLink>
      </nav>

      <div v-if="loading" class="flex min-h-[320px] items-center justify-center">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-[var(--brand)]"></div>
      </div>

      <section
        v-else-if="shouldShowLoadError"
        data-testid="legal-load-error"
        class="rounded-lg border border-red-200 bg-red-50 p-6 text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200"
      >
        <h1 class="text-lg font-semibold">{{ t('legal.loadFailed') }}</h1>
        <p class="mt-2 text-sm">{{ t('legal.retryLater') }}</p>
        <button
          type="button"
          data-testid="legal-load-retry"
          class="btn btn-outline mt-4 text-xs"
          @click="loadSettings"
        >
          {{ t('publicPortal.legal.retry') }}
        </button>
      </section>

      <section
        v-else-if="!currentDocument"
        class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-6  "
      >
        <div class="flex items-start gap-3">
          <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-md bg-[var(--surface-2)] text-[var(--muted-foreground)]  ">
            <Icon name="document" size="sm" />
          </span>
          <div>
            <h1 class="text-lg font-semibold text-[var(--foreground)] ">{{ t('legal.notFound') }}</h1>
            <p class="mt-2 text-sm leading-6 text-[var(--muted-foreground)] ">
              {{ t('legal.notFoundDescription') }}
            </p>
          </div>
        </div>
      </section>

      <article v-else class="rounded-xl border border-[var(--border)] bg-[var(--card)] p-5 sm:p-10">
        <div class="mb-8 border-b border-[var(--border)] pb-6 ">
          <div class="flex items-start gap-4">
            <span class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-md bg-[var(--surface-2)] text-[var(--brand-text)]">
              <Icon :name="documentIcon" size="md" />
            </span>
            <div class="min-w-0">
              <p class="text-sm font-medium text-[var(--brand-text)]">{{ documentTypeLabel }}</p>
              <h1 class="mt-2 break-words text-2xl font-bold tracking-normal text-[var(--foreground)]  sm:text-3xl">
                {{ currentDocument.title.replace(/sub2api/gi, 'OmniHive') }}
              </h1>
              <p v-if="updatedAt" class="mt-3 text-sm text-[var(--muted-foreground)] ">
                {{ t('legal.updatedAt', { date: updatedAt }) }}
              </p>
            </div>
          </div>
        </div>

        <div
          v-if="usingTemplateFallback"
          data-testid="legal-template-notice"
          class="mb-6 rounded-lg border border-amber-200 bg-amber-50/80 p-4 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100"
        >
          <p class="font-semibold">{{ t('publicPortal.legal.templateNoticeTitle') }}</p>
          <p class="mt-1 leading-6 opacity-90">{{ t('publicPortal.legal.templateNoticeBody') }}</p>
          <p v-if="contactInfo" class="mt-2 font-medium">{{ contactInfo }}</p>
        </div>

        <div
          v-if="hasContent"
          class="legal-document-content"
          v-html="renderedHtml"
        ></div>
        <div
          v-else
          class="rounded-lg border border-dashed border-[var(--border)] bg-[var(--card)] px-6 py-14 text-center text-sm text-[var(--muted-foreground)]   "
        >
          {{ t('legal.empty') }}
        </div>
      </article>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { getLocale } from '@/i18n'
import { sanitizeUrl } from '@/utils/url'
import { useAppStore } from '@/stores/app'
import {
  canonicalLegalDocumentId,
  DEFAULT_LEGAL_NAV_SLOTS,
  findConfiguredLegalDocument,
  getLegalSlotTemplateKeys,
  legalDocumentRouteParam,
} from '@/features/public/legal'
import type { LoginAgreementDocument } from '@/types'
import zhAdminCompliance from '../../../../docs/legal/admin-compliance.zh.md?raw'
import enAdminCompliance from '../../../../docs/legal/admin-compliance.en.md?raw'

type LegalDocumentIcon = 'document' | 'shield' | 'globe' | 'cog'

const route = useRoute()
const { t } = useI18n()
const appStore = useAppStore()
const loadedPublicSettings = ref(appStore.cachedPublicSettings)
const settings = computed(() => loadedPublicSettings.value ?? appStore.cachedPublicSettings)
const loading = ref(!settings.value)
const loadError = ref(false)

marked.setOptions({
  breaks: true,
  gfm: true,
})

const documentId = computed(() => String(route.params.documentId || ''))
const isAdminComplianceDocument = computed(() => documentId.value === 'admin-compliance')
const documents = computed(() => settings.value?.login_agreement_documents ?? [])
const siteName = computed(() => (settings.value?.site_name || 'OmniHive').replace(/sub2api/gi, 'OmniHive'))
const contactInfo = computed(() => (settings.value?.contact_info || '').trim())
const siteLogo = computed(() => sanitizeUrl(settings.value?.site_logo || '', {
  allowRelative: true,
  allowDataUrl: true,
}))
const shouldShowLoadError = computed(
  () => !isAdminComplianceDocument.value && loadError.value && !settings.value
)
const updatedAt = computed(() =>
  isAdminComplianceDocument.value ? '' : settings.value?.login_agreement_updated_at || ''
)
const documentTypeLabel = computed(() =>
  isAdminComplianceDocument.value ? t('legal.adminCompliance') : t('legal.loginAgreement')
)

const navDocuments = computed(() => {
  const result: Array<{ routeId: string; canonicalId: string; title: string }> = []
  const seenCanonical = new Set<string>()

  for (const doc of documents.value) {
    const canonicalId = canonicalLegalDocumentId(doc.id || doc.title)
    if (!canonicalId || seenCanonical.has(canonicalId)) continue
    seenCanonical.add(canonicalId)
    const slotKeys = getLegalSlotTemplateKeys(doc.id || canonicalId)
    result.push({
      routeId: legalDocumentRouteParam(doc),
      canonicalId,
      title: (doc.title?.trim() || (slotKeys ? t(slotKeys.titleKey) : doc.id)).replace(/sub2api/gi, 'OmniHive'),
    })
  }

  for (const slotId of DEFAULT_LEGAL_NAV_SLOTS) {
    if (seenCanonical.has(slotId)) continue
    seenCanonical.add(slotId)
    const slotKeys = getLegalSlotTemplateKeys(slotId)
    if (!slotKeys) continue
    result.push({
      routeId: slotId === 'privacy-policy' ? 'privacy' : slotId,
      canonicalId: slotId,
      title: t(slotKeys.titleKey),
    })
  }

  return result
})

function isActiveNavDoc(routeId: string): boolean {
  return canonicalLegalDocumentId(documentId.value) === canonicalLegalDocumentId(routeId)
}

const resolvedDocument = computed<{
  document: LoginAgreementDocument | null
  usingFallback: boolean
}>(() => {
  if (isAdminComplianceDocument.value) {
    return {
      document: {
        id: 'admin-compliance',
        title: t('adminCompliance.title'),
        content_md: getLocale() === 'zh' ? zhAdminCompliance : enAdminCompliance
      },
      usingFallback: false
    }
  }
  const id = documentId.value
  if (!id) {
    return { document: null, usingFallback: false }
  }
  const configuredDocument = findConfiguredLegalDocument(documents.value, id)
  if (configuredDocument && configuredDocument.content_md?.trim()) {
    return { document: configuredDocument, usingFallback: false }
  }
  const slotKeys = getLegalSlotTemplateKeys(id)
  if (slotKeys) {
    return {
      document: {
        id: configuredDocument?.id || id,
        title: configuredDocument?.title?.trim() || t(slotKeys.titleKey),
        content_md: t(slotKeys.templateKey)
      },
      usingFallback: true
    }
  }
  if (configuredDocument) {
    return { document: configuredDocument, usingFallback: !configuredDocument.content_md?.trim() }
  }
  return { document: null, usingFallback: false }
})

const currentDocument = computed<LoginAgreementDocument | null>(() => resolvedDocument.value.document)
const usingTemplateFallback = computed<boolean>(() => resolvedDocument.value.usingFallback)

const hasContent = computed(() => Boolean(currentDocument.value?.content_md?.trim()))

const renderedHtml = computed(() => {
  const content = currentDocument.value?.content_md?.trim() || ''
  if (!content) {
    return ''
  }
  const html = marked.parse(content) as string
  const fragment = DOMPurify.sanitize(html, { RETURN_DOM_FRAGMENT: true })
  const walker = document.createTreeWalker(fragment, NodeFilter.SHOW_TEXT)
  while (walker.nextNode()) {
    walker.currentNode.textContent = walker.currentNode.textContent?.replace(/sub2api/gi, 'OmniHive') ?? ''
  }
  const container = document.createElement('div')
  container.append(fragment)
  return container.innerHTML
})

const documentIcon = computed<LegalDocumentIcon>(() => {
  const canonical = canonicalLegalDocumentId(currentDocument.value?.id || documentId.value)
  if (canonical === 'privacy-policy' || canonical === 'usage-policy') {
    return 'shield'
  }
  if (canonical === 'supported-regions') {
    return 'globe'
  }
  if (canonical === 'service-specific-terms' || canonical === 'refund-policy') {
    return 'cog'
  }
  const title = currentDocument.value?.title || ''
  if (/privacy|policy|政策|隐私/i.test(title)) {
    return 'shield'
  }
  return 'document'
})

async function loadSettings(): Promise<void> {
  loading.value = true
  loadError.value = false
  try {
    const loadedSettings = await appStore.fetchPublicSettings()
    loadedPublicSettings.value = loadedSettings
    loadError.value = !loadedSettings
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  if (isAdminComplianceDocument.value) {
    loading.value = false
    return
  }
  await loadSettings()
})
</script>

<style scoped>
.legal-document-content {
  line-height: 1.75;
  overflow-wrap: anywhere;
  color: inherit;
}

.legal-document-content :deep(h1) {
  @apply mb-4 mt-8 border-b border-[var(--border)] pb-3 text-3xl font-bold ;
}

.legal-document-content :deep(h2) {
  @apply mb-3 mt-7 text-2xl font-bold;
}

.legal-document-content :deep(h3) {
  @apply mb-2 mt-6 text-xl font-semibold;
}

.legal-document-content :deep(h4) {
  @apply mb-2 mt-5 text-lg font-semibold;
}

.legal-document-content :deep(p) {
  @apply mb-4 text-[var(--foreground)] ;
}

.legal-document-content :deep(a) {
  @apply text-[var(--brand-text)] underline underline-offset-4;
}

.legal-document-content :deep(ul) {
  @apply mb-4 list-disc pl-6;
}

.legal-document-content :deep(ol) {
  @apply mb-4 list-decimal pl-6;
}

.legal-document-content :deep(li) {
  @apply mb-1 text-[var(--foreground)] ;
}

.legal-document-content :deep(blockquote) {
  @apply my-5 border-l-4 border-[var(--border)] pl-4 text-[var(--muted-foreground)]  ;
}

.legal-document-content :deep(code) {
  @apply rounded bg-[var(--surface-2)] px-1.5 py-0.5 font-mono text-sm ;
}

.legal-document-content :deep(pre) {
  @apply my-5 overflow-x-auto rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-4 text-[var(--foreground)];
}

.legal-document-content :deep(pre code) {
  @apply bg-transparent p-0 text-inherit;
}

.legal-document-content :deep(table) {
  @apply my-5 block w-full overflow-x-auto border-collapse;
}

.legal-document-content :deep(th) {
  @apply border border-[var(--border)] bg-[var(--surface-2)] px-3 py-2 text-left font-semibold  ;
}

.legal-document-content :deep(td) {
  @apply border border-[var(--border)] px-3 py-2 ;
}

.legal-document-content :deep(img) {
  @apply my-5 h-auto max-w-full rounded-lg;
}

.legal-document-content :deep(hr) {
  @apply my-7 border-[var(--border)] ;
}
</style>
