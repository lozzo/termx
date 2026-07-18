import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RemoteControlApp, type RemoteControlAppProps } from './RemoteControlApp'
import { createMachineStore } from '../state/machineStore'
import { parsePairingPayload } from '../state/pairingPayload'
import type { RemoteNetworkRuntime, RemoteRuntimeStorage, RtcConnectionStateSnapshot, RtcSession, RtcSessionNegotiationTarget, RtcSubscription } from '../core/transport'
import type { WebControlFetch } from '../api/webControlApi'
import type { MachineConnectionSnapshot } from '../connection/machineConnectionSnapshot'

describe('RemoteControlApp', () => {
  afterEach(() => cleanup())

  it('defaults to the App-first machine home, uses the built-in control URL, and shows terminal settings', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.controlUrl', 'http://127.0.0.1:5174')

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    const shell = screen.getByTestId('termx-web-control-remote')
    expect(shell.className).toContain('h-full')
    expect(shell.className).toContain('min-h-0')
    expect(shell.className).not.toContain('min-h-[100dvh]')
    expect(screen.getByTestId('termx-app-home')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Machines' })).toBeTruthy()
    expect(screen.getByText('No machines yet')).toBeTruthy()
    expect(screen.getByText(/No devices found/)).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))

    expect(screen.getByTestId('termx-app-settings')).toBeTruthy()
    expect(screen.queryByLabelText(/web control/i)).toBeNull()
    expect(screen.getByText('http://114.66.58.243:12306')).toBeTruthy()
    expect((screen.getByLabelText(/^terminal font size$/i) as HTMLInputElement).value).toBe('14')
    expect((screen.getByLabelText(/^terminal font$/i) as HTMLSelectElement).value).toBe('"JetBrainsMono NF", monospace')
    expect(screen.getByRole('radiogroup', { name: 'Font previews' })).toBeTruthy()
    expect(screen.getByRole('radio', { name: /FiraCode NF/ })).toBeTruthy()
    expect((screen.getByLabelText(/terminal theme/i) as HTMLSelectElement).value).toBe('termx-dark')
    expect(screen.getByRole('option', { name: 'Tokyo Night' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'Dracula' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'GitHub Light' })).toBeTruthy()
    expect((screen.getByLabelText(/terminal renderer/i) as HTMLSelectElement).value).toBe('auto')
    expect((screen.getByLabelText(/terminal keyboard mode/i) as HTMLSelectElement).value).toBe('auto')
    expect((screen.getByLabelText(/^terminal scrollback$/i) as HTMLInputElement).value).toBe('10000')
    expect((screen.getByLabelText(/terminal scrollback prefetch threshold/i) as HTMLInputElement).value).toBe('30')
    expect(screen.getByLabelText(/terminal cursor blink/i).getAttribute('aria-pressed')).toBe('true')
  })

  it('shows a web activation code for Official App login without opening a browser', async () => {
    const storage = new MemoryStorage()
    const beginActivation = vi.fn(async () => ({ userCode: 'ABCDE-FGHJK', expiresAtUnix: Date.now() / 1000 + 300 }))
    const cancelActivation = vi.fn(async () => {})
    const awaitActivation = vi.fn(() => new Promise<{ accountId: string; accountLabel: string }>(() => {}))

    render(
      <RemoteControlApp
        cloudAccountAdapter={{
          current: async () => null,
          beginActivation,
          claimActivation: async () => ({ userCode: 'ABCDE-FGHJK', expiresAtUnix: Date.now() / 1000 + 300 }),
          awaitActivation,
          cancelActivation,
          listMachines: async () => [],
          logout: async () => {},
        }}
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
    expect(screen.queryByText(/continue in browser/i)).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: /activate with web/i }))
    await waitFor(() => expect(screen.getByText('ABCDE-FGHJK')).toBeTruthy())
    expect(beginActivation).toHaveBeenCalledTimes(1)
    expect(awaitActivation).toHaveBeenCalledTimes(1)
    await userEvent.click(screen.getByRole('button', { name: /cancel activation/i }))
    await waitFor(() => expect(cancelActivation).toHaveBeenCalledTimes(1))
  })

  it('routes a Cloud activation QR from the home scanner to native account activation', async () => {
    const storage = new MemoryStorage()
    const qrPayload = 'termx-cloud-activate:v1:ABCDE-FGHJK'
    const claimActivation = vi.fn(async () => ({ userCode: 'ABCDE-FGHJK', expiresAtUnix: Date.now() / 1000 + 300 }))
    const awaitActivation = vi.fn(() => new Promise<{ accountId: string; accountLabel: string }>(() => {}))

    render(
      <RemoteControlApp
        cloudAccountAdapter={{
          current: async () => null,
          beginActivation: async () => ({ userCode: 'OTHER-CODE2', expiresAtUnix: Date.now() / 1000 + 300 }),
          claimActivation,
          awaitActivation,
          cancelActivation: async () => {},
          listMachines: async () => [],
          logout: async () => {},
        }}
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        scanPairingCode={vi.fn(async () => qrPayload)}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())
    await waitFor(() => expect(screen.getByText('ABCDE-FGHJK')).toBeTruthy())
    expect(screen.getByTestId('termx-app-settings')).toBeTruthy()
    expect(claimActivation).toHaveBeenCalledWith(qrPayload)
    expect(awaitActivation).toHaveBeenCalledTimes(1)
  })

  it('discovers same-account native cloud daemons while keeping pairing authorization separate', async () => {
    const storage = new MemoryStorage()
    const listMachines = vi.fn(async () => [{
      id: 'daemon-cloud-1',
      name: 'Build workstation',
      osInfo: 'darwin/arm64',
      online: true,
      source: 'hub' as const,
      hubUrls: [],
      hubStatus: 'online',
    }])

    render(
      <RemoteControlApp
        cloudAccountAdapter={{
          current: async () => ({ accountId: 'account-1', accountLabel: 'TermX Dev' }),
          beginActivation: async () => ({ userCode: 'OTHER-CODE2', expiresAtUnix: Date.now() / 1000 + 300 }),
          claimActivation: async () => ({ userCode: 'OTHER-CODE2', expiresAtUnix: Date.now() / 1000 + 300 }),
          awaitActivation: async () => ({ accountId: 'account-1', accountLabel: 'TermX Dev' }),
          cancelActivation: async () => {},
          listMachines,
          logout: async () => {},
        }}
        externalPairingAdapter={{
          import: async () => null,
          isAuthorized: () => false,
          forget: () => {},
        }}
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /^pair build workstation$/i })).toBeTruthy())
    expect(listMachines).toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: /open build workstation/i })).toBeNull()
  })

  it('keeps the Hub daemon name when a pairing bundle carries a client-like label', async () => {
    const storage = new MemoryStorage()
    const authorized = new Set<string>()
    const rawBundle = '{"fixture":"pairing"}'

    render(
      <RemoteControlApp
        cloudAccountAdapter={{
          current: async () => ({ accountId: 'account-1', accountLabel: 'TermX Dev' }),
          beginActivation: async () => ({ userCode: 'OTHER-CODE2', expiresAtUnix: Date.now() / 1000 + 300 }),
          claimActivation: async () => ({ userCode: 'OTHER-CODE2', expiresAtUnix: Date.now() / 1000 + 300 }),
          awaitActivation: async () => ({ accountId: 'account-1', accountLabel: 'TermX Dev' }),
          cancelActivation: async () => {},
          listMachines: async () => [{
            id: 'daemon-cloud-1',
            name: 'Build workstation',
            hostname: 'build-host',
            online: true,
            source: 'hub' as const,
            hubUrls: [],
            hubStatus: 'online',
          }],
          logout: async () => {},
        }}
        externalPairingAdapter={{
          import: async () => {
            authorized.add('daemon-cloud-1')
            return { machine: { id: 'daemon-cloud-1', name: 'HUAWEI JAD-AL00' }, expiresAt: '2099-05-05T10:30:00Z' }
          },
          isAuthorized: (machineId) => authorized.has(machineId),
          authorizationExpiresAt: () => '2099-05-05T10:30:00Z',
          forget: (machineId) => { authorized.delete(machineId) },
        }}
        machineRuntimeFactory={({ machine }) => ({
          api: {
            async getStatus() { return { machine: { machineId: machine.id, name: machine.name, state: 'online' }, localWeb: { httpUrl: '', rtcOfferUrl: '' } } },
            async listTerminals() { return [] },
          },
          connector: { connect: vi.fn(async () => fakeRtcSession()) },
        })}
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(await screen.findByRole('button', { name: /^pair build workstation$/i }))
    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: rawBundle } })
    await userEvent.click(screen.getByRole('button', { name: /^pair device$/i }))

    await waitFor(() => expect(createMachineStore({ storage }).getMachine('daemon-cloud-1')?.name).toBe('Build workstation'))
    expect(createMachineStore({ storage }).getMachine('daemon-cloud-1')?.hostname).toBe('build-host')
    expect(screen.queryByText('HUAWEI JAD-AL00')).toBeNull()
  })

  it('returns to signed-out state when web management removes this client access', async () => {
    const storage = new MemoryStorage()
    const current = vi.fn()
      .mockResolvedValueOnce({ accountId: 'account-1', accountLabel: 'TermX Dev' })
      .mockResolvedValue(null)

    render(
      <RemoteControlApp
        cloudAccountAdapter={{
          current,
          beginActivation: async () => ({ userCode: 'OTHER-CODE2', expiresAtUnix: Date.now() / 1000 + 300 }),
          claimActivation: async () => ({ userCode: 'OTHER-CODE2', expiresAtUnix: Date.now() / 1000 + 300 }),
          awaitActivation: async () => ({ accountId: 'account-1', accountLabel: 'TermX Dev' }),
          cancelActivation: async () => {},
          listMachines: async () => { throw new Error('This device was removed from TermX Cloud') },
          logout: async () => {},
        }}
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(current).toHaveBeenCalledTimes(2))
    expect(screen.queryByText('TermX Dev')).toBeNull()
    expect(screen.getByText('Sign in to sync devices')).toBeTruthy()
  })

  it('persists terminal settings from the settings page', async () => {
    const storage = new MemoryStorage()

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
    fireEvent.change(screen.getByLabelText(/^terminal font size$/i), { target: { value: '18' } })
    fireEvent.change(screen.getByLabelText(/^terminal font$/i), { target: { value: '"FiraCode NF", monospace' } })
    fireEvent.change(screen.getByLabelText(/terminal theme/i), { target: { value: 'dracula' } })
    fireEvent.change(screen.getByLabelText(/terminal renderer/i), { target: { value: 'canvas' } })
    fireEvent.change(screen.getByLabelText(/terminal keyboard mode/i), { target: { value: 'shift' } })
    fireEvent.change(screen.getByLabelText(/^terminal scrollback$/i), { target: { value: '15000' } })
    fireEvent.change(screen.getByLabelText(/terminal scrollback prefetch threshold/i), { target: { value: '120' } })
    fireEvent.click(screen.getByLabelText(/terminal cursor blink/i))

    expect(JSON.parse(storage.getItem('termx.terminal.settings.v1') ?? '{}')).toMatchObject({
      fontSize: 18,
      fontFamily: '"FiraCode NF", monospace',
      themeId: 'dracula',
      renderer: 'canvas',
      keyboardMode: 'shift',
      scrollback: 15000,
      scrollbackPrefetchThresholdRows: 120,
      cursorBlink: false,
    })
  })

  it('logs into Web Control, lists account machines, and claims a TermX pairing code through the machine Hub', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({
      machineId: 'device-1',
      name: 'RedmiBook',
      addresses: {
        public: [],
      },
    }))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        token_type: 'Bearer',
        access_token: 'access-token-1',
        refresh_token: '',
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          control_url: 'http://114.66.58.243:12306',
          current_hub_url: 'http://114.66.58.243:8447',
          hub_urls: ['http://114.66.58.243:8447'],
          hub_status: 'online',
        }],
      }),
      jsonResponse(200, {
        claim_id: 'claim-1',
        machine_id: 'device-1',
        machine_name: 'RedmiBook',
        session_token: 'session-token-device-1',
        expires_at: '2099-05-05T10:30:00Z',
      }),
    ])
    const listTerminals = vi.fn(async () => [{
      terminalId: 'terminal-1',
      machineId: 'device-1',
      title: 'zsh',
      state: 'running' as const,
      command: '/bin/zsh -l',
      cols: 100,
      rows: 30,
    }])
    const connect = vi.fn(async () => fakeRtcSession())
    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        machineRuntimeFactory={({ machine }) => ({
          api: {
            async getStatus() {
              return {
                machine: {
                  machineId: machine.id,
                  name: machine.name,
                  state: 'online',
                },
                localWeb: {
                  httpUrl: '',
                  rtcOfferUrl: machine.hubUrls[0] ?? '',
                },
              }
            },
            listTerminals,
          },
          connector: { connect },
        })}
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
    await userEvent.type(screen.getByLabelText(/email or username/i), 'lozzow@example.test')
    await userEvent.type(screen.getByLabelText(/password/i), 'secret')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    expect(screen.getAllByText('Pair again').length).toBeGreaterThan(0)
    expect(screen.getByText('Cloud')).toBeTruthy()
    expect(screen.queryByText('Ready')).toBeNull()

    await userEvent.click(screen.getByRole('button', { name: /scan to pair redmibook/i }))
    expect(screen.getByTestId('termx-pair-sheet')).toBeTruthy()
    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: pairUri } })
    await userEvent.click(screen.getByRole('button', { name: /^pair device$/i }))

    await waitFor(() => expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy())
    expect(screen.queryByText('Paired RedmiBook')).toBeNull()
    expect(screen.getByRole('heading', { name: 'Terminals' })).toBeTruthy()
    expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
    expect(screen.queryByText('Terminal runtime is not connected yet.')).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: /back to machines/i }))
    expect(screen.getAllByText('Available').length).toBeGreaterThan(0)
    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy()
    await waitFor(() => expect(listTerminals.mock.calls.length).toBeGreaterThanOrEqual(2))
    const hubPairRequest = fetch.requests.find((request) => request.url === 'http://114.66.58.243:8447/api/v1/pairing/claims')
    expect(hubPairRequest?.method).toBe('POST')
    expect(hubPairRequest?.body).toMatchObject({
      machine_id: 'device-1',
      pair_session_id: 'pair-session-1',
      pair_secret: 'pair-secret-1',
      app_device_id: expect.stringMatching(/^appweb_/),
      app_name: 'TermX Remote App',
      requested_capabilities: ['terminal', 'file_manager', 'terminal_management'],
    })
    expect(fetch.requests.some((request) => request.url.includes('/api/v1/machines/device-1/pairing/claims'))).toBe(false)
    const stored = JSON.parse(storage.getItem('termx.app.machines.v2') ?? '[]') as Array<Record<string, unknown>>
    expect(stored).toHaveLength(1)
    expect(stored[0]?.machineId).toBe('device-1')
    expect(stored[0]?.source).toBe('hub')
    expect(stored[0]?.preferredPath).toBe('hub')
    expect(stored[0]?.addresses).toMatchObject({
      public: [],
    })
    expect(stored[0]?.endpoints).toMatchObject({
      hub: 'http://114.66.58.243:8447',
    })
    expect(storage.getItem('termx.session.device-1.token')).toBe('session-token-device-1')
  })

  it('races a global scan through local QR addresses and Hub URLs when the QR matches an account machine', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({
      machineId: 'device-1',
      name: 'RedmiBook',
      addresses: {
        local: ['http://127.0.0.1:18888'],
        lan: [],
        public: [],
      },
    }))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        token_type: 'Bearer',
        access_token: 'access-token-1',
        refresh_token: '',
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          control_url: 'http://114.66.58.243:12306',
          current_hub_url: 'http://114.66.58.243:8447',
          hub_urls: ['http://114.66.58.243:8447'],
          hub_status: 'online',
        }],
      }),
      jsonResponse(200, {
        claim_id: 'claim-1',
        machine_id: 'device-1',
        machine_name: 'RedmiBook',
        session_token: 'session-token-device-1',
        expires_at: '2099-05-05T10:30:00Z',
      }),
    ])

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        machineRuntimeFactory={({ machine }) => ({
          api: {
            async getStatus() {
              return {
                machine: {
                  machineId: machine.id,
                  name: machine.name,
                  state: 'online',
                },
                localWeb: {
                  httpUrl: '',
                  rtcOfferUrl: machine.hubUrls[0] ?? '',
                },
              }
            },
            async listTerminals() {
              return [{
                terminalId: 'terminal-1',
                machineId: 'device-1',
                title: 'zsh',
                state: 'running' as const,
                command: '/bin/zsh -l',
                cols: 100,
                rows: 30,
              }]
            },
          },
          connector: { connect: vi.fn(async () => fakeRtcSession()) },
        })}
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
    await userEvent.type(screen.getByLabelText(/email or username/i), 'lozzow@example.test')
    await userEvent.type(screen.getByLabelText(/password/i), 'secret')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))

    await userEvent.click(headerAddLocalDeviceButton())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: pairUri } })
    await userEvent.click(screen.getByRole('button', { name: /^add device$/i }))

    await waitFor(() => expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy())
    expect(fetch.requests.some((request) => request.url === 'http://127.0.0.1:18888/api/v1/pairing/claims')).toBe(true)
    expect(fetch.requests.some((request) => request.url === 'http://114.66.58.243:8447/api/v1/pairing/claims')).toBe(true)
    expect(fetch.requests.some((request) => request.url.includes('/api/v1/machines/device-1/pairing/claims'))).toBe(false)
    expect(storage.getItem('termx.session.device-1.token')).toBe('session-token-device-1')
    const stored = JSON.parse(storage.getItem('termx.app.machines.v2') ?? '[]') as Array<Record<string, unknown>>
    expect(stored[0]).toMatchObject({
      machineId: 'device-1',
      source: 'hub',
      addresses: {
        local: ['http://127.0.0.1:18888'],
        lan: [],
        public: [],
      },
      endpoints: {
        hub: 'http://114.66.58.243:8447',
      },
    })
  })

  it('uses the first successful local QR pairing claim instead of waiting for Web Control', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({
      machineId: 'device-1',
      name: 'RedmiBook',
      addresses: {
        local: ['http://127.0.0.1:18888'],
        lan: ['http://192.168.1.20:18888'],
        public: [],
      },
    }))
    const fetch = new LocalPairRaceFetch()

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        machineRuntimeFactory={({ machine }) => ({
          api: {
            async getStatus() {
              return {
                machine: {
                  machineId: machine.id,
                  name: machine.name,
                  state: 'online',
                },
                localWeb: {
                  httpUrl: '',
                  rtcOfferUrl: machine.hubUrls[0] ?? '',
                },
              }
            },
            async listTerminals() {
              return []
            },
          },
          connector: { connect: vi.fn(async () => fakeRtcSession()) },
        })}
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
    await userEvent.type(screen.getByLabelText(/email or username/i), 'lozzow@example.test')
    await userEvent.type(screen.getByLabelText(/password/i), 'secret')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))

    await userEvent.click(headerAddLocalDeviceButton())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: pairUri } })
    await userEvent.click(screen.getByRole('button', { name: /^add device$/i }))

    await waitFor(() => expect(storage.getItem('termx.session.device-1.token')).toBe('session-token-local-lan'))
    expect(fetch.requests.some((request) => request.url === 'http://127.0.0.1:18888/api/v1/pairing/claims')).toBe(true)
    expect(fetch.requests.some((request) => request.url === 'http://192.168.1.20:18888/api/v1/pairing/claims')).toBe(true)
    expect(fetch.requests.some((request) => request.url === 'http://114.66.58.243:8447/api/v1/pairing/claims')).toBe(true)
    expect(fetch.requests.some((request) => request.url.includes('/api/v1/machines/device-1/pairing/claims'))).toBe(false)
  })

  it('claims a self-hosted local pairing code through payload public hub URLs', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({
      machineId: 'self-hosted-1',
      name: 'Self Hosted Box',
      schemaVersion: 4,
      addresses: {
        local: [],
        lan: [],
        public: ['https://self-hub-1.termx.test', 'https://self-hub-2.termx.test'],
      },
    }))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        claim_id: 'claim-self-1',
        machine_id: 'self-hosted-1',
        machine_name: 'Self Hosted Box',
        session_token: 'session-token-self-hosted',
        expires_at: '2099-05-05T10:30:00Z',
      }),
    ])

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: pairUri } })
    await userEvent.click(screen.getByRole('button', { name: /^add device$/i }))

    await waitFor(() => expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy())
    expect(screen.queryByText('Paired Self Hosted Box')).toBeNull()
    expect(fetch.requests[0]).toMatchObject({
      method: 'POST',
      url: 'https://self-hub-1.termx.test/api/v1/pairing/claims',
      body: expect.objectContaining({
        machine_id: 'self-hosted-1',
        pair_session_id: 'pair-session-1',
        pair_secret: 'pair-secret-1',
      }),
    })
    const stored = JSON.parse(storage.getItem('termx.app.machines.v2') ?? '[]') as Array<Record<string, unknown>>
    expect(stored[0]).toMatchObject({
      machineId: 'self-hosted-1',
      source: 'local',
      addresses: {
        public: ['https://self-hub-1.termx.test', 'https://self-hub-2.termx.test'],
      },
    })
    expect(storage.getItem('termx.session.self-hosted-1.token')).toBe('session-token-self-hosted')
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Terminals' })).toBeTruthy())
    expect(screen.queryByText('Local app identity storage is required before opening this machine')).toBeNull()
  })

  it('rejects removed QR Hub URLs instead of using them for local pairing fallback', async () => {
    const storage = new MemoryStorage()
    const payload = pairPayload({
      machineId: 'dual-mode-1',
      name: 'Dual Mode Box',
      addresses: {
        local: ['http://127.0.0.1:18888'],
        lan: [],
        public: [],
      },
    })
    payload.hub = {
      hub_urls: ['http://114.66.58.243:8447'],
    }
    const pairUri = termxPairUri(payload)

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: pairUri } })
    await userEvent.click(screen.getByRole('button', { name: /^add device$/i }))

    await waitFor(() => expect(screen.getByText(/unsupported field hub/i)).toBeTruthy())
    expect(storage.getItem('termx.session.dual-mode-1.token')).toBeNull()
  })

  it('claims copied termx remote pair JSON output as a v4 pairing payload', async () => {
    const storage = new MemoryStorage()
    const payload = pairPayload({
      machineId: 'copied-json-1',
      name: 'Copied JSON Box',
      addresses: {
        local: [],
        lan: [],
        public: ['https://copied-json-hub.termx.test'],
      },
    })
    const pairUri = termxPairUri(payload)
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        claim_id: 'claim-copied-json-1',
        machine_id: 'copied-json-1',
        machine_name: 'Copied JSON Box',
        session_token: 'session-token-copied-json',
        expires_at: '2099-05-05T10:30:00Z',
      }),
    ])

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), {
      target: { value: JSON.stringify({ uri: pairUri, payload }, null, 2) },
    })
    await userEvent.click(screen.getByRole('button', { name: /^add device$/i }))

    await waitFor(() => expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy())
    expect(fetch.requests[0]).toMatchObject({
      method: 'POST',
      url: 'https://copied-json-hub.termx.test/api/v1/pairing/claims',
      body: expect.objectContaining({
        machine_id: 'copied-json-1',
        pair_session_id: 'pair-session-1',
        pair_secret: 'pair-secret-1',
      }),
    })
    expect(storage.getItem('termx.session.copied-json-1.token')).toBe('session-token-copied-json')
  })

  it('imports native managed pairing without calling the legacy Hub claim path', async () => {
    const storage = new MemoryStorage()
    const authorized = new Set<string>()
    const rawBundle = JSON.stringify({
      version: 1,
      label: 'Managed Lab',
      device_id: 'managed-device-1',
      device_fingerprint: 'ed25519-sha256:fixture',
      capability_grant: 'termx-grant-v1.secret',
    })
    const importPairing = vi.fn(async (rawValue: string) => {
      expect(rawValue).toBe(rawBundle)
      authorized.add('managed-device-1')
      return {
        machine: { id: 'managed-device-1', name: 'Managed Lab' },
        expiresAt: '2099-05-05T10:30:00Z',
      }
    })

    render(
      <RemoteControlApp
        externalPairingAdapter={{
          import: importPairing,
          isAuthorized: (machineId) => authorized.has(machineId),
          authorizationExpiresAt: () => '2099-05-05T10:30:00Z',
          forget: (machineId) => { authorized.delete(machineId) },
        }}
        machineRuntimeFactory={({ machine }) => ({
          api: {
            async getStatus() {
              return {
                machine: { machineId: machine.id, name: machine.name, state: 'online' },
                localWeb: { httpUrl: '', rtcOfferUrl: '' },
              }
            },
            async listTerminals() { return [] },
          },
          connector: { connect: vi.fn(async () => fakeRtcSession()) },
        })}
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: rawBundle } })
    await userEvent.click(screen.getByRole('button', { name: /^add device$/i }))

    await waitFor(() => expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy())
    expect(importPairing).toHaveBeenCalledTimes(1)
    expect(createMachineStore({ storage }).getMachine('managed-device-1')?.name).toBe('Managed Lab')
    expect(storage.getItem('termx.session.managed-device-1.token')).toBeNull()
  })

  it('requires a fresh native pairing when secure-store metadata points to an expired grant', async () => {
    const storage = new MemoryStorage()
    createMachineStore({ storage }).saveMachine({
      machineId: 'managed-device-1',
      name: 'Managed Lab',
      state: 'online',
      terminalCount: 0,
      source: 'manual',
      addresses: { local: [], lan: [], public: [] },
      endpoints: {},
      addedAt: '2026-07-11T00:00:00Z',
      updatedAt: '2026-07-11T00:00:00Z',
    })

    render(
      <RemoteControlApp
        externalPairingAdapter={{
          import: async () => null,
          isAuthorized: () => true,
          authorizationExpiresAt: () => '2020-01-01T00:00:00Z',
          forget: () => {},
        }}
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    expect(screen.getAllByText('Authorization expired').length).toBeGreaterThan(0)
    await userEvent.click(screen.getByRole('button', { name: /^pair managed lab$/i }))
    expect(screen.getByTestId('termx-pair-sheet')).toBeTruthy()
    expect(screen.getByText(/Authorize Device/i)).toBeTruthy()
  })

  it('keeps the pairing sheet on the app light surface when the terminal theme is dark', async () => {
    const storage = new MemoryStorage()

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())

    const sheet = screen.getByTestId('termx-pair-sheet')
    expect(sheet.className).toContain('h-full')
    expect(sheet.className).toContain('bg-white')
    expect(sheet.className).not.toContain('bg-[var(--termx-surface)]')
    expect(screen.getByLabelText(/termx qr content/i).className).toContain('bg-white')
  })

  it('claims a local pairing code from the camera scanner callback', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({
      machineId: 'camera-local-1',
      name: 'Camera Local Box',
      addresses: {
        local: ['http://192.168.10.2:18888'],
        lan: [],
        public: [],
      },
    }))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        claim_id: 'claim-camera-1',
        machine_id: 'camera-local-1',
        machine_name: 'Camera Local Box',
        session_token: 'session-token-camera-local',
        expires_at: '2099-05-05T10:30:00Z',
      }),
    ])
    const scanPairingCode = vi.fn(async () => pairUri)

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        scanPairingCode={scanPairingCode}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())

    await waitFor(() => expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy())
    expect(scanPairingCode).toHaveBeenCalledTimes(1)
    expect(fetch.requests[0]).toMatchObject({
      method: 'POST',
      url: 'http://192.168.10.2:18888/api/v1/pairing/claims',
    })
    expect(storage.getItem('termx.session.camera-local-1.token')).toBe('session-token-camera-local')
  })

  it('shows a pairing state after the camera scanner returns a QR code', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({
      machineId: 'camera-pending-1',
      name: 'Camera Pending Box',
      addresses: {
        local: ['http://192.168.10.8:18888'],
        lan: [],
        public: [],
      },
    }))
    let resolvePair!: (response: Response) => void
    const fetch: WebControlFetch = async () => new Promise<Response>((resolve) => {
      resolvePair = resolve
    })
    const scanPairingCode = vi.fn(async () => pairUri)

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch, storage)}
        scanPairingCode={scanPairingCode}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())

    await waitFor(() => expect(screen.getByText(/QR code scanned. Pairing this phone/i)).toBeTruthy())
    expect(screen.getByRole('button', { name: /pairing device/i })).toBeTruthy()

    resolvePair(jsonResponse(200, {
      claim_id: 'claim-camera-pending-1',
      machine_id: 'camera-pending-1',
      machine_name: 'Camera Pending Box',
      session_token: 'session-token-camera-pending',
      expires_at: '2099-05-05T10:30:00Z',
    }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy())
  })

  it('closes the pairing sheet when the camera scanner is cancelled', async () => {
    const storage = new MemoryStorage()
    const scanPairingCode = vi.fn<NonNullable<RemoteControlAppProps['scanPairingCode']>>(async (options) => {
      options?.onCancel?.()
      return null
    })

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        scanPairingCode={scanPairingCode}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())

    await waitFor(() => expect(scanPairingCode).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.queryByTestId('termx-pair-sheet')).toBeNull())
  })

  it('keeps the scanned QR content visible when camera pairing fails', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({
      machineId: 'camera-fail-1',
      name: 'Camera Fail Box',
      addresses: {
        local: ['http://192.168.10.9:18888'],
        lan: [],
        public: [],
      },
    }))
    const scanPairingCode = vi.fn(async () => pairUri)
    const fetch: WebControlFetch = async () => {
      throw new TypeError('Failed to fetch')
    }

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch, storage)}
        scanPairingCode={scanPairingCode}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())

    await waitFor(() => expect(screen.getByText(/Cannot reach Hub at http:\/\/192\.168\.10\.9:18888/i)).toBeTruthy())
    expect((screen.getByLabelText(/termx qr content/i) as HTMLTextAreaElement).value).toBe(pairUri)
    expect(screen.getByRole('button', { name: /^add device$/i })).toBeTruthy()
  })

  it('shows an actionable error when the browser cannot reach the scanned Hub', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({
      machineId: 'self-hosted-1',
      name: 'Self Hosted Box',
      addresses: {
        local: [],
        lan: [],
        public: ['https://blocked-hub.termx.test'],
      },
    }))
    const fetch: WebControlFetch = async () => {
      throw new TypeError('Failed to fetch')
    }

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(headerAddLocalDeviceButton())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: pairUri } })
    await userEvent.click(screen.getByRole('button', { name: /^add device$/i }))

    await waitFor(() => expect(screen.getByText(/Cannot reach Hub at https:\/\/blocked-hub\.termx\.test/i)).toBeTruthy())
    expect(screen.queryByText(/^Failed to fetch$/)).toBeNull()
  })

  it('rejects hub-only QR codes when adding a local machine', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({
      machineId: 'local-machine-1',
      name: 'Local Debug Machine',
      addresses: { local: [], lan: [], public: [] },
    }))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        token_type: 'Bearer',
        access_token: 'access-token-1',
        refresh_token: '',
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          online: true,
          source: 'hub',
          hub_status: 'online',
        }],
      }),
    ])
    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
    await userEvent.type(screen.getByLabelText(/email or username/i), 'lozzow@example.test')
    await userEvent.type(screen.getByLabelText(/password/i), 'secret')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))

    await userEvent.click(headerAddLocalDeviceButton())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: pairUri } })
    await userEvent.click(screen.getByRole('button', { name: /^add device$/i }))

    await waitFor(() => expect(screen.getByText(/does not include local addresses/i)).toBeTruthy())
    expect(fetch.requests.some((request) => request.url.includes('/api/v1/pairing/claims'))).toBe(false)
    expect(storage.getItem('termx.session.local-machine-1.token')).toBeNull()
    const stored = JSON.parse(storage.getItem('termx.app.machines.v2') ?? '[]') as Array<Record<string, unknown>>
    expect(stored.some((machine) => machine.machineId === 'local-machine-1')).toBe(false)
  })

  it('shows Hub machines without a local session as needing QR authorization', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.accessToken', 'access-token-1')
    createMachineStore({ storage }).saveFromPairingPayload(parsePairingPayload(JSON.stringify(pairPayload({
      machineId: 'device-1',
      name: 'RedmiBook',
      addresses: { local: [], lan: [], public: ['https://hub-1.termx.test'] },
    }))))
    storage.setItem('termx.session.device-1.token', 'expired-session-token')
    storage.setItem('termx.session.device-1.exp', '2020-01-01T00:00:00Z')
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          current_hub_url: 'https://hub-1.termx.test',
          hub_urls: ['https://hub-1.termx.test'],
          hub_status: 'online',
        }],
      }),
    ])
    const rawManagedBundle = JSON.stringify({
      version: 1,
      label: 'Other daemon',
      device_id: 'device-2',
      device_fingerprint: 'ed25519-sha256:fixture',
      capability_grant: 'termx-grant-v1.secret',
    })
    const importPairing = vi.fn(async (_rawValue: string, _expectedMachineId?: string) => {
      throw new Error('managed pairing endpoint mismatch')
    })

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        externalPairingAdapter={{
          import: importPairing,
          isAuthorized: () => false,
          forget: () => {},
        }}
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /^pair redmibook$/i })).toBeTruthy())
    expect(screen.getAllByText('Authorization expired').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/authorization expired/i).length).toBeGreaterThan(0)
    expect(storage.getItem('termx.session.device-1.token')).toBeNull()
    expect(storage.getItem('termx.session.device-1.exp')).toBe('2020-01-01T00:00:00Z')

    await userEvent.click(screen.getByRole('button', { name: /scan to pair redmibook/i }))

    expect(screen.getByTestId('termx-pair-sheet')).toBeTruthy()
    expect(screen.getByText(/Authorize Device/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /open redmibook/i })).toBeNull()

    fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: rawManagedBundle } })
    await userEvent.click(screen.getByRole('button', { name: /^pair device$/i }))
    await waitFor(() => expect(importPairing).toHaveBeenCalledWith(rawManagedBundle, 'device-1'))
  })

  it('removes local authorization for a Hub machine without deleting the Hub list record', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.accessToken', 'access-token-1')
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    storage.setItem('termx.session.device-1.exp', '2099-05-05T10:30:00Z')
    storage.setItem('termx.session.device-1.answerProofSecret', 'proof-secret-1')
    storage.setItem('termx.app.machines.v2', JSON.stringify([{
      machineId: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      state: 'online',
      terminalCount: 0,
      source: 'hub',
      addresses: { local: [], lan: [], public: [] },
      endpoints: { hub: 'https://hub-1.termx.test' },
      addedAt: '2026-05-05T10:00:00.000Z',
      updatedAt: '2026-05-05T10:00:00.000Z',
    }]))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          current_hub_url: 'https://hub-1.termx.test',
          hub_urls: ['https://hub-1.termx.test'],
          hub_status: 'online',
        }],
      }),
    ])

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /open redmibook/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /more actions for redmibook/i }))
    await userEvent.click(screen.getByRole('button', { name: /remove authorization for redmibook/i }))

    expect(storage.getItem('termx.session.device-1.token')).toBeNull()
    expect(storage.getItem('termx.session.device-1.exp')).toBeNull()
    expect(storage.getItem('termx.session.device-1.answerProofSecret')).toBeNull()
    expect(screen.getByRole('button', { name: /^pair redmibook$/i })).toBeTruthy()
    expect(screen.getAllByText('Pair again').length).toBeGreaterThan(0)
    expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0)
  })

  it('removes hub-only machines and sessions when signing out', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.accessToken', 'access-token-1')
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    storage.setItem('termx.session.device-1.exp', '2099-05-05T10:30:00Z')
    storage.setItem('termx.app.machines.v2', JSON.stringify([{
      machineId: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      state: 'online',
      terminalCount: 0,
      source: 'hub',
      addresses: {
        local: [],
        lan: [],
        public: [],
      },
      endpoints: {
        webControl: 'http://114.66.58.243:12306',
        hub: 'https://hub-1.termx.test',
      },
      addedAt: '2026-05-05T10:00:00.000Z',
      updatedAt: '2026-05-05T10:00:00.000Z',
    }]))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          current_hub_url: 'https://hub-1.termx.test',
          hub_urls: ['https://hub-1.termx.test'],
          hub_status: 'online',
        }],
      }),
    ])

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
    await userEvent.click(screen.getByRole('button', { name: /sign out/i }))

    expect(storage.getItem('termx.remote.accessToken')).toBeNull()
    expect(storage.getItem('termx.session.device-1.token')).toBeNull()
    expect(storage.getItem('termx.app.machines.v2')).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: /back to machines/i }))
    expect(screen.queryByText('RedmiBook')).toBeNull()
  })

  it('downgrades dual-mode Hub machines to local on sign out and merges them again after sign in', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.accessToken', 'access-token-1')
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    storage.setItem('termx.session.device-1.exp', '2099-05-05T10:30:00Z')
    storage.setItem('termx.app.machines.v2', JSON.stringify([{
      machineId: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      state: 'online',
      terminalCount: 0,
      preferredPath: 'hub',
      source: 'hub',
      addresses: {
        local: ['http://127.0.0.1:18888'],
        lan: ['http://192.168.1.20:18888'],
        public: ['https://frp-old.termx.test'],
      },
      endpoints: {
        webControl: 'http://114.66.58.243:12306',
        hub: 'https://hub-old.termx.test',
      },
      addedAt: '2026-05-05T10:00:00.000Z',
      updatedAt: '2026-05-05T10:00:00.000Z',
    }]))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          control_url: 'http://114.66.58.243:12306',
          current_hub_url: 'https://hub-new.termx.test',
          hub_urls: ['https://hub-new.termx.test'],
          hub_status: 'online',
        }],
      }),
      jsonResponse(200, {
        token_type: 'Bearer',
        access_token: 'access-token-2',
        refresh_token: '',
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          control_url: 'http://114.66.58.243:12306',
          current_hub_url: 'https://hub-new.termx.test',
          hub_urls: ['https://hub-new.termx.test'],
          hub_status: 'online',
        }],
      }),
    ])

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
    await userEvent.click(screen.getByRole('button', { name: /sign out/i }))

    let stored = JSON.parse(storage.getItem('termx.app.machines.v2') ?? '[]') as Array<Record<string, unknown>>
    expect(stored).toHaveLength(1)
    expect(stored[0]).toMatchObject({
      machineId: 'device-1',
      source: 'local',
      preferredPath: 'local',
      addresses: {
        local: ['http://127.0.0.1:18888'],
        lan: ['http://192.168.1.20:18888'],
        public: ['https://frp-old.termx.test'],
      },
    })
    expect(storage.getItem('termx.session.device-1.token')).toBe('session-token-device-1')

    await userEvent.type(screen.getByLabelText(/email or username/i), 'lozzow@example.test')
    await userEvent.type(screen.getByLabelText(/password/i), 'secret')
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => expect(screen.getAllByText('Local + Cloud').length).toBeGreaterThan(0))

    stored = JSON.parse(storage.getItem('termx.app.machines.v2') ?? '[]') as Array<Record<string, unknown>>
    expect(stored[0]).toMatchObject({
      machineId: 'device-1',
      source: 'hub',
      preferredPath: 'hub',
      addresses: {
        local: ['http://127.0.0.1:18888'],
        lan: ['http://192.168.1.20:18888'],
        public: ['https://frp-old.termx.test'],
      },
      endpoints: {
        webControl: 'http://114.66.58.243:12306',
        hub: 'https://hub-new.termx.test',
      },
    })
  })

  it('marks stored local machines online from local Hub health before opening', async () => {
    const storage = new MemoryStorage()
    createMachineStore({ storage }).saveFromPairingPayload(parsePairingPayload(JSON.stringify(pairPayload({
      machineId: 'local-device-1',
      name: 'Local Box',
      addresses: {
        local: ['http://192.168.0.103:18888'],
        lan: [],
        public: [],
      },
    }))))
    const fetch = new RouteFetch((url) => {
      if (url === 'http://192.168.0.103:18888/api/health') {
        return jsonResponse(200, { ok: true })
      }
      throw new Error(`unexpected request to ${url}`)
    })

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(fetch.requests.some((request) => request.url === 'http://192.168.0.103:18888/api/health')).toBe(true))
    await waitFor(() => expect(screen.getByText('Action required')).toBeTruthy())
    expect(screen.getByText('Local')).toBeTruthy()
    expect(screen.getByRole('button', { name: /^pair local box$/i })).toBeTruthy()
  })

  it('keeps dual-mode machines online when Web Control is offline but local Hub health is reachable', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.accessToken', 'access-token-1')
    createMachineStore({ storage }).saveFromPairingPayload(parsePairingPayload(JSON.stringify(pairPayload({
      machineId: 'device-1',
      name: 'RedmiBook',
      addresses: {
        local: ['http://192.168.0.103:18888'],
        lan: [],
        public: [],
      },
    }))))
    const fetch = new RouteFetch((url) => {
      if (url === 'http://192.168.0.103:18888/api/health') {
        return jsonResponse(200, { ok: true })
      }
      if (url === 'http://114.66.58.243:12306/api/v1/auth/me') {
        return jsonResponse(200, {
          user: {
            id: 'user-1',
            username: 'lozzow',
            email: 'lozzow@example.test',
          },
        })
      }
      if (url === 'http://114.66.58.243:12306/api/v1/machines') {
        return jsonResponse(200, {
          machines: [{
            id: 'device-1',
            name: 'RedmiBook',
            hostname: 'redmibook',
            online: false,
            source: 'hub',
            current_hub_url: 'https://hub-1.termx.test',
            hub_urls: ['https://hub-1.termx.test'],
            hub_status: 'offline',
          }],
        })
      }
      throw new Error(`unexpected request to ${url}`)
    })

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await waitFor(() => expect(fetch.requests.some((request) => request.url === 'http://192.168.0.103:18888/api/health')).toBe(true))
    await waitFor(() => expect(screen.getByText('Action required')).toBeTruthy())
    expect(screen.getByText('Local + Cloud')).toBeTruthy()
    expect(screen.getByText(/Local online · Cloud offline/)).toBeTruthy()
  })

  it('opens authorized Web Control machines by racing the registered hubs from Web Control', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.accessToken', 'access-token-1')
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    storage.setItem('termx.app.machines.v2', JSON.stringify([{
      machineId: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      state: 'online',
      terminalCount: 0,
      source: 'hub',
      addresses: {
        local: [],
        lan: [],
        public: [],
      },
      endpoints: {
        webControl: 'http://114.66.58.243:12306',
        hub: 'https://hub-1.termx.test',
      },
      addedAt: '2026-05-05T10:00:00.000Z',
      updatedAt: '2026-05-05T10:00:00.000Z',
    }]))
    const fetch = new WebControlHubFallbackFetch()

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())

    const sessionRequests = fetch.requests.filter((request) => request.url.endsWith('/api/v1/sessions'))
    expect(sessionRequests.map((request) => request.url)).toEqual([
      'https://hub-2.termx.test/api/v1/sessions',
      'https://hub-1.termx.test/api/v1/sessions',
    ])
    expect(sessionRequests.find((request) => request.url === 'https://hub-2.termx.test/api/v1/sessions')).toMatchObject({
      url: 'https://hub-2.termx.test/api/v1/sessions',
      body: expect.objectContaining({
        machine_id: 'device-1',
        session_token: 'session-token-device-1',
      }),
    })
  })

  it('races saved local addresses with Web Control hubs and prefers local for signed-in machines', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.accessToken', 'access-token-1')
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    storage.setItem('termx.app.machines.v2', JSON.stringify([{
      machineId: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      state: 'online',
      terminalCount: 0,
      source: 'hub',
      preferredPath: 'hub',
      addresses: {
        local: ['http://127.0.0.1:18888'],
        lan: ['http://192.168.1.20:18888'],
        public: [],
      },
      endpoints: {
        webControl: 'http://114.66.58.243:12306',
        hub: 'https://hub-1.termx.test',
      },
      addedAt: '2026-05-05T10:00:00.000Z',
      updatedAt: '2026-05-05T10:00:00.000Z',
    }]))
    const fetch = new SignedInLocalFirstFetch()
    const sessions: Array<ReturnType<typeof hubTestRtcSession>> = []

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await waitFor(() => expect(healthRequestUrls(fetch.requests)).toEqual(expect.arrayContaining([
      'http://127.0.0.1:18888/api/health',
      'http://192.168.1.20:18888/api/health',
    ])))
    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())

    expect(nonHealthRequestUrls(fetch.requests)).toEqual([
      'http://114.66.58.243:12306/api/v1/auth/me',
      'http://114.66.58.243:12306/api/v1/machines',
      'http://192.168.1.20:18888/api/v1/sessions/ice',
      'http://127.0.0.1:18888/api/v1/sessions/ice',
      'https://hub-1.termx.test/api/v1/sessions/ice',
      'http://192.168.1.20:18888/api/v1/sessions',
    ])
    expect(sessions.some((session) => session.lastPath() === 'local')).toBe(true)
  })

  it('races saved public local mappings with Web Control hubs and prefers local for signed-in machines', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.accessToken', 'access-token-1')
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    storage.setItem('termx.app.machines.v2', JSON.stringify([{
      machineId: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      state: 'online',
      terminalCount: 0,
      source: 'hub',
      preferredPath: 'hub',
      addresses: {
        local: ['http://127.0.0.1:18888'],
        lan: ['http://192.168.1.20:18888'],
        public: ['https://frp.termx.test', 'https://hub-1.termx.test'],
      },
      endpoints: {
        webControl: 'http://114.66.58.243:12306',
        hub: 'https://hub-1.termx.test',
      },
      addedAt: '2026-05-05T10:00:00.000Z',
      updatedAt: '2026-05-05T10:00:00.000Z',
    }]))
    const fetch = new SignedInPublicLocalFallbackFetch()
    const sessions: Array<ReturnType<typeof hubTestRtcSession>> = []

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await waitFor(() => expect(healthRequestUrls(fetch.requests)).toEqual(expect.arrayContaining([
      'http://127.0.0.1:18888/api/health',
      'http://192.168.1.20:18888/api/health',
      'https://frp.termx.test/api/health',
    ])))
    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())

    expect(nonHealthRequestUrls(fetch.requests)).toEqual([
      'http://114.66.58.243:12306/api/v1/auth/me',
      'http://114.66.58.243:12306/api/v1/machines',
      'http://127.0.0.1:18888/api/v1/sessions/ice',
      'http://192.168.1.20:18888/api/v1/sessions/ice',
      'https://frp.termx.test/api/v1/sessions/ice',
      'https://hub-1.termx.test/api/v1/sessions/ice',
      'https://frp.termx.test/api/v1/sessions',
    ])
    expect(sessions.some((session) => session.lastPath() === 'local')).toBe(true)
  })

  it('opens local machines by racing all local endpoints without hub hub fallback', async () => {
    const storage = new MemoryStorage()
    createMachineStore({ storage }).saveFromPairingPayload(parsePairingPayload(JSON.stringify(pairPayload({
      machineId: 'device-1',
      name: 'RedmiBook',
      addresses: {
        local: ['http://127.0.0.1:18888/api/v1/pairing/claims'],
        lan: ['http://192.168.1.20:18888'],
        public: ['http://114.66.58.243:8447', 'https://frp.termx.test'],
      },
    }))))
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    const fetch = new LocalRuntimeFetch()
    const sessions: Array<ReturnType<typeof hubTestRtcSession>> = []

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await waitFor(() => expect(healthRequestUrls(fetch.requests)).toEqual(expect.arrayContaining([
      'http://127.0.0.1:18888/api/health',
      'http://192.168.1.20:18888/api/health',
      'http://114.66.58.243:8447/api/health',
      'https://frp.termx.test/api/health',
    ])))
    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())

    expect(nonHealthRequestUrls(fetch.requests)).toEqual([
      'http://192.168.1.20:18888/api/v1/sessions/ice',
      'http://127.0.0.1:18888/api/v1/sessions/ice',
      'http://114.66.58.243:8447/api/v1/sessions/ice',
      'https://frp.termx.test/api/v1/sessions/ice',
      'http://192.168.1.20:18888/api/v1/sessions',
    ])
    expect(fetch.requests.map((request) => request.url).join('\n')).not.toContain('https://hub-1.termx.test')
    expect(sessions.some((session) => session.lastPath() === 'local')).toBe(true)
  })

  it('keeps a machine runtime alive when returning to the machine list', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.app.machines.v2', JSON.stringify([{
      machineId: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      state: 'online',
      terminalCount: 0,
      source: 'hub',
      addresses: { local: [], lan: [], public: [] },
      endpoints: { hub: 'https://hub-1.termx.test' },
      addedAt: '2026-05-05T10:00:00.000Z',
      updatedAt: '2026-05-05T10:00:00.000Z',
    }]))
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    const dispose = vi.fn()
    const connectionSnapshot: MachineConnectionSnapshot = {
      machineId: 'device-1',
      phase: 'connected',
      statusText: 'Connected through relay',
      connectionInfo: {
        path: 'hub',
        observedPath: 'single_relay',
        connectionId: 'connection-1',
        machineId: 'device-1',
        relayInUse: true,
        rtt: 62,
      },
      forceRelay: true,
      relayInUse: true,
      reconnectAttempt: 0,
      error: null,
    }
    const listTerminals = vi.fn(async () => [{
      terminalId: 'terminal-1',
      machineId: 'device-1',
      title: 'zsh',
      state: 'running' as const,
      command: '/bin/zsh -l',
      cols: 100,
      rows: 30,
    }])
    const runtimeFactory = vi.fn(({ machine }) => ({
      api: {
        async getStatus() {
          return {
            machine: {
              machineId: machine.id,
              name: machine.name,
              state: 'online' as const,
            },
            localWeb: {
              httpUrl: '',
              rtcOfferUrl: machine.hubUrls[0] ?? '',
            },
          }
        },
        listTerminals,
      },
      connector: { connect: vi.fn(async () => fakeRtcSession()) },
      listConnectionState: {
        getSnapshot: () => connectionSnapshot,
        subscribe: () => () => {},
      },
      dispose,
    }))

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        machineRuntimeFactory={runtimeFactory}
        networkRuntime={testNetworkRuntime(fetchNoRequests, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
    expect(runtimeFactory).toHaveBeenCalledTimes(1)

    await userEvent.click(screen.getByRole('button', { name: /back to machines/i }))
    expect(screen.getByTestId('termx-app-home')).toBeTruthy()
    expect(screen.getByText('Access')).toBeTruthy()
    expect(screen.getByText('Connection')).toBeTruthy()
    expect(screen.getByText('Connected')).toBeTruthy()
    expect(screen.getByText('Cloud')).toBeTruthy()
    expect(screen.getByText('0 terminals · Single relay · 62 ms')).toBeTruthy()
    expect(dispose).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
    expect(runtimeFactory).toHaveBeenCalledTimes(1)
    expect(dispose).not.toHaveBeenCalled()
  })

  it('keeps the default hub WebRTC session alive when returning to the machine list', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.app.machines.v2', JSON.stringify([{
      machineId: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      state: 'online',
      terminalCount: 0,
      source: 'hub',
      addresses: { local: [], lan: [], public: [] },
      endpoints: { hub: 'https://hub-1.termx.test' },
      addedAt: '2026-05-05T10:00:00.000Z',
      updatedAt: '2026-05-05T10:00:00.000Z',
    }]))
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    const fetch = new ManagedRuntimeFetch()
    const sessions: Array<ReturnType<typeof hubTestRtcSession>> = []

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
    expect(sessions).toHaveLength(1)

    await userEvent.click(screen.getByRole('button', { name: /back to machines/i }))
    expect(screen.getByTestId('termx-app-home')).toBeTruthy()
    expect(sessions[0]?.disconnectCalls).toBe(0)

    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
    expect(sessions).toHaveLength(1)
    expect(sessions[0]?.disconnectCalls).toBe(0)
  })

  it('reuses the last machine inventory immediately when reopening a machine page', async () => {
    const storage = new MemoryStorage()
    createMachineStore({ storage }).saveFromPairingPayload(parsePairingPayload(JSON.stringify(pairPayload({
      machineId: 'device-1',
      name: 'RedmiBook',
      addresses: { local: [], lan: [], public: ['https://hub-1.termx.test'] },
    }))))
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    const fetch = new ManagedRuntimeFetch()
    let listCallCount = 0

    render(
      <RemoteControlApp
        defaultControlUrl="http://114.66.58.243:12306"
        machineRuntimeFactory={({ machine }) => ({
          api: {
            async getStatus() {
              return {
                machine: {
                  machineId: machine.id,
                  name: machine.name,
                  state: 'online',
                },
                localWeb: {
                  httpUrl: machine.controlUrl ?? '',
                  rtcOfferUrl: machine.hubUrls[0] ?? '',
                },
              }
            },
            async listTerminals() {
              listCallCount += 1
              await new Promise((resolve) => setTimeout(resolve, listCallCount === 1 ? 0 : 20))
              return [{
                terminalId: 'terminal-1',
                machineId: machine.id,
                title: 'zsh',
                state: 'running' as const,
                command: '/bin/zsh -l',
                cols: 100,
                rows: 30,
              }]
            },
          },
          connector: { connect: vi.fn(async () => fakeRtcSession()) },
        })}
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /back to machines/i }))
    expect(screen.getByTestId('termx-app-home')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    expect(screen.getByText('zsh')).toBeTruthy()
    expect(screen.queryByText('Connecting to TermX...')).toBeNull()
  })
})

