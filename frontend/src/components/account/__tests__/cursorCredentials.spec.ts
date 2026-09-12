import { describe, expect, it } from 'vitest'
import { buildCursorCredentials } from '../credentialsBuilder'

describe('Cursor credentials', () => {
  it('removes relay and token fields when building an official key account', () => {
    expect(buildCursorCredentials({ mode: 'official', apiKey: ' crsr_key ', existing: { access_token: 'derived', base_url: 'https://relay.example', machine_id: 'machine', model_mapping: { chat: 'default' } } })).toEqual({ api_key: 'crsr_key', client_version: '', machine_id: 'machine', model_mapping: { chat: 'default' } })
  })

  it('keeps only relay credentials and preserves a blank replacement key', () => {
    expect(buildCursorCredentials({ mode: 'relay', apiKey: '', baseUrl: ' https://relay.example/v1 ', existing: { api_key: 'never-echo', machine_id: 'machine', access_token: 'derived' } })).toEqual({ base_url: 'https://relay.example/v1', api_protocol: 'adaptive' })
  })
})
