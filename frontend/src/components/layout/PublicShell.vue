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
      <div
        data-testid="public-shell-subbar"
        class="mx-auto w-full max-w-[1560px] px-4 py-2.5 sm:px-6 lg:px-8 2xl:px-10 3xl:px-12 min-[1560px]:border-x min-[1560px]:border-oh-default"
      >
        <slot name="subbar" />
      </div>
    </div>

    <main class="flex flex-1 flex-col">
      <div
        data-testid="public-shell-main"
        :class="[
          'mx-auto flex w-full max-w-[1560px] flex-1 flex-col min-[1560px]:border-x min-[1560px]:border-oh-default',
          padded ? 'px-4 py-8 sm:px-6 lg:px-8 lg:py-12 2xl:px-10 3xl:px-12' : ''
        ]"
      >
        <div
          data-testid="public-shell-content"
          :class="[prose ? 'mx-auto w-full max-w-[68ch]' : 'w-full flex-1']"
        >
          <slot />
        </div>
      </div>
    </main>

    <footer
      v-if="$slots.footer"
      class="border-t border-oh-default bg-oh-subtle text-xs text-oh-muted"
    >
      <div
        data-testid="public-shell-footer"
        class="mx-auto w-full max-w-[1560px] px-4 py-5 sm:px-6 lg:px-8 2xl:px-10 3xl:px-12 min-[1560px]:border-x min-[1560px]:border-oh-default"
      >
        <slot name="footer" />
      </div>
    </footer>
  </div>
</template>
