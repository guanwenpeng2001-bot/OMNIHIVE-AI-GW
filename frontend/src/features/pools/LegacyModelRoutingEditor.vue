<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { Button, Tooltip } from '@/components/ui'

interface Account {
  id: number
  name: string
}
interface Rule {
  pattern: string
  accounts: Account[]
}

const enabled = defineModel<boolean>({ required: true })
const props = defineProps<{
  rules: Rule[]
  renderKey: (rule: Rule) => string
  searchKey: (rule: Rule) => string
  keywords: Record<string, string>
  results: Record<string, Account[]>
  showResults: Record<string, boolean>
}>()
const emit = defineEmits<{
  add: []
  remove: [rule: Rule]
  selectAccount: [rule: Rule, account: Account]
  removeAccount: [rule: Rule, accountId: number]
  search: [rule: Rule, keyword: string]
  focusSearch: [rule: Rule]
}>()
const { t } = useI18n()
</script>

<template>
  <section class="space-y-3">
    <div class="flex flex-wrap items-center gap-2">
      <h3 class="text-sm font-semibold text-oh-primary">{{ t('admin.groups.modelRouting.title') }}</h3>
      <Tooltip :content="t('admin.groups.modelRouting.tooltip')">
        <button type="button" :aria-label="t('admin.groups.modelRouting.title')" class="oh-focus-ring rounded-oh-xs text-oh-muted">
          <Icon name="questionCircle" size="sm" />
        </button>
      </Tooltip>
      <Toggle v-model="enabled" :aria-label="t('admin.groups.modelRouting.title')" />
      <span class="text-xs text-oh-secondary">
        {{ t(enabled ? 'admin.groups.modelRouting.enabled' : 'admin.groups.modelRouting.disabled') }}
      </span>
    </div>
    <p class="text-xs text-oh-muted">{{ t(enabled ? 'admin.groups.modelRouting.noRulesHint' : 'admin.groups.modelRouting.disabledHint') }}</p>
    <template v-if="enabled">
      <div v-for="rule in props.rules" :key="renderKey(rule)" class="space-y-3 rounded-oh-md border border-oh-default bg-oh-surface p-3">
        <div class="flex items-end gap-2">
          <label class="min-w-0 flex-1">
            <span class="input-label text-xs">{{ t('admin.groups.modelRouting.modelPattern') }}</span>
            <input v-model="rule.pattern" type="text" class="input font-mono text-sm" :placeholder="t('admin.groups.modelRouting.modelPatternPlaceholder')" />
          </label>
          <Button class="shrink-0" :aria-label="t('admin.groups.modelRouting.removeRule')" @click="emit('remove', rule)">
            <Icon name="trash" size="sm" />
          </Button>
        </div>
        <div class="space-y-2">
          <div v-if="rule.accounts.length" class="flex flex-wrap gap-1.5">
            <span v-for="account in rule.accounts" :key="account.id" class="inline-flex max-w-full items-center gap-1.5 rounded-oh-md border border-oh-default bg-oh-subtle px-2 py-1 text-xs">
              <span class="min-w-0 break-words">{{ account.name }}</span>
              <button
                type="button" class="oh-focus-ring rounded-oh-xs text-oh-muted" :aria-label="t('routing.removeAccount', { name: account.name })"
                @click="emit('removeAccount', rule, account.id)"
              ><Icon name="x" size="xs" /></button>
            </span>
          </div>
          <div class="account-search-container relative">
            <label class="block">
              <span class="input-label text-xs">{{ t('admin.groups.modelRouting.accounts') }}</span>
              <input
                :value="keywords[searchKey(rule)]" type="text" class="input text-sm"
                :placeholder="t('admin.groups.modelRouting.searchAccountPlaceholder')"
                @input="emit('search', rule, ($event.target as HTMLInputElement).value)"
                @focus="emit('focusSearch', rule)"
              />
            </label>
            <div
              v-if="showResults[searchKey(rule)] && results[searchKey(rule)]?.length"
              class="absolute z-50 mt-1 max-h-48 w-full overflow-auto rounded-oh-md border border-oh-default bg-oh-elevated shadow-oh-md"
            >
              <button
                v-for="account in results[searchKey(rule)]" :key="account.id" type="button"
                class="block w-full break-words px-3 py-2 text-left text-sm hover:bg-oh-hover disabled:opacity-50"
                :disabled="rule.accounts.some(selected => selected.id === account.id)"
                @click="emit('selectAccount', rule, account)"
              >
                {{ account.name }} <span class="ml-1 font-mono text-xs text-oh-muted oh-tabular">#{{ account.id }}</span>
              </button>
            </div>
          </div>
          <p class="text-xs text-oh-muted">{{ t('admin.groups.modelRouting.accountsHint') }}</p>
        </div>
      </div>
      <Button size="sm" @click="emit('add')">{{ t('admin.groups.modelRouting.addRule') }}</Button>
    </template>
  </section>
</template>
