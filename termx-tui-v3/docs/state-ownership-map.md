# TUI / core state ownership map

本文梳理当前 `termx-tui-v3` 与 `termx-core-v2` 持有的状态数据，以及这些状态能不能作为 lifecycle、input routing、history truth 或共享协作 truth 使用。

本文不是 wire schema，也不是持久化格式；它是排查边界问题时看的 owner 清单。字段名按当前代码里的主要 struct 归并，少量派生字段只写语义。

## 总览 JSON

```json
{
  "core_v2": {
    "terminal_entity_truth": {
      "owner": "termx-core-v2",
      "primary_files": [
        "termx-core-v2/server.go",
        "termx-core-v2/registry.go",
        "termx-core-v2/terminal.go",
        "termx-core-v2/types.go"
      ],
      "state": {
        "terminal_registry": {
          "struct": "terminalRegistry",
          "fields": ["terminals: map[terminal_id]TerminalInfo"],
          "truth": [
            "terminal id/name/command/tags",
            "terminal PTY size",
            "terminal lifecycle: created/running/exited/removed",
            "created_at/exit_code/exited_at"
          ],
          "not_truth": [
            "TUI pane/floating identity",
            "TUI active focus",
            "TUI runtime channel"
          ]
        },
        "terminal_runtime": {
          "struct": "Terminal",
          "fields": [
            "info",
            "process",
            "live: *live.SurfaceTrack",
            "history: *terminalHistoryPipeline",
            "historyQ",
            "events"
          ],
          "truth": [
            "running process handle",
            "current terminal lifecycle",
            "live surface/cursor/modes",
            "authoritative logical-line history"
          ],
          "not_truth": [
            "which pane is active",
            "which TUI should receive keyboard focus",
            "workbench layout"
          ]
        },
        "terminal_process": {
          "structs": ["ProcessSpec", "TerminalProcess", "ProcessExit"],
          "fields": ["terminal_id", "command", "size", "wait/exit"],
          "truth": [
            "the spawned command",
            "process exit code/time/reason available to core"
          ]
        }
      }
    },
    "attachment_and_protocol_session_state": {
      "owner": "termx-core-v2 protocol session",
      "primary_file": "termx-core-v2/protocol_service.go",
      "state": {
        "protocol_session": {
          "struct": "protocolSession",
          "fields": [
            "attachments: map[channel]protocolAttachment",
            "resizeOwners: map[terminal_id]channel",
            "sizeLocks: map[terminal_id]bool",
            "ownerEpoch",
            "eventCancels",
            "historyPins",
            "historyLatest"
          ],
          "truth": [
            "per-connection attachment channel validity",
            "channel -> terminal/view/surface binding",
            "resize owner and size lock projection for this protocol session",
            "frozen history snapshot pins for copy/history paging"
          ],
          "not_truth": [
            "TUI pane layout",
            "TUI active pane",
            "terminal lifecycle itself"
          ]
        },
        "protocol_attachment": {
          "struct": "protocolAttachment",
          "fields": [
            "terminal_id",
            "channel",
            "mode",
            "resize_policy",
            "surface_id",
            "view_id",
            "epoch"
          ],
          "truth": [
            "whether a protocol input/resize request is allowed for this channel",
            "which terminal a channel currently targets"
          ],
          "not_truth": [
            "pane/floating existence",
            "whether the terminal is exited"
          ]
        }
      }
    },
    "history_truth": {
      "owner": "termx-core-v2 history track",
      "primary_files": [
        "termx-core-v2/history/track.go",
        "termx-core-v2/history/types.go",
        "termx-core-v2/history/store.go",
        "termx-core-v2/history/index.go",
        "termx-core-v2/history/frontier.go",
        "termx-core-v2/history/window.go",
        "termx-core-v2/history/snapshot.go"
      ],
      "state": {
        "history_track": {
          "struct": "history.HistoryTrack",
          "fields": [
            "store",
            "committed",
            "frontier",
            "activeLine",
            "activeCol",
            "overwrite",
            "altScreen",
            "generation",
            "screenRows",
            "screenRow",
            "screen ownership map"
          ],
          "truth": [
            "authoritative history state machine",
            "current primary screen ownership",
            "current mutable frontier",
            "history generation for stale guard"
          ]
        },
        "logical_line_store": {
          "struct": "MemoryLogicalLineStore",
          "fields": ["backend", "nextID"],
          "truth": [
            "logical line payload allocation",
            "line payload replacement/deletion"
          ]
        },
        "storage_backend": {
          "struct": "MemoryStorageBackend",
          "fields": ["lines: map[LogicalLineID]LogicalLine"],
          "truth": [
            "logical line payload bytes/cells"
          ],
          "not_truth": [
            "whether a line is committed",
            "whether a line is still mutable"
          ]
        },
        "committed_index": {
          "struct": "CommittedHistoryIndex",
          "fields": ["ids", "present", "generation"],
          "truth": [
            "which logical lines count as committed history",
            "committed ordering",
            "older pagination boundary"
          ]
        },
        "mutable_frontier": {
          "struct": "MutableFrontier",
          "fields": ["ids", "present", "hidden", "generation"],
          "truth": [
            "which logical lines can still be mutated by terminal semantics",
            "hidden frontier after resize"
          ]
        },
        "frozen_snapshot": {
          "struct": "history.FrozenSnapshot",
          "fields": ["token", "generation", "lines", "committedLines"],
          "truth": [
            "read-only pin of logical lines for one copy/history session"
          ],
          "not_truth": [
            "a second history model",
            "TUI selection state"
          ]
        },
        "history_window": {
          "struct": "history.HistoryWindow",
          "fields": [
            "token",
            "op",
            "cols",
            "rows",
            "spans",
            "cursor",
            "hasMore",
            "generation",
            "firstLineID",
            "lastLineID",
            "loadedLines",
            "totalRows",
            "totalLines"
          ],
          "truth": [
            "authoritative projection response for requested cols/rows"
          ],
          "not_truth": [
            "stored history payload",
            "TUI pane scroll position"
          ]
        }
      }
    },
    "live_surface_truth": {
      "owner": "termx-core-v2 live surface",
      "primary_files": [
        "termx-core-v2/live/types.go",
        "termx-core-v2/terminal.go",
        "termx-core-v2/terminal_live_queue.go"
      ],
      "state": {
        "surface_track": {
          "struct": "live.SurfaceTrack",
          "fields": [
            "size",
            "vt",
            "pending",
            "preserveAltScreenFrameOnExit"
          ],
          "truth": [
            "current live screen cell matrix",
            "current live cursor",
            "current terminal modes",
            "pending incomplete escape sequence"
          ],
          "not_truth": [
            "committed history",
            "copy/history frozen window"
          ]
        },
        "surface_snapshot": {
          "struct": "live.SurfaceSnapshot",
          "fields": ["size", "screen", "cursor", "modes"],
          "truth": [
            "size-bound live projection returned to clients"
          ],
          "not_truth": [
            "history source"
          ]
        },
        "live_ingest_queue": {
          "struct": "terminalLiveIngestQueue",
          "fields": ["pending", "closed", "done"],
          "truth": [
            "queued live output waiting to update current screen"
          ],
          "not_truth": [
            "terminal lifecycle",
            "history completeness"
          ]
        }
      }
    },
    "daemon_storage_and_events": {
      "owner": "termx-core-v2 daemon",
      "primary_files": [
        "termx-core-v2/storage.go",
        "termx-core-v2/events.go",
        "termx-core-v2/workbench.go"
      ],
      "state": {
        "opaque_storage": {
          "struct": "storageStore",
          "fields": ["entries: map[app_id, scope, owner_id, key]StorageEntry"],
          "truth": [
            "opaque app-scoped value bytes",
            "storage version",
            "updated_at",
            "CAS conflict boundary"
          ],
          "not_truth": [
            "terminal lifecycle",
            "input channel validity",
            "history truth"
          ]
        },
        "event_broker": {
          "struct": "eventBroker",
          "fields": ["subscribers", "filters", "buffer", "nextID", "closed"],
          "truth": [
            "who should receive core events"
          ],
          "not_truth": [
            "event delivery as durable state"
          ]
        },
        "legacy_workbench_store": {
          "struct": "workbenchStore",
          "fields": ["snapshot", "nextID"],
          "status": "legacy/migration debt",
          "truth": [
            "old workbench API state if that API is used"
          ],
          "not_truth": [
            "new tui-v3 terminal lifecycle",
            "new input routing"
          ]
        }
      }
    }
  },
  "tui_v3": {
    "reducer_owned_root": {
      "owner": "current TUI process",
      "primary_file": "termx-tui-v3/state/root.go",
      "state": {
        "root": {
          "struct": "state.Root",
          "fields": [
            "generation",
            "history",
            "copyMode",
            "clipboard",
            "surface",
            "session",
            "terminalViews",
            "terminalPool",
            "viewport",
            "shell",
            "hostTheme",
            "workbenchSync"
          ],
          "truth": [
            "current TUI process UI state",
            "current reducer-owned caches/projections"
          ],
          "not_truth": [
            "core terminal lifecycle",
            "core history truth",
            "daemon attachment registry"
          ]
        }
      }
    },
    "shell_and_workbench_ui_state": {
      "owner": "current TUI process, optionally persisted as opaque workbench storage",
      "primary_files": [
        "termx-tui-v3/state/shell.go",
        "termx-tui-v3/state/workbench_storage.go"
      ],
      "state": {
        "shell": {
          "struct": "ShellStore",
          "fields": [
            "workspace",
            "workspaces",
            "floatings",
            "activeFloatingID",
            "panelPresentation",
            "activePaneID",
            "zoomedPaneID",
            "interactionMode",
            "ownerConfirm",
            "headerVisible",
            "footerVisible",
            "overlay",
            "emptyPaneCTA",
            "exitedPaneCTA",
            "toasts",
            "nextToastSeq",
            "nextFloatingSeq"
          ],
          "truth": [
            "which pane/floating is active in this TUI",
            "workspace/tab/pane/floating layout",
            "overlay and prompt state",
            "UI CTA selection",
            "current interaction mode"
          ],
          "not_truth": [
            "terminal running/exited",
            "input channel",
            "history source"
          ]
        },
        "pane": {
          "struct": "PaneState",
          "fields": ["id", "title", "kind", "terminalID", "active"],
          "truth": [
            "panel slot identity",
            "presentation-level terminal intent"
          ],
          "not_truth": [
            "terminal lifecycle",
            "attachment channel"
          ],
          "allowed_kinds": ["empty", "terminal-live"]
        },
        "floating": {
          "struct": "FloatingPaneState",
          "fields": ["id", "title", "pane", "rect", "z", "active", "collapsed", "fitMode", "autoFit"],
          "truth": [
            "floating panel local layout and presentation"
          ],
          "not_truth": [
            "terminal lifecycle"
          ]
        },
        "workbench_storage_snapshot": {
          "struct": "WorkbenchStorageSnapshot",
          "fields": [
            "schema",
            "schemaVersion",
            "workspace",
            "workspaces",
            "floatings",
            "activeFloatingID",
            "panelPresentation",
            "activePaneID",
            "zoomedPaneID",
            "headerVisible",
            "footerVisible",
            "terminalViews"
          ],
          "truth": [
            "persisted/shared TUI workbench layout",
            "persisted/shared connection intent"
          ],
          "not_truth": [
            "current terminal lifecycle",
            "runtime channel freshness",
            "copy mode selection",
            "live cursor"
          ]
        },
        "workbench_sync": {
          "struct": "WorkbenchSyncStore",
          "fields": [
            "ref",
            "lastSavedVersion",
            "lastAppliedVersion",
            "lastEventVersion",
            "baseVersion",
            "conflictVersion",
            "conflict"
          ],
          "truth": [
            "current TUI sync bookkeeping against core opaque storage"
          ],
          "not_truth": [
            "workbench schema itself",
            "terminal lifecycle"
          ]
        }
      }
    },
    "terminal_view_and_input_binding_state": {
      "owner": "current TUI process",
      "primary_file": "termx-tui-v3/state/terminal_view.go",
      "state": {
        "terminal_views": {
          "struct": "TerminalViewStore",
          "fields": [
            "views: map[view_id]TerminalViewBinding",
            "paneViews: map[pane_id]view_id",
            "floatingViews: map[floating_id]view_id"
          ],
          "truth": [
            "which TerminalView binding belongs to which panel",
            "active panel input target lookup basis"
          ],
          "not_truth": [
            "core terminal lifecycle",
            "global fallback input target"
          ]
        },
        "terminal_view_binding": {
          "struct": "TerminalViewBinding",
          "fields": [
            "viewID",
            "surfaceID",
            "terminalID",
            "channel",
            "layout",
            "resizeRole",
            "desiredCols",
            "desiredRows",
            "requestSeq",
            "lastError",
            "paneID",
            "floatingID",
            "attached",
            "canResize",
            "sizeLocked",
            "controlReason",
            "ownerSurfaceID",
            "ownerViewID",
            "resizeEpoch"
          ],
          "truth": [
            "current TUI's view-scoped connection intent and latest known channel",
            "current TUI's resize projection for this view",
            "input routing target after active pane/floating is resolved"
          ],
          "not_truth": [
            "whether terminal process is running",
            "whether a stale channel is still accepted by core",
            "sibling panel channel"
          ]
        },
        "terminal_view_layout": {
          "struct": "TerminalViewLayout",
          "fields": ["sizeLocked", "mode", "panX", "panY", "alignX", "alignY"],
          "truth": [
            "view-local presentation/layout preference"
          ],
          "not_truth": [
            "terminal PTY size lock truth",
            "core resize owner truth"
          ]
        }
      }
    },
    "live_projection_and_session_cache": {
      "owner": "current TUI process",
      "primary_file": "termx-tui-v3/state/live.go",
      "state": {
        "surface": {
          "struct": "TerminalSurfaceStore",
          "fields": [
            "terminalID",
            "revision",
            "cols",
            "rows",
            "lines",
            "screen",
            "title",
            "cursor",
            "modes",
            "ready",
            "state",
            "exitCode",
            "exitReason",
            "exitedAt",
            "command",
            "err",
            "resizeBoundary",
            "surfaces: map[terminal_id]LiveSurfaceSnapshot"
          ],
          "truth": [
            "current TUI's cached live surface projection",
            "current TUI's cached lifecycle projection from core list/surface/event"
          ],
          "not_truth": [
            "authoritative history",
            "primary terminal lifecycle owner",
            "input target if active view binding disagrees"
          ]
        },
        "live_snapshot": {
          "struct": "LiveSurfaceSnapshot",
          "fields": [
            "terminalID",
            "revision",
            "cols",
            "rows",
            "lines",
            "screen",
            "title",
            "cursor",
            "modes",
            "lifecycleKnown",
            "state",
            "exitCode",
            "exitReason",
            "exitedAt",
            "command",
            "err"
          ],
          "truth": [
            "service/event result to be reduced into TerminalSurfaceStore"
          ],
          "not_truth": [
            "TUI storage state"
          ]
        },
        "session": {
          "struct": "TerminalSessionStore",
          "fields": [
            "terminalID",
            "channel",
            "inputChannels",
            "attached",
            "cols",
            "rows",
            "resizePolicy",
            "surfaceID",
            "viewID",
            "desiredCols",
            "desiredRows",
            "resizeRequestSeq",
            "resizeConfirmedSeq",
            "lastError",
            "state",
            "exitCode",
            "exitReason",
            "exitedAt",
            "command"
          ],
          "truth": [
            "current TUI's attach/session cache",
            "legacy/global-ish compatibility projection for active session"
          ],
          "not_truth": [
            "input target for multi-view routing",
            "terminal lifecycle authority"
          ]
        },
        "live_modes": {
          "struct": "LiveTerminalModes",
          "fields": [
            "mouseTracking",
            "mouseX10",
            "mouseNormal",
            "mouseButton",
            "mouseAny",
            "mouseSGR",
            "bracketedPaste"
          ],
          "truth": [
            "whether host mouse/key sequences should be passed through to terminal"
          ],
          "not_truth": [
            "which terminal receives ordinary keyboard input"
          ]
        }
      }
    },
    "history_and_copy_interaction_state": {
      "owner": "current TUI process",
      "primary_file": "termx-tui-v3/state/history.go",
      "state": {
        "history_store": {
          "struct": "HistoryStore",
          "fields": [
            "viewID",
            "paneID",
            "terminalID",
            "token",
            "cols",
            "sourceLines",
            "rows",
            "lines",
            "cursor",
            "generation",
            "boundary",
            "hasMore",
            "exhausted",
            "pending"
          ],
          "truth": [
            "current TUI's accepted authoritative history window/cache",
            "pending request guard"
          ],
          "not_truth": [
            "committed history storage",
            "live surface truth"
          ]
        },
        "copy_mode": {
          "struct": "CopyModeStore",
          "fields": [
            "active",
            "entering",
            "paneID",
            "viewID",
            "terminalID",
            "viewportTop",
            "viewRows",
            "cursor",
            "mark",
            "selection",
            "query",
            "matches",
            "activeMatch",
            "boundToken",
            "boundCols",
            "requestID",
            "empty"
          ],
          "truth": [
            "copy/history user interaction state",
            "selection/cursor/search state over current authoritative window"
          ],
          "not_truth": [
            "history payload truth",
            "terminal lifecycle"
          ]
        },
        "history_window_dto": {
          "struct": "state.HistoryWindow",
          "fields": [
            "viewID",
            "paneID",
            "terminalID",
            "token",
            "op",
            "cols",
            "sourceLines",
            "rows",
            "lines",
            "cursor",
            "hasMore",
            "generation",
            "boundary",
            "loadedLines",
            "totalLines",
            "responseKind"
          ],
          "truth": [
            "TUI-side DTO converted from core authoritative response"
          ],
          "not_truth": [
            "a second history model"
          ]
        }
      }
    },
    "terminal_pool_projection_state": {
      "owner": "current TUI process",
      "primary_file": "termx-tui-v3/state/terminal_pool.go",
      "state": {
        "terminal_pool": {
          "struct": "TerminalPoolStore",
          "fields": [
            "status",
            "items",
            "requestSeq",
            "appliedSeq",
            "lastError",
            "lastCreatedID",
            "lastAttachedID",
            "lastKilledID",
            "lastRemovedID",
            "lastEditedID",
            "lastRestartedID"
          ],
          "truth": [
            "current TUI's cached result of core terminal list/actions",
            "stale guard for terminal list"
          ],
          "not_truth": [
            "core terminal registry",
            "input route target"
          ]
        },
        "terminal_pool_item": {
          "struct": "TerminalPoolItem",
          "fields": [
            "terminalID",
            "title",
            "state",
            "cwd",
            "command",
            "tags",
            "exitCode",
            "exitedAt",
            "cols",
            "rows",
            "attached"
          ],
          "truth": [
            "projection of core terminal info for picker/list/render"
          ],
          "not_truth": [
            "TUI storage truth",
            "channel freshness"
          ]
        }
      }
    },
    "clipboard_and_host_state": {
      "owner": "current TUI process, optionally synced through core opaque storage",
      "primary_files": [
        "termx-tui-v3/state/clipboard.go",
        "termx-tui-v3/state/viewport.go",
        "termx-tui-v3/state/host_theme.go"
      ],
      "state": {
        "clipboard": {
          "struct": "ClipboardStore",
          "fields": [
            "entries",
            "lastSavedVersion",
            "lastAppliedVersion",
            "lastEventVersion",
            "baseVersion",
            "conflictVersion",
            "conflict",
            "dirty",
            "dirtyMergeable"
          ],
          "truth": [
            "current TUI clipboard history projection",
            "storage sync bookkeeping"
          ],
          "not_truth": [
            "system clipboard state"
          ]
        },
        "viewport": {
          "struct": "ViewportStore",
          "fields": ["cols", "rows", "valid"],
          "truth": [
            "host TTY drawable canvas size"
          ],
          "not_truth": [
            "terminal PTY size",
            "history projection cols unless explicitly requested"
          ]
        },
        "host_theme": {
          "struct": "HostThemeStore",
          "fields": ["defaultFG", "defaultBG", "palette", "probed"],
          "truth": [
            "current host terminal theme/palette probe result"
          ],
          "not_truth": [
            "terminal content SGR colors"
          ]
        }
      }
    },
    "runtime_host_and_render_ephemeral_state": {
      "owner": "current TUI process runtime/host/render output boundary",
      "primary_files": [
        "termx-tui-v3/app/runtime.go",
        "termx-tui-v3/terminalhost/host.go",
        "termx-tui-v3/terminalhost/input_parser.go",
        "termx-tui-v3/terminalhost/frame_sink.go",
        "termx-tui-v3/terminalhost/latest_frame_sink.go",
        "termx-tui-v3/render/types.go",
        "termx-tui-v3/render/vm.go"
      ],
      "state": {
        "app_runtime": {
          "struct": "AppRuntime",
          "fields": [
            "state",
            "queue",
            "lastHitRegions",
            "mouseDrag",
            "lastMouseAction",
            "hostSizeInitialized",
            "lastToastTick",
            "running",
            "quit",
            "firstFrameWritten",
            "startupFrameReady",
            "copyHistoryPatch",
            "diagnostics"
          ],
          "truth": [
            "event loop scheduling and current process interaction cache",
            "last render hit regions for mouse routing"
          ],
          "not_truth": [
            "terminal lifecycle",
            "history truth",
            "persistent workbench state"
          ]
        },
        "terminal_host": {
          "struct": "terminalhost.Host",
          "fields": [
            "input/output/fd",
            "cancelReader",
            "resizeSignalStop",
            "events",
            "ready",
            "sink",
            "latestSink",
            "themeProbe",
            "state",
            "entered",
            "closed"
          ],
          "truth": [
            "host TTY mode and event stream state"
          ],
          "not_truth": [
            "core terminal process state",
            "TUI business state"
          ]
        },
        "input_parser": {
          "struct": "InputParser",
          "fields": ["pending"],
          "truth": [
            "incomplete host input escape sequence"
          ],
          "not_truth": [
            "terminal input route"
          ]
        },
        "frame_sink": {
          "struct": "FrameSink",
          "fields": ["lastLines", "lastWidth", "lastHeight", "lastCursor", "hasLastFrame"],
          "truth": [
            "host output diff cache"
          ],
          "not_truth": [
            "render view model",
            "terminal content"
          ]
        },
        "latest_frame_sink": {
          "struct": "LatestFrameSink",
          "fields": ["pending", "patches", "closed", "highWaterMark"],
          "truth": [
            "output backpressure queue"
          ],
          "not_truth": [
            "UI state",
            "terminal state"
          ]
        },
        "render_frame": {
          "struct": "render.Frame",
          "fields": [
            "lines",
            "styledLines",
            "ansiLines",
            "patch",
            "cursor",
            "cursorRect",
            "blink",
            "hitRegions",
            "metadata",
            "theme"
          ],
          "truth": [
            "derived output for one render pass"
          ],
          "not_truth": [
            "persistent or reducer-owned state"
          ]
        }
      }
    }
  }
}
```

