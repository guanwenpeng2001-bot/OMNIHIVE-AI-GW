<template>
  <div class="empty-state">
    <!-- OpenCode-style Data-Shape Skeleton + Honeycomb Badge -->
    <div
      class="mb-4 flex flex-col items-center gap-2"
    >
      <div
        class="flex h-12 w-12 items-center justify-center rounded-xl border transition-colors"
        style="border-color: var(--border); background: var(--surface-2); color: var(--muted-foreground)"
      >
        <slot name="icon">
          <component v-if="icon" :is="icon" class="h-6 w-6" aria-hidden="true" />
          <svg
            v-else
            class="h-6 w-6"
            viewBox="0 0 32 32"
            fill="none"
            aria-hidden="true"
          >
            <path d="M16 3 27 9.3v13.4L16 29 5 22.7V9.3Z" stroke="currentColor" stroke-width="1.6" stroke-opacity="0.55" />
            <path d="M16 9.5 21.6 12.7v6.6L16 22.5l-5.6-3.2v-6.6Z" stroke="var(--brand, #f59e0b)" stroke-width="1.5" stroke-opacity="0.8" />
            <circle cx="16" cy="16" r="1.8" fill="var(--brand, #f59e0b)" fill-opacity="0.85" />
          </svg>
        </slot>
      </div>
      <div class="flex w-36 flex-col items-center gap-1 opacity-45" aria-hidden="true">
        <span class="h-1.5 w-full rounded-full" style="background: var(--border)"></span>
        <span class="h-1.5 w-2/3 rounded-full" style="background: var(--border)"></span>
      </div>
    </div>

    <!-- Title -->
    <h3 class="empty-state-title">
      {{ displayTitle }}
    </h3>

    <!-- Description -->
    <p class="empty-state-description">
      {{ description }}
    </p>

    <!-- Action -->
    <div v-if="actionText || $slots.action" class="mt-6">
      <slot name="action">
        <component
          :is="actionTo ? 'RouterLink' : 'button'"
          v-if="actionText"
          :to="actionTo"
          @click="!actionTo && $emit('action')"
          class="btn btn-primary"
        >
          <Icon v-if="actionIcon" name="plus" size="md" class="mr-2" />
          {{ actionText }}
        </component>
      </slot>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Component } from 'vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

interface Props {
  icon?: Component | string
  title?: string
  description?: string
  actionText?: string
  actionTo?: string | object
  actionIcon?: boolean
  message?: string
}

const props = withDefaults(defineProps<Props>(), {
  description: '',
  actionIcon: true
})

const displayTitle = computed(() => props.title || t('common.noData'))

defineEmits(['action'])
</script>
