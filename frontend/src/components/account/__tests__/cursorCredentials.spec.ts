import { describe, expect, it } from 'vitest'
import { buildCursorCredentials } from '../credentialsBuilder'

describe('Cursor credentials', () => {
  it('imports auto tokens and machine IDs while preserving the stored protocol', () => {
    const result = buildCursorCredentials({ mode: 'auto', apiKey: 'stale-key', baseUrl: 'https://stale.example', autoCredentials: { access_token: ' access ', refresh_token: ' refresh ', machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64) }, existing: { api_key: 'stale', api_protocol: 'adaptive', model_mapping: { chat: 'live' } } })
    expect(result).toEqual({ api_protocol: 'adaptive', access_token: 'access', refresh_token: 'refresh', machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64), client_version: '', model_mapping: { chat: 'live' } })
  })

  it('does not echo stored tokens when editing auto credentials', () => {
    expect(buildCursorCredentials({ mode: 'auto', apiKey: '', autoCredentials: { access_token: '', refresh_token: '' }, existing: { access_token: 'never-echo', refresh_token: 'never-echo', machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64) } })).toEqual({ client_version: '', machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64) })
  })
  it('removes relay and token fields when building an official key account', () => {
    expect(buildCursorCredentials({ mode: 'official', apiKey: ' crsr_key ', existing: { access_token: 'derived', base_url: 'https://relay.example', machine_id: 'machine', model_mapping: { chat: 'default' } } })).toEqual({ api_key: 'crsr_key', client_version: '', machine_id: 'machine', model_mapping: { chat: 'default' } })
  })

  it('keeps only relay credentials and preserves a blank replacement key', () => {
    expect(buildCursorCredentials({ mode: 'relay', apiKey: '', baseUrl: ' https://relay.example/v1 ', existing: { api_key: 'never-echo', machine_id: 'machine', access_token: 'derived' } })).toEqual({ base_url: 'https://relay.example/v1', api_protocol: 'adaptive' })
  })
})
