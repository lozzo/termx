import { describe, expect, it } from 'vitest'
import { parsePairingPayload } from './pairingPayload'

describe('pairing payload parser', () => {
  it('parses termx:// QR payloads with local, public, control, hub, and pairing metadata', () => {
    const payload = parsePairingPayload(`termx://pair?payload=${base64url(JSON.stringify({
      type: 'termx_pair_v2',
      schema_version: 2,
      machine: {
        id: 'machine-1',
        name: '开发 MacBook',
        hostname: 'dev-mac.local',
      },
      addresses: {
        local: ['http://127.0.0.1:7788'],
        lan: ['http://192.168.1.40:7788'],
        public: ['https://machine-1.public.termx.test'],
      },
      endpoints: {
        web_control: 'https://control.termx.test',
        hub: 'https://hub.termx.test',
        local_pairing: 'http://192.168.1.40:7788/pair',
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
      schemaVersion: 2,
      machine: {
        id: 'machine-1',
        name: '开发 MacBook',
        hostname: 'dev-mac.local',
      },
      addresses: {
        local: ['http://127.0.0.1:7788'],
        lan: ['http://192.168.1.40:7788'],
        public: ['https://machine-1.public.termx.test'],
      },
      endpoints: {
        webControl: 'https://control.termx.test',
        hub: 'https://hub.termx.test',
        localPairing: 'http://192.168.1.40:7788/pair',
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

  it('accepts v1 JSON payloads and normalizes them to the current store shape', () => {
    const payload = parsePairingPayload(JSON.stringify({
      type: 'termx_pair_v1',
      machine_id: 'machine-legacy',
      machine_name: 'Legacy Mini',
      hostname: 'legacy-mini.local',
      local_pair_url: 'http://192.168.1.20:7788/pair',
      local_rtc_url: 'http://192.168.1.20:7788/rtc',
      public_url: 'https://legacy.public.termx.test',
      web_control_url: 'https://control.termx.test',
      hub_url: 'https://hub.termx.test',
      pair_session_id: 'pair-legacy',
      pair_secret: 'legacy-secret',
      expires_at: '2026-05-04T12:30:00Z',
    }))

    expect(payload).toMatchObject({
      schemaVersion: 2,
      machine: {
        id: 'machine-legacy',
        name: 'Legacy Mini',
        hostname: 'legacy-mini.local',
      },
      addresses: {
        local: ['http://192.168.1.20:7788/rtc'],
        lan: ['http://192.168.1.20:7788/pair'],
        public: ['https://legacy.public.termx.test'],
      },
      endpoints: {
        webControl: 'https://control.termx.test',
        hub: 'https://hub.termx.test',
        localPairing: 'http://192.168.1.20:7788/pair',
      },
      pairing: {
        sessionId: 'pair-legacy',
        secret: 'legacy-secret',
        expiresAt: '2026-05-04T12:30:00Z',
      },
      preferredPath: 'local',
    })
  })

  it('rejects machine private-key material even when nested inside bootstrap metadata', () => {
    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair_v2',
      schema_version: 2,
      machine: { id: 'machine-1', name: 'Dev MacBook' },
      pairing: { session_id: 'pair-1', secret: 'pair-secret-1' },
      bootstrap: {
        machine_private_key: '-----BEGIN PRIVATE KEY-----\nnot-allowed\n-----END PRIVATE KEY-----',
      },
    }))).toThrow(/machine private key/i)

    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair_v2',
      schema_version: 2,
      machine: {
        id: 'machine-1',
        name: 'Dev MacBook',
        privateKey: 'not-allowed',
      },
      pairing: { session_id: 'pair-1', secret: 'pair-secret-1' },
    }))).toThrow(/private key/i)

    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair_v2',
      schema_version: 2,
      machine: { id: 'machine-1', name: 'Dev MacBook' },
      pairing: { session_id: 'pair-1', secret: 'pair-secret-1' },
      bootstrap: {
        jwk: { kty: 'OKP', crv: 'Ed25519', x: 'public', d: 'private' },
      },
    }))).toThrow(/private key/i)
  })

  it('rejects relay as a client-visible connection path', () => {
    expect(() => parsePairingPayload(JSON.stringify({
      type: 'termx_pair_v2',
      schema_version: 2,
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
