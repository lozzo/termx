import { describe, expect, it } from 'vitest'
import { parsePairingPayload } from './pairingPayload'
import { createMachineStore } from './machineStore'

describe('machine store', () => {
  it('saves QR pairing metadata needed for local, managed, control, and pairing flows', () => {
    const storage = new MemoryStorage()
    const store = createMachineStore({ storage, now: () => new Date('2026-05-03T16:00:00Z') })
    const payload = parsePairingPayload(JSON.stringify({
      type: 'termx_pair',
      schema_version: 3,
      machine: {
        id: 'machine-1',
        name: 'Dev MacBook',
        hostname: 'dev-mac.local',
      },
      addresses: {
        local: ['http://127.0.0.1:7788'],
        lan: ['http://192.168.1.40:7788'],
        public: ['https://machine-1.public.termx.test'],
      },
      endpoints: {
        web_control: 'https://control.termx.test',
      },
      pairing: {
        session_id: 'pair-1',
        secret: 'pair-secret-1',
        expires_at: '2026-05-03T20:00:00Z',
      },
      bootstrap: {},
      preferred_path: 'local',
    }))

    const saved = store.saveFromPairingPayload(payload)

    expect(saved).toEqual({
      machineId: 'machine-1',
      name: 'Dev MacBook',
      hostname: 'dev-mac.local',
      state: 'unknown',
      terminalCount: 0,
      source: 'local',
      preferredPath: 'local',
      addresses: {
        local: ['http://127.0.0.1:7788'],
        lan: ['http://192.168.1.40:7788'],
        public: ['https://machine-1.public.termx.test'],
      },
      endpoints: {
        webControl: 'https://control.termx.test',
      },
      pairing: {
        sessionId: 'pair-1',
        secret: 'pair-secret-1',
        expiresAt: '2026-05-03T20:00:00Z',
      },
      addedAt: '2026-05-03T16:00:00.000Z',
      updatedAt: '2026-05-03T16:00:00.000Z',
    })
    expect(store.listMachines()).toEqual([saved])
    expect(store.getMachine('machine-1')).toEqual(saved)
    expect(JSON.parse(storage.getItem('termx.app.machines.v1') ?? '[]')).toHaveLength(1)
  })

  it('merges rescanned metadata without dropping runtime status fields', () => {
    const storage = new MemoryStorage()
    const store = createMachineStore({ storage, now: () => new Date('2026-05-03T16:00:00Z') })

    store.saveMachine({
      machineId: 'machine-1',
      name: 'Old Name',
      state: 'online',
      terminalCount: 3,
      lastSeenAt: '2026-05-03T15:55:00Z',
      lastConnectionPath: 'managed',
      source: 'cloud',
      addresses: { local: [], lan: [], public: [] },
      endpoints: {},
      addedAt: '2026-05-03T15:00:00.000Z',
      updatedAt: '2026-05-03T15:00:00.000Z',
    })

    const saved = store.saveFromPairingPayload(parsePairingPayload(JSON.stringify({
      type: 'termx_pair',
      schema_version: 3,
      machine: { id: 'machine-1', name: 'New Name' },
      addresses: {
        lan: ['http://192.168.1.41:7788'],
        public: ['https://hub.termx.test'],
      },
      pairing: { session_id: 'pair-2', secret: 'pair-secret-2' },
    })))

    expect(saved).toMatchObject({
      machineId: 'machine-1',
      name: 'New Name',
      state: 'online',
      terminalCount: 3,
      lastSeenAt: '2026-05-03T15:55:00Z',
      lastConnectionPath: 'managed',
      source: 'cloud',
      addresses: { local: [], lan: ['http://192.168.1.41:7788'], public: ['https://hub.termx.test'] },
      endpoints: {},
      pairing: { sessionId: 'pair-2', secret: 'pair-secret-2' },
      addedAt: '2026-05-03T15:00:00.000Z',
      updatedAt: '2026-05-03T16:00:00.000Z',
    })
  })

  it('normalizes embedded Hub endpoint URLs before storing and reading machines', () => {
    const storage = new MemoryStorage()
    const store = createMachineStore({ storage, now: () => new Date('2026-05-03T16:00:00Z') })

    const saved = store.saveFromPairingPayload(parsePairingPayload(JSON.stringify({
      type: 'termx_pair',
      schema_version: 3,
      machine: { id: 'machine-1', name: 'Dev MacBook' },
      addresses: {
        local: [' http://127.0.0.1:18888/api/v1/pairing/claims '],
        lan: ['http://192.168.1.20:18888/api/v1/sessions/ice'],
        public: ['https://frp.termx.test/api/v1/sessions'],
      },
      endpoints: {
        hub: 'https://cloud-hub.termx.test/api/v1/pairing/claims',
      },
      pairing: { session_id: 'pair-1', secret: 'pair-secret-1' },
    })))

    expect(saved.addresses).toEqual({
      local: ['http://127.0.0.1:18888'],
      lan: ['http://192.168.1.20:18888'],
      public: ['https://frp.termx.test'],
    })
    expect(saved.endpoints.hub).toBe('https://cloud-hub.termx.test')
    expect(store.listMachines()[0]?.addresses).toEqual(saved.addresses)
  })

  it('rejects machine private keys before persistence', () => {
    const storage = new MemoryStorage()
    const store = createMachineStore({ storage })

    expect(() => store.saveMachine({
      machineId: 'machine-1',
      name: 'Dev MacBook',
      state: 'unknown',
      terminalCount: 0,
      source: 'manual',
      addresses: { local: [], lan: [], public: [] },
      endpoints: {},
      addedAt: '2026-05-03T16:00:00.000Z',
      updatedAt: '2026-05-03T16:00:00.000Z',
      machinePrivateKey: 'not-allowed',
    } as never)).toThrow(/machine private key/i)
    expect(storage.getItem('termx.app.machines.v1')).toBeNull()
  })

  it('rejects app private keys in legacy bootstrap metadata', () => {
    const storage = new MemoryStorage()
    const store = createMachineStore({ storage })

    expect(() => store.saveMachine({
      machineId: 'machine-1',
      name: 'Dev MacBook',
      state: 'unknown',
      terminalCount: 0,
      source: 'manual',
      addresses: { local: [], lan: [], public: [] },
      endpoints: {},
      appBootstrap: {
        appPrivateKey: 'not-allowed',
      },
      addedAt: '2026-05-03T16:00:00.000Z',
      updatedAt: '2026-05-03T16:00:00.000Z',
    } as never)).toThrow(/app private key/i)

    expect(JSON.stringify(storage.dump())).not.toMatch(/app_private_key|appPrivateKey|BEGIN PRIVATE KEY/i)
  })

  it('rejects contaminated persisted records before returning machines', () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.app.machines.v1', JSON.stringify([{
      machineId: 'machine-1',
      name: 'Dev MacBook',
      state: 'unknown',
      terminalCount: 0,
      source: 'manual',
      addresses: { local: [], lan: [], public: [] },
      endpoints: {},
      addedAt: '2026-05-03T16:00:00.000Z',
      updatedAt: '2026-05-03T16:00:00.000Z',
      appBootstrap: {
        private_key: 'not-allowed',
      },
    }]))
    const store = createMachineStore({ storage })

    expect(() => store.listMachines()).toThrow(/private key/i)
    expect(storage.getItem('termx.app.machines.v1')).toMatch(/private_key/)
  })
})

class MemoryStorage implements Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> {
  private readonly values = new Map<string, string>()

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  removeItem(key: string): void {
    this.values.delete(key)
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
  }

  dump(): Record<string, string> {
    return Object.fromEntries(this.values)
  }
}