function fakeRtcSession(): RtcSession {
  return {
    async openTerminal() {
      throw new Error('terminal channel is not used by Web Control app tests')
    },
    async openApi() {
      throw new Error('api channel is not used by Web Control app tests')
    },
    async openFileChannel() {
      throw new Error('file transfer is not used by Web Control app tests')
    },
    subscribeEvents() {
      return { close() {} }
    },
    async getConnectionInfo() {
      return {
        path: 'hub',
        connectionId: 'test-session',
        machineId: 'device-1',
        relayInUse: false,
      }
    },
    async getCapabilities() {
      return {
        terminalAllowed: true,
        apiAllowed: true,
        eventsAllowed: true,
        fileTransferAllowed: true,
        terminalManagementAllowed: true,
        relayInUse: false,
      }
    },
    async disconnect() {},
  }
}

let nextFakeManagedSessionId = 0

const fakeHubRtcSessionFactory = (target?: { machineId?: string | undefined }) => ({
  async createOffer() {
    nextFakeManagedSessionId += 1
    return {
      sessionId: `rtc-web-control-${nextFakeManagedSessionId}`,
      description: { type: 'offer' as const, sdp: `offer-sdp-${nextFakeManagedSessionId}` },
    }
  },
  async acceptAnswer() {},
  async openTerminal() {
    throw new Error('terminal channel is not used by Web Control app tests')
  },
  async openApi() {
    return {
      async request<TResponse>(method: string): Promise<TResponse> {
        if (method !== 'list') throw new Error(`unexpected api request ${method}`)
        return {
          terminals: [{
            terminal_id: 'terminal-1',
            title: 'zsh',
            state: 'running',
            command: '/bin/zsh -l',
            cols: 100,
            rows: 30,
          }],
        } as TResponse
      },
      close() {},
    }
  },
  async openFileChannel() {
    throw new Error('file transfer is not used by Web Control app tests')
  },
  subscribeEvents() {
    return { close() {} }
  },
  subscribeConnectionState() {
    return { close() {} }
  },
  onDisconnect() {
    return { close() {} }
  },
  isAlive() {
    return true
  },
  async handleAppResume() {
    return true
  },
  async waitUntilConnected() {},
  closeTerminalDataChannel() {},
  async getConnectionInfo() {
    return {
      path: 'hub' as const,
      connectionId: 'test-session',
      machineId: target?.machineId ?? 'device-1',
      relayInUse: false,
    }
  },
  async getCapabilities() {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: false,
    }
  },
  async disconnect() {},
}) satisfies RtcSession & {
  createOffer(): Promise<{ sessionId: string; description: { type: 'offer'; sdp: string } }>
  acceptAnswer(): Promise<void>
  subscribeConnectionState(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription
  onDisconnect(handler: () => void): RtcSubscription
  isAlive(): boolean
  handleAppResume(): Promise<boolean>
  waitUntilConnected(): Promise<void>
  closeTerminalDataChannel(terminalId: string): void
}

function hubTestRtcSession(machineId: string) {
  let path: RtcSessionNegotiationTarget['path'] | undefined
  let connectionId = ''
  const connectionStateHandlers = new Set<(snapshot: RtcConnectionStateSnapshot) => void>()
  return {
    disconnectCalls: 0,
    async createOffer(input: RtcSessionNegotiationTarget) {
      path = input.path
      nextFakeManagedSessionId += 1
      connectionId = `rtc-hub-test-${nextFakeManagedSessionId}`
      return {
        sessionId: connectionId,
        description: { type: 'offer' as const, sdp: `offer-sdp-${nextFakeManagedSessionId}` },
      }
    },
    async acceptAnswer() {
      for (const handler of connectionStateHandlers) {
        handler({
          machineId,
          phase: 'connected',
          path: 'hub',
          statusText: 'Connected',
          relayInUse: false,
        })
      }
    },
    async openTerminal() {
      throw new Error('terminal channel is not used by this test')
    },
    async openApi() {
      return {
        async request<TResponse>(method: string): Promise<TResponse> {
          if (method !== 'list') throw new Error(`unexpected api request ${method}`)
          return {
            terminals: [{
              terminal_id: 'terminal-1',
              title: 'zsh',
              state: 'running',
              command: '/bin/zsh -l',
              cols: 100,
              rows: 30,
            }],
          } as TResponse
        },
        close() {},
      }
    },
    async openFileChannel() {
      throw new Error('file transfer is not used by this test')
    },
    subscribeEvents() {
      return { close() {} }
    },
    onDisconnect() {
      return { close() {} }
    },
    subscribeConnectionState(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription {
      connectionStateHandlers.add(handler)
      if (path) {
        handler({
          machineId,
          phase: 'connected',
          path,
          statusText: 'Connected',
          relayInUse: false,
        })
      }
      return { close: () => connectionStateHandlers.delete(handler) }
    },
    isAlive() {
      return true
    },
    async handleAppResume() {
      return true
    },
    async waitUntilConnected() {},
    closeTerminalDataChannel() {},
    async getConnectionInfo() {
      return {
        path: path ?? 'hub',
        connectionId: connectionId || 'rtc-hub-test',
        machineId,
        relayInUse: false,
      }
    },
    async getCapabilities() {
      return {
        terminalAllowed: true,
        apiAllowed: true,
        eventsAllowed: true,
        fileTransferAllowed: true,
        terminalManagementAllowed: true,
        relayInUse: false,
      }
    },
    async disconnect() {
      this.disconnectCalls += 1
    },
    lastPath() {
      return path
    },
  } satisfies RtcSession & {
    disconnectCalls: number
    createOffer(input: RtcSessionNegotiationTarget): Promise<{ sessionId: string; description: { type: 'offer'; sdp: string } }>
    acceptAnswer(): Promise<void>
    subscribeConnectionState(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription
    onDisconnect(handler: () => void): RtcSubscription
    isAlive(): boolean
    handleAppResume(): Promise<boolean>
    waitUntilConnected(): Promise<void>
    closeTerminalDataChannel(terminalId: string): void
    lastPath(): RtcSessionNegotiationTarget['path'] | undefined
  }
}

interface RecordedRequest {
  url: string
  method: string
  headers: Record<string, string>
  body?: unknown
}

class RecordingFetch {
  readonly requests: RecordedRequest[] = []
  private readonly responses: Response[]

  constructor(responses: Response[]) {
    this.responses = [...responses]
  }

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const url = String(input)
    this.requests.push({
      url,
      method: init.method ?? 'GET',
      headers: recordHeaders(init.headers),
      ...(typeof init.body === 'string' ? { body: JSON.parse(init.body) } : {}),
    })
    if (url.endsWith('/api/health')) {
      return jsonResponse(503, { ok: false })
    }
    const response = this.responses.shift()
    if (!response) {
      throw new Error(`unexpected request to ${url}`)
    }
    return response
  }
}

class RouteFetch {
  readonly requests: RecordedRequest[] = []

  constructor(private readonly route: (url: string, init: RequestInit, body: unknown) => Response | Promise<Response>) {}

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined
    const url = String(input)
    this.requests.push({
      url,
      method: init.method ?? 'GET',
      headers: recordHeaders(init.headers),
      ...(body !== undefined ? { body } : {}),
    })
    return this.route(url, init, body)
  }
}

class LocalPairRaceFetch {
  readonly requests: RecordedRequest[] = []

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const url = String(input)
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined
    this.requests.push({
      url,
      method: init.method ?? 'GET',
      headers: recordHeaders(init.headers),
      ...(body !== undefined ? { body } : {}),
    })
    if (url === 'http://114.66.58.243:12306/api/v1/auth/login') {
      return jsonResponse(200, {
        token_type: 'Bearer',
        access_token: 'access-token-1',
        refresh_token: '',
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      })
    }
    if (url === 'http://114.66.58.243:12306/api/v1/auth/me') {
      return jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      })
    }
    if (url === 'http://114.66.58.243:12306/api/v1/machines') {
      return jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          control_url: 'http://114.66.58.243:12306',
          current_hub_url: 'http://114.66.58.243:8447',
          hub_urls: ['http://114.66.58.243:8447'],
          hub_status: 'online',
        }],
      })
    }
    if (url === 'http://127.0.0.1:18888/api/v1/pairing/claims') {
      throw new TypeError('Failed to fetch')
    }
    if (url === 'http://192.168.1.20:18888/api/v1/pairing/claims') {
      return jsonResponse(200, {
        claim_id: 'claim-local-lan',
        machine_id: 'device-1',
        machine_name: 'RedmiBook',
        session_token: 'session-token-local-lan',
        expires_at: '2099-05-05T10:30:00Z',
      })
    }
    if (url === 'http://114.66.58.243:8447/api/v1/pairing/claims') {
      return new Promise<Response>((resolve) => {
        setTimeout(() => {
          resolve(jsonResponse(200, {
            claim_id: 'claim-hub',
            machine_id: 'device-1',
            machine_name: 'RedmiBook',
            session_token: 'session-token-hub',
            expires_at: '2099-05-05T10:30:00Z',
          }))
        }, 50)
      })
    }
    if (url.endsWith('/api/health')) {
      const online = url.includes('192.168.1.20')
      return jsonResponse(online ? 200 : 503, { ok: online })
    }
    throw new Error(`unexpected request to ${url}`)
  }
}

