export * from './connection/connectionMessageReducer'
export * from './connection/eventQueue'
export {
  TERMX_NATIVE_BACK_EVENT,
  addNativeBackHandler,
  dispatchNativeBack,
} from './platform/nativeBack'
export type { TermxNativeBackHandler } from './platform/nativeBack'
export {
  TERMX_NATIVE_KEYBOARD_EVENT,
  addNativeKeyboardListener,
  dispatchNativeKeyboardEvent,
} from './platform/nativeKeyboard'
export type { TermxNativeKeyboardEventDetail, TermxNativeKeyboardHandler } from './platform/nativeKeyboard'
export {
  assertRemoteModelShape,
  normalizeMachine,
  normalizeTerminal,
} from './core/model'
export type {
  LocalRTCInfo,
  Machine,
  MachineState,
  Terminal as RemoteTerminal,
  TerminalState,
} from './core/model'
export { Terminal } from './terminal/Terminal'
export type { TerminalHandle, TerminalProps } from './terminal/Terminal'
export { TerminalList } from './terminal/TerminalList'
export type { OpenTerminalIntent, TerminalListProps } from './terminal/TerminalList'
export { MobileTerminalKeybar } from './terminal/MobileTerminalKeybar'
export type { MobileTerminalKeybarProps } from './terminal/MobileTerminalKeybar'
export {
  applyTerminalModifiers,
  nextModifierState,
} from './terminal/mobileTerminalInput'
export type {
  ModifierState,
  TerminalModifierResult,
  TerminalModifierState,
} from './terminal/mobileTerminalInput'
export { createFileApi, createFilePreviewSource } from './files/fileApi'
export type {
  DirListResponse,
  DownloadInitResponse,
  FileApi,
  FileEntry,
  FileEntryType,
  FilePreviewSource,
  FilePreviewStreamChunk,
  FilePreviewStreamOptions,
  FilePreviewStreamProgress,
  FilePreviewStreamResult,
  FileTransferContext,
  TransferInfo,
  TransferStatus,
} from './files/fileApi'
export { FileTransferPanel } from './files/FileTransferPanel'
export { ConnectionStatusBanner } from './connection/ConnectionStatusBanner'
export type { ConnectionStatusBannerProps } from './connection/ConnectionStatusBanner'
export {
  connectionPathLabel,
  connectionPhaseLabel,
  connectionSnapshotFromStatus,
  connectionStateFromAttempt,
  connectionStatusIsSettled,
  createConnectionStatePublisher,
  inferConnectionPhase,
} from './connection/connectionState'
export type { ConnectionStatePublisher } from './connection/connectionState'
export { FileManager } from './files/FileManager'
export type { FileManagerProps } from './files/FileManager'
export { MachineWorkspace } from './app/MachineWorkspace'
export type { MachineWorkspaceProps, MachineWorkspaceConnector, MachineWorkspaceSessionInput, MachineWorkspaceInventoryApi } from './app/MachineWorkspace'
export { MachineList } from './machines/MachineList'
export type { MachineListProps } from './machines/MachineList'
export { MachineBrowserShell } from './app/MachineBrowserShell'
export type { MachineBrowserShellProps } from './app/MachineBrowserShell'
export { RemoteControlApp } from './app/RemoteControlApp'
export type { RemoteControlAppProps } from './app/RemoteControlApp'
export type { ExternalPairingAdapter, ExternalPairingImportResult } from './app/RemoteControlApp'
export { mountRemoteControlApp } from './entries/mountRemoteControlApp'
export type { RemoteControlEntryOptions } from './entries/mountRemoteControlApp'
export type {
  AppMachineRecord,
  AppMachineSource,
  AppMachineState,
  ConnectionFlowSnapshot,
  ConnectionFlowStage,
} from './state/appMachine'
export { parsePairingPayload } from './state/pairingPayload'
export type {
  PairingPayload,
  PairingPayloadLocal,
  PairingPayloadMachine,
  PairingPayloadPairing,
} from './state/pairingPayload'
export { createMachineStore } from './state/machineStore'
export type {
  MachineStore,
  MachineStoreOptions,
  StoredMachineAddresses,
  StoredMachineEndpoints,
  StoredMachinePairing,
  StoredMachineRecord,
} from './state/machineStore'
export { createConnectionOrchestrator } from './connection/connectionOrchestrator'
export type {
  ConnectionAttemptError,
  ConnectionAttemptSnapshot,
  ConnectionAttemptStage,
  ConnectionPolicy,
  ConnectionOrchestrator,
  ConnectionOrchestratorInput,
  ConnectionOrchestratorOptions,
  ConnectionOrchestratorResult,
  HubEndpoint,
  HubEndpointKind,
  HubEndpointScope,
  HubEndpointSource,
} from './connection/connectionOrchestrator'
export { PairDevicePanel } from './pairing/PairDevicePanel'
export type { PairDevicePanelProps } from './pairing/PairDevicePanel'
export {
  createBrowserRemoteNetworkRuntime,
  createFutureNativeRemoteNetworkRuntime,
} from './connection/browserNetworkRuntime'
export type { BrowserRemoteNetworkRuntimeOptions } from './connection/browserNetworkRuntime'
export { MachineConnectionStore } from './connection/machineConnectionStore'
export type { MachineConnectionSnapshot, MachineConnectionStoreOptions } from './connection/machineConnectionStore'
export { RemoteNetworkStateManager } from './connection/remoteNetworkState'
export type { RemoteNetworkState, RemoteResumeType } from './connection/remoteNetworkState'
export {
  createMachineSessionStore,
} from './state/localAppIdentity'
export type {
  MachineSessionStore,
} from './state/localAppIdentity'
export { createTerminalProtocolClient } from './terminal/terminalProtocolClient'
export type { TerminalProtocolClientOptions } from './terminal/terminalProtocolClient'
export { createTermxProtocolMultiplexer } from './terminal/termxProtocolMultiplexer'
export type { TermxProtocolMultiplexer } from './terminal/termxProtocolMultiplexer'
export {
  TERMX_FRAME_TYPES,
  TERMX_MAX_FRAME_SIZE,
  TERMX_PROTOCOL_VERSION,
  decodeTermxFrame,
  encodeResizePayload,
  encodeTermxFrame,
  rowsToText,
  snapshotToReplay,
} from './terminal/termxProtocol'
export type { TermxFrame, TermxFrameType } from './terminal/termxProtocol'
export { TerminalClient } from './terminal/terminalClient'
export type {
  TerminalClientCallbacks,
  TerminalInfoPayload,
  TerminalProtocolEvent,
  TerminalProtocolSession,
  TerminalSnapshotPayload,
} from './terminal/terminalClient'
export {
  copyHistorySelection,
  rangeFromHistorySelection,
  searchHistorySurface,
  selectionFromSurfaceRows,
} from './terminal/coreV2HistoryInteraction'
export {
  CoreV2HistorySurfaceStaleError,
  createCoreV2HistorySurface,
} from './terminal/coreV2HistorySurface'
export {
  CORE_V2_HISTORY_WINDOW_MODES,
  CORE_V2_HISTORY_WINDOW_OPS,
  CORE_V2_TERMINAL_METHODS,
  assertLiveCacheOnlyAPIName,
  coreV2EventFromRuntimeEvent,
  coreV2HistoryCopyRequestToProtocolRequest,
  coreV2HistoryReleaseRequestToProtocolRequest,
  coreV2HistoryWindowFromAPI,
  coreV2HistoryWindowRequestToParams,
} from './terminal/coreV2TerminalProtocol'
export { createCoreV2HistorySource } from './terminal/coreV2HistorySource'
export type {
  CoreV2HistoryCellPoint,
  CoreV2HistorySearchMatch,
  CoreV2HistorySelection,
} from './terminal/coreV2HistoryInteraction'
export type {
  CoreV2HistoryRenderWindow,
  CoreV2HistorySurface,
  CoreV2HistorySurfaceLoadOptions,
  CoreV2HistorySurfaceOptions,
  CoreV2HistorySurfaceSnapshot,
} from './terminal/coreV2HistorySurface'
export type {
  CoreV2HistoryCell,
  CoreV2HistoryCellStyle,
  CoreV2HistoryCopyRequest,
  CoreV2HistoryCursor,
  CoreV2HistoryLineSpan,
  CoreV2HistoryRange,
  CoreV2HistoryReleaseRequest,
  CoreV2HistoryRow,
  CoreV2HistoryWindow,
  CoreV2HistoryWindowMode,
  CoreV2HistoryWindowOp,
  CoreV2HistoryWindowParams,
  CoreV2HistoryWindowRequest,
  CoreV2TerminalProtocolEvent,
  CoreV2TerminalProtocolRequest,
} from './terminal/coreV2TerminalProtocol'
export type { CoreV2HistorySource } from './terminal/coreV2HistorySource'
export {
  DEFAULT_TERMINAL_SETTINGS,
  TERMINAL_FONT_OPTIONS,
  TERMINAL_SETTINGS_STORAGE_KEY,
  TERMINAL_THEME_OPTIONS,
  TERMX_DARK_TERMINAL_THEME,
  normalizeTerminalSettings,
  readTerminalSettings,
  resolveTerminalThemeOption,
  resolveTerminalTheme,
  resolveTerminalThemeUi,
  terminalThemeCssVariables,
  writeTerminalSettings,
} from './terminal/terminalSettings'
export type {
  TerminalFontOption,
  TerminalKeyboardMode,
  TerminalSettings,
  TerminalThemeId,
  TerminalThemeOption,
  TerminalThemeUi,
} from './terminal/terminalSettings'
export {
  haptic,
  hapticError,
  hapticImpact,
  hapticSelection,
  hapticSuccess,
  setHapticImpactHandler,
} from './platform/haptics'
export type { HapticImpactHandler, HapticPattern } from './platform/haptics'
export {
  createTerminalInventorySnapshot,
  normalizeTerminalInventory,
  selectTerminal,
} from './terminal/terminalInventory'
export type { TerminalInventoryInput, TerminalInventorySnapshot } from './terminal/terminalInventory'
export * from './core/transport'
export { createHubApi } from './api/hubApi'
export { createHubRtcConnector } from './webrtc/hubRtcConnector'
export {
  decodeRuntimeAPIRequest,
  decodeRuntimeAPIResponse,
  decodeRuntimeEventEnvelope,
  decodeRuntimeEventSubscribeRequest,
  decodeRuntimeRequestBody,
  decodeRuntimeResponseBody,
  encodeRuntimeAPIRequest,
  encodeRuntimeAPIResponse,
  encodeRuntimeEventEnvelope,
  encodeRuntimeEventSubscribeRequest,
  encodeRuntimeRequestBody,
  encodeRuntimeResponseBody,
  runtimeEventEnvelopeToRtcEvent,
} from './webrtc/runtimeProtocol'
export type {
  RuntimeAPIRequest,
  RuntimeAPIResponse,
  RuntimeEventEnvelope,
  RuntimeEventSubscribeRequest,
} from './webrtc/runtimeProtocol'
export type {
  CreateHubSessionInput,
  HubIceServer,
  HubApi,
  HubApiOptions,
  HubCreateSessionResult,
  HubFetch,
  HubPendingSession,
  HubSession,
  HubSessionIceConfig,
  HubSessionIceInput,
  HubSessionPath,
  HubRelayPolicy,
  PollHubSessionAnswerInput,
} from './api/hubApi'
export type {
  HubRtcConnectInput,
  HubRtcConnectorOptions,
} from './webrtc/hubRtcConnector'
export { createWebControlApi } from './api/webControlApi'
export type {
  WebControlApi,
  WebControlApiOptions,
  WebControlFetch,
  WebControlAuthResult,
  WebControlLoginInput,
  WebControlMachine,
  WebControlUser,
} from './api/webControlApi'
export { useFileManager } from './files/useFileManager'
export type { FileManagerVisibleError, UseFileManagerOptions, UseFileManagerResult } from './files/useFileManager'
export * from './terminal/useTerminalSession'