## Owner table

| Owner | Main structs | Data held | Can decide terminal lifecycle? | Can route ordinary keyboard input? | Can decide history truth? |
| --- | --- | --- | --- | --- | --- |
| core terminal registry | `terminalRegistry`, `TerminalInfo` | terminal id/name/command/tags/size/state/exit metadata | Yes | No | No |
| core terminal runtime | `Terminal` | process handle, live surface, history pipeline, queues | Yes | Only after protocol/session validates channel | Owns history through `HistoryTrack` |
| core protocol session | `protocolSession`, `protocolAttachment` | per-connection channel attachment, resize owner, size lock, history pins | No, reads terminal info | Yes, validates channel/view/terminal | Pins frozen snapshots, not the base truth |
| core history | `HistoryTrack`, `LogicalLineStore`, `CommittedHistoryIndex`, `MutableFrontier` | logical lines, committed order, mutable frontier, screen ownership | No | No | Yes |
| core live surface | `live.SurfaceTrack` | current screen/cursor/modes/pending escape | No | Mouse/bracketed-paste mode projection only | No |
| core opaque storage | `storageStore`, `StorageEntry` | app-scoped bytes/version/update time | No | No | No |
| TUI shell | `ShellStore`, `PaneState`, `FloatingPaneState` | active pane/floating, layout, overlay, CTA, interaction mode | No | Only selects active panel before `TerminalViewStore` lookup | No |
| TUI terminal views | `TerminalViewStore`, `TerminalViewBinding` | panel -> view -> terminal/channel binding | No | Yes, this is the only TUI-side input target source | No |
| TUI live/session cache | `TerminalSurfaceStore`, `TerminalSessionStore` | live projection, lifecycle projection, session cache | No, cache only | No global fallback allowed | No |
| TUI history/copy | `HistoryStore`, `CopyModeStore` | accepted history windows, copy cursor/selection/search | No | Consumes copy-mode keys before terminal routing | No |
| TUI terminal pool | `TerminalPoolStore` | cached terminal list/action results | Cache only | No | No |
| TUI workbench storage projection | `WorkbenchStorageSnapshot`, `WorkbenchSyncStore` | persisted layout and connection intent | No | No | No |
| TUI runtime/host | `AppRuntime`, `Host`, `InputParser`, `FrameSink` | event queue, hit regions, mouse drag, raw mode, output diff cache | No | Generates input events and hit regions only | No |
| render output | `RenderVM`, `RenderResult`, `Frame` | derived view model/frame/hit regions | No | Hit regions help mouse focus/action only | No |