class WebControlHubFallbackFetch {
  readonly requests: RecordedRequest[] = []

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const url = String(input)
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined
    this.requests.push({
      url,
      method: init.method ?? 'GET',
      headers: recordHeaders(init.headers),
      ...(body !== undefined ? { body } : {}),
    })
    if (url === 'http://114.66.58.243:12306/api/v1/auth/me') {
      return jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      })
    }
    if (url === 'http://114.66.58.243:12306/api/v1/machines') {
      return jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          control_url: 'http://114.66.58.243:12306',
          current_hub_url: 'https://hub-2.termx.test',
          hub_urls: ['https://hub-1.termx.test', 'https://hub-2.termx.test'],
          hub_status: 'online',
        }],
      })
    }
    if (url.endsWith('/api/health')) {
      return jsonResponse(503, { ok: false })
    }
    if (url === 'https://hub-1.termx.test/api/v1/sessions/ice') {
      return jsonResponse(200, {
        path: 'hub',
        machine_id: 'device-1',
        ice_servers: [],
        relay_policy: { allow_relay: true, allow_relay_transfer: false },
      })
    }
    if (url === 'https://hub-1.termx.test/api/v1/sessions') {
      return jsonResponse(503, {
        error: {
          code: 'hub_unavailable',
          message: 'first hub unavailable',
        },
      })
    }
    if (url === 'https://hub-2.termx.test/api/v1/sessions/ice') {
      const request = body as {
        machine_id: string
        terminal_id?: string | undefined
      }
      return jsonResponse(200, {
        path: 'hub',
        machine_id: request.machine_id,
        ...(request.terminal_id ? { terminal_id: request.terminal_id } : {}),
        ice_servers: [],
        relay_policy: { allow_relay: true, allow_relay_transfer: false },
      })
    }
    if (url === 'https://hub-2.termx.test/api/v1/sessions') {
      const request = body as {
        machine_id: string
        terminal_id?: string | undefined
        offer: { session_id: string }
      }
      return jsonResponse(200, {
        session_id: request.offer.session_id,
        path: 'hub',
        machine_id: request.machine_id,
        ...(request.terminal_id ? { terminal_id: request.terminal_id } : {}),
        answer: { sdp: 'answer-sdp', ice_candidates: [] },
        ice_servers: [],
        relay_policy: { allow_relay: true, allow_relay_transfer: true },
        relay_in_use: true,
      })
    }
    throw new Error(`unexpected request to ${url}`)
  }
}

