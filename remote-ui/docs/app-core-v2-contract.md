# App core-v2 remote contract

This document records the current contract for connecting the real `termx-app/`
to the CLI-started remote/core-v2 runtime through `remote-ui`.

The active scope and execution order are still defined by the repository-root
`workflow.md`. This file records the TypeScript runtime/history boundary that
the App slices R189-R196 established.

## Current code boundary

- `termx-app/src/TermxApp.tsx` mounts `RemoteControlApp` from `@termx/remote-ui`
  and injects a `machineRuntimeFactory`.
- `termx-app/src/NativeConnectionProxy.ts` adapts Capacitor native connection
  state and localhost bridge frames into `remote-ui` interfaces such as
  `RtcSession`, `RtcConnector`, `RtcJsonRpcChannel` and `RtcBinaryChannel`.
- `termx-app/src/plugins/nativeConnection.ts` exposes machine/session/bridge and
  file-transfer operations only. It does not expose terminal lifecycle,
  logical-line history, selection or copy truth.
- `remote-ui/src/core/transport.ts` is the current shared runtime interface
  boundary. UI components consume `RtcSession`, not browser or native transport
  primitives.
- `remote-ui/src/terminal/terminalManagementApi.ts` uses the runtime API channel
  for create/update/restart/remove/directory.
- `remote-ui/src/terminal/terminalProtocolClient.ts` currently owns the live
  terminal datachannel protocol adapter, including attach, input, resize,
  snapshot recovery and visual scrollback replay.
- `remote-ui/src/terminal/useTerminalSession.tsx` currently merges snapshot,
  stream output and visual scrollback into a bounded live text projection.
- `remote-ui/src/terminal/coreV2HistorySource.ts` owns the App/shared
  `history.window` and `history.copy` adapter for core-v2 logical-line history.
- `remote-ui/src/terminal/coreV2HistorySurface.ts` owns the App render/cache
  window projection over `CoreV2HistorySource`.
- `remote-ui/src/terminal/coreV2HistoryInteraction.ts` owns logical-line
  selection, search range and copy request assembly.

## Truth sources

The App stack has three different responsibilities:

| Layer | Owner | Allowed truth |
| --- | --- | --- |
| Core daemon | `termx-core-v2` behind CLI `termx daemon` and `termx-remote.Service` | terminal lifecycle, PTY size, attachment, events, storage, logical-line history |
| Runtime transport | `termx-remote` plus browser/native WebRTC/datachannel bridge | authorization, pairing, session, channel routing, request delivery |
| UI/App projection | `remote-ui` and `termx-app` | connection UI state, live terminal display, short scrollback/render cache, native bridge liveness, file-transfer UI |

`remote-ui` and `termx-app` must not create another terminal, history, storage or
copy truth. If a value needs to survive reconnects or be shared across clients,
it must be represented by the core-v2 remote protocol/runtime API.

## Runtime TypeScript contract

Later implementation slices should converge on these interface roles:

- `RemoteMachineRuntime`
  - owns one machine-scoped `RtcSession` lease;
  - lists terminals through the runtime API channel;
  - exposes terminal management, storage, events and connection state;
  - may be implemented by browser WebRTC or App/native bridge.
- `RemoteTerminalRuntime`
  - opens terminal datachannels through the current `RtcSession`;
  - sends input and resize only through core-v2 terminal protocol methods;
  - receives live stream/snapshot data as display projection.
- `CoreV2HistorySource`
  - requests `HistoryWindow` data from core-v2 logical-line history;
  - supports latest/older/newer/range-style window requests;
  - calls `history.copy` for final copied text;
  - returns logical-line identifiers, cursor/token/generation metadata and cell
    content/style sufficient for render, search, selection and copy.
- `CoreV2HistorySurface`
  - creates the frozen latest window for explicit history mode;
  - pages older/newer with logical cursors and line boundaries;
  - holds only App render/cache rows and overscan metadata;
  - invalidates local cache when token/generation changes.
- `CoreV2HistoryInteraction`
  - maps selection points to logical-line ranges;
  - searches only loaded logical-line surface rows;
  - sends copy requests through `CoreV2HistorySource.copy`.
- `LiveTerminalSurface`
  - consumes stream output, snapshot recovery and short display cache;
  - can use xterm, canvas/WebGL, replay strings and local scrollback;
  - cannot answer copy/search/selection as authoritative history.
- `HistorySurface`
  - enters an explicit history mode with a frozen/tokenized window;
  - uses `CoreV2HistorySource` for paging, invalidation and text assembly;
  - owns only render/cache windows, not history truth.

Browser and native implementations may differ below these interfaces, but
React components must not depend directly on `RTCPeerConnection`,
`RTCDataChannel`, Capacitor plugins, Kotlin bridge frames, `fetch` or
`localStorage`.

## Channel and method boundary

