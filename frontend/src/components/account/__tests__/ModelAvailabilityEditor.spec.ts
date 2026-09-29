import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ModelAvailabilityEditor from '../ModelAvailabilityEditor.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import type { ModelAvailabilityState } from '@/api/admin/modelAvailability'

const api = vi.hoisted(() => ({ get: vi.fn(), declare: vi.fn(), revoke: vi.fn() }))
vi.mock('@/api/admin/modelAvailability', () => ({ modelAvailabilityAPI: api }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const saved: ModelAvailabilityState = { identity_revision: '9007199254740993', rules_revision: '4', snapshot_revision: '8', declarations: [] }
const mountEditor = () => mount(ModelAvailabilityEditor, {
  props: { accountId: 12, active: true, configurationDirty: false, connectionContext: 'saved', savedRevision: 0 },
  global: { stubs: { ConfirmDialog: true } }
})
async function fill(wrapper: ReturnType<typeof mountEditor>) {
  await wrapper.get('[data-testid="load-availability"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="availability-model"]').setValue('private-target')
  await wrapper.get('[data-testid="availability-reason"]').setValue('deployment owner verified')
}
beforeEach(() => {
  api.get.mockReset().mockResolvedValue(saved)
  api.declare.mockReset().mockResolvedValue({ ...saved, rules_revision: '5' })
  api.revoke.mockReset().mockResolvedValue({ ...saved, rules_revision: '5' })
})

describe('account availability declarations', () => {
  it('does not offer the unwired videos family for new declarations', async () => {
    const wrapper = mountEditor()
    await wrapper.get('[data-testid="load-availability"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="availability-endpoint"] option').map(option => option.attributes('value'))).toEqual([
      'chat_completions', 'responses', 'messages', 'generate_content', 'images'
    ])
    wrapper.unmount()
  })

  it('preserves and revokes a legacy videos declaration using its original endpoint', async () => {
    const declaration = { account_id: 12, model: 'legacy-video-target', endpoint: 'videos', identity_revision: 1, rules_revision: 4, reason: 'legacy video basis', confirmed_by: 3, confirmed_at: '2026-01-01T00:00:00Z', evidence_kind: 'administrator_declaration' as const, valid: true }
    api.get.mockResolvedValueOnce({ ...saved, declarations: [declaration] })
    const wrapper = mountEditor()
    await wrapper.get('[data-testid="load-availability"]').trigger('click')
    await flushPromises()
    const evidence = wrapper.get('[data-testid="availability-evidence"]')
    expect(evidence.text()).toContain('legacy-video-target · videos')
    expect(evidence.text()).toContain('legacy video basis')
    expect(wrapper.get('[data-testid="revoke-availability"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="availability-endpoint"]').setValue('images')
    await wrapper.get('[data-testid="availability-reason"]').setValue('legacy access removed')
    await wrapper.get('[data-testid="revoke-availability"]').trigger('click')
    expect(api.revoke).not.toHaveBeenCalled()
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(true)
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(api.revoke).toHaveBeenCalledOnce()
    expect(api.revoke).toHaveBeenCalledWith(12, {
      identity_revision: saved.identity_revision, rules_revision: saved.rules_revision, snapshot_revision: saved.snapshot_revision,
      model: 'legacy-video-target', endpoint: 'videos', reason: 'legacy access removed', confirmed: true
    })
    expect(api.declare).not.toHaveBeenCalled()
    expect(wrapper.emitted('changed')).toHaveLength(1)
    wrapper.unmount()
  })

  it('requires basis and a separate confirmation, preserves opaque CAS, and omits optional expiry', async () => {
    const wrapper = mountEditor()
    expect(api.get).not.toHaveBeenCalled()
    await fill(wrapper)
    await wrapper.get('[data-testid="availability-reason"]').setValue('  ')
    expect(wrapper.get('[data-testid="declare-availability"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="availability-reason"]').setValue('verified basis')
    await wrapper.get('[data-testid="declare-availability"]').trigger('click')
    expect(api.declare).not.toHaveBeenCalled()
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(true)
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(api.declare).toHaveBeenCalledOnce()
    expect(api.declare).toHaveBeenCalledWith(12, {
      identity_revision: saved.identity_revision, rules_revision: '4', snapshot_revision: '8',
      model: 'private-target', endpoint: 'chat_completions', reason: 'verified basis', confirmed: true
    })
    expect(wrapper.emitted('changed')).toHaveLength(1)
    wrapper.unmount()
  })

  it('validates optional expiry and exact IDs, and converts local time to RFC3339 only after confirmation', async () => {
    const wrapper = mountEditor()
    await fill(wrapper)
    await wrapper.get('[data-testid="availability-model"]').setValue('private-*')
    expect(wrapper.get('[data-testid="declare-availability"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="availability-model"]').setValue('private-target')
    await wrapper.get('[data-testid="availability-expiry"]').setValue('2000-01-01T12:00')
    expect(wrapper.get('[data-testid="availability-expiry"]').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('[data-testid="declare-availability"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="availability-expiry"]').setValue('2099-01-01T12:00')
    await wrapper.get('[data-testid="declare-availability"]').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(api.declare.mock.calls[0][1].expires_at).toBe(new Date('2099-01-01T12:00').toISOString())
    wrapper.unmount()
  })

  it('shows endpoint-specific administrator, probe, expired and revoked evidence; revocation needs basis and confirmation', async () => {
    const declaration = { account_id: 12, model: 'private-target', endpoint: 'responses', identity_revision: 1, rules_revision: 4, reason: 'old basis', confirmed_by: 3, confirmed_at: '2026-01-01T00:00:00Z', evidence_kind: 'administrator_declaration' as const, valid: true }
    api.get.mockResolvedValueOnce({ ...saved, declarations: [declaration,
      { ...declaration, endpoint: 'messages', evidence_kind: 'probe_success', valid: false },
      { ...declaration, endpoint: 'images', expires_at: '2000-01-01T00:00:00Z', valid: false },
      { ...declaration, endpoint: 'videos', revoked_at: '2026-01-02T00:00:00Z', valid: false }
    ] })
    const wrapper = mountEditor()
    await wrapper.get('[data-testid="load-availability"]').trigger('click')
    await flushPromises()
    for (const label of ['administrator_declaration', 'probe_success', 'expired', 'revoked', 'invalid']) expect(wrapper.text()).toContain(`modelAvailability.${label}`)
    expect(wrapper.findAll('[data-testid="revoke-availability"]')).toHaveLength(3)
    expect(wrapper.get('[data-testid="revoke-availability"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="availability-reason"]').setValue('access removed')
    await wrapper.get('[data-testid="revoke-availability"]').trigger('click')
    expect(api.revoke).not.toHaveBeenCalled()
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(api.revoke.mock.calls[0]).toEqual([12, expect.objectContaining({ model: 'private-target', endpoint: 'responses', reason: 'access removed' })])
    wrapper.unmount()
  })

  it('requires reloading and another confirmation after conflict, without retrying automatically', async () => {
    api.declare.mockRejectedValueOnce({ status: 409 })
    const wrapper = mountEditor()
    await fill(wrapper)
    await wrapper.get('[data-testid="declare-availability"]').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(wrapper.text()).toContain('modelAvailability.conflict')
    expect(wrapper.find('[data-testid="declare-availability"]').exists()).toBe(false)
    expect(api.get).toHaveBeenCalledTimes(1)
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    expect(api.declare).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it.each(['account', 'close', 'configuration'])('discards pending confirmation on %s changes', async change => {
    const wrapper = mountEditor()
    await fill(wrapper)
    await wrapper.get('[data-testid="declare-availability"]').trigger('click')
    await wrapper.setProps(change === 'account' ? { accountId: 13 } : change === 'close' ? { active: false } : { configurationDirty: true })
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    expect(api.declare).not.toHaveBeenCalled()
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(false)
    wrapper.unmount()
  })

  it.each([false, true])('ignores a late mutation response (reject=%s) after reopening a different account', async reject => {
    let finish!: (value: unknown) => void
    api.declare.mockImplementationOnce(() => new Promise((resolve, rejectPromise) => { finish = reject ? rejectPromise : resolve }))
    const wrapper = mountEditor()
    await fill(wrapper)
    await wrapper.get('[data-testid="declare-availability"]').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true, accountId: 13 })
    finish(reject ? { status: 409 } : saved)
    await flushPromises()
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="availability-model"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('ignores late reads after connection changes and blocks unsaved configuration', async () => {
    let finish!: (value: unknown) => void
    api.get.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountEditor()
    await wrapper.get('[data-testid="load-availability"]').trigger('click')
    await wrapper.setProps({ configurationDirty: true, connectionContext: 'changed' })
    finish(saved)
    await flushPromises()
    expect(wrapper.get('[data-testid="load-availability"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="availability-model"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