class SignedInLocalFirstFetch {
  readonly requests: RecordedRequest[] = []

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const url = String(input)
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined
    this.requests.push({
      url,
      method: init.method ?? 'GET',
      headers: recordHeaders(init.headers),
      ...(body !== undefined ? { body } : {}),
    })
    if (url === 'http://114.66.58.243:12306/api/v1/auth/me') {
      return jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      })
    }
    if (url === 'http://114.66.58.243:12306/api/v1/machines') {
      return jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          control_url: 'http://114.66.58.243:12306',
          current_hub_url: 'https://hub-1.termx.test',
          hub_urls: ['https://hub-1.termx.test'],
          hub_status: 'online',
        }],
      })
    }
    if (url.endsWith('/api/health')) {
      const online = url.includes('192.168.1.20')
      return jsonResponse(online ? 200 : 503, { ok: online })
    }
    if (url === 'http://127.0.0.1:18888/api/v1/sessions/ice') {
      throw new TypeError('Failed to fetch')
    }
    if (url === 'http://192.168.1.20:18888/api/v1/sessions/ice') {
      return jsonResponse(200, {
        path: 'hub',
        machine_id: 'device-1',
        ice_servers: [],
        relay_policy: { allow_relay: false, allow_relay_transfer: false },
      })
    }
    if (url === 'http://192.168.1.20:18888/api/v1/sessions') {
      const request = body as {
        machine_id: string
        offer: { session_id: string }
      }
      return jsonResponse(200, {
        session_id: request.offer.session_id,
        path: 'hub',
        machine_id: request.machine_id,
        answer: { type: 'answer', sdp: 'answer-sdp' },
        ice_candidates: [],
        ice_servers: [],
        relay_policy: { allow_relay: false, allow_relay_transfer: false },
        relay_in_use: false,
      })
    }
    throw new Error(`unexpected request to ${url}`)
  }
}

