import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { rotateMock, copyToClipboardMock, showSuccessMock } = vi.hoisted(() => ({
  rotateMock: vi.fn(),
  copyToClipboardMock: vi.fn().mockResolvedValue(true),
  showSuccessMock: vi.fn()
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/api', () => ({
  keysAPI: {
    rotate: rotateMock
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: { value: false },
    copyToClipboard: copyToClipboardMock
  })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: showSuccessMock })
}))

import KeyRotationDialog from '../KeyRotationDialog.vue'
import KeySecretDialog from '../KeySecretDialog.vue'

const dialogStubs = {
  BaseDialog: {
    template: '<div><slot /><slot name="footer" /></div>'
  }
}

const apiKey = {
  id: 42,
  name: 'team-key'
} as never

describe('KeyRotationDialog', () => {
  afterEach(() => {
    rotateMock.mockReset()
    showSuccessMock.mockClear()
  })

  it('uses the injected rotate call so the admin surface hits the admin endpoint', async () => {
    const rotateFn = vi.fn().mockResolvedValue({ secret: 'sk-rotated', grace_until: '2026-09-22T00:00:00Z' })
    const wrapper = mount(KeyRotationDialog, {
      props: { show: true, apiKey, rotateFn },
      global: { stubs: dialogStubs }
    })

    await wrapper.get('button.btn.btn-primary').trigger('click')
    await flushPromises()

    expect(rotateFn).toHaveBeenCalledWith(42, { grace_ttl_hours: undefined, revoke_previous: false })
    expect(rotateMock).not.toHaveBeenCalled()
    const emitted = wrapper.emitted('issued')
    expect(emitted?.[0]?.[0]).toEqual({ secret: 'sk-rotated', graceUntil: '2026-09-22T00:00:00Z' })
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('falls back to the user rotate call and forwards the grace window', async () => {
    rotateMock.mockResolvedValue({ secret: 'sk-user-rotated', grace_until: null })
    const wrapper = mount(KeyRotationDialog, {
      props: { show: true, apiKey },
      global: { stubs: dialogStubs }
    })

    await wrapper.get('#rotate-grace-hours').setValue(6)
    await wrapper.get('button.btn.btn-primary').trigger('click')
    await flushPromises()

    expect(rotateMock).toHaveBeenCalledWith(42, { grace_ttl_hours: 6, revoke_previous: false })
    expect(wrapper.emitted('issued')?.[0]?.[0]).toEqual({ secret: 'sk-user-rotated', graceUntil: null })
  })

  it('asks for immediate revocation without a grace window', async () => {
    rotateMock.mockResolvedValue({ secret: 'sk-immediate', grace_until: null })
    const wrapper = mount(KeyRotationDialog, {
      props: { show: true, apiKey },
      global: { stubs: dialogStubs }
    })

    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('button.btn.btn-primary').trigger('click')
    await flushPromises()

    expect(rotateMock).toHaveBeenCalledWith(42, { grace_ttl_hours: undefined, revoke_previous: true })
  })

  it('surfaces a rotation failure and does not emit a secret', async () => {
    rotateMock.mockRejectedValue(new Error('rotation rejected'))
    const wrapper = mount(KeyRotationDialog, {
      props: { show: true, apiKey },
      global: { stubs: dialogStubs }
    })

    await wrapper.get('button.btn.btn-primary').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('rotation rejected')
    expect(wrapper.emitted('issued')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeFalsy()
  })
})

describe('KeySecretDialog', () => {
  afterEach(() => {
    copyToClipboardMock.mockClear()
  })

  it('shows the one-time secret with its warning and copies it on demand', async () => {
    const wrapper = mount(KeySecretDialog, {
      props: { show: true, secret: 'sk-shown-once', graceUntil: '2026-09-22T00:00:00Z' },
      global: { stubs: dialogStubs }
    })

    expect(wrapper.text()).toContain('sk-shown-once')
    expect(wrapper.text()).toContain('keys.secretOnce.warning')
    expect(wrapper.text()).toContain('keys.secretOnce.graceUntil')

    await wrapper.get('button.btn.btn-secondary').trigger('click')
    expect(copyToClipboardMock).toHaveBeenCalledWith('sk-shown-once')
  })

  it('hides the grace deadline when the previous secret was revoked immediately', () => {
    const wrapper = mount(KeySecretDialog, {
      props: { show: true, secret: 'sk-shown-once', graceUntil: null },
      global: { stubs: dialogStubs }
    })

    expect(wrapper.text()).not.toContain('keys.secretOnce.graceUntil')
  })
})
