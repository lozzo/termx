import { describe, expect, expectTypeOf, it } from 'vitest'
import { CONNECTION_PATHS } from './transport'
import type {
  ConnectionCapabilities,
  ConnectionInfo,
  ConnectionPath,
  LocalAgentApi,
  ManagedRtcConnector,
  ManagedRtcSession,
  RemoteNetworkRuntime,
  RemoteRuntimeStorage,
  RtcBinaryChannel,
  RtcConnector,
  RtcJsonRpcChannel,
  RtcSession,
  TerminalInventoryEvents,
} from './transport'
import transportSource from './transport.ts?raw'

describe('RtcSession public interfaces', () => {
  it('keeps exactly two client-visible connection paths', () => {
    expect(CONNECTION_PATHS).toEqual(['local', 'hub'])
    expectTypeOf<(typeof CONNECTION_PATHS)[number]>().toEqualTypeOf<ConnectionPath>()
    expect(transportSource).not.toMatch(/anonymous_p2p|managed_p2p|paid_relay/)
  })

  it('exposes one platform-neutral runtime session surface', () => {
    expectTypeOf<keyof RtcSession>().toEqualTypeOf<
      | 'openTerminal'
      | 'openApi'
      | 'openFileTransfer'
      | 'subscribeEvents'
      | 'getConnectionInfo'
      | 'getCapabilities'
      | 'disconnect'
    >()
    expectTypeOf<RtcSession>().toMatchTypeOf<{
      openTerminal(terminalId: string): Promise<RtcBinaryChannel>
      openApi(): Promise<RtcJsonRpcChannel>
      openFileTransfer(transferId: string): Promise<RtcBinaryChannel>
      subscribeEvents(handler: (event: unknown) => void): { close(): void }
      getConnectionInfo(): Promise<ConnectionInfo>
      getCapabilities(): Promise<ConnectionCapabilities>
      disconnect(): Promise<void>
    }>()
    expectTypeOf<keyof ConnectionInfo>().toEqualTypeOf<
      | 'path'
      | 'observedPath'
      | 'connectionId'
      | 'machineId'
      | 'terminalId'
      | 'relayInUse'
      | 'type'
      | 'localAddr'
      | 'remoteAddr'
      | 'candidateType'
      | 'remoteCandidateType'
      | 'rtt'
    >()
    expectTypeOf<keyof ConnectionCapabilities>().toEqualTypeOf<
      | 'terminalAllowed'
      | 'apiAllowed'
      | 'eventsAllowed'
      | 'fileTransferAllowed'
      | 'terminalManagementAllowed'
      | 'relayInUse'
      | 'denialReason'
    >()
    expectTypeOf<RtcConnector<{ machineId: string }>>().toMatchTypeOf<{
      connect(input: { machineId: string }, options?: unknown): Promise<RtcSession>
    }>()
    expectTypeOf<keyof ManagedRtcSession>().toEqualTypeOf<
      | keyof RtcSession
      | 'subscribeConnectionState'
      | 'onDisconnect'
      | 'isAlive'
      | 'handleAppResume'
      | 'waitUntilConnected'
      | 'closeTerminalDataChannel'
    >()
    expectTypeOf<ManagedRtcConnector<{ machineId: string }>>().toMatchTypeOf<{
      connect(input: { machineId: string }, options?: unknown): Promise<ManagedRtcSession>
    }>()
    expectTypeOf<RtcBinaryChannel>().toMatchTypeOf<{
      onMessage(handler: (data: Uint8Array) => void): { close(): void }
      onClose(handler: () => void): { close(): void }
    }>()
  })

  it('does not expose old transport boundaries or browser/native implementation details', () => {
    expect(transportSource).not.toMatch(/\bRemoteTransport\b|\bPeerTransport\b|\bTerminalTransport\b/)
    expect(transportSource).not.toMatch(/RTCPeerConnection|RTCDataChannel|nativePlugin|turnCredential|relayTransport/i)
  })

  it('keeps local status api and inventory events outside runtime transport taxonomy', () => {
    expectTypeOf<LocalAgentApi>().toHaveProperty('getStatus')
    expectTypeOf<TerminalInventoryEvents>().toMatchTypeOf<{
      subscribe(machineId: string, handler: (event: { type: 'inventory_changed' }) => void): { close(): void }
    }>()
  })

  it('defines browser-neutral network runtime dependencies before browser implementations', () => {
    expectTypeOf<RemoteRuntimeStorage>().toMatchTypeOf<{
      getItem(key: string): string | null
      setItem(key: string, value: string): void
      removeItem(key: string): void
    }>()
    expectTypeOf<RemoteNetworkRuntime>().toMatchTypeOf<{
      fetch(input: string, init?: RequestInit): Promise<Response>
      storage?: RemoteRuntimeStorage | undefined
      queryParam(name: string): string | null
    }>()
    expect(transportSource).toMatch(/interface RemoteNetworkRuntime/)
  })
})