class SignedInPublicLocalFallbackFetch {
  readonly requests: RecordedRequest[] = []

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const url = String(input)
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined
    this.requests.push({
      url,
      method: init.method ?? 'GET',
      headers: recordHeaders(init.headers),
      ...(body !== undefined ? { body } : {}),
    })
    if (url === 'http://114.66.58.243:12306/api/v1/auth/me') {
      return jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      })
    }
    if (url === 'http://114.66.58.243:12306/api/v1/machines') {
      return jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          source: 'hub',
          control_url: 'http://114.66.58.243:12306',
          current_hub_url: 'https://hub-1.termx.test',
          hub_urls: ['https://hub-1.termx.test'],
          hub_status: 'online',
        }],
      })
    }
    if (url.endsWith('/api/health')) {
      const online = url === 'https://frp.termx.test/api/health'
      return jsonResponse(online ? 200 : 503, { ok: online })
    }
    if (url === 'http://127.0.0.1:18888/api/v1/sessions/ice') {
      throw new TypeError('Failed to fetch')
    }
    if (url === 'http://192.168.1.20:18888/api/v1/sessions/ice') {
      throw new TypeError('Failed to fetch')
    }
    if (url === 'https://frp.termx.test/api/v1/sessions/ice') {
      return jsonResponse(200, {
        path: 'hub',
        machine_id: 'device-1',
        ice_servers: [],
        relay_policy: { allow_relay: false, allow_relay_transfer: false },
      })
    }
    if (url === 'https://frp.termx.test/api/v1/sessions') {
      const request = body as {
        machine_id: string
        offer: { session_id: string }
      }
      return jsonResponse(200, {
        session_id: request.offer.session_id,
        path: 'hub',
        machine_id: request.machine_id,
        answer: { type: 'answer', sdp: 'answer-sdp' },
        ice_candidates: [],
        ice_servers: [],
        relay_policy: { allow_relay: false, allow_relay_transfer: false },
        relay_in_use: false,
      })
    }
    throw new Error(`unexpected request to ${url}`)
  }
}

