<script setup lang="ts">
import { Skeleton } from '@/components/ui'
import type { AsyncViewState } from '@/features/shared/asyncState'

defineProps<{ state: AsyncViewState; message?: string }>()

const labels: Record<AsyncViewState, string> = {
  skeleton: '正在加载',
  refreshing: '正在刷新',
  ready: '',
  empty: '暂无记录',
  'zero-filter': '没有符合条件的记录',
  'partial-error': '部分数据暂不可用',
  forbidden: '当前账号没有权限',
  stale: '显示上次成功读取的数据',
  conflict: '状态冲突，请刷新后核对',
  confirmed: '操作已确认',
  unknown: '结果未知，请核对后再继续',
}
</script>

<template>
  <div v-if="state !== 'ready' || message" role="status" class="space-y-2 text-sm text-oh-secondary">
    <Skeleton v-if="state === 'skeleton'" class="h-8 w-full" />
    <p>{{ message || labels[state] }}</p>
  </div>
</template>
