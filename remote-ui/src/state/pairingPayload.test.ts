import { describe, expect, it } from 'vitest'
import { parsePairingPayload } from './pairingPayload'

describe('pairing payload parser', () => {
  it('parses termx:// QR payloads with the single v4 schema', () => {
    const payload = parsePairingPayload(`termx://pair?payload=${base64url(JSON.stringify({
      type: 'termx_pair',
      schema_version: 4,
      machine: {
        id: 'machine-1',
        name: '开发 MacBook',
        hostname: 'dev-mac.local',
      },
      local: {
        hub_urls: ['http://127.0.0.1:7788', 'http://192.168.1.40:7788'],
      },
      hub: {
        hub_urls: ['https://hub.termx.test'],
        web_control: 'https://control.termx.test',
      },
      pairing: {
        session_id: 'pair-1',
        secret: 'pair-secret-1',
        expires_at: '2026-05-03T12:30:00Z',
      },
      bootstrap: {},
      preferred_path: 'local',
    }))}`)

    expect(payload).toEqual({
      schemaVersion: 4,
      machine: {
        id: 'machine-1',
        name: '开发 MacBook',
        hostname: 'dev-mac.local',
      },
      local: {
        hubUrls: ['http://127.0.0.1:7788', 'http://192.168.1.40:7788'],
      },
      hub: {
        hubUrls: ['https://hub.termx.test'],
        webControl: 'https://control.termx.test',
      },
      pairing: {
        sessionId: 'pair-1',
        secret: 'pair-secret-1',
        expiresAt: '2026-05-03T12:30:00Z',
      },
      bootstrap: {},
      preferredPath: 'local',
    })
  })

  it('rejects old pairing payload schemas instead of normalizing them', () => {
    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair_v1',
      machine_id: 'machine-legacy',
      pair_session_id: 'pair-legacy',
      pair_secret: 'legacy-secret',
    }))).toThrow(/unsupported pairing payload type/i)

    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair_v2',
      schema_version: 2,
      machine: { id: 'machine-old', name: 'Old Machine' },
      pairing: { session_id: 'pair-old', secret: 'old-secret' },
    }))).toThrow(/unsupported pairing payload type/i)

    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair',
      schema_version: 2,
      machine: { id: 'machine-old', name: 'Old Machine' },
      pairing: { session_id: 'pair-old', secret: 'old-secret' },
    }))).toThrow(/schema_version 2/i)
  })

  it('rejects machine private-key material even when nested inside bootstrap metadata', () => {
    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair',
      schema_version: 4,
      machine: { id: 'machine-1', name: 'Dev MacBook' },
      pairing: { session_id: 'pair-1', secret: 'pair-secret-1' },
      bootstrap: {
        machine_private_key: '-----BEGIN PRIVATE KEY-----\nnot-allowed\n-----END PRIVATE KEY-----',
      },
    }))).toThrow(/machine private key/i)

    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair',
      schema_version: 4,
      machine: {
        id: 'machine-1',
        name: 'Dev MacBook',
        privateKey: 'not-allowed',
      },
      pairing: { session_id: 'pair-1', secret: 'pair-secret-1' },
    }))).toThrow(/private key/i)

    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair',
      schema_version: 4,
      machine: { id: 'machine-1', name: 'Dev MacBook' },
      pairing: { session_id: 'pair-1', secret: 'pair-secret-1' },
      bootstrap: {
        jwk: { kty: 'OKP', crv: 'Ed25519', x: 'public', d: 'private' },
      },
    }))).toThrow(/private key/i)
  })

  it('rejects relay as a client-visible connection path', () => {
    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair',
      schema_version: 4,
      machine: { id: 'machine-1', name: 'Dev MacBook' },
      pairing: { session_id: 'pair-1', secret: 'pair-secret-1' },
      preferred_path: 'relay',
    }))).toThrow(/connection path/i)
  })
})

function base64url(value: string): string {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}