class ManagedRuntimeFetch {
  readonly requests: RecordedRequest[] = []

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const url = String(input)
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined
    this.requests.push({
      url,
      method: init.method ?? 'GET',
      headers: recordHeaders(init.headers),
      ...(body !== undefined ? { body } : {}),
    })
    if (url === 'https://hub-1.termx.test/api/v1/sessions/ice') {
      return jsonResponse(200, {
        path: 'hub',
        machine_id: 'device-1',
        ice_servers: [],
        relay_policy: { allow_relay: true, allow_relay_transfer: false },
      })
    }
    if (url === 'https://hub-1.termx.test/api/v1/sessions') {
      const request = body as {
        machine_id: string
        offer: { session_id: string }
      }
      return jsonResponse(200, {
        session_id: request.offer.session_id,
        path: 'hub',
        machine_id: request.machine_id,
        answer: { type: 'answer', sdp: 'answer-sdp' },
        ice_candidates: [],
        ice_servers: [],
        relay_policy: { allow_relay: true, allow_relay_transfer: false },
        relay_in_use: false,
      })
    }
    throw new Error(`unexpected request to ${url}`)
  }
}

class LocalRuntimeFetch {
  readonly requests: RecordedRequest[] = []

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const url = String(input)
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined
    this.requests.push({
      url,
      method: init.method ?? 'GET',
      headers: recordHeaders(init.headers),
      ...(body !== undefined ? { body } : {}),
    })
    if (url.endsWith('/api/health')) {
      const online = url.includes('192.168.1.20')
      return jsonResponse(online ? 200 : 503, { ok: online })
    }
    if (url === 'http://127.0.0.1:18888/api/v1/sessions/ice') {
      throw new TypeError('Failed to fetch')
    }
    if (url === 'http://192.168.1.20:18888/api/v1/sessions/ice') {
      return jsonResponse(200, {
        path: 'hub',
        machine_id: 'device-1',
        ice_servers: [],
        relay_policy: { allow_relay: false, allow_relay_transfer: false },
      })
    }
    if (url === 'http://192.168.1.20:18888/api/v1/sessions') {
      const request = body as {
        machine_id: string
        offer: { session_id: string }
      }
      return jsonResponse(200, {
        session_id: request.offer.session_id,
        path: 'hub',
        machine_id: request.machine_id,
        answer: { type: 'answer', sdp: 'answer-sdp' },
        ice_candidates: [],
        ice_servers: [],
        relay_policy: { allow_relay: false, allow_relay_transfer: false },
        relay_in_use: false,
      })
    }
    throw new Error(`unexpected request to ${url}`)
  }
}

