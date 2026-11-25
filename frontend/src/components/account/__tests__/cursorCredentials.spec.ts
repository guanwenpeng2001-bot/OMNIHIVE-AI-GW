import { describe, expect, it } from 'vitest'
import { buildCursorCredentials, resolveCursorRelayProtocol } from '../credentialsBuilder'

const CURSOR_MACHINE_ID = 'a'.repeat(64)
const CURSOR_MAC_MACHINE_ID = 'b'.repeat(64)
const RELAY_BASE_URL = 'https://relay.example/v1'
/** 历史脏数据：后端 NormalizeCursorAccount 只接受 adaptive / chat_completions。 */
const LEGACY_UNSUPPORTED_PROTOCOL = 'anthropic'

describe('resolveCursorRelayProtocol', () => {
  it('returns supported protocols unchanged', () => {
    expect(resolveCursorRelayProtocol('adaptive')).toBe('adaptive')
    expect(resolveCursorRelayProtocol('chat_completions')).toBe('chat_completions')
  })

  it('falls back to Adaptive only for a missing or empty stored protocol', () => {
    expect(resolveCursorRelayProtocol(undefined)).toBe('adaptive')
    expect(resolveCursorRelayProtocol(null)).toBe('adaptive')
    expect(resolveCursorRelayProtocol('')).toBe('adaptive')
  })

  it('treats blank or padded protocol strings as unsupported instead of trimming them', () => {
    expect(resolveCursorRelayProtocol('   ')).toBeNull()
    expect(resolveCursorRelayProtocol(' adaptive ')).toBeNull()
    expect(resolveCursorRelayProtocol('CHAT_COMPLETIONS')).toBeNull()
  })

  it('rejects stored protocols the Cursor relay backend cannot serve', () => {
    expect(resolveCursorRelayProtocol(LEGACY_UNSUPPORTED_PROTOCOL)).toBeNull()
    expect(resolveCursorRelayProtocol('responses')).toBeNull()
    expect(resolveCursorRelayProtocol(42)).toBeNull()
  })
})

