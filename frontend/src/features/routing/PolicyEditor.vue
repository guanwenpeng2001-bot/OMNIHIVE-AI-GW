<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Badge, Button, Input } from '@/components/ui'
import { templates, type PolicyForm } from './policy'

const props = defineProps<{
  disabled: boolean
  saving: boolean
  saveDisabled: boolean
  candidateCount: number | null
  validation: string | null
}>()
const emit = defineEmits<{ save: [] }>()
const form = defineModel<PolicyForm>({ required: true })
const { t } = useI18n()
const ruleFields = ['models', 'regions'] as const
const modeOptions = ['inherit', 'allow_list', 'deny_all'] as const
const gateOptions = ['inherit', 'allow', 'deny'] as const
</script>

<template>
  <form class="space-y-4" @submit.prevent="emit('save')">
    <fieldset :disabled="props.disabled" class="min-w-0 space-y-4">
      <section class="routing-panel space-y-4">
        <h3 class="routing-heading">{{ t('routing.definition') }}</h3>
        <Input v-model="form.name" :label="t('routing.name')" :maxlength="120" required />
        <div class="space-y-2.5">
          <div class="flex flex-wrap items-baseline justify-between gap-2">
            <h4 class="routing-label">{{ t('routing.templates') }}</h4>
            <span class="text-xs text-oh-secondary">
              {{ t('routing.candidatesAffected') }}
              <b class="ml-1 font-mono oh-tabular" :aria-label="candidateCount === null ? t('routing.unknown') : undefined">
                {{ candidateCount ?? '—' }}
              </b>
            </span>
          </div>
          <div class="grid grid-cols-1 gap-2 min-[350px]:grid-cols-2">
            <button
              v-for="template in templates"
              :key="template"
              type="button"
              class="routing-template"
              :class="{ 'routing-template-active': form.template === template }"
              :aria-pressed="form.template === template"
              @click="form.template = form.template === template ? '' : template"
            >
              <span class="block text-[13px] font-semibold">{{ t('routing.template.' + template) }}</span>
              <span class="mt-1 block text-xs text-oh-secondary">{{ t('routing.templateDetail.' + template) }}</span>
            </button>
          </div>
          <p v-if="candidateCount === null" class="routing-hint">{{ t('routing.candidateEvidence') }}</p>
        </div>
      </section>

      <section class="routing-panel space-y-4">
        <h3 class="routing-heading">{{ t('routing.constraints') }}</h3>
        <div class="grid gap-4 sm:grid-cols-2">
          <div v-for="field in ruleFields" :key="field" class="min-w-0 space-y-2">
            <label class="routing-field">
              <span>{{ t('routing.' + field) }}</span>
              <select v-model="form[field].mode" class="routing-control">
                <option v-for="mode in modeOptions" :key="mode" :value="mode">{{ t('routing.mode.' + mode) }}</option>
              </select>
            </label>
            <Input v-if="form[field].mode === 'allow_list'" v-model="form[field].items" :label="t('routing.' + field + 'Values')" mono required />
          </div>

          <label class="routing-field">
            <span>{{ t('routing.retention') }}</span>
            <select v-model="form.retention" class="routing-control">
              <option value="inherit">{{ t('routing.mode.inherit') }}</option>
              <option value="strict_zero">{{ t('routing.strictZero') }}</option>
            </select>
          </label>
          <label class="routing-field">
            <span>{{ t('routing.training') }}</span>
            <select v-model="form.training" class="routing-control">
              <option value="inherit">{{ t('routing.mode.inherit') }}</option>
              <option value="prohibited">{{ t('routing.prohibited') }}</option>
            </select>
          </label>
          <Input v-model="form.rate" :label="t('routing.priceCap')" :hint="t('routing.priceUnit')" mono />
          <label class="routing-field">
            <span>{{ t('routing.funding') }}</span>
            <select v-model="form.funding" class="routing-control">
              <option v-for="gate in gateOptions" :key="gate" :value="gate">{{ t('routing.mode.' + gate) }}</option>
            </select>
            <span class="routing-hint">{{ t('routing.fundingHint') }}</span>
          </label>
        </div>
        <details class="border-t border-oh-subtle pt-3">
          <summary class="oh-focus-ring cursor-pointer text-sm font-medium text-oh-primary">{{ t('routing.advanced') }}</summary>
          <div class="mt-4 grid gap-4 sm:grid-cols-2">
            <Input v-model="form.capabilities" :label="t('routing.capabilities')" :hint="t('routing.commaList')" mono />
            <Input v-model="form.pools" :label="t('routing.poolIds')" :hint="t('routing.commaList')" mono />
            <Input v-model="form.deniedProviders" :label="t('routing.blockedProviders')" :hint="t('routing.commaList')" mono />
            <label class="routing-field">
              <span>{{ t('routing.expansion') }}</span>
              <select v-model="form.expansion" class="routing-control">
                <option v-for="gate in gateOptions" :key="gate" :value="gate">{{ t('routing.mode.' + gate) }}</option>
              </select>
            </label>
            <label class="routing-field">
              <span>{{ t('routing.endpointFallback') }}</span>
              <select v-model="form.endpoint" class="routing-control">
                <option v-for="gate in gateOptions" :key="gate" :value="gate">{{ t('routing.mode.' + gate) }}</option>
              </select>
            </label>
            <label class="routing-check self-end">
              <input v-model="form.requireQuota" type="checkbox" />
              {{ t('routing.requireQuota') }}
            </label>
            <div class="space-y-2">
              <label class="routing-check">
                <input v-model="form.overrideProviders" type="checkbox" />
                {{ t('routing.overrideProviders') }}
              </label>
              <Input v-if="form.overrideProviders" v-model="form.providers" :label="t('routing.providerOrder')" :hint="t('routing.commaList')" mono />
            </div>
            <div class="space-y-2">
              <label class="routing-check">
                <input v-model="form.restrictFallbackModels" type="checkbox" />
                {{ t('routing.restrictFallback') }}
              </label>
              <Input v-if="form.restrictFallbackModels" v-model="form.fallbackModels" :label="t('routing.fallbackModels')" :hint="t('routing.commaList')" mono />
            </div>
          </div>
        </details>
      </section>
    </fieldset>
    <div class="flex flex-wrap items-center justify-between gap-3">
      <p v-if="validation" role="alert" class="text-sm text-oh-status-danger-fg">{{ t('routing.validation.' + validation) }}</p>
      <Badge v-else-if="!saveDisabled" tone="neutral">{{ t('routing.unsaved') }}</Badge>
      <Button class="ml-auto" type="submit" variant="primary" :loading="saving" :disabled="saveDisabled">
        {{ t('routing.save') }}
      </Button>
    </div>
  </form>
</template>
