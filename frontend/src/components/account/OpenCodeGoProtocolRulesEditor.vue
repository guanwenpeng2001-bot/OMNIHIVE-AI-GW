<template>
  <div class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-3">
    <div class="mb-2 flex items-center justify-between gap-2">
      <label class="input-label mb-0">{{ t('admin.accounts.opencodeGo.protocolRules.title') }}</label>
      <button
        type="button"
        class="text-xs text-primary-600 hover:text-primary-700 dark:text-primary-400"
        @click="restoreDefaults"
      >
        {{ t('admin.accounts.opencodeGo.protocolRules.restoreDefaults') }}
      </button>
    </div>
    <p class="input-hint mb-2">{{ t(platform === 'commandcode' ? 'admin.accounts.platformDetails.commandcodeDescription' : 'admin.accounts.opencodeGo.protocolRules.hint') }}</p>
    <div v-if="rows.length > 0" class="mb-2 space-y-2">
      <div
        v-for="(row, index) in rows"
        :key="getRowKey(row)"
        class="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--surface-2)] p-2"
      >
        <input
          v-model="row.pattern"
          type="text"
          class="input min-w-0 basis-40 flex-1 font-mono text-sm"
          :placeholder="t('admin.accounts.opencodeGo.protocolRules.patternPlaceholder')"
          :data-testid="`opencode-go-protocol-pattern-${index}`"
        />
        <select
          v-model="row.protocol"
          class="input w-44 max-w-full shrink-0 font-mono text-xs"
          :data-testid="`opencode-go-protocol-select-${index}`"
        >
          <option value="chat_completions">
            {{ t('admin.accounts.cnProviders.apiProtocol.chatCompletions') }}
          </option>
          <option value="responses">
            {{ t('admin.accounts.cnProviders.apiProtocol.responses') }}
          </option>
          <option value="anthropic">
            {{ t('admin.accounts.cnProviders.apiProtocol.anthropic') }}
          </option>
        </select>
        <button
          type="button"
          class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
          :aria-label="t('admin.accounts.opencodeGo.protocolRules.remove')"
          @click="removeRow(index)"
        >
          <Icon name="trash" size="sm" />
        </button>
      </div>
    </div>
    <div
      class="mb-2 flex items-center gap-2 rounded-lg border border-dashed px-3 py-2 text-xs text-gray-500 border-[var(--border)] bg-[var(--card)] dark:text-gray-400"
      data-testid="opencode-go-protocol-fallback"
    >
      <span class="flex-1 font-mono">*</span>
      <span>{{ t('admin.accounts.opencodeGo.protocolRules.fallback') }}</span>
    </div>
    <button
      type="button"
      class="w-full rounded-lg border-2 border-dashed px-4 py-2 text-gray-600 transition-colors hover:text-gray-700 border-[var(--border)] dark:text-gray-400 hover:border-[var(--border)] dark:hover:text-gray-300"
      data-testid="opencode-go-protocol-add-rule"
      @click="addRow"
    >
      {{ t('admin.accounts.opencodeGo.protocolRules.add') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { createStableObjectKeyResolver } from '@/utils/stableObjectKey'
import {
  cloneOpenCodeGoProtocolRules,
  DEFAULT_COMMANDCODE_PROTOCOL_RULES,
  defaultOpenCodeProtocolRules,
  type OpenCodeAccountMode,
  type OpenCodeGoProtocolRule
} from '@/components/account/credentialsBuilder'

const props = withDefaults(defineProps<{
  rows: OpenCodeGoProtocolRule[]
  platform?: string
  plan?: OpenCodeAccountMode
}>(), {
  plan: 'go',
  platform: 'opencode_go'
})

const emit = defineEmits<{
  (e: 'update:rows', rows: OpenCodeGoProtocolRule[]): void
}>()

const { t } = useI18n()
const getRowKey = createStableObjectKeyResolver<OpenCodeGoProtocolRule>('opencode-go-protocol-rule')

const addRow = () => {
  emit('update:rows', [...props.rows, { pattern: '', protocol: 'chat_completions' }])
}

const removeRow = (index: number) => {
  emit('update:rows', props.rows.filter((_, i) => i !== index))
}

const restoreDefaults = () => {
  emit('update:rows', cloneOpenCodeGoProtocolRules(props.platform === 'commandcode' ? DEFAULT_COMMANDCODE_PROTOCOL_RULES : defaultOpenCodeProtocolRules(props.plan)))
}
</script>
