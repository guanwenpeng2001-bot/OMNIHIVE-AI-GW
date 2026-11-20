<template>
  <a
    class="inline-flex max-w-full shrink-0 items-center gap-1 rounded-md border border-[var(--border)] bg-[var(--surface-2)] px-2 py-1 text-xs font-normal text-[var(--brand-text)] transition-colors hover:underline focus:outline-none focus:ring-2 focus:ring-[var(--ring)] focus:ring-offset-2 focus:ring-offset-[var(--card)]"
    href="https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/blob/dev/deploy/mihomo/README.md"
    target="_blank"
    rel="noopener noreferrer"
  >
    <span class="truncate">{{ t('admin.proxies.setupGuide') }}</span>
    <Icon name="externalLink" size="xs" />
  </a>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
</script>
