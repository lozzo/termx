import { describe, expectTypeOf, it } from 'vitest'
import type {
  BinaryChannel,
  ConnectionInfo,
  LocalAgentApi,
  PeerTransport,
  RemoteTransport,
  TerminalInventoryEvents,
} from './transport'

describe('transport interfaces', () => {
  it('keeps business code behind machine/terminal transport interfaces', () => {
    expectTypeOf<RemoteTransport>().toMatchTypeOf<{
      connect(target: { machineId: string }, options?: unknown): Promise<unknown>
      disconnect(): Promise<void>
      status(): unknown
      openTerminal(terminalId: string): Promise<BinaryChannel>
      openApi(): Promise<unknown>
      openFileTransfer(transferId: string): Promise<BinaryChannel>
      getConnectionInfo(): Promise<ConnectionInfo>
    }>()

    expectTypeOf<PeerTransport>().toHaveProperty('openTerminal')
    expectTypeOf<LocalAgentApi>().toHaveProperty('createRTCAnswer')
    expectTypeOf<TerminalInventoryEvents>().toMatchTypeOf<{
      subscribe(machineId: string, handler: (event: { type: 'inventory_changed' }) => void): { close(): void }
    }>()
  })
})
