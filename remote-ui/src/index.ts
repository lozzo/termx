export * from './connectionMessageReducer'
export * from './eventQueue'
export {
  assertRemoteModelShape,
  normalizeMachine,
  normalizeTerminal,
} from './model'
export type {
  LocalRTCInfo,
  Machine,
  MachineState,
  Terminal as RemoteTerminal,
  TerminalState,
} from './model'
export { Terminal } from './Terminal'
export type { TerminalHandle, TerminalProps } from './Terminal'
export { TerminalList } from './TerminalList'
export type { OpenTerminalIntent, TerminalListProps } from './TerminalList'
export { TerminalClient } from './terminalClient'
export type {
  TerminalClientCallbacks,
  TerminalInfoPayload,
  TerminalSnapshotPayload,
  TerminalTransport,
  TerminalTransportEvent,
} from './terminalClient'
export {
  createTerminalInventorySnapshot,
  normalizeTerminalInventory,
  selectTerminal,
} from './terminalInventory'
export type { TerminalInventoryInput, TerminalInventorySnapshot } from './terminalInventory'
export * from './transport'
export * from './useTerminalSession'
