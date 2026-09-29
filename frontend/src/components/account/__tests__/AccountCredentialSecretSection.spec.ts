import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountCredentialSecretSection from '../AccountCredentialSecretSection.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import type { CredentialSecretMetadata } from '@/api/admin/credentialSecrets'
import { convertCredentialSecret, getCredentialSecret, revokeCredentialSecret, rotateCredentialSecret } from '@/api/admin/credentialSecrets'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/api/admin/credentialSecrets', () => ({
  getCredentialSecret: vi.fn(),
  convertCredentialSecret: vi.fn(),
  rotateCredentialSecret: vi.fn(),
  revokeCredentialSecret: vi.fn(),
}))

const mockedGet = vi.mocked(getCredentialSecret)
const mockedConvert = vi.mocked(convertCredentialSecret)
const mockedRotate = vi.mocked(rotateCredentialSecret)
const mockedRevoke = vi.mocked(revokeCredentialSecret)

function metadata(overrides: Partial<CredentialSecretMetadata> = {}): CredentialSecretMetadata {
  return {
    reference: 'ref-123',
    workspace_id: 1,
    account_id: 7,
    version: 2,
    updated_at: '2026-09-21T00:00:00Z',
    ...overrides,
  }
}

function mountSection(accountRevision = '2026-09-20T00:00:00Z') {
  return mount(AccountCredentialSecretSection, {
    props: { accountId: 7, accountRevision, active: true },
  })
}

describe('AccountCredentialSecretSection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows inline state for unconverted accounts and converts with the reviewed revision', async () => {
    mockedGet.mockRejectedValueOnce({ reason: 'CREDENTIAL_SECRET_NOT_FOUND' })
    const wrapper = mountSection()
    await flushPromises()
    expect(wrapper.find('[data-testid="credential-secret-inline"]').exists()).toBe(true)

    mockedConvert.mockResolvedValueOnce(metadata())
    mockedGet.mockResolvedValueOnce(metadata())
    await wrapper.find('[data-testid="convert-credential-secret"]').trigger('click')
    expect(document.body.textContent).toContain('admin.accounts.credentialSecret.confirm.convert')
    await wrapper.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(mockedConvert).toHaveBeenCalledWith(7, '2026-09-20T00:00:00Z')
    expect(wrapper.find('[data-testid="credential-secret-metadata"]').exists()).toBe(true)
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('rotates with the current version and a parsed JSON payload', async () => {
    mockedGet.mockResolvedValueOnce(metadata())
    const wrapper = mountSection()
    await flushPromises()
    expect(wrapper.text()).toContain('ref-123')

    await wrapper.find('[data-testid="rotate-credential-payload"]').setValue('{"refresh_token":"new-token"}')
    await wrapper.find('[data-testid="rotate-credential-secret"]').trigger('click')
    mockedRotate.mockResolvedValueOnce(metadata({ version: 3 }))
    mockedGet.mockResolvedValueOnce(metadata({ version: 3 }))
    await wrapper.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(mockedRotate).toHaveBeenCalledWith(7, 2, { refresh_token: 'new-token' })
  })

  it('rejects invalid rotate payloads before calling the API', async () => {
    mockedGet.mockResolvedValueOnce(metadata())
    const wrapper = mountSection()
    await flushPromises()
    await wrapper.find('[data-testid="rotate-credential-payload"]').setValue('not-json')
    expect(wrapper.find('[data-testid="rotate-credential-secret"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('admin.accounts.credentialSecret.invalidJSON')
    expect(mockedRotate).not.toHaveBeenCalled()
  })

  it('revokes after a danger confirmation', async () => {
    mockedGet.mockResolvedValueOnce(metadata())
    const wrapper = mountSection()
    await flushPromises()
    await wrapper.find('[data-testid="revoke-credential-secret"]').trigger('click')
    mockedRevoke.mockResolvedValueOnce(metadata({ revoked_at: '2026-09-21T01:00:00Z' }))
    mockedGet.mockResolvedValueOnce(metadata({ revoked_at: '2026-09-21T01:00:00Z' }))
    await wrapper.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(mockedRevoke).toHaveBeenCalledWith(7, 2)
    expect(wrapper.find('[data-testid="credential-secret-revoked"]').exists()).toBe(true)
  })

  it('surfaces a version conflict and reloads the latest state', async () => {
    mockedGet.mockResolvedValueOnce(metadata())
    const wrapper = mountSection()
    await flushPromises()
    await wrapper.find('[data-testid="rotate-credential-payload"]').setValue('{"api_key":"k"}')
    await wrapper.find('[data-testid="rotate-credential-secret"]').trigger('click')
    mockedRotate.mockRejectedValueOnce({ reason: 'CREDENTIAL_SECRET_CONFLICT' })
    mockedGet.mockResolvedValueOnce(metadata({ version: 3 }))
    await wrapper.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.credentialSecret.conflict')
    expect(mockedGet).toHaveBeenCalledTimes(2)
  })
})
