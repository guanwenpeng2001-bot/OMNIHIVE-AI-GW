<template>
  <div class="px-0.5">
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
              {{ doc.title }}
            </RouterLink>
            <span v-if="index < displayedDocuments.length - 1">{{ t('legal.loginAgreementPrompt.documentSeparator') }}</span>
          </template>
        </p>
      </div>
    </div>
  </div>

  <div class="auth-legal-actions">
    <button type="button" class="btn btn-outline" @click="openDocument(termsDocument || fallbackDocuments[0])">查看服务条款</button>
    <button type="button" class="btn btn-outline" @click="openDocument(privacyDocument || fallbackDocuments[1])">查看隐私政策</button>
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
                  <span class="block truncate text-sm font-semibold text-[var(--foreground)]">{{ doc.title }}</span>
                </span>
                <span class="flex h-8 w-8 shrink-0 items-center justify-center text-[var(--muted-foreground)] group-hover:text-[var(--foreground)]">
                  <Icon name="document" size="sm" />
                </span>
              </button>
            </div>
            <section v-if="selectedDocument" class="mt-5 border-t border-[var(--border)] pt-5" aria-labelledby="agreement-document-title">
              <h3 id="agreement-document-title" class="mb-3 text-lg font-semibold">{{ selectedDocument.title }}</h3>
              <div class="agreement-document-content" v-html="renderedDocument"></div>
              <RouterLink :to="documentRoute(selectedDocument)" target="_blank" rel="noopener noreferrer" class="btn btn-outline mt-4">
                新窗口打开完整文档
              </RouterLink>
            </section>
          </div>

          <div class="border-t border-[var(--border)] bg-[var(--surface-2)] px-6 py-4">
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
import { getLocale } from '@/i18n'
import type { LoginAgreementDocument } from '@/types'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  accepted: boolean
  documents: LoginAgreementDocument[]
  mode: 'modal' | 'checkbox' | string
  updatedAt?: string
  visible: boolean
}>(), {
  updatedAt: ''
})

const emit = defineEmits<{
  accept: []
  reject: []
  open: []
}>()

const dialogVisible = computed(() => props.visible)
const documents = computed(() => props.documents.filter((doc) => doc.title.trim()))
const termsDocument = computed(() => documents.value.find((doc) => doc.id === 'terms' || /terms|服务条款/i.test(doc.title)))
const privacyDocument = computed(() => documents.value.find((doc) => doc.id === 'privacy' || /privacy|隐私/i.test(doc.title)))
const fallbackDocuments = computed<LoginAgreementDocument[]>(() => getLocale() === 'zh'
  ? [
      {
        id: 'terms',
        title: 'OmniHive 服务条款',
        content_md: '## 服务与账户\nOmniHive 提供模型接口访问及账户、用量管理。请保护密码与 API 密钥，遵守适用法律及所选提供方的使用要求。\n\n## 费用与支持\n使用付费功能前，请核对站点展示的价格、额度与有效期。服务、账单及账户问题请联系站点管理员。\n\n完整条款还包括第三方服务、可用性及更新说明，请通过下方链接阅读。'
      },
      {
        id: 'privacy',
        title: 'OmniHive 隐私政策',
        content_md: '## 信息与用途\n账户信息、请求及用量记录用于验证身份、提供服务、计费和保障安全。模型请求可能由所选上游提供方处理，请避免提交无关的个人信息或敏感凭证。\n\n## 存储与联系\n浏览器存储用于登录状态、偏好及注册流程。具体保存期限与数据管理安排请向站点运营方了解。\n\n完整政策还包括服务提供方及信息查询、更正和删除的联系说明，请通过下方链接阅读。'
      }
    ]
  : [
      {
        id: 'terms',
        title: 'OmniHive Terms of Service',
        content_md: '## Service and accounts\nOmniHive provides model API access, account management, and usage information. Protect passwords and API keys and follow applicable laws and your selected provider’s requirements.\n\n## Charges and support\nReview displayed prices, quotas, and validity periods before using paid features. Contact the site administrator about services, billing, or your account.\n\nThe full terms also cover third-party services, availability, and updates. Read them using the link below.'
      },
      {
        id: 'privacy',
        title: 'OmniHive Privacy Policy',
        content_md: '## Information and purposes\nAccount information, requests, and usage records support verification, services, billing, and security. Your selected upstream provider may process model requests. Avoid submitting unrelated personal information or sensitive credentials.\n\n## Storage and contact\nBrowser storage supports sign-in, preferences, and registration. Ask the site operator about retention periods and data management.\n\nThe full policy also covers providers and contacting the operator for access, correction, or deletion. Read it using the link below.'
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
const renderedDocument = computed(() => {
  const selected = selectedDocument.value
  const fallback = selected === termsDocument.value ? fallbackDocuments.value[0] : selected === privacyDocument.value ? fallbackDocuments.value[1] : undefined
  const content = selected?.content_md?.trim() || fallback?.content_md || t('legal.empty')
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
      documentId: doc.id || doc.title,
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