## TUI reducer-owned state

| Store | File | Important fields | Owner meaning | Boundary note |
| --- | --- | --- | --- | --- |
| `Root` | `state/root.go` | `Generation`, `History`, `CopyMode`, `Clipboard`, `Surface`, `Session`, `TerminalViews`, `TerminalPool`, `Viewport`, `Shell`, `HostTheme`, `WorkbenchSync` | One current TUI process reducer state root | No field here is core truth; some fields are projections of core truth. |
| `ShellStore` | `state/shell.go` | workspace, workspaces, floatings, active ids, `InteractionMode`, overlay, CTA, toasts | Workbench UI structure and current focus | `PaneState.Kind` is only `empty` or `terminal-live`; exited/copy-history are not current pane states. |
| `TerminalViewStore` | `state/terminal_view.go` | `Views`, `PaneViews`, `FloatingViews` | Current process view binding map | Ordinary terminal input must resolve through active pane/floating -> binding. |
| `TerminalViewBinding` | `state/terminal_view.go` | `ViewID`, `SurfaceID`, `TerminalID`, `Channel`, `ResizeRole`, desired size, pane/floating ids, `Attached`, resize projection | Current TUI's connection intent and latest known attachment projection | Channel may be stale; send failure must reattach this view only. |
| `TerminalSurfaceStore` | `state/live.go` | current terminal projection plus `Surfaces` map | Cached live surface/lifecycle projection | It can clear stale exited UI when core sends lifecycle-known running, but it does not own lifecycle truth. |
| `TerminalSessionStore` | `state/live.go` | `TerminalID`, `Channel`, `InputChannels`, attach status, desired resize, lifecycle cache | Legacy/session projection for attach/live path | Must not be used as global input fallback when a specific TerminalView binding exists. |
| `TerminalPoolStore` | `state/terminal_pool.go` | list status, items, request/applied seq, last action ids | Cached core terminal list/action response | Useful for picker and lifecycle projection; not input routing truth. |
| `HistoryStore` | `state/history.go` | accepted `SourceLines`, `Rows`, token/generation/cursor/boundary, pending/exhausted | Current authoritative history window cache | Payload comes from core; TUI only caches and locally reflows accepted windows. |
| `CopyModeStore` | `state/history.go` | active/entering, pane/view/terminal ids, cursor, mark, selection, query, matches, bound token/cols | Copy/history interaction state | Never a history source. It selects and searches over `HistoryStore`. |
| `ClipboardStore` | `state/clipboard.go` | entries, storage versions, conflict/dirty flags | Current TUI clipboard-history projection | System clipboard IO and core storage are separate. |
| `ViewportStore` | `state/viewport.go` | host cols/rows/valid | Host TTY drawable size | Not the PTY size of any terminal. |
| `HostThemeStore` | `state/host_theme.go` | default fg/bg, palette, probed | Host terminal theme probe | Does not rewrite terminal content colors. |
| `WorkbenchSyncStore` | `state/root.go` | storage ref, saved/applied/event/base/conflict versions | Sync bookkeeping for core opaque workbench storage | Version state only; not lifecycle or input truth. |

