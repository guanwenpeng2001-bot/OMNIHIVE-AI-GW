<template>
  <div v-if="enabled" class="px-0.5">
    <div class="flex items-start gap-2">
      <input
        id="login-agreement-consent"
        type="checkbox"
        :checked="accepted"
        class="mt-[2px] h-4 w-4 flex-shrink-0 rounded border-[var(--border)] text-[var(--brand-text)] focus:ring-[var(--brand)]"
        @change="handleCheckboxChange"
      />
      <div class="min-w-0 flex-1">
        <p class="text-[13px] leading-5 text-[var(--muted-foreground)]">
          <label
            for="login-agreement-consent"
            class="cursor-pointer text-[var(--foreground)]"
          >
            {{ t('legal.loginAgreementPrompt.checkboxPrefix') }}
          </label>
          <template v-for="(doc, index) in displayedDocuments" :key="doc.id || doc.title">
            <RouterLink
              :to="documentRoute(doc)"
              target="_blank"
              rel="noopener noreferrer"
              class="font-medium text-[var(--brand-text)] underline-offset-4 transition hover:underline"
            >
              {{ localizedDocumentTitle(doc) }}
            </RouterLink>
            <span v-if="index < displayedDocuments.length - 1">{{ t('legal.loginAgreementPrompt.documentSeparator') }}</span>
          </template>
        </p>
      </div>
    </div>
  </div>

  <div class="auth-legal-actions">
    <button type="button" class="btn btn-outline" @click="openDocument(termsDocument || fallbackDocuments[0])">{{ t('publicPortal.legal.viewTermsButton') }}</button>
    <button type="button" class="btn btn-outline" @click="openDocument(privacyDocument || fallbackDocuments[1])">{{ t('publicPortal.legal.viewPrivacyButton') }}</button>
  </div>

  <Teleport to="body">
    <Transition name="agreement-fade">
      <div
        v-if="dialogVisible"
        class="fixed inset-0 z-[140] flex items-center justify-center overflow-y-auto bg-gray-950/60 p-4 backdrop-blur-sm"
      >
        <div role="dialog" aria-modal="true" aria-labelledby="agreement-dialog-title" class="w-full max-w-[600px] overflow-hidden rounded-lg border border-[var(--border)] bg-[var(--card)] text-[var(--foreground)]">
          <div class="border-b border-[var(--border)] bg-[var(--card)] px-6 py-6">
            <div class="flex items-start gap-4">
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-[var(--border)] bg-[var(--surface-2)] text-[var(--brand-text)]">
                <Icon name="shield" size="md" />
              </span>
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <h2 id="agreement-dialog-title" class="text-xl font-bold tracking-normal text-[var(--foreground)]">
                    {{ t('legal.loginAgreementPrompt.dialogTitle') }}
                  </h2>
                  <span
                    v-if="updatedAt"
                    class="pill pill-ghost text-[var(--muted-foreground)]"
                  >
                    {{ updatedAt }}
                  </span>
                </div>
                <p class="mt-2 text-sm leading-6 text-[var(--muted-foreground)]">
                  {{
                    t('legal.loginAgreementPrompt.dialogDescription', {
                      date: updatedAt || t('legal.loginAgreementPrompt.recently'),
                    })
                  }}
                </p>
              </div>
            </div>
          </div>

          <div class="max-h-[58vh] overflow-y-auto px-6 py-5">
            <div class="mb-3 flex items-center justify-between gap-3">
              <p class="text-sm font-semibold text-[var(--foreground)]">{{ t('legal.loginAgreementPrompt.relatedDocuments') }}</p>
            </div>
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <button
                v-for="(doc, index) in displayedDocuments"
                :key="doc.id || doc.title"
                type="button"
                :aria-pressed="selectedDocument === doc"
                @click="selectedDocumentId = doc.id || doc.title"
                class="group flex min-h-[72px] w-full items-center gap-3 rounded-lg border border-[var(--border)] bg-[var(--card)] px-4 py-3 text-left transition-colors hover:bg-[var(--surface-2)]"
              >
                <span class="flex h-8 w-8 shrink-0 items-center justify-center text-[var(--muted-foreground)]">
                  <Icon :name="documentIcon(index, doc.title)" size="sm" />
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm font-semibold text-[var(--foreground)]">{{ localizedDocumentTitle(doc) }}</span>
                </span>
                <span class="flex h-8 w-8 shrink-0 items-center justify-center text-[var(--muted-foreground)] group-hover:text-[var(--foreground)]">
                  <Icon name="document" size="sm" />
                </span>
              </button>
            </div>
            <section v-if="selectedDocument" class="mt-5 border-t border-[var(--border)] pt-5" aria-labelledby="agreement-document-title">
              <h3 id="agreement-document-title" class="mb-3 text-lg font-semibold">{{ localizedDocumentTitle(selectedDocument) }}</h3>
              <div
                v-if="selectedDocumentIsTemplate"
                data-testid="agreement-template-notice"
                class="mb-3 rounded-lg border border-amber-200 bg-amber-50/80 p-3 text-xs text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100"
              >
                {{ t('publicPortal.legal.templateNoticeBody') }}
              </div>
              <div class="agreement-document-content" v-html="renderedDocument"></div>
              <RouterLink :to="documentRoute(selectedDocument)" target="_blank" rel="noopener noreferrer" class="btn btn-outline mt-4">
                {{ t('publicPortal.legal.openFullDocument') }}
              </RouterLink>
            </section>
          </div>

          <div v-if="enabled" class="border-t border-[var(--border)] bg-[var(--surface-2)] px-6 py-4">
            <div class="grid grid-cols-2 gap-3">
              <button
                type="button"
                class="btn btn-outline px-4 py-3"
                @click="emit('reject')"
              >
                {{ t('legal.loginAgreementPrompt.reject') }}
              </button>
              <button
                type="button"
                class="btn btn-primary px-4 py-3"
                @click="emit('accept')"
              >
                {{ t('legal.loginAgreementPrompt.accept') }}
              </button>
            </div>
          </div>
          <div v-else class="border-t border-[var(--border)] bg-[var(--surface-2)] px-6 py-4">
            <button
              type="button"
              class="btn btn-outline w-full px-4 py-3"
              @click="emit('close')"
            >
              {{ t('common.close') }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import {
  getLegalSlotTemplateKeys,
  isPrivacyLegalDocument,
  isTermsLegalDocument,
  legalDocumentRouteParam,
} from '@/features/public/legal'
import type { LoginAgreementDocument } from '@/types'

const { t, locale } = useI18n()

function localizedDocumentTitle(doc: LoginAgreementDocument): string {
  const rawTitle = (doc.title || '').trim()
  const activeLocale = String(locale.value || '').toLowerCase()
  const slotKeys = getLegalSlotTemplateKeys(doc.id || '')
  if (slotKeys && (!rawTitle || (!activeLocale.startsWith('zh') && /[\u4e00-\u9fff]/.test(rawTitle)))) {
    return t(slotKeys.titleKey)
  }
  return rawTitle
}

const props = withDefaults(defineProps<{
  enabled?: boolean
  accepted: boolean
  documents: LoginAgreementDocument[]
  mode: 'modal' | 'checkbox' | string
  updatedAt?: string
  visible: boolean
}>(), {
  enabled: true,
  updatedAt: ''
})

const emit = defineEmits<{
  accept: []
  reject: []
  open: []
  close: []
}>()

const dialogVisible = computed(() => props.visible)
const documents = computed(() => props.documents.filter((doc) => doc.id?.trim() || doc.title?.trim()))
const termsDocument = computed(() => documents.value.find((doc) => isTermsLegalDocument(doc)))
const privacyDocument = computed(() => documents.value.find((doc) => isPrivacyLegalDocument(doc)))
const fallbackDocuments = computed<LoginAgreementDocument[]>(() => [
  {
    id: 'terms',
    title: t('publicPortal.legal.slotTitles.terms'),
    content_md: t('publicPortal.legal.promptSummary.terms')
  },
  {
    id: 'privacy',
    title: t('publicPortal.legal.slotTitles.privacy'),
    content_md: t('publicPortal.legal.promptSummary.privacy')
  }
])
const displayedDocuments = computed(() => [
  ...documents.value,
  ...(!termsDocument.value ? [fallbackDocuments.value[0]] : []),
  ...(!privacyDocument.value ? [fallbackDocuments.value[1]] : [])
])
const selectedDocumentId = ref('')
const selectedDocument = computed(() =>
  displayedDocuments.value.find((doc) => (doc.id || doc.title) === selectedDocumentId.value) || displayedDocuments.value[0]
)
const selectedFallbackContent = computed(() => {
  const selected = selectedDocument.value
  if (!selected) return ''
  if (isTermsLegalDocument(selected)) return fallbackDocuments.value[0].content_md
  if (isPrivacyLegalDocument(selected)) return fallbackDocuments.value[1].content_md
  const slotKeys = getLegalSlotTemplateKeys(selected.id || '')
  return slotKeys ? t(slotKeys.templateKey) : ''
})
const selectedDocumentIsTemplate = computed(() => {
  const selected = selectedDocument.value
  return Boolean(selected && !selected.content_md?.trim() && selectedFallbackContent.value)
})
const renderedDocument = computed(() => {
  const selected = selectedDocument.value
  const content = selected?.content_md?.trim() || selectedFallbackContent.value || t('legal.empty')
  return DOMPurify.sanitize(marked.parse(content, { async: false, breaks: true, gfm: true }))
})
const updatedAt = computed(() => props.updatedAt || '')
const accepted = computed(() => props.accepted)

function openDocument(doc: LoginAgreementDocument): void {
  selectedDocumentId.value = doc.id || doc.title
  emit('open')
}

function documentRoute(doc: LoginAgreementDocument) {
  return {
    name: 'LegalDocument',
    params: {
      documentId: legalDocumentRouteParam(doc),
    },
  }
}

function handleCheckboxChange(event: Event): void {
  const checked = (event.target as HTMLInputElement).checked
  if (checked) {
    emit('accept')
  } else {
    emit('reject')
  }
}

function documentIcon(index: number, title: string): 'document' | 'shield' | 'globe' | 'cog' {
  const normalizedTitle = title.toLowerCase()
  if (
    normalizedTitle.includes('policy') ||
    normalizedTitle.includes('privacy') ||
    title.includes('政策') ||
    title.includes('隐私')
  ) {
    return 'shield'
  }
  if (
    normalizedTitle.includes('country') ||
    normalizedTitle.includes('region') ||
    title.includes('国家') ||
    title.includes('地区')
  ) {
    return 'globe'
  }
  if (index === 3) {
    return 'cog'
  }
  return 'document'
}
</script>

<style scoped>
.agreement-document-content { line-height: 1.75; overflow-wrap: anywhere; }
.agreement-document-content :deep(h1),
.agreement-document-content :deep(h2),
.agreement-document-content :deep(h3) { margin: 1em 0 0.5em; font-weight: 600; }
.agreement-document-content :deep(p) { margin-bottom: 0.75em; }
.agreement-document-content :deep(ul) { list-style: disc; padding-left: 1.5em; }
.agreement-document-content :deep(ol) { list-style: decimal; padding-left: 1.5em; }
.agreement-document-content :deep(a) { color: var(--brand-text); text-decoration: underline; }
.agreement-document-content :deep(pre) { overflow-x: auto; padding: 0.75em; background: var(--surface-2); }
.agreement-document-content :deep(img) { max-width: 100%; height: auto; }
.agreement-document-content :deep(table) { display: block; max-width: 100%; overflow-x: auto; }
.auth-legal-actions { display: flex; gap: 8px; margin-top: 10px; }
.auth-legal-actions .btn { flex: 1; min-height: 32px; padding: 5px 10px; font-size: 12px; }
:is(button, a).btn {
  min-height: 40px;
  border-radius: 8px;
  background-image: none;
  box-shadow: none;
  transform: none;
  transition: background-color 150ms ease, border-color 150ms ease;
}

.btn.btn-primary { background: var(--primary); color: var(--primary-foreground); }
.btn.btn-primary:hover:not(:disabled) { background: color-mix(in srgb, var(--primary) 90%, var(--card)); }
.btn.btn-outline { border-color: var(--border); background: var(--card); color: var(--foreground); }
.btn.btn-outline:hover:not(:disabled) { background: var(--surface-2); }

div.card {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--card);
  color: var(--foreground);
  box-shadow: none;
}

button:focus-visible, a:focus-visible, summary:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 3px;
}

input[type='checkbox'] { accent-color: var(--brand); }


.agreement-fade-enter-active,
.agreement-fade-leave-active {
  transition: opacity 0.18s ease;
}

.agreement-fade-enter-from,
.agreement-fade-leave-to {
  opacity: 0;
}

.agreement-fade-enter-active > div,
.agreement-fade-leave-active > div {
  transition: transform 0.18s ease, opacity 0.18s ease;
}

.agreement-fade-enter-from > div,
.agreement-fade-leave-to > div {
  opacity: 0;
  transform: translateY(8px) scale(0.98);
}
@media (prefers-reduced-motion: reduce) {
  :is(button, a).btn, :is(input, select, textarea).input, .fade-enter-active, .fade-leave-active,
  .agreement-fade-enter-active, .agreement-fade-leave-active,
  .agreement-fade-enter-active > div, .agreement-fade-leave-active > div {
    transition: none;
  }
}
</style>
