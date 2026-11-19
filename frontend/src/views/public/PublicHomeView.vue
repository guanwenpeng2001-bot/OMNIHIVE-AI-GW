<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import PublicShell from '@/components/layout/PublicShell.vue'
import Button from '@/components/ui/Button.vue'
import Badge from '@/components/ui/Badge.vue'
import { useAuthStore } from '@/stores/auth'
import { listPublicModels } from '@/features/public/api'
import { getPublicStatus, type PublicStatusResponse } from '@/features/public/api'

const authStore = useAuthStore()
const modelCount = ref<number | null>(null)
const providerCount = ref<number | null>(null)
const status = ref<PublicStatusResponse | null>(null)

onMounted(async () => {
  try {
    const page = await listPublicModels({ limit: 100, sort: 'name' })
    modelCount.value = page.items?.length ?? null
    providerCount.value = new Set((page.items ?? []).map((item) => item.provider).filter(Boolean)).size || null
  } catch {
    modelCount.value = null
    providerCount.value = null
  }
  try {
    status.value = await getPublicStatus()
  } catch {
    status.value = null
  }
})

const ctaTarget = () => (authStore.isAuthenticated ? '/dashboard' : '/login')
</script>

<template>
  <PublicShell>
    <section class="relative overflow-hidden border-b border-oh-default">
      <div class="pointer-events-none absolute inset-y-0 right-0 hidden w-1/2 lg:block" aria-hidden="true">
        <svg viewBox="0 0 400 400" class="h-full w-full text-oh-accent/10">
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
            <RouterLink :to="ctaTarget()">
              <Button variant="primary" size="lg">Get started</Button>
            </RouterLink>
            <RouterLink to="/models">
              <Button variant="secondary" size="lg">Browse models</Button>
            </RouterLink>
          </div>
          <dl class="mt-10 flex flex-wrap gap-8 font-mono text-sm">
            <div>
              <dt class="text-[11px] uppercase tracking-wider text-oh-muted">Models</dt>
              <dd class="mt-1 text-2xl tabular-nums text-oh-primary">{{ modelCount ?? '—' }}</dd>
            </div>
            <div>
              <dt class="text-[11px] uppercase tracking-wider text-oh-muted">Providers</dt>
              <dd class="mt-1 text-2xl tabular-nums text-oh-primary">{{ providerCount ?? '—' }}</dd>
            </div>
            <div>
              <dt class="text-[11px] uppercase tracking-wider text-oh-muted">Status</dt>
              <dd class="mt-2">
                <Badge v-if="status?.stale" tone="warning" variant="subtle" dot>Stale</Badge>
                <Badge v-else-if="status" tone="success" variant="subtle" dot>Operational</Badge>
                <Badge v-else tone="neutral" variant="subtle" dot>Unknown</Badge>
              </dd>
            </div>
          </dl>
        </div>
        <div class="hidden items-center justify-center lg:flex">
          <div class="w-full max-w-md border border-oh-default bg-oh-surface p-4 font-mono text-xs">
            <div class="mb-3 flex items-center gap-1.5" aria-hidden="true">
              <span class="h-2.5 w-2.5 rounded-full border border-oh-default" />
              <span class="h-2.5 w-2.5 rounded-full border border-oh-default" />
              <span class="h-2.5 w-2.5 rounded-full border border-oh-default" />
            </div>
            <pre class="overflow-x-auto leading-6 text-oh-secondary"><code><span class="text-oh-muted">$</span> curl https://api.omnihive.dev/v1/chat/completions \
  -H "Authorization: Bearer $OMNIHIVE_KEY" \
  -d '{"model":"openai/gpt-6.2-sol",
       "messages":[{"role":"user",
                    "content":"hello"}]}'

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
        <RouterLink to="/models"><Button variant="secondary">See all models</Button></RouterLink>
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
