import { describe, expect, it } from 'vitest'
import { createMachineSessionStore } from './localAppIdentity'

describe('MachineSessionStore', () => {
  it('stores session tokens per machine without app certificates or key material', () => {
    const storage = new MemoryStorage()
    const store = createMachineSessionStore(storage)

    store.saveSessionToken('machine-1', 'session-token-1', '2026-05-06T00:00:00Z')

    expect(store.getSessionToken('machine-1')).toBe('session-token-1')
    expect(storage.dump()).toEqual({
      'termx.session.machine-1.token': 'session-token-1',
      'termx.session.machine-1.exp': '2026-05-06T00:00:00Z',
    })
    expect(JSON.stringify(storage.dump())).not.toMatch(/appCertificate|appPublicKey|privateKey|ed25519|turn|credential/i)
  })

  it('clears a stored machine session token and expiration together', () => {
    const storage = new MemoryStorage()
    const store = createMachineSessionStore(storage)

    store.saveSessionToken('machine-1', 'session-token-1', '2026-05-06T00:00:00Z')
    store.clearSessionToken('machine-1')

    expect(store.getSessionToken('machine-1')).toBeNull()
    expect(storage.dump()).toEqual({})
  })
})

class MemoryStorage implements Storage {
  private readonly values = new Map<string, string>()
  length = 0

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
