<script setup lang="ts">
import { computed } from 'vue'
import { sanitizeUrl } from '@/utils/url'
import { resolveBrandSiteName } from '@/router/brandConfig'

export interface BrandMarkProps {
  siteName?: string | null
  siteLogo?: string | null
  size?: 'sm' | 'md' | 'lg'
  showWordmark?: boolean
  tierLabel?: string
  tierClass?: string
}

const props = withDefaults(defineProps<BrandMarkProps>(), {
  siteName: undefined,
  siteLogo: undefined,
  size: 'md',
  showWordmark: true,
  tierLabel: '',
  tierClass: ''
})

const displayName = computed(() => resolveBrandSiteName(props.siteName))
const sanitizedLogo = computed(() =>
  props.siteLogo
    ? sanitizeUrl(props.siteLogo, { allowRelative: true, allowDataUrl: true })
    : ''
)

const boxClasses = computed(() => {
  switch (props.size) {
    case 'sm':
      return 'h-6 w-6'
    case 'lg':
      return 'h-10 w-10'
    default:
      return 'h-7 w-7'
  }
})

const wordmarkClasses = computed(() => {
  switch (props.size) {
    case 'sm':
      return 'text-[13.5px]'
    case 'lg':
      return 'text-lg'
    default:
      return 'text-[15px]'
  }
})
</script>

<template>
  <span class="inline-flex min-w-0 items-center gap-2.5">
    <img
      v-if="sanitizedLogo"
      :src="sanitizedLogo"
      :alt="displayName"
      :class="[boxClasses, 'shrink-0 rounded-oh-sm object-contain']"
    />
    <svg
      v-else
      :class="[boxClasses, 'shrink-0']"
      viewBox="0 0 32 32"
      fill="none"
      aria-hidden="true"
    >
      <path
        d="M16 2 28.1 9v14L16 30 3.9 23V9Z"
        fill="#141519"
        stroke="#262b38"
        stroke-width="1"
      />
      <path
        d="M16 5.6 25 10.8v10.4L16 26.4 7 21.2V10.8Z"
        stroke="#f59e0b"
        stroke-width="1.75"
      />
      <path d="M16 10.2 21 13.1v5.8L16 21.8l-5-2.9v-5.8Z" fill="#f59e0b" />
      <circle cx="16" cy="16" r="2.1" fill="#141008" />
    </svg>

    <span v-if="showWordmark" class="inline-flex min-w-0 items-center gap-2">
      <span
        :class="[
          wordmarkClasses,
          'truncate font-semibold tracking-tight text-oh-primary'
        ]"
      >
        {{ displayName }}
      </span>
      <span
        v-if="tierLabel"
        :class="[
          'shrink-0 whitespace-nowrap rounded-oh-xs border border-oh-default bg-oh-subtle px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wider text-oh-muted',
          tierClass
        ]"
      >
        {{ tierLabel }}
      </span>
    </span>
  </span>
</template>
