import { describe, expect, it } from 'vitest'
import {
  buildCredentialInputPayload,
  canProceedFromStep,
  createInitialWizardState,
  getWizardStepStatuses,
  hydrateWizardFromResource,
  invalidatePreview,
  invalidateVerification
} from '../wizardState'
import type { ConnectionResource } from '../types'

const stepLabels = {
  provider: { title: 'Provider', description: 'Select provider' },
  credentials: { title: 'Credentials', description: 'Set secret' },
  verify: { title: 'Verify', description: 'Run probe' },
  preview: { title: 'Preview', description: 'Assign pools' },
  activate: { title: 'Activate', description: 'Confirm version' }
}

describe('connections/wizardState', () => {
  it('enforces step 1 provider & endpoint validation', () => {
    const state = createInitialWizardState('create')
    expect(canProceedFromStep(state, 'provider').valid).toBe(false)

    state.provider = 'openai'
    state.authType = 'api_key'
    state.name = 'OpenAI Main'
    state.endpoint = 'https://api.openai.com'
    expect(canProceedFromStep(state, 'provider').valid).toBe(true)
  })

  it('enforces explicit keep / set / clear credential semantics', () => {
    const createState = createInitialWizardState('create')
    expect(buildCredentialInputPayload(createState)).toEqual({
      operation: 'set',
      type: 'api_key',
      secret: ''
    })
    expect(canProceedFromStep(createState, 'credentials').valid).toBe(false)

    createState.credentialSecret = 'sk-test-1234'
    expect(canProceedFromStep(createState, 'credentials').valid).toBe(true)

    const editState = createInitialWizardState('edit')
    editState.credentialMode = 'keep'
    expect(buildCredentialInputPayload(editState)).toEqual({ operation: 'keep' })
    expect(canProceedFromStep(editState, 'credentials').valid).toBe(true)

    editState.credentialMode = 'clear'
    expect(buildCredentialInputPayload(editState)).toEqual({ operation: 'clear' })
    expect(canProceedFromStep(editState, 'credentials').valid).toBe(false)
  })

  it('blocks preview progression when verification is missing, stale, or failed', () => {
    const now = Date.parse('2026-11-19T10:00:00Z')
    const state = createInitialWizardState('edit')
    expect(canProceedFromStep(state, 'verify', null, now).valid).toBe(false)

    state.verification = {
      verification_id: 'ver_1',
      models: ['gpt-4o'],
      verified_at: '2026-11-19T09:55:00Z',
      expires_at: '2026-11-19T10:30:00Z',
      checks: [{ name: 'authorization', state: 'passed', reason: null }]
    }
    expect(canProceedFromStep(state, 'verify', null, now).valid).toBe(true)

    const stale = invalidateVerification(state)
    expect(stale.verificationStale).toBe(true)
    expect(canProceedFromStep(stale, 'verify', null, now).valid).toBe(false)
  })

  it('hydrates from ConnectionResource and marks stepper warning when stale', () => {
    const resource: ConnectionResource = {
      id: 'conn_1',
      workspace_id: 'ws_1',
      version: '7',
      state: 'draft',
      provider: 'anthropic',
      schema_version: '1',
      auth_type: 'api_key',
      name: 'Anthropic Backup',
      endpoint: 'https://api.anthropic.com',
      network_policy_id: 'default',
      credential: {
        version: '3',
        type: 'api_key',
        state: 'verified',
        last_four: '9876',
        updated_at: '2026-11-19T09:00:00Z'
      },
      last_verification: {
        verification_id: 'ver_2',
        models: ['claude-sonnet-4'],
        verified_at: '2026-11-19T08:00:00Z',
        expires_at: '2026-11-19T09:00:00Z',
        checks: [{ name: 'authorization', state: 'passed', reason: null }]
      },
      model_ids: ['claude-sonnet-4'],
      pool_ids: ['pool_1'],
      limits: { concurrency: 8 },
      quota: { state: 'fresh', remaining: '100', last_success_at: '2026-11-19T09:00:00Z' },
      allowed_actions: ['update', 'verify', 'activate'],
      redactions: []
    }
    let state = hydrateWizardFromResource(createInitialWizardState('edit'), resource)
    state.currentStep = 'preview'
    state.preview = {
      preview_id: 'prev_1',
      adds: [],
      removes: [],
      blocked: [],
      expires_at: '2026-11-19T12:00:00Z',
      items: [{ id: 'conn_1', base_version: '7', state: 'applicable' }]
    }
    state = invalidatePreview(state)
    const steps = getWizardStepStatuses(state, stepLabels, Date.parse('2026-11-19T10:00:00Z'))
    expect(steps.find((s) => s.id === 'verify')?.status).toBe('warning')
    expect(steps.find((s) => s.id === 'preview')?.status).toBe('current')
  })
})
