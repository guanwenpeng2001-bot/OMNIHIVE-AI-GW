<script setup lang="ts">
import type { UiStepItem, UiStepStatus } from './types'
import { cn } from './utils'

export interface StepperProps {
  steps: UiStepItem[]
  currentStepId: string
  orientation?: 'horizontal' | 'vertical'
  allowStepClick?: boolean
  ariaLabel?: string
}

const props = withDefaults(defineProps<StepperProps>(), {
  orientation: 'horizontal',
  allowStepClick: true,
  ariaLabel: 'Progress steps'
})

const emit = defineEmits<{
  'update:currentStepId': [stepId: string]
  select: [step: UiStepItem, index: number]
}>()

function resolveStatus(step: UiStepItem, index: number): UiStepStatus {
  if (step.status) return step.status
  const activeIndex = props.steps.findIndex((candidate) => candidate.id === props.currentStepId)
  if (index === activeIndex) return 'current'
  if (activeIndex !== -1 && index < activeIndex) return 'complete'
  return 'upcoming'
}

function canActivate(step: UiStepItem, index: number): boolean {
  if (!props.allowStepClick || step.disabled) return false
  const status = resolveStatus(step, index)
  return status !== 'upcoming'
}

function handleStepClick(step: UiStepItem, index: number): void {
  if (!canActivate(step, index)) return
  emit('update:currentStepId', step.id)
  emit('select', step, index)
}
</script>

<template>
  <nav :aria-label="ariaLabel">
    <ol
      :class="
        cn(
          orientation === 'vertical'
            ? 'flex flex-col gap-3'
            : 'grid gap-2 sm:grid-cols-2 lg:flex lg:items-center lg:gap-3'
        )
      "
    >
      <li
        v-for="(step, index) in steps"
        :key="step.id"
        :class="
          cn(
            'min-w-0',
            orientation === 'horizontal' && 'lg:flex lg:flex-1 lg:items-center lg:gap-3'
          )
        "
      >
        <button
          type="button"
          :disabled="!canActivate(step, index) && step.id !== currentStepId"
          :aria-current="step.id === currentStepId ? 'step' : undefined"
          :class="
            cn(
              'oh-focus-ring group flex w-full items-start gap-3 rounded-oh-md border p-2.5 text-left transition-colors duration-150',
              step.id === currentStepId
                ? 'border-oh-accent bg-oh-selected'
                : resolveStatus(step, index) === 'complete'
                  ? 'border-oh-default bg-oh-surface hover:border-oh-strong'
                  : resolveStatus(step, index) === 'error'
                    ? 'border-oh-status-danger-border bg-oh-status-danger-bg/50'
                    : resolveStatus(step, index) === 'warning'
                      ? 'border-oh-status-warn-border bg-oh-status-warn-bg/50'
                      : 'border-oh-subtle bg-oh-subtle/50 opacity-75',
              canActivate(step, index) ? 'cursor-pointer' : 'cursor-default'
            )
          "
          @click="handleStepClick(step, index)"
        >
          <span
            :class="
              cn(
                'oh-tabular mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-xs font-semibold',
                resolveStatus(step, index) === 'current' &&
                  'border-transparent bg-oh-accent text-oh-accent-ink',
                resolveStatus(step, index) === 'complete' &&
                  'border-oh-status-ok-border bg-oh-status-ok-bg text-oh-status-ok-fg',
                resolveStatus(step, index) === 'error' &&
                  'border-oh-status-danger-border bg-oh-status-danger-bg text-oh-status-danger-fg',
                resolveStatus(step, index) === 'warning' &&
                  'border-oh-status-warn-border bg-oh-status-warn-bg text-oh-status-warn-fg',
                resolveStatus(step, index) === 'upcoming' &&
                  'border-oh-default bg-oh-surface text-oh-muted'
              )
            "
            aria-hidden="true"
          >
            <svg
              v-if="resolveStatus(step, index) === 'complete'"
              viewBox="0 0 14 14"
              class="h-3.5 w-3.5"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <path stroke-linecap="round" stroke-linejoin="round" d="M2.75 7.25l2.75 2.75 5.75-6" />
            </svg>
            <span v-else>{{ index + 1 }}</span>
          </span>

          <span class="min-w-0 flex-1">
            <span class="flex items-center gap-1.5">
              <span class="truncate text-xs font-semibold text-oh-primary">
                {{ step.title }}
              </span>
              <span v-if="step.optional" class="font-mono text-[10px] text-oh-muted">
                (Optional)
              </span>
            </span>
            <span v-if="step.description" class="mt-0.5 block truncate text-[11px] text-oh-muted">
              {{ step.description }}
            </span>
          </span>
        </button>

        <div
          v-if="orientation === 'horizontal' && index < steps.length - 1"
          class="hidden h-px w-4 shrink-0 bg-oh-default lg:block"
          aria-hidden="true"
        />
      </li>
    </ol>
  </nav>
</template>
