<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useRoute } from 'vue-router'

// One canonical directory URL. The price view retains its scenario calculator
// while sharing the same catalog API, publication filters and price presentation.
const route = useRoute()
const Models = defineAsyncComponent(() => import('./PublicModelsView.vue'))
const Pricing = defineAsyncComponent(() => import('./PublicPricingView.vue'))
const currentView = computed(() => route.query.view === 'pricing' ? Pricing : Models)
</script>

<template>
  <component :is="currentView" />
</template>
