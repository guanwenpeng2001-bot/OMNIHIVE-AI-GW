<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import PublicHeader from './PublicHeader.vue'
import BrandMark from './BrandMark.vue'

withDefaults(
  defineProps<{
    wide?: boolean
  }>(),
  {
    wide: false
  }
)

const appStore = useAppStore()
const { locale } = useI18n()
const isZh = computed(() => String(locale?.value || 'zh').toLowerCase().startsWith('zh'))
const brand = computed(
  () =>
    appStore.brandConfig ?? {
      siteName: appStore.siteName || 'OmniHive',
      siteLogo: appStore.siteLogo || '',
      subtitle: isZh.value ? '统一 AI 推理网关' : 'Unified AI Inference Gateway'
    }
)
</script>

<template>
  <div class="flex min-h-screen flex-col bg-oh-canvas text-oh-primary">
    <main class="flex flex-1 flex-col">
      <div class="mx-auto flex w-full max-w-[1200px] flex-1 items-center justify-center px-4 py-8 sm:px-6 sm:py-12 xl:border-x xl:border-oh-default">
        <section
          data-testid="auth-card"
          :class="[
            'w-full rounded-oh-lg border border-oh-default bg-oh-surface p-5 shadow-oh-xs sm:p-7',
            wide ? 'max-w-xl' : 'max-w-[440px]'
          ]"
        >
          <div class="mb-5 flex items-center justify-between gap-3 border-b border-oh-subtle pb-4">
            <div class="min-w-0">
              <BrandMark
                :site-name="brand.siteName"
                :site-logo="brand.siteLogo"
                size="md"
              />
              <p class="mt-1 truncate text-xs text-oh-muted">
                {{ brand.subtitle }}
              </p>
            </div>

            <router-link
              to="/home"
              class="oh-focus-ring shrink-0 rounded-oh-sm border border-oh-default bg-oh-subtle px-2.5 py-1 font-mono text-[11px] font-medium text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary"
            >
              {{ isZh ? '← 首页' : '← HOME' }}
            </router-link>
          </div>

          <slot />

          <div
            v-if="$slots.footer"
            class="mt-5 border-t border-oh-subtle pt-4 text-center text-[13px] text-oh-secondary"
          >
            <slot name="footer" />
          </div>
        </section>
      </div>
    </main>

    <PublicHeader class="-order-1" />
  </div>
</template>

<style scoped>
:deep(.auth-title) {
  font-size: 1.25rem;
  line-height: 1.75rem;
  font-weight: 600;
  letter-spacing: -0.015em;
  color: var(--oh-fg-primary);
}

:deep(.auth-intro) {
  margin-top: 0.25rem;
  font-size: 0.8125rem;
  line-height: 1.35rem;
  color: var(--oh-fg-secondary);
}

:deep(.auth-error) {
  border: 1px solid var(--oh-danger-border);
  background: var(--oh-danger-bg);
  color: var(--oh-danger-fg);
  border-radius: var(--oh-radius-md);
  padding: 0.625rem 0.75rem;
  font-size: 0.8125rem;
  line-height: 1.25rem;
}

:deep(.auth-field-error) {
  margin-top: 0.25rem;
  font-size: 0.75rem;
  line-height: 1rem;
  color: var(--oh-danger-fg);
}

:deep(.auth-summary) {
  border: 1px solid var(--oh-border-default);
  background: var(--oh-bg-subtle);
  color: var(--oh-fg-primary);
  border-radius: var(--oh-radius-md);
  padding: 0.75rem 0.875rem;
  font-size: 0.8125rem;
  line-height: 1.35rem;
  overflow-wrap: anywhere;
}

:deep(.auth-actions) {
  display: flex;
  flex-wrap: wrap;
  align-items: stretch;
  gap: 0.625rem;
  padding-top: 0.25rem;
}

:deep(.auth-actions > *) {
  flex: 1;
  min-width: 110px;
}

:deep(.auth-divider) {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin: 1rem 0;
  font-family: var(--oh-font-mono);
  font-size: 0.6875rem;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--oh-fg-muted);
}

:deep(.auth-divider::before),
:deep(.auth-divider::after) {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--oh-border-subtle);
}

:deep(label.input-label) {
  display: block;
  margin-bottom: 0.375rem;
  font-size: 0.75rem;
  line-height: 1rem;
  font-weight: 500;
  color: var(--oh-fg-primary);
}

:deep(:is(input, select, textarea).input) {
  width: 100%;
  min-width: 0;
  min-height: 2.25rem;
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--oh-border-default);
  border-radius: var(--oh-radius-md);
  background: var(--oh-bg-surface);
  color: var(--oh-fg-primary);
  font-size: 0.875rem;
  line-height: 1.25rem;
  box-shadow: none;
  transition: border-color 120ms ease, background-color 120ms ease;
}

:deep(:is(input, select, textarea).input::placeholder) {
  color: var(--oh-fg-muted);
}

:deep(:is(input, select, textarea).input:hover:not(:disabled)) {
  border-color: var(--oh-border-strong);
}

:deep(:is(input, select, textarea).input:focus) {
  border-color: var(--oh-accent);
  outline: 2px solid var(--oh-focus-ring);
  outline-offset: 2px;
  box-shadow: none;
}

:deep(:is(input, select, textarea).input:disabled) {
  background: var(--oh-bg-subtle);
  color: var(--oh-fg-disabled);
  cursor: not-allowed;
}

:deep(:is(input, select, textarea).input.input-error) {
  border-color: var(--oh-danger-border);
}

:deep(:is(button, a).btn) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  min-height: 2.25rem;
  padding: 0.5rem 0.875rem;
  border-radius: var(--oh-radius-md);
  font-size: 0.8125rem;
  line-height: 1.25rem;
  font-weight: 500;
  box-shadow: none;
  background-image: none;
  transform: none;
  transition: background-color 120ms ease, border-color 120ms ease, color 120ms ease;
}

:deep(:is(button, a).btn-primary) {
  border: 1px solid transparent;
  background: var(--oh-accent);
  color: var(--oh-accent-ink);
  font-weight: 600;
}

:deep(:is(button, a).btn-primary:hover:not(:disabled)) {
  background: var(--oh-accent-hover);
}

:deep(:is(button, a).btn-outline),
:deep(:is(button, a).btn-secondary) {
  border: 1px solid var(--oh-border-default);
  background: var(--oh-bg-surface);
  color: var(--oh-fg-primary);
}

:deep(:is(button, a).btn-outline:hover:not(:disabled)),
:deep(:is(button, a).btn-secondary:hover:not(:disabled)) {
  border-color: var(--oh-border-strong);
  background: var(--oh-bg-hover);
}

:deep(input[type='checkbox']) {
  accent-color: var(--oh-accent);
}

:deep(div.card) {
  border: 1px solid var(--oh-border-default);
  border-radius: var(--oh-radius-md);
  background: var(--oh-bg-surface);
  color: var(--oh-fg-primary);
  box-shadow: none;
}
</style>