describe('Cursor credentials', () => {
  it('imports auto tokens and machine IDs while preserving the configured API protocol', () => {
    const result = buildCursorCredentials({ mode: 'auto', apiKey: 'stale-key', baseUrl: 'https://stale.example', autoCredentials: { access_token: ' access ', refresh_token: ' refresh ', machine_id: CURSOR_MACHINE_ID, mac_machine_id: CURSOR_MAC_MACHINE_ID }, existing: { api_key: 'stale', api_protocol: 'adaptive', base_url: 'https://relay.example/v1', api_base_urls: { anthropic: 'https://relay.example/anthropic' }, model_mapping: { chat: 'live' } } })
    expect(result).toEqual({ access_token: 'access', refresh_token: 'refresh', machine_id: CURSOR_MACHINE_ID, mac_machine_id: CURSOR_MAC_MACHINE_ID, client_version: '', model_mapping: { chat: 'live' } })
  })

  it('keeps native Cursor credentials free of relay fields', () => {
    const storedRelayFields = {
      api_protocol: LEGACY_UNSUPPORTED_PROTOCOL,
      base_url: RELAY_BASE_URL,
      api_base_urls: { anthropic: 'https://relay.example/anthropic' }
    }
    const official = buildCursorCredentials({ mode: 'official', apiKey: 'crsr_key', baseUrl: RELAY_BASE_URL, apiProtocol: 'chat_completions', clientVersion: '0.48.0', existing: { ...storedRelayFields, model_mapping: { chat: 'default' } } })
    expect(official).toEqual({ api_key: 'crsr_key', client_version: '0.48.0', model_mapping: { chat: 'default' } })

    const auto = buildCursorCredentials({ mode: 'auto', apiKey: 'stale-key', baseUrl: RELAY_BASE_URL, apiProtocol: 'adaptive', clientVersion: '0.48.0', autoCredentials: { access_token: 'access', refresh_token: 'refresh', machine_id: CURSOR_MACHINE_ID, mac_machine_id: CURSOR_MAC_MACHINE_ID }, existing: { ...storedRelayFields, api_key: 'stale', model_mapping: { chat: 'live' } } })
    expect(auto).toEqual({ access_token: 'access', refresh_token: 'refresh', machine_id: CURSOR_MACHINE_ID, mac_machine_id: CURSOR_MAC_MACHINE_ID, client_version: '0.48.0', model_mapping: { chat: 'live' } })
  })

  it('does not echo stored tokens when editing auto credentials', () => {
    expect(buildCursorCredentials({ mode: 'auto', apiKey: '', autoCredentials: { access_token: '', refresh_token: '' }, existing: { access_token: 'never-echo', refresh_token: 'never-echo', machine_id: CURSOR_MACHINE_ID, mac_machine_id: CURSOR_MAC_MACHINE_ID } })).toEqual({ client_version: '', machine_id: CURSOR_MACHINE_ID, mac_machine_id: CURSOR_MAC_MACHINE_ID })
  })

  it('removes relay and token fields when building an official key account', () => {
    expect(buildCursorCredentials({ mode: 'official', apiKey: ' crsr_key ', existing: { access_token: 'derived', base_url: RELAY_BASE_URL, machine_id: 'machine', model_mapping: { chat: 'default' } } })).toEqual({ api_key: 'crsr_key', client_version: '', machine_id: 'machine', model_mapping: { chat: 'default' } })
  })

  it.each(['adaptive', 'chat_completions'] as const)('uses the selected supported relay protocol (%s)', (protocol) => {
    expect(buildCursorCredentials({ mode: 'relay', apiKey: ' crsr_key ', baseUrl: ` ${RELAY_BASE_URL} `, apiProtocol: protocol, existing: { api_protocol: LEGACY_UNSUPPORTED_PROTOCOL } })).toEqual({ api_key: 'crsr_key', base_url: RELAY_BASE_URL, api_protocol: protocol })
  })

  it('keeps a stored Chat relay protocol', () => {
    expect(buildCursorCredentials({ mode: 'relay', apiKey: 'crsr_key', baseUrl: RELAY_BASE_URL, existing: { api_protocol: 'chat_completions' } })).toEqual({ api_key: 'crsr_key', base_url: RELAY_BASE_URL, api_protocol: 'chat_completions' })
  })

  it('defaults a missing relay protocol to Adaptive', () => {
    expect(buildCursorCredentials({ mode: 'relay', apiKey: 'crsr_key', baseUrl: RELAY_BASE_URL })).toEqual({ api_key: 'crsr_key', base_url: RELAY_BASE_URL, api_protocol: 'adaptive' })
    expect(buildCursorCredentials({ mode: 'relay', apiKey: 'crsr_key', baseUrl: RELAY_BASE_URL, existing: { api_protocol: '' } })).toEqual({ api_key: 'crsr_key', base_url: RELAY_BASE_URL, api_protocol: 'adaptive' })
  })

  it.each([LEGACY_UNSUPPORTED_PROTOCOL, 'responses', ' adaptive '])('rejects an unsupported relay protocol without a new selection (%s)', (storedProtocol) => {
    expect(() => buildCursorCredentials({ mode: 'relay', apiKey: 'crsr_key', baseUrl: RELAY_BASE_URL, existing: { api_protocol: storedProtocol } })).toThrow(/adaptive or chat_completions/)
  })

  it('keeps only relay credentials and preserves a blank replacement key', () => {
    expect(buildCursorCredentials({ mode: 'relay', apiKey: '', baseUrl: ` ${RELAY_BASE_URL} `, existing: { api_key: 'never-echo', machine_id: 'machine', access_token: 'derived', client_version: '0.48.0' } })).toEqual({ base_url: RELAY_BASE_URL, api_protocol: 'adaptive' })
  })

  it('does not forward an empty model_mapping object from existing credentials', () => {
    expect(buildCursorCredentials({ mode: 'official', apiKey: 'crsr_key', existing: { model_mapping: {} } })).toEqual({ api_key: 'crsr_key', client_version: '' })
  })
})
