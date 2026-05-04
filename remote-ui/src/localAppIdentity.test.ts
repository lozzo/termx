import { describe, expect, it, vi } from 'vitest'
import {
  createBrowserLocalAppCrypto,
  createLocalAppIdentityStore,
  canonicalLocalOfferMessage,
  createLocalOfferSigner,
  ensureLocalAppIdentity,
  pairLocalApp,
} from './localAppIdentity'
import type { LocalAgentApi } from './transport'

describe('local app identity', () => {
  it('creates and persists only browser-local app key material', async () => {
    const storage = new MemoryStorage()
    const crypto = createMockCrypto()

    const identity = await ensureLocalAppIdentity({
      storage: createLocalAppIdentityStore(storage),
      crypto,
      appName: 'TermX Local Web',
    })

    expect(identity.appPublicKey).toBe('AQIDBA==')
    expect(identity.appDeviceId).toMatch(/^appweb_/)
    expect(storage.dump()).toMatchObject({
      'termx.local.appDeviceId': identity.appDeviceId,
      'termx.local.appName': 'TermX Local Web',
      'termx.local.appPublicKey': 'AQIDBA==',
    })
    expect(storage.getItem('termx.local.appPrivateKey')).toBeNull()
    expect(crypto.privateKeyHandles).toEqual(['appweb_AQIDBAUGBwgJCgsMDQ4PEA'])
    expect(JSON.stringify(storage.dump())).not.toMatch(/privateKey|private_key|machine_private_key|machinePrivateKey|turn|credential/i)
  })

  it('can scope app identity and certificate per account machine pair', async () => {
    const storage = new MemoryStorage()
    const crypto = createMockCrypto()
    const scopedStore = createLocalAppIdentityStore(storage, { scope: 'user:user-1:machine:machine-1' })

    const identity = await ensureLocalAppIdentity({
      storage: scopedStore,
      crypto,
      appName: 'TermX Remote App',
    })
    scopedStore.saveCertificate('{"payload":{"machine_id":"machine-1"}}')

    expect(identity.appDeviceId).toMatch(/^appweb_/)
    expect(storage.dump()).toMatchObject({
      'termx.local.user%3Auser-1%3Amachine%3Amachine-1.appDeviceId': identity.appDeviceId,
      'termx.local.user%3Auser-1%3Amachine%3Amachine-1.appName': 'TermX Remote App',
      'termx.local.user%3Auser-1%3Amachine%3Amachine-1.appPublicKey': 'AQIDBA==',
      'termx.local.user%3Auser-1%3Amachine%3Amachine-1.appCertificate': '{"payload":{"machine_id":"machine-1"}}',
    })
    expect(createLocalAppIdentityStore(storage).loadCertificate()).toBeNull()
    expect(JSON.stringify(storage.dump())).not.toMatch(/privateKey|private_key|machine_private_key|machinePrivateKey|turn|credential/i)
  })

  it('canonicalizes local offers the same way as the Go verifier and signs with the app key', async () => {
    const storage = new MemoryStorage({
      'termx.local.appDeviceId': 'appweb_123',
      'termx.local.appName': 'TermX Local Web',
      'termx.local.appPublicKey': 'AQIDBA==',
      'termx.local.appCertificate': '{"payload":{"machine_id":"machine-local","app_public_key":"AQIDBA=="}}',
    })
    const crypto = createMockCrypto()
    crypto.privateKeys.set('appweb_123', { keyId: 'stored-app-key' })
    const signer = createLocalOfferSigner({
      storage: createLocalAppIdentityStore(storage),
      crypto,
      nonce: () => 'nonce-1',
      now: () => new Date('2026-05-01T10:30:00Z'),
    })

    const message = await canonicalLocalOfferMessage({
      sessionId: 'rtc-1',
      ticketId: 'ticket-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sdp: 'v=0\r\ns=termx\r\n',
      candidates: ['candidate:host-a', ' candidate:host-b '],
      nonce: 'nonce-1',
      timestamp: 1777631400,
    })
    expect(message).toContain('termx-webrtc-offer-v1:')
    expect(message).toContain('ticket_id:ticket-1')
    expect(message).toContain('machine_id:machine-local')
    expect(message).toContain('terminal_id:terminal-1')
    expect(message).toContain('sha256(sdp):dd33fcfb47f1bcefb7e8f57c03aa4778c5f7e2490f14259f9b892c05d0aa0158')
    expect(message).toContain('sha256(candidates):a00196786082bed059a8712b03f5355783521d39a724846935ae8deaa9a6cd96')
    expect(message).toContain('nonce:nonce-1')
    expect(message).toContain('timestamp:1777631400')
    expect(message).not.toContain('candidate:host-a')
    expect(message).not.toContain('v=0')

    const signature = await signer.signOffer({
      sessionId: 'rtc-1',
      ticketId: 'ticket-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sdp: 'v=0\r\ns=termx\r\n',
      candidates: ['candidate:host-a', ' candidate:host-b '],
    })

    expect(signature).toEqual({
      signature: 'c2lnbmVkLWJ5LWFwcC1rZXk=',
      nonce: 'nonce-1',
      timestamp: '1777631400',
    })
    expect(crypto.signedMessages).toEqual([message])
    expect(crypto.loadedPrivateKeys).toEqual(['appweb_123'])
  })

  it('preserves candidate list boundaries in the signed offer message', async () => {
    const common = {
      sessionId: 'rtc-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sdp: 'v=0\r\ns=termx\r\n',
      nonce: 'nonce-1',
      timestamp: 1777631400,
    }

    const oneCandidate = await canonicalLocalOfferMessage({
      ...common,
      candidates: ['candidate:a\ncandidate:b'],
    })
    const twoCandidates = await canonicalLocalOfferMessage({
      ...common,
      candidates: ['candidate:a', 'candidate:b'],
    })

    expect(oneCandidate).not.toBe(twoCandidates)
  })

  it('canonicalizes candidate JSON escaping the same way as the Go verifier', async () => {
    const message = await canonicalLocalOfferMessage({
      sessionId: 'rtc-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sdp: 'v=0\r\ns=termx\r\n',
      candidates: ['candidate:<host>&'],
      nonce: 'nonce-1',
      timestamp: 1777631400,
    })

    expect(message).toContain('sha256(candidates):8cd5bdbbbe5fc4e653636ccd241028b014e95153072e56d6ffa6aff2bef9ef0e')
  })

  it('claims and stores an app certificate without storing machine private keys or TURN credentials', async () => {
    const storage = new MemoryStorage()
    const crypto = createMockCrypto()
    const api: LocalAgentApi = {
      async getStatus() {
        throw new Error('not used')
      },
      async listTerminals() {
        throw new Error('not used')
      },
      pair: vi.fn(async () => ({
        machineId: 'machine-local',
        appCertificate: '{"payload":{"machine_id":"machine-local","app_public_key":"AQIDBA=="},"signature":"machine-sig"}',
        expiresAt: '2026-05-02T10:30:00Z',
      })),
      async createRTCAnswer() {
        throw new Error('not used')
      },
      async createInventoryRTCAnswer() {
        throw new Error('not used')
      },
      async createTerminal() {
        throw new Error('not used')
      },
      async updateTerminal() {
        throw new Error('not used')
      },
      async deleteTerminal() {
        throw new Error('not used')
      },
    }

    const result = await pairLocalApp({
      api,
      storage: createLocalAppIdentityStore(storage),
      crypto,
      appName: 'TermX Local Web',
      machineId: 'machine-local',
      pairSessionId: 'pair-1',
      pairSecret: 'secret-1',
    })

    expect(result.machineId).toBe('machine-local')
    expect(api.pair).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-local',
      pairSessionId: 'pair-1',
      pairSecret: 'secret-1',
      appDeviceId: expect.stringMatching(/^appweb_/),
      appName: 'TermX Local Web',
      appPublicKey: 'AQIDBA==',
      requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
    }))
    expect(storage.getItem('termx.local.appCertificate')).toContain('machine-local')
    expect(storage.getItem('termx.local.machinePrivateKey')).toBeNull()
    expect(storage.getItem('termx.local.appPrivateKey')).toBeNull()
    expect(JSON.stringify(storage.dump())).not.toMatch(/privateKey|private_key|machine_private_key|machinePrivateKey|turn|credential/i)
  })

  it('uses a non-exportable browser WebCrypto private key', async () => {
    const crypto = createBrowserLocalAppCrypto()
    const keyPair = await crypto.generateKeyPair()

    expect(keyPair.publicKey.raw.byteLength).toBeGreaterThan(0)
    await expect(globalThis.crypto.subtle.exportKey('jwk', keyPair.privateKey as CryptoKey))
      .rejects.toThrow()
  })
})