## Core state

| Area | File | Structs | Data held | Boundary note |
| --- | --- | --- | --- | --- |
| Server runtime | `server.go` | `Server`, `serverConfig` | config, registry, storage, workbench legacy store, terminal map, event broker, listeners/transports, lifecycle/closed flags | Owns daemon process runtime, not TUI focus. |
| Terminal registry | `registry.go`, `types.go` | `terminalRegistry`, `TerminalInfo` | terminal identity, metadata, size, state, create/exit metadata | This is the simple source for running/exited. |
| Terminal runtime | `terminal.go` | `Terminal` | `TerminalInfo`, process, live surface, history pipeline, ingest queues, event broker callback | Restart changes process and lifecycle but keeps terminal identity/history/live tail as designed. |
| Protocol attachments | `protocol_service.go` | `protocolSession`, `protocolAttachment` | channel registry, per-view surface/view id, resize owner, size lock, event subscriptions, history pins | Channel validity lives here; storage snapshots cannot validate current channel. |
| Process | `process.go` | `ProcessSpec`, `TerminalProcess`, `ProcessExit` | command, size, PTY/process handle, exit result | TUI only sees projected terminal info and events. |
| Live surface | `live/types.go` | `SurfaceTrack`, `SurfaceSnapshot` | VTerm screen, cursor, modes, pending escape, preserve-alt option | Live surface is not history truth. |
| History parser/pipeline | `terminal_history_pipeline.go`, `history_ingest.go` | `terminalHistoryPipeline`, `historyANSIParser` | parser state, screen size, alt capture, `HistoryTrack` | Converts PTY stream to history events; does not read TUI layout. |
| History truth | `history/*.go` | `HistoryTrack`, `LogicalLineStore`, `CommittedHistoryIndex`, `MutableFrontier` | logical line payloads, committed index, mutable frontier, generation, primary screen ownership | The only committed history truth. |
| History windows/snapshots | `history/window.go`, `history/snapshot.go` | `HistoryWindow`, `FrozenSnapshot` | requested projection and frozen read-only copy session pin | Output contracts derived from history truth. |
| Core opaque storage | `storage.go` | `storageStore`, `StorageEntry` | `app_id/scope/owner_id/key -> bytes/version/updated_at` | Daemon stores bytes and versions; it does not understand TUI pane lifecycle. |
| Events | `events.go` | `eventBroker`, `Event`, `EventFilter` | event subscriptions and filters | Notification boundary, not durable truth. |
| Legacy workbench API | `workbench.go` | `workbenchStore` | old protocol workbench snapshot | Migration debt; do not use as new TUI lifecycle/input source. |

