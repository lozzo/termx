import type { ConnectionInfo, RtcConnectionPhase } from '../core/transport'

/** MachineConnectionSnapshot is a UI-only connection projection; it never owns a session or reconnect state. */
export interface MachineConnectionSnapshot {
  machineId: string
  phase: RtcConnectionPhase
  statusText: string
  connectionInfo: ConnectionInfo | null
  forceRelay: boolean
  relayInUse: boolean
  reconnectAttempt: number
  error: string | null
}
