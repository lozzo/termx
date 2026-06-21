# App core-v2 remote contract

This document is the R189 admission contract for connecting the real
`termx-app/` to the CLI-started remote/core-v2 runtime through `remote-ui`.

The active scope and execution order are still defined by the repository-root
`workflow.md`. This file only records the TypeScript runtime/history boundary
that later App slices must implement.

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
  - supports latest/older/range-or-equivalent window requests;
  - returns logical-line identifiers, cursor/token/generation metadata and cell
    content/style sufficient for render, search, selection and copy.
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

The runtime API channel is machine-scoped. It can carry:

- terminal inventory/list;
- terminal create, metadata update, restart, remove and directory requests;
- storage get/put/delete/list;
- event subscription;
- future history window requests that map directly to core-v2 domain.

The terminal datachannel is terminal-scoped. It can carry:

- attach/hello and attachment metadata;
- live output/screen update/snapshot recovery;
- input and resize;
- resize ownership state;
- terminal close/lifecycle notifications.

History/copy must not be hidden inside terminal-scoped `loadScrollback` visual
rows. A history request that is used for copy/search/selection must be a typed
logical-line window request and must include enough identity metadata to detect
generation changes and stale cursors.

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

## History window contract shape

The TypeScript contract should mirror core-v2 concepts rather than xterm rows:

```ts
export interface CoreV2HistoryWindowRequest {
  terminalId: string
  mode: 'latest' | 'older' | 'range'
  limit: number
  cursor?: string
  afterCursor?: string
  startLineId?: string
  endLineId?: string
  generation?: string | number
}

export interface CoreV2LogicalLine {
  id: string
  number?: number
  text: string
  cells?: CoreV2HistoryCell[]
  hardWrapped?: boolean
  timestampUnixMs?: number
}

export interface CoreV2HistoryWindow {
  terminalId: string
  generation: string | number
  lines: CoreV2LogicalLine[]
  firstCursor?: string
  lastCursor?: string
  hasOlder: boolean
  hasNewer: boolean
}
```

Exact names may change during R190-R193, but the semantics cannot: logical lines
are the unit of truth, cursor/generation metadata gates cache validity, and
visual wrapping is a renderer concern.

## Acceptance for later slices

- R190 should introduce the shared core-v2 terminal/history TypeScript contract
  and tests that keep old snapshot/scrollback APIs out of copy/history paths.
- R191 should prove `termx-app` connects to the CLI-started remote runtime using
  the same injected runtime interfaces, without a private App terminal API.
- R192 should prove terminal list/create/attach/input/resize/restart/remove go
  through the runtime API and terminal datachannel backed by core-v2 truth.
- R193 should connect App history to core-v2 `HistoryWindow` logical lines.
- R194 should build the infinite history surface/cache on top of logical-line
  windows; `termx-app-history-ref/` is only a renderer/cache reference.
- R195 should prove copy/search/selection assemble text from logical-line
  history, not xterm/snapshot/DOM/native/local append cache.
- R196 should run the full CLI daemon -> remote local enable -> App connect ->
  terminal -> history rollback -> logical-line copy smoke.