The runtime API channel is machine-scoped. It carries:

- terminal inventory/list;
- terminal create, metadata update, restart, remove and directory requests;
- storage get/put/delete/list;
- event subscription;
- `history.window`, `history.copy` and `history.release` requests that map
  directly to core-v2 domain.

The terminal datachannel is terminal-scoped. It can carry:

- attach/hello and attachment metadata;
- live output/screen update/snapshot recovery;
- input and resize;
- resize ownership state;
- terminal close/lifecycle notifications.

History/copy must not be hidden inside terminal-scoped `loadScrollback` visual
rows. A history request that is used for copy/search/selection is a typed
logical-line window/copy request and must include enough identity metadata to
detect generation changes and stale cursors.

## Existing APIs that are live-cache only

The following current remote-ui APIs can remain temporarily for the live
terminal surface, but later slices must not use them as copy/history truth:

- `TerminalSnapshotPayload.text`, `replay`, `screenReplay`, `screenText` and
  `scrollbackRows`;
- `TerminalProtocolSession.loadScrollback`;
- `TerminalScrollbackPage` and `TerminalScrollbackLoadResult`;
- `TERMX_FRAME_TYPES.historyRequest/historyReplay`;
- `useTerminalSession` state such as `terminalText`, `scrollbackPrefixTextRef`,
  `loadedScrollbackRowsRef` and `historyRevisionRef`;
- xterm selection or buffer rows inside `Terminal.tsx`;
- native bridge `FRAME_SYNC_RESPONSE` and cached connection snapshots.

These are display projection/cache. They may improve live rendering, recovery
and scroll ergonomics, but they cannot produce final copied text or search
matches once App copy/history is implemented.

## History Contract Shape

The TypeScript contract mirrors core-v2 concepts rather than xterm rows:

```ts
export interface CoreV2HistoryWindowRequest {
  terminalId: string
  mode: 'latest' | 'older' | 'newer' | 'oldest' | 'range'
  limit: number
  cols: number
  token?: string
  generation?: string | number | bigint
  beforeCursor?: { lineId: string; rowInLine: number }
  afterCursor?: { lineId: string; rowInLine: number }
  boundaryFirstLineId?: string
  boundaryLastLineId?: string
  range?: {
    startLineId: string
    startCol: number
    endLineId: string
    endCol: number
  }
}

export interface CoreV2HistoryWindow {
  terminalId: string
  token: string
  generation: string
  renderRows: CoreV2HistoryRow[]
  lines: CoreV2HistoryLineSpan[]
  firstLineId?: string
  lastLineId?: string
  cursor?: { lineId: string; rowInLine: number }
  hasMore: boolean
}

export interface CoreV2HistorySurfaceSnapshot {
  token: string | null
  generation: string | null
  rows: CoreV2HistoryRow[]
  renderRows: CoreV2HistoryRow[]
  renderWindow: CoreV2HistoryRenderWindow
  stale: boolean
}

export interface CoreV2HistorySelection {
  anchor: { lineId: string; col: number }
  focus: { lineId: string; col: number }
}
```

The semantics are fixed: logical lines are the unit of truth,
token/generation/cursor metadata gates cache validity, and visual wrapping is a
renderer concern.

## Completion Evidence

- R190 introduced the shared core-v2 terminal/history TypeScript contract and
  tests that keep old snapshot/scrollback APIs out of copy/history paths.
- R191 removed the App/remote-ui private local status API caller. Native local
  probing now uses the CLI remote local/hub `/api/v1/sessions/ice` route.
- R192 proved terminal list/create/attach/input/resize/restart/remove go through
  the runtime API and terminal datachannel backed by core-v2 truth.
- R193 connected App history to core-v2 `HistoryWindow` logical-line rows.
- R194 built the infinite history surface/cache over logical-line windows;
  `termx-app-history-ref/` remains only a renderer/cache reference.
- R195 proved copy/search/selection assemble logical ranges from the history
  surface and final copy text comes from core-v2 `history.copy`, not
  xterm/snapshot/DOM/native/local append cache.
- R196 added an App core-v2 smoke that covers terminal create, attach, input,
  resize, history rollback and logical-line copy through one runtime session.

Current verification commands:

| Scope | Command |
| --- | --- |
| remote-ui | `cd remote-ui && npm run typecheck && npm run test && npm run build` |
| App build | `cd termx-app && npm run build` |
| App e2e smoke | `cd remote-ui && npm run test -- src/integration/appCoreV2EndToEndSmoke.test.ts` |
| App history/copy | `cd remote-ui && npm run test -- src/terminal/coreV2HistorySource.test.ts src/terminal/coreV2HistorySurface.test.ts src/terminal/coreV2HistoryInteraction.test.ts` |

Android/Kotlin compilation was not part of the completed checkpoint because the
machine is missing the Android SDK referenced by
`termx-app/android/local.properties`.