class MemoryStorage implements RemoteRuntimeStorage {
  private readonly values = new Map<string, string>()

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
  }

  removeItem(key: string): void {
    this.values.delete(key)
  }

  dump(): Record<string, string> {
    return Object.fromEntries(this.values)
  }
}

const fetchNoRequests: WebControlFetch = async (input) => {
  throw new Error(`unexpected request to ${String(input)}`)
}

function testNetworkRuntime(fetch: WebControlFetch, storage?: RemoteRuntimeStorage | undefined): RemoteNetworkRuntime {
  return {
    fetch,
    ...(storage ? { storage } : {}),
    queryParam() {
      return null
    },
  }
}

function headerAddLocalDeviceButton(): HTMLElement {
  return screen.getAllByRole('button', { name: /^scan new machine$/i })[0] as HTMLElement
}

function healthRequestUrls(requests: readonly RecordedRequest[]): string[] {
  return requests.filter((request) => request.url.endsWith('/api/health')).map((request) => request.url)
}

function nonHealthRequestUrls(requests: readonly RecordedRequest[]): string[] {
  return requests.filter((request) => !request.url.endsWith('/api/health')).map((request) => request.url)
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

function recordHeaders(headers: HeadersInit | undefined): Record<string, string> {
  if (!headers) return {}
  return Object.fromEntries(new Headers(headers).entries())
}

function pairPayload({
  machineId,
  name,
  schemaVersion = 4,
  addresses,
}: {
  machineId: string
  name: string
  schemaVersion?: 4 | undefined
  addresses?: {
    local?: string[] | undefined
    lan?: string[] | undefined
    public?: string[] | undefined
  } | undefined
}): Record<string, unknown> {
  return {
    type: 'termx_pair',
    schema_version: schemaVersion,
    machine: {
      id: machineId,
      name,
      hostname: 'redmibook',
    },
    local: {
      hub_urls: [
        ...(addresses?.local ?? ['http://127.0.0.1:18888']),
        ...(addresses?.lan ?? []),
        ...(addresses?.public ?? ['http://114.66.58.243:8447']),
      ],
    },
    pairing: {
      session_id: 'pair-session-1',
      secret: 'pair-secret-1',
    },
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
