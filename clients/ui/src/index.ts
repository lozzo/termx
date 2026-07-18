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
export { openProtoEventSubscription } from './core/protoEventSubscription'
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
export {
  decodeFileStreamErrorPayload,
  decodeFileTransferAckPayload,
  decodeFileTransferDataPayload,
  decodeFileTransferFinishPayload,
  decodeFileTransferResultPayload,
  encodeFileTransferAckPayload,
  encodeFileTransferDataPayload,
  encodeFileTransferFinishPayload,
} from './files/fileStreamProtocol'
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
export type { MachineWorkspaceProps, MachineWorkspaceConnector, MachineWorkspaceClientSession, MachineWorkspaceSessionInput, MachineWorkspaceInventoryApi } from './app/MachineWorkspace'
export { MachineList } from './machines/MachineList'
export type { MachineListProps } from './machines/MachineList'
export { MachineBrowserShell } from './app/MachineBrowserShell'
export type { MachineBrowserShellProps } from './app/MachineBrowserShell'
export { RemoteControlApp } from './app/RemoteControlApp'
export type { MachineRuntime, MachineRuntimeFactory, RemoteControlAppProps } from './app/RemoteControlApp'
export type { CloudAccountAdapter, ExternalPairingAdapter, ExternalPairingImportResult } from './app/RemoteControlApp'
export { mountRemoteControlApp } from './entries/mountRemoteControlApp'
export type { RemoteControlEntryOptions } from './entries/mountRemoteControlApp'
export type {
  AppMachineRecord,
  AppMachineSource,
  AppMachineState,
  ConnectionFlowSnapshot,
  ConnectionFlowStage,
} from './state/appMachine'
export { createMachineStore } from './state/machineStore'
export type {
  MachineStore,
  MachineStoreOptions,
  StoredMachineAddresses,
  StoredMachineEndpoints,
  StoredMachineRecord,
} from './state/machineStore'
export {
  createBrowserRemoteNetworkRuntime,
  createFutureNativeRemoteNetworkRuntime,
} from './connection/browserNetworkRuntime'
export type { BrowserRemoteNetworkRuntimeOptions } from './connection/browserNetworkRuntime'
export type { MachineConnectionSnapshot } from './connection/machineConnectionSnapshot'
export { RemoteNetworkStateManager } from './connection/remoteNetworkState'
export type { RemoteNetworkState, RemoteResumeType } from './connection/remoteNetworkState'
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
export * as TermxApiAccessRemote from './generated/apipb/access_remote_pb'
export * as TermxApiApplication from './generated/apipb/application_pb'
export * as TermxApiCommon from './generated/apipb/common_pb'
export * as TermxApiEvents from './generated/apipb/events_pb'
export * as TermxApiFile from './generated/apipb/file_pb'
export * as TermxApiHistory from './generated/apipb/history_pb'
export * as TermxApiRuntime from './generated/apipb/runtime_pb'
export * as TermxApiStorage from './generated/apipb/storage_pb'
export * as TermxApiTerminal from './generated/apipb/terminal_pb'
export * as TermxApiWorkbench from './generated/apipb/workbench_pb'
export * as TermxClientBinding from './generated/bindingpb/client_binding_pb'
export type {
  ProtoClientSession,
  ProtoClientSubscription,
  ProtoResourceStream,
} from './core/protoClientSession'
export { BrowserWasmLifecycle } from './binding/browserWasmLifecycle'
export type { BrowserWasmGeneration, BrowserWasmGenerationFactory, BrowserWasmGenerationListener } from './binding/browserWasmLifecycle'
export { BrowserWasmPlatform, verifyRemoteDTLSCertificate } from './binding/browserWasmPlatform'
export type { BrowserCloudPlatform, BrowserPlatformDiagnostic, BrowserPlatformEventSink } from './binding/browserWasmPlatform'
export { TermxWasmRuntime, loadTermxWasmExports } from './binding/wasmRuntime'
export type { LoadTermxWasmOptions, TermxWasmResult, WasmPlatformDispatcher } from './binding/wasmRuntime'
export { BindingOperation, ProtoBindingClient, ProtoBindingConnector } from './binding/protoBindingClient'
export type { BindingOperationCode, ManagedEndpointInput, ProtoBindingBackend } from './binding/protoBindingClient'
export { WasmBindingBackend } from './binding/wasmBindingBackend'
export { BrowserBindingRuntime } from './binding/browserBindingRuntime'
export { BrowserCloudHttpPlatform } from './binding/browserCloudHttpPlatform'
export type { BrowserCloudEndpoint } from './binding/browserCloudHttpPlatform'
export * as TermxCloud from './generated/cloudpb/cloud_companion_pb'
