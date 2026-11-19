<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import PublicShell from '@/components/layout/PublicShell.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import Badge from '@/components/ui/Badge.vue'
import { useAuthStore } from '@/stores/auth'
import { publicValue } from '@/features/public/format'
import { listPublicModels, getPublicStatus, type PublicStatusResponse } from '@/features/public/api'

const authStore = useAuthStore()
const modelCount = ref<number | null>(null)
const providerCount = ref<number | null>(null)
const status = ref<PublicStatusResponse | null>(null)

const loadingCatalog = ref(true)
const loadingStatus = ref(true)
const example = `curl https://api.omnihive.dev/v1/chat/completions \\
  -H "Authorization: Bearer $OMNIHIVE_KEY" \\
  -d '{"model":"openai/gpt-6.2-sol",
       "messages":[{"role":"user",
                    "content":"hello"}]}'`

onMounted(() => {
  void listPublicModels({ limit: 100, sort: 'name' }).then((page) => {
    if (page.next_cursor) return
    modelCount.value = page.items.length
    providerCount.value = new Set(page.items.map((item) => item.provider).filter(Boolean)).size
  }).catch(() => {}).finally(() => { loadingCatalog.value = false })

  void getPublicStatus().then((response) => {
    status.value = response
  }).catch(() => {}).finally(() => { loadingStatus.value = false })
})

const ctaTarget = () => (authStore.isAuthenticated ? '/dashboard' : '/login')
</script>

<template>
  <PublicShell>
    <section class="relative overflow-hidden border-b border-oh-default">
      <div class="pointer-events-none absolute inset-y-0 right-0 hidden w-1/2 lg:block" aria-hidden="true">
        <svg viewBox="0 0 400 400" class="h-full w-full text-oh-accent opacity-10">
          <defs>
            <pattern id="comb" width="40" height="34" patternUnits="userSpaceOnUse">
              <path d="M20 1 39 12v22L20 45 1 34V12Z" fill="none" stroke="currentColor" stroke-width="1" />
            </pattern>
          </defs>
          <rect width="400" height="400" fill="url(#comb)" />
        </svg>
      </div>
      <div class="relative grid gap-10 py-16 lg:grid-cols-2 lg:py-24">
        <div>
          <p class="font-mono text-xs uppercase tracking-[0.22em] text-oh-muted">OMNIHIVE · AI GATEWAY</p>
          <h1 class="mt-4 max-w-xl text-4xl font-semibold tracking-tight text-oh-primary sm:text-5xl">
            One API for every model.<br />Built for teams that bill.
          </h1>
          <p class="mt-5 max-w-xl text-base text-oh-secondary">
            Route to hundreds of providers through a single compatible endpoint. Exact pricing,
            explainable routing and per-request receipts — no surprises.
          </p>
          <div class="mt-8 flex flex-wrap items-center gap-3">
            <RouterLink :to="ctaTarget()" class="oh-focus-ring inline-flex h-10 items-center rounded-oh-md bg-oh-accent px-4 text-sm font-semibold text-oh-accent-ink hover:bg-oh-accent-hover active:bg-oh-accent-active">
              Get started
            </RouterLink>
            <RouterLink to="/models" class="oh-focus-ring inline-flex items-center rounded-oh-md border border-oh-default bg-oh-surface px-4 text-sm font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover h-10">Browse models</RouterLink>
          </div>
          <dl class="mt-10 flex flex-wrap gap-8 font-mono text-sm">
            <div>
              <dt class="text-[11px] uppercase tracking-wider text-oh-muted">Models</dt>
              <dd class="mt-1 text-2xl tabular-nums text-oh-primary"><Skeleton v-if="loadingCatalog" label="Loading model count" /><template v-else>{{ modelCount ?? '—' }}</template></dd>
            </div>
            <div>
              <dt class="text-[11px] uppercase tracking-wider text-oh-muted">Providers</dt>
              <dd class="mt-1 text-2xl tabular-nums text-oh-primary"><Skeleton v-if="loadingCatalog" label="Loading provider count" /><template v-else>{{ providerCount ?? '—' }}</template></dd>
            </div>
            <div>
              <dt class="text-[11px] uppercase tracking-wider text-oh-muted">Status</dt>
              <dd class="mt-2">
                <Skeleton v-if="loadingStatus" label="Loading service status" />
                <Badge v-else-if="status?.overall === 'operational'" tone="success" variant="subtle" dot>Operational</Badge>
                <Badge v-else-if="status?.overall === 'outage'" tone="danger" variant="subtle" dot>Outage</Badge>
                <Badge v-else-if="status?.overall === 'degraded' || status?.overall === 'maintenance'" tone="warning" variant="subtle" dot>{{ publicValue(status.overall) }}</Badge>
                <Badge v-else tone="unknown" variant="subtle" dot>—</Badge>
              </dd>
            </div>
          </dl>
        </div>
        <div class="hidden items-center justify-center lg:flex">
          <div class="w-full max-w-md border border-oh-default bg-oh-surface p-4 font-mono text-xs">
            <div class="mb-3 flex justify-end">
              <CopyButton :value="example" variant="secondary" aria-label="Copy API request" />
            </div>
            <pre class="oh-focus-ring overflow-x-auto leading-6 text-oh-secondary" tabindex="0" aria-label="Example API request"><code>{{ example }}

<span class="text-oh-accent-text">HTTP/2 200</span>
<span class="text-oh-muted">x-execution-id:</span> 7ca789de-e632
<span class="text-oh-muted">x-provider:</span> openai</code></pre>
          </div>
        </div>
      </div>
    </section>

    <section class="grid gap-px border-b border-oh-default bg-oh-default sm:grid-cols-3">
      <div v-for="feature in [
        { title: 'Exact billing', body: 'Decimal pricing, per-attempt receipts and a ledger you can reconcile.' },
        { title: 'Explainable routing', body: 'See every candidate, exclusion reason and the final pick for each request.' },
        { title: 'Provider freedom', body: 'OpenAI, Anthropic, Gemini and more behind one compatible surface.' }
      ]" :key="feature.title" class="bg-oh-canvas p-6">
        <h2 class="text-sm font-semibold text-oh-primary">{{ feature.title }}</h2>
        <p class="mt-2 text-sm text-oh-secondary">{{ feature.body }}</p>
      </div>
    </section>

    <section class="py-12">
      <div class="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 class="text-2xl font-semibold text-oh-primary">Start in three steps</h2>
          <p class="mt-2 text-sm text-oh-secondary">Create a key, call the standard endpoint, then watch the request and its receipt.</p>
        </div>
        <RouterLink to="/models" class="oh-focus-ring inline-flex items-center rounded-oh-md border border-oh-default bg-oh-surface px-4 text-sm font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover h-9">See all models</RouterLink>
      </div>
      <ol class="mt-8 grid gap-4 sm:grid-cols-3">
        <li v-for="(step, index) in ['Create a restricted API key', 'Call any supported model', 'Inspect request, route and cost']"
          :key="step" class="border border-oh-default p-5">
          <span class="font-mono text-xs text-oh-muted">0{{ index + 1 }}</span>
          <p class="mt-3 text-sm text-oh-primary">{{ step }}</p>
        </li>
      </ol>
    </section>
  </PublicShell>
</template>
