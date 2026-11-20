<script setup lang="ts">
import type { UiBreadcrumbItem } from './types'
import { cn } from './utils'

export interface PageHeaderProps {
  title: string
  eyebrow?: string
  description?: string
  breadcrumbs?: UiBreadcrumbItem[]
  bordered?: boolean
}

withDefaults(defineProps<PageHeaderProps>(), {
  eyebrow: undefined,
  description: undefined,
  breadcrumbs: undefined,
  bordered: true
})
</script>

<template>
  <header :class="cn('space-y-3', bordered && 'border-b border-oh-subtle pb-4')">
    <nav
      v-if="breadcrumbs && breadcrumbs.length > 0"
      aria-label="Breadcrumb"
      class="flex flex-wrap items-center gap-1.5 text-xs text-oh-muted"
    >
      <template v-for="(crumb, crumbIndex) in breadcrumbs" :key="`${crumb.label}-${crumbIndex}`">
        <router-link
          v-if="crumb.to"
          :to="crumb.to"
          class="oh-focus-ring rounded-oh-xs hover:text-oh-primary transition-colors"
        >
          {{ crumb.label }}
        </router-link>
        <span v-else class="text-oh-secondary font-medium">{{ crumb.label }}</span>
        <span
          v-if="crumbIndex < breadcrumbs.length - 1"
          class="font-mono text-oh-muted/60"
          aria-hidden="true"
        >
          /
        </span>
      </template>
    </nav>

    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0 flex-1 space-y-1">
        <p v-if="eyebrow" class="oh-eyebrow">{{ eyebrow }}</p>
        <div class="flex flex-wrap items-center gap-2.5">
          <h1 class="truncate text-2xl font-semibold tracking-tight text-oh-primary">
            {{ title }}
          </h1>
          <slot name="badges" />
        </div>
        <p v-if="description" class="max-w-3xl text-sm leading-5 text-oh-secondary">
          {{ description }}
        </p>
      </div>

      <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2 shrink-0">
        <slot name="actions" />
      </div>
    </div>

    <div v-if="$slots.tabs" class="pt-1">
      <slot name="tabs" />
    </div>
  </header>
</template>
