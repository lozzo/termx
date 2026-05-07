import { describe, expect, it } from 'vitest'
import { createMachineSessionStore } from './localAppIdentity'

describe('MachineSessionStore', () => {
  it('stores session tokens per machine without app certificates or key material', () => {
    const storage = new MemoryStorage()
    const store = createMachineSessionStore(storage)

    store.saveSessionToken('machine-1', 'session-token-1', '2099-01-01T00:00:00Z', 'answer-proof-secret')

    expect(store.getSessionToken('machine-1')).toBe('session-token-1')
    expect(store.getAnswerProofSecret('machine-1')).toBe('answer-proof-secret')
    expect(storage.dump()).toEqual({
      'termx.session.machine-1.answerProofSecret': 'answer-proof-secret',
      'termx.session.machine-1.token': 'session-token-1',
      'termx.session.machine-1.exp': '2099-01-01T00:00:00Z',
    })
    expect(JSON.stringify(storage.dump())).not.toMatch(/appCertificate|appPublicKey|privateKey|ed25519|turn|credential/i)
  })

  it('clears a stored machine session token and expiration together', () => {
    const storage = new MemoryStorage()
    const store = createMachineSessionStore(storage)

    store.saveSessionToken('machine-1', 'session-token-1', '2099-01-01T00:00:00Z')
    store.clearSessionToken('machine-1')

    expect(store.getSessionToken('machine-1')).toBeNull()
    expect(store.getAnswerProofSecret('machine-1')).toBeNull()
    expect(storage.dump()).toEqual({})
  })

  it('returns null and clears storage when session token is expired', () => {
    const storage = new MemoryStorage()
    const store = createMachineSessionStore(storage)

    store.saveSessionToken('machine-1', 'session-token-1', '2020-01-01T00:00:00Z', 'answer-proof-secret')

    expect(store.getSessionToken('machine-1')).toBeNull()
    expect(storage.dump()).toEqual({})
  })

  it('returns null and clears storage when exp is exactly now (boundary)', () => {
    const storage = new MemoryStorage()
    const store = createMachineSessionStore(storage)
    const pastMs = Date.now() - 1

    store.saveSessionToken('machine-1', 'session-token-1', new Date(pastMs).toISOString(), 'secret')

    expect(store.getSessionToken('machine-1')).toBeNull()
    expect(storage.dump()).toEqual({})
  })

  it('returns token when exp is missing (no expiry enforcement)', () => {
    const storage = new MemoryStorage()
    const store = createMachineSessionStore(storage)

    storage.setItem('termx.session.machine-1.token', 'session-token-1')

    expect(store.getSessionToken('machine-1')).toBe('session-token-1')
  })

  it('returns token and ignores exp when exp is an invalid date string', () => {
    const storage = new MemoryStorage()
    const store = createMachineSessionStore(storage)

    storage.setItem('termx.session.machine-1.token', 'session-token-1')
    storage.setItem('termx.session.machine-1.exp', 'not-a-date')

    expect(store.getSessionToken('machine-1')).toBe('session-token-1')
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
