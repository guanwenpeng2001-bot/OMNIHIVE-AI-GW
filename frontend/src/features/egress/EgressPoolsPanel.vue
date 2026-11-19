<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Badge, Button, Card, Input } from '@/components/ui'
import { connectionsV2Api } from '@/features/connections/api'
import AsyncStateBanner from '@/features/connections/components/AsyncStateBanner.vue'
import { mapApiErrorToAsyncContext } from '@/features/connections/rules'
import type { AsyncErrorContext, AsyncPageState, ConnectionPool } from '@/features/connections/types'

const props = defineProps<{
  workspaceId?: string
}>()

const pools = ref<ConnectionPool[]>([])
const newPoolName = ref('')
const creating = ref(false)
const asyncState = ref<AsyncPageState | null>('initial_load')
const asyncError = ref<AsyncErrorContext | null>(null)

async function loadPools() {
  asyncState.value = pools.value.length > 0 ? 'background_refresh' : 'initial_load'
  asyncError.value = null
  try {
    const page = await connectionsV2Api.listPools({ limit: 100 }, props.workspaceId)
    pools.value = page.items
    asyncState.value = page.items.length === 0 ? 'empty' : null
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Failed to load v2 egress resource pools.')
    asyncState.value = mapped.state
    asyncError.value = mapped
  }
}

async function handleCreatePool() {
  if (!newPoolName.value.trim() || creating.value) return
  creating.value = true
  asyncError.value = null
  try {
    const created = await connectionsV2Api.createPool(
      { name: newPoolName.value.trim(), max_concurrency: 10 },
      props.workspaceId
    )
    pools.value = [...pools.value, created]
    newPoolName.value = ''
    asyncState.value = 'confirmed'
  } catch (err) {
    const mapped = mapApiErrorToAsyncContext(err, 'Failed to create pool.')
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
        <div class="flex items-center gap-2">
          <h2 class="text-sm font-semibold text-oh-fg">BE-04 Resource Pools</h2>
          <Badge tone="accent" variant="subtle">v2</Badge>
        </div>
        <p class="text-xs text-oh-muted">
          Workspace-scoped egress & connection pools used by the 5-step Connection Wizard.
        </p>
      </div>
      <div class="flex items-center gap-2">
        <Input
          v-model="newPoolName"
          size="sm"
          placeholder="New pool name"
        />
        <Button
          size="sm"
          variant="primary"
          :loading="creating"
          :disabled="!newPoolName.trim()"
          @click="handleCreatePool"
        >
          Create Pool
        </Button>
        <Button size="sm" variant="ghost" @click="loadPools">
          Refresh
        </Button>
      </div>
    </div>

    <AsyncStateBanner
      v-if="asyncState"
      :state="asyncState"
      :error="asyncError"
      confirmed-label="Pool created"
      empty-title="No v2 resource pools yet"
      empty-description="Create a pool to assign upstream connections and egress policies."
      @retry="loadPools"
    />

    <div v-if="pools.length > 0" class="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
      <div
        v-for="pool in pools"
        :key="pool.id"
        class="flex items-center justify-between rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2.5 text-xs"
      >
        <span class="font-medium text-oh-fg">{{ pool.name }}</span>
        <span class="font-mono tabular-nums text-oh-muted">{{ pool.id }}</span>
      </div>
    </div>
  </Card>
</template>
