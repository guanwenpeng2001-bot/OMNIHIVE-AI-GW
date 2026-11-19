<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui'
defineProps<{ next: boolean; previous: boolean; loading: boolean }>()
const emit = defineEmits<{ next: []; previous: [] }>()
const { t } = useI18n()
</script>

<template>
  <div v-if="next || previous" class="flex flex-wrap items-center justify-end gap-2">
    <Button size="sm" variant="secondary" :disabled="loading || !previous" @click="emit('previous')">
      {{ t('wp6.pagination.previous') }}
    </Button>
    <Button size="sm" variant="secondary" :disabled="loading || !next" @click="emit('next')">
      {{ t('wp6.pagination.next') }}
    </Button>
  </div>
</template>
