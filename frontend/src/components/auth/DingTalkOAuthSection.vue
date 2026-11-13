<template>
  <div class="space-y-4">
    <button type="button" :disabled="disabled" class="btn btn-secondary btn-outline w-full" @click="startLogin">
      <svg
        class="icon mr-2"
        viewBox="0 0 24 24"
        xmlns="http://www.w3.org/2000/svg"
        width="20"
        height="20"
        aria-hidden="true"
        style="flex-shrink: 0"
      >
        <rect x="1" y="1" width="22" height="22" rx="5" fill="none" stroke="currentColor" />
        <text
          x="12"
          y="17"
          font-family="sans-serif"
          font-size="13"
          font-weight="bold"
          fill="currentColor"
          text-anchor="middle"
        >D</text>
      </svg>
      {{ t('auth.dingtalk.signIn') }}
    </button>

    <div v-if="showDivider" class="flex items-center gap-3">
      <div class="h-px flex-1 bg-[var(--border)]"></div>
      <span class="text-xs text-[var(--muted-foreground)]">
        {{ t('auth.oauthOrContinue') }}
      </span>
      <div class="h-px flex-1 bg-[var(--border)]"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { OAuthLoginStart } from '@/api/auth'
import { resolveAffiliateReferralCode, storeOAuthAffiliateCode } from '@/utils/oauthAffiliate'

const props = withDefaults(defineProps<{
  disabled?: boolean
  affCode?: string
  showDivider?: boolean
}>(), {
  showDivider: true
})
const emit = defineEmits<{
  start: [request: OAuthLoginStart]
}>()

const route = useRoute()
const { t } = useI18n()

function startLogin(): void {
  const redirectTo = (route.query.redirect as string) || '/dashboard'
  storeOAuthAffiliateCode(resolveAffiliateReferralCode(props.affCode, route.query.aff, route.query.aff_code))
  emit('start', { provider: 'dingtalk', params: { redirect: redirectTo } })
}
</script>

<style scoped>
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

button:focus-visible, a:focus-visible, summary:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 3px;
}

input[type='checkbox'] { accent-color: var(--brand); }

@media (prefers-reduced-motion: reduce) {
  :is(button, a).btn, :is(input, select, textarea).input, .fade-enter-active, .fade-leave-active,
  .agreement-fade-enter-active, .agreement-fade-leave-active,
  .agreement-fade-enter-active > div, .agreement-fade-leave-active > div {
    transition: none;
  }
}
</style>
