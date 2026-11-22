import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import KeySecretDialog from '../KeySecretDialog.vue'

const copyToClipboard = vi.fn().mockResolvedValue(true)
const copiedRef = { value: false }

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: copiedRef,
    copyToClipboard: async (val: string) => {
      copiedRef.value = true
      return copyToClipboard(val)
    }
  })
}))

describe('KeySecretDialog one-time display and copy feedback', () => {
  it('displays secret once, provides copy feedback, and never writes secret to web storage', async () => {
    localStorage.clear()
    sessionStorage.clear()
    copiedRef.value = false

    const wrapper = mount(KeySecretDialog, {
      props: {
        show: true,
        secret: 'oh_live_secret_never_cached_999',
        graceUntil: '2026-11-20T00:00:00Z'
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show', 'title'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>'
          }
        }
      }
    })

    expect(wrapper.get('[data-testid="key-secret-value"]').text()).toBe(
      'oh_live_secret_never_cached_999'
    )

    await wrapper.get('[data-testid="copy-key-secret-btn"]').trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('oh_live_secret_never_cached_999')
    expect(JSON.stringify(localStorage)).not.toContain('oh_live_secret_never_cached_999')
    expect(JSON.stringify(sessionStorage)).not.toContain('oh_live_secret_never_cached_999')

    wrapper.unmount()
  })

  it('blocks closing until secret is copied or saved confirmation is checked', async () => {
    copiedRef.value = false
    const wrapper = mount(KeySecretDialog, {
      props: {
        show: true,
        secret: 'oh_live_secret_guard_123',
        graceUntil: null
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show', 'title', 'closeOnEscape', 'closeOnClickOutside', 'showCloseButton'],
            template: '<div v-if="show" :data-esc="String(closeOnEscape)" :data-outside="String(closeOnClickOutside)"><slot /><slot name="footer" /></div>'
          }
        }
      }
    })

    const confirmBtn = wrapper.get('[data-testid="confirm-secret-close-btn"]')
    expect(confirmBtn.attributes('disabled')).toBeDefined()
    await confirmBtn.trigger('click')
    expect(wrapper.emitted('close')).toBeUndefined()

    await wrapper.get('[data-testid="confirm-secret-saved-checkbox"]').setValue(true)
    expect(confirmBtn.attributes('disabled')).toBeUndefined()
    await confirmBtn.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)

    wrapper.unmount()
  })
})
