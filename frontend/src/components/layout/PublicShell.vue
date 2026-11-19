<script setup lang="ts">
import PublicHeader from './PublicHeader.vue'

withDefaults(
  defineProps<{
    prose?: boolean
    padded?: boolean
  }>(),
  {
    prose: false,
    padded: true
  }
)
</script>

<template>
  <div class="flex min-h-screen flex-col bg-oh-canvas text-oh-primary">
    <PublicHeader />

    <div v-if="$slots.subbar" class="border-b border-oh-default bg-oh-subtle">
      <div class="mx-auto max-w-[1200px] px-4 py-2.5 sm:px-6 xl:border-x xl:border-oh-default">
        <slot name="subbar" />
      </div>
    </div>

    <main class="flex flex-1 flex-col">
      <div
        :class="[
          'mx-auto flex w-full max-w-[1200px] flex-1 flex-col xl:border-x xl:border-oh-default',
          padded ? 'px-4 py-8 sm:px-6 lg:py-12' : ''
        ]"
      >
        <div :class="[prose ? 'mx-auto w-full max-w-[68ch]' : 'w-full flex-1']">
          <slot />
        </div>
      </div>
    </main>

    <footer
      v-if="$slots.footer"
      class="border-t border-oh-default bg-oh-subtle text-xs text-oh-muted"
    >
      <div class="mx-auto max-w-[1200px] px-4 py-5 sm:px-6 xl:border-x xl:border-oh-default">
        <slot name="footer" />
      </div>
    </footer>
  </div>
</template>
