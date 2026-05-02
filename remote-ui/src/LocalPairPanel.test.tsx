import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LocalPairPanel } from './LocalPairPanel'
import { createLocalAppIdentityStore, type LocalAppCrypto } from './localAppIdentity'
import type { LocalAgentApi } from './transport'

describe('LocalPairPanel', () => {
  afterEach(() => {
    cleanup()
  })

  it('claims a local pair session and stores only app identity plus certificate material', async () => {
    const storage = new MemoryStorage()
    const crypto = createMockCrypto()
    const pair = vi.fn(async () => ({
      machineId: 'machine-local',
      appCertificate: '{"payload":{"machine_id":"machine-local","app_public_key":"AQIDBA=="},"signature":"machine-sig"}',
      expiresAt: '2026-05-02T10:30:00Z',
    }))
    const api = createMockApi(pair)

    render(
      <LocalPairPanel
        api={api}
        storage={createLocalAppIdentityStore(storage)}
        crypto={crypto}
        appName="TermX Local Web"
      />,
    )

    await userEvent.type(screen.getByLabelText('Pair ID'), 'pair-1')
    await userEvent.type(screen.getByLabelText('Pair secret'), 'secret-1')
    await userEvent.click(screen.getByRole('button', { name: /^pair device$/i }))

    await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Paired with machine-local'))
    expect(pair).toHaveBeenCalledWith(expect.objectContaining({
      pairSessionId: 'pair-1',
      pairSecret: 'secret-1',
      appDeviceId: expect.stringMatching(/^appweb_/),
      appPublicKey: 'AQIDBA==',
      requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
    }))
    expect(storage.getItem('termx.local.appCertificate')).toContain('machine-local')
    expect(storage.getItem('termx.local.appPrivateKey')).toBeNull()
    expect(JSON.stringify(storage.dump())).not.toMatch(/workspace|tab|pane|machine_private_key|machinePrivateKey|turn|credential/i)
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

function createMockCrypto(): LocalAppCrypto {
  return {
    async generateKeyPair() {
      return {
        publicKey: { raw: new Uint8Array([1, 2, 3, 4]) },
        privateKey: { keyId: 'generated-app-key' },
      }
    },
    async savePrivateKey() {},
    async loadPrivateKey() {
      return { keyId: 'generated-app-key' }
    },
    async sign() {
      return new TextEncoder().encode('signed-by-app-key')
    },
    async randomBytes(length: number) {
      return new Uint8Array(Array.from({ length }, (_, index) => index + 1))
    },
    async sha256() {
      return new Uint8Array(32)
    },
  }
}

function createMockApi(pair: LocalAgentApi['pair']): LocalAgentApi {
  return {
    async getStatus() {
      throw new Error('not used')
    },
    async listTerminals() {
      throw new Error('not used')
    },
    pair,
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
}