## Workbench storage payload owned by TUI schema

This payload is stored in core opaque storage, but interpreted by `termx-tui-v3`.

| Field | Stored? | Meaning | Must not mean |
| --- | --- | --- | --- |
| `Workspace`, `Workspaces` | Yes | Shared TUI workspace/tab/pane tree | Core workbench domain truth |
| `Floatings`, `ActiveFloatingID` | Yes | Shared TUI floating layout/focus intent | Core attachment truth |
| `PanelPresentation`, `ZoomedPaneID`, header/footer flags | Yes | Presentation preference | Terminal lifecycle |
| `ActivePaneID` | Yes | Restore focus intent | Current process input route until runtime activates and binding exists |
| `TerminalViews` | Yes | Connection intent: view/pane/floating -> terminal plus last known view metadata | Current channel validity or lifecycle |
| legacy `"exited"` / `"copy-history"` pane kind | May exist in old snapshots | Restore compatibility input | Current pane state; must be scrubbed to `terminal-live` intent |

## Rules for using state

1. Terminal lifecycle is decided by core terminal info or lifecycle-known live projection. TUI storage, pane kind, copy mode, session cache, and render output cannot decide it.
2. Ordinary keyboard input target is resolved only from current active pane/floating plus `TerminalViewStore` binding. `TerminalSessionStore`, storage snapshot, terminal pool selection, sibling binding, and global fallback cannot select the target.
3. Channel validity is decided by core protocol attachment registry. TUI may cache a channel in `TerminalViewBinding`, but any stale-channel error must reattach the same view and replay only that input.
4. History truth is only core `HistoryTrack` logical lines. TUI `HistoryStore` and `CopyModeStore` cache authoritative windows and interaction state; they cannot synthesize committed history from live rows.
5. Workbench storage is layout and connection intent only. It can restore a panel connected to terminal X; it cannot say terminal X is exited/running.
6. Live surface is current screen/cursor/modes. It can include lifecycle projection for UI clearing, but it is not history truth and cannot be used to reconstruct committed history.
7. Render `Frame` and hit regions are derived output. Hit regions can activate focus or actions, but render output cannot become state owner for lifecycle, input route, or history.
8. Runtime/host caches are process-local performance and IO state. Queue coalescing, last frame, input parser pending bytes, and mouse drag state must not be persisted or treated as shared truth.

## Common bug classification

| Symptom | First owner to inspect | Do not explain it with |
| --- | --- | --- |
| Re-enter TUI still shows restart after successful restart | core terminal list state, lifecycle-known surface/event, `TerminalSurfaceStore` projection | workbench pane kind |
| Active panel border moves but keyboard goes nowhere | `ShellStore` active ids -> `TerminalViewStore` binding -> protocol attachment channel | `TerminalSessionStore.Channel` global fallback |
| Reattach current panel breaks sibling panel | `TerminalViewStore` update scope and protocol attachment detach/restart behavior | terminal pool selected item |
| Copy/history shows wrong content | core `HistoryTrack` window response and TUI `HistoryStore` stale guard | live surface scrollback |
| Cursor wrong after restart | core `SurfaceTrack` cursor in snapshot and TUI live cursor projection | text tail synthesis |
| Mouse works but keyboard fails | host input parser and runtime input routing split after `InputMsg` | storage snapshot |