class MemoryStorage implements Storage {
  private readonly values = new Map<string, string>()
  length = 0

  constructor(seed: Record<string, string> = {}) {
    for (const [key, value] of Object.entries(seed)) {
      this.setItem(key, value)
    }
  }

  clear(): void {
    this.values.clear()
    this.length = 0
  }

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  key(index: number): string | null {
    return Array.from(this.values.keys())[index] ?? null
  }

  removeItem(key: string): void {
    this.values.delete(key)
    this.length = this.values.size
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
    this.length = this.values.size
  }

  dump(): Record<string, string> {
    return Object.fromEntries(this.values)
  }
}

function createMockCrypto() {
  return {
    signedMessages: [] as string[],
    loadedPrivateKeys: [] as string[],
    privateKeyHandles: [] as string[],
    privateKeys: new Map<string, { keyId: string }>(),
    async generateKeyPair() {
      return {
        publicKey: { raw: new Uint8Array([1, 2, 3, 4]) },
        privateKey: { keyId: 'generated-app-key' },
      }
    },
    async savePrivateKey(appDeviceId: string, privateKey: { keyId: string }) {
      this.privateKeyHandles.push(appDeviceId)
      this.privateKeys.set(appDeviceId, privateKey)
    },
    async loadPrivateKey(appDeviceId: string) {
      this.loadedPrivateKeys.push(appDeviceId)
      return this.privateKeys.get(appDeviceId) ?? null
    },
    async sign(privateKey: { keyId: string }, message: Uint8Array) {
      expect(privateKey.keyId).toBe('stored-app-key')
      const text = new TextDecoder().decode(message)
      this.signedMessages.push(text)
      return new TextEncoder().encode('signed-by-app-key')
    },
    async randomBytes(length: number) {
      return new Uint8Array(Array.from({ length }, (_, index) => index + 1))
    },
    async sha256(data: Uint8Array) {
      const text = new TextDecoder().decode(data)
      if (text === 'v=0\r\ns=termx\r\n') {
        return Uint8Array.from([
          0xdd, 0x33, 0xfc, 0xfb, 0x47, 0xf1, 0xbc, 0xef,
          0xb7, 0xe8, 0xf5, 0x7c, 0x03, 0xaa, 0x47, 0x78,
          0xc5, 0xf7, 0xe2, 0x49, 0x0f, 0x14, 0x25, 0x9f,
          0x9b, 0x89, 0x2c, 0x05, 0xd0, 0xaa, 0x01, 0x58,
        ])
      }
      if (text === '["candidate:host-a","candidate:host-b"]') {
        return Uint8Array.from([
          0xa0, 0x01, 0x96, 0x78, 0x60, 0x82, 0xbe, 0xd0,
          0x59, 0xa8, 0x71, 0x2b, 0x03, 0xf5, 0x35, 0x57,
          0x83, 0x52, 0x1d, 0x39, 0xa7, 0x24, 0x84, 0x69,
          0x35, 0xae, 0x8d, 0xea, 0xa9, 0xa6, 0xcd, 0x96,
        ])
      }
      throw new Error(`unexpected hash input ${text}`)
    },
  }
}
