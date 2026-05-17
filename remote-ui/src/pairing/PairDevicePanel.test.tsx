import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PairDevicePanel } from './PairDevicePanel'
import { createMachineSessionStore } from '../state/localAppIdentity'
import type { LocalPairingApi } from '../core/transport'

describe('PairDevicePanel', () => {
  afterEach(() => {
    cleanup()
  })

  it('claims a local pair session and stores only the machine session token', async () => {
    const storage = new MemoryStorage()
    const pair = vi.fn(async () => ({
      machineId: 'machine-local',
      sessionToken: 'session-token-local',
      expiresAt: '2099-05-02T10:30:00Z',
    }))
    const api = createMockApi(pair)

    render(
      <PairDevicePanel
        api={api}
        sessionStore={createMachineSessionStore(storage)}
        appName="TermX Local Web"
        machineId="machine-local"
      />,
    )

    await userEvent.type(screen.getByLabelText('Pair ID'), 'pair-1')
    await userEvent.type(screen.getByLabelText('Pair secret'), 'secret-1')
    await userEvent.click(screen.getByRole('button', { name: /^pair device$/i }))

    await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Paired with machine-local'))
    expect(pair).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-local',
      pairSessionId: 'pair-1',
      pairSecret: 'secret-1',
      appDeviceId: expect.stringMatching(/^appweb_/),
      requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
    }))
    expect(storage.getItem('termx.session.machine-local.token')).toBe('session-token-local')
    expect(storage.getItem('termx.session.machine-local.exp')).toBe('2099-05-02T10:30:00Z')
    expect(JSON.stringify(storage.dump())).not.toMatch(/workspace|tab|pane|appCertificate|machine_private_key|machinePrivateKey|turn|credential/i)
  })

  it('claims a copied termx remote pair uri line as v4 pairing content', async () => {
    const storage = new MemoryStorage()
    const pair = vi.fn(async () => ({
      machineId: 'machine-copied',
      sessionToken: 'session-token-copied',
      expiresAt: '2099-05-02T10:30:00Z',
    }))

    render(
      <PairDevicePanel
        api={createMockApi(pair)}
        sessionStore={createMachineSessionStore(storage)}
        appName="TermX Local Web"
      />,
    )

    await userEvent.type(screen.getByLabelText('TermX QR content'), `uri:\t${termxPairUri(v4PairPayload())}`)
    await userEvent.click(screen.getByRole('button', { name: /^pair device$/i }))

    await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Paired with machine-copied'))
    expect(pair).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-copied',
      pairSessionId: 'pair-copied',
      pairSecret: 'secret-copied',
    }))
    expect(storage.getItem('termx.session.machine-copied.token')).toBe('session-token-copied')
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

function createMockApi(pair: LocalPairingApi['pair']): LocalPairingApi {
  return {
    pair,
  }
}

function v4PairPayload(): Record<string, unknown> {
  return {
    type: 'termx_pair',
    schema_version: 4,
    machine: {
      id: 'machine-copied',
      name: 'Copied Machine',
    },
    local: {
      hub_urls: ['http://127.0.0.1:18888'],
    },
    hub: {
      hub_urls: [],
    },
    pairing: {
      session_id: 'pair-copied',
      secret: 'secret-copied',
    },
    bootstrap: {},
    preferred_path: 'local',
  }
}

function termxPairUri(payload: Record<string, unknown>): string {
  const bytes = new TextEncoder().encode(JSON.stringify(payload))
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return `termx://pair?payload=${btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')}`
}
