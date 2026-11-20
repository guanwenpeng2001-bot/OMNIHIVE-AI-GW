<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge, Button, Card, Input } from '@/components/ui'
import { connectionsApi } from '@/features/connections/api'
import AsyncStateBanner from '@/features/connections/components/AsyncStateBanner.vue'
import { mapApiErrorToAsyncContext } from '@/features/connections/rules'
import type { AsyncErrorContext, AsyncPageState, ConnectionPool } from '@/features/connections/types'

const props = defineProps<{
  workspaceId?: string
}>()

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').toLowerCase().startsWith('zh'))

const pools = ref<ConnectionPool[]>([])
const newPoolName = ref('')
const newPoolMaxConcurrency = ref('10')
const creating = ref(false)
const asyncState = ref<AsyncPageState | null>('initial_load')
const asyncError = ref<AsyncErrorContext | null>(null)

async function loadPools() {
  asyncState.value = pools.value.length > 0 ? 'background_refresh' : 'initial_load'
  asyncError.value = null
  try {
    const page = await connectionsApi.listPools({ limit: 100 }, props.workspaceId)
    pools.value = page.items
    asyncState.value = page.items.length === 0 ? 'empty' : null
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '加载出站资源池失败。' : 'Failed to load egress resource pools.',
      isZh.value
    )
    asyncState.value = mapped.state
    asyncError.value = mapped
  }
}

async function handleCreatePool() {
  if (!newPoolName.value.trim() || creating.value) return
  creating.value = true
  asyncError.value = null
  try {
    const maxConcurrency = Math.min(
      10000,
      Math.max(1, Math.trunc(Number(newPoolMaxConcurrency.value) || 10))
    )
    const created = await connectionsApi.createPool(
      { name: newPoolName.value.trim(), max_concurrency: maxConcurrency },
      props.workspaceId
    )
    pools.value = [...pools.value, created]
    newPoolName.value = ''
    asyncState.value = 'confirmed'
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(
      err,
      isZh.value ? '创建出站资源池失败。' : 'Failed to create resource pool.',
      isZh.value
    )
    asyncState.value = mapped.state
    asyncError.value = mapped
  } finally {
    creating.value = false
  }
}

onMounted(() => {
  void loadPools()
})
</script>

<template>
  <Card padding="md" class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-sm font-semibold text-oh-fg">
          {{ isZh ? '出站资源池' : 'Egress Resource Pools' }}
        </h2>
        <p class="text-xs text-oh-muted">
          {{
            isZh
              ? '管理工作空间内的上游连接资源池、并发上限与出站调度边界。'
              : 'Workspace-scoped connection resource pools and concurrency boundaries.'
          }}
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Input
          v-model="newPoolName"
          size="sm"
          :placeholder="isZh ? '输入新资源池名称' : 'New pool name'"
        />
        <div class="w-28">
          <Input
            v-model="newPoolMaxConcurrency"
            size="sm"
            type="number"
            mono
            :placeholder="isZh ? '并发上限' : 'Max conc.'"
            :aria-label="isZh ? '最大并发数' : 'Max concurrency'"
          />
        </div>
        <Button
          size="sm"
          variant="primary"
          :loading="creating"
          :disabled="!newPoolName.trim()"
          @click="handleCreatePool"
        >
          {{ isZh ? '新建资源池' : 'Create Pool' }}
        </Button>
        <Button size="sm" variant="ghost" @click="loadPools">
          {{ isZh ? '刷新' : 'Refresh' }}
        </Button>
      </div>
    </div>

    <AsyncStateBanner
      v-if="asyncState"
      :state="asyncState"
      :error="asyncError"
      :confirmed-label="isZh ? '资源池已创建' : 'Resource pool created'"
      :empty-title="isZh ? '暂无出站资源池' : 'No resource pools yet'"
      :empty-description="
        isZh
          ? '新建资源池后，可在上游连接向导或批量配置中绑定连接与并发策略。'
          : 'Create a pool to assign upstream connections and concurrency policies.'
      "
      @retry="loadPools"
    />

    <div v-if="pools.length > 0" class="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
      <div
        v-for="pool in pools"
        :key="pool.id"
        class="flex flex-col gap-1.5 rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2.5 text-xs"
      >
        <div class="flex items-center justify-between gap-2">
          <span class="font-medium text-oh-fg truncate">{{ pool.name }}</span>
          <Badge tone="neutral" variant="outline" mono>#{{ pool.id }}</Badge>
        </div>
        <div class="flex items-center justify-between text-[11px] text-oh-muted tabular-nums">
          <span>
            {{
              isZh
                ? `关联连接：${pool.connection_ids?.length ?? 0}`
                : `Connections: ${pool.connection_ids?.length ?? 0}`
            }}
          </span>
          <span>
            {{
              isZh
                ? `最大并发：${pool.max_concurrency}`
                : `Max concurrency: ${pool.max_concurrency}`
            }}
          </span>
        </div>
      </div>
    </div>
  </Card>
</template>
