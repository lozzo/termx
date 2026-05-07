import { describe, expect, it, vi } from 'vitest'
import { createConnectionOrchestrator } from './connectionOrchestrator'
import { ManualLocalHubUrlProvider } from './localHubUrlProvider'
import { createManagedHubApi } from './managedHubApi'
import type { ConnectionInfo, RtcBinaryChannel, RtcJsonRpcChannel, RtcSession } from './transport'

describe('local connection over Hub API', () => {
  it('uses local embedded Hub sessions instead of localweb rtc offer endpoint', async () => {
    const calls: Array<{ url: string; init: RequestInit | undefined }> = []
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return jsonResponse({
        session_id: 'rtc-local-1',
        path: 'managed',
        machine_id: 'machine-local',
        terminal_id: 'terminal-1',
        answer: { sdp: 'answer-sdp', ice_candidates: [] },
        ice_servers: [],
        relay_policy: { allow_relay: false, allow_relay_transfer: false },
        relay_in_use: false,
      })
    })
    const session = new MockManagedLocalSession()
    const connectorInputs: unknown[] = []
    const orchestrator = createConnectionOrchestrator({
      localHubUrlProvider: new ManualLocalHubUrlProvider('http://192.168.1.100:18888'),
      managedHubApiFactory: (hubUrl) => createManagedHubApi({ baseUrl: hubUrl, fetch }),
      managedHubRtcConnectorFactory: ({ api }) => ({
        connect(input) {
          connectorInputs.push(input)
          return api.getSessionIce({
            machineId: input.machineId,
            terminalId: input.terminalId,
            sessionToken: input.sessionToken,
          })
            .then(() => api.createSession({
              machineId: input.machineId,
              terminalId: input.terminalId,
              sessionToken: input.sessionToken,
              offer: { sessionId: 'rtc-local-1', sdp: 'offer-sdp', iceCandidates: [] },
            }))
            .then(() => session)
        },
      }),
    })

    const result = await orchestrator.connect({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-local',
      hubUrls: ['http://192.168.1.100:18888'],
    })

    expect(result.path).toBe('local')
    expect(connectorInputs).toEqual([expect.objectContaining({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      sessionToken: 'session-token-local',
    })])
    expect(calls.map((call) => call.url)).toContain('http://192.168.1.100:18888/api/v1/sessions/ice')
    expect(calls.map((call) => call.url)).toContain('http://192.168.1.100:18888/api/v1/sessions')
    expect(calls.map((call) => call.url).join('\n')).not.toContain(legacyLocalPath('rtc', 'offer'))
  })

  it('uses local embedded Hub pairing claims instead of localweb pair endpoint', async () => {
    const calls: Array<{ url: string; init: RequestInit | undefined }> = []
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return jsonResponse({
        claim_id: 'claim-1',
        machine_id: 'machine-local',
        session_token: 'session-token-local',
        expires_at: '2099-05-06T00:00:00Z',
      })
    })
    const api = createManagedHubApi({ baseUrl: 'http://192.168.1.100:18888', fetch })

    await expect(api.pair({
      machineId: 'machine-local',
      pairSessionId: 'pair-1',
      pairSecret: 'secret-1',
      appDeviceId: 'app-device',
      appName: 'TermX',
      requestedCapabilities: ['terminal'],
    })).resolves.toMatchObject({ machineId: 'machine-local' })

    expect(calls.map((call) => call.url)).toEqual(['http://192.168.1.100:18888/api/v1/pairing/claims'])
    expect(calls.map((call) => call.url).join('\n')).not.toContain(legacyLocalPath('pair'))
  })
})

function legacyLocalPath(...tail: string[]): string {
  return ['', 'api', 'local', ...tail].join('/')
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

class MockManagedLocalSession implements RtcSession {
  async openTerminal(): Promise<RtcBinaryChannel> {
    throw new Error('not used')
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    throw new Error('not used')
  }

  async openFileTransfer(): Promise<RtcBinaryChannel> {
    throw new Error('not used')
  }

  subscribeEvents() {
    return { close() {} }
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    return {
      path: 'local',
      connectionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      relayInUse: false,
    }
  }

  async getCapabilities() {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: false,
    }
  }

  async disconnect() {}
}
