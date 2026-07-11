# TUI 与 Core 状态归属图

本文梳理当前 `tui` 与 `core` 持有的状态数据，以及这些状态能不能作为终端生命周期、输入路由、历史真相或共享协作真相使用。

本文不是协议结构，也不是持久化格式。它是排查边界问题时看的状态归属清单：代码里的 struct 名、字段名、文件路径保留原名，语义说明全部使用中文。

## 大 JSON 总图

```json
{
  "Core_v2持有状态": {
    "终端实体真相": {
      "归属方": "core",
      "主要文件": [
        "core/server.go",
        "core/registry.go",
        "core/terminal.go",
        "core/types.go"
      ],
      "状态分组": {
        "终端注册表": {
          "关键结构": "terminalRegistry",
          "关键字段": ["terminals: map[terminal_id]TerminalInfo"],
          "可作为真相": [
            "终端 id/name/command/tags",
            "终端 PTY 尺寸",
            "终端生命周期: created/running/exited/removed",
            "created_at/exit_code/exited_at"
          ],
          "不能作为真相": [
            "TUI pane/floating identity",
            "TUI 当前焦点",
            "TUI runtime channel"
          ]
        },
        "终端运行实体": {
          "关键结构": "Terminal",
          "关键字段": [
            "info",
            "process",
            "live: *live.SurfaceTrack",
            "history: *terminalHistoryPipeline",
            "historyQ",
            "events"
          ],
          "可作为真相": [
            "正在运行的进程句柄",
            "当前终端生命周期",
            "实时表面、光标和终端模式",
            "权威 logical-line history"
          ],
          "不能作为真相": [
            "哪个 pane 当前 active",
            "哪个 TUI 应该接收键盘焦点",
            "workbench 布局"
          ]
        },
        "进程状态": {
          "关键结构": ["ProcessSpec", "TerminalProcess", "ProcessExit"],
          "关键字段": ["terminal_id", "command", "size", "wait/exit"],
          "可作为真相": [
            "实际启动的 command",
            "core 可以观测到的进程退出码、退出时间和退出原因"
          ]
        }
      }
    },
    "协议连接与视图附件状态": {
      "归属方": "core 协议会话",
      "主要文件": ["core/protocol_service.go"],
      "状态分组": {
        "协议会话": {
          "关键结构": "protocolSession",
          "关键字段": [
            "attachments: map[channel]protocolAttachment",
            "resizeOwners: map[terminal_id]channel",
            "sizeLocks: map[terminal_id]bool",
            "ownerEpoch",
            "eventCancels",
            "historyPins",
            "historyLatest"
          ],
          "可作为真相": [
            "当前连接内 attachment channel 是否有效",
            "channel -> terminal/view/surface 绑定",
            "当前协议会话内 resize owner 和 size lock 投影",
            "copy/history 分页用的冻结历史快照 pin"
          ],
          "不能作为真相": [
            "TUI pane 布局",
            "TUI active pane",
            "终端生命周期本身"
          ]
        },
        "协议附件": {
          "关键结构": "protocolAttachment",
          "关键字段": [
            "terminal_id",
            "channel",
            "mode",
            "resize_policy",
            "surface_id",
            "view_id",
            "epoch"
          ],
          "可作为真相": [
            "某个 protocol input/resize request 是否允许走这个 channel",
            "某个 channel 当前指向哪个 terminal"
          ],
          "不能作为真相": [
            "pane/floating 是否存在",
            "终端是否 exited"
          ]
        }
      }
    },
    "历史真相": {
      "归属方": "core 历史轨道",
      "主要文件": [
        "core/history/track.go",
        "core/history/types.go",
        "core/history/store.go",
        "core/history/index.go",
        "core/history/frontier.go",
        "core/history/window.go",
        "core/history/snapshot.go"
      ],
      "状态分组": {
        "历史状态机": {
          "关键结构": "history.HistoryTrack",
          "关键字段": [
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
          "可作为真相": [
            "权威历史状态机",
            "当前 primary screen ownership",
            "当前 mutable frontier",
            "历史 generation stale guard"
          ]
        },
        "逻辑行存储": {
          "关键结构": "MemoryLogicalLineStore",
          "关键字段": ["backend", "nextID"],
          "可作为真相": [
            "logical line payload 分配",
            "line payload 替换和删除"
          ]
        },
        "payload 后端": {
          "关键结构": "MemoryStorageBackend",
          "关键字段": ["lines: map[LogicalLineID]LogicalLine"],
          "可作为真相": [
            "logical line payload bytes/cells"
          ],
          "不能作为真相": [
            "某条 line 是否 committed",
            "某条 line 是否仍然 mutable"
          ]
        },
        "已提交历史索引": {
          "关键结构": "CommittedHistoryIndex",
          "关键字段": ["ids", "present", "generation"],
          "可作为真相": [
            "哪些 logical lines 计入已提交历史",
            "已提交历史顺序",
            "older pagination boundary"
          ]
        },
        "可变前沿": {
          "关键结构": "MutableFrontier",
          "关键字段": ["ids", "present", "hidden", "generation"],
          "可作为真相": [
            "哪些 logical lines 仍可被终端语义修改",
            "resize 后隐藏的 frontier"
          ]
        },
        "冻结快照": {
          "关键结构": "history.FrozenSnapshot",
          "关键字段": ["token", "generation", "lines", "committedLines"],
          "可作为真相": [
            "某次 copy/history 会话 pin 住的只读 logical-line 序列"
          ],
          "不能作为真相": [
            "第二份历史模型",
            "TUI 选择状态"
          ]
        },
        "历史窗口": {
          "关键结构": "history.HistoryWindow",
          "关键字段": [
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
          "可作为真相": [
            "core 针对某次 cols/rows 请求返回的权威投影"
          ],
          "不能作为真相": [
            "存储中的历史 payload",
            "TUI pane 滚动位置"
          ]
        }
      }
    },
    "实时终端表面": {
      "归属方": "core 实时表面",
      "主要文件": [
        "core/live/types.go",
        "core/terminal.go",
        "core/terminal_live_queue.go"
      ],
      "状态分组": {
        "实时表面": {
          "关键结构": "live.SurfaceTrack",
          "关键字段": [
            "size",
            "vt",
            "pending",
            "preserveAltScreenFrameOnExit"
          ],
          "可作为真相": [
            "当前实时 screen cell matrix",
            "当前实时光标",
            "当前终端模式",
            "尚未完整解析的转义序列"
          ],
          "不能作为真相": [
            "已提交历史",
            "copy/history 冻结窗口"
          ]
        },
        "实时快照": {
          "关键结构": "live.SurfaceSnapshot",
          "关键字段": ["size", "screen", "cursor", "modes"],
          "可作为真相": [
            "返回给 client 的尺寸绑定实时投影"
          ],
          "不能作为真相": [
            "历史来源"
          ]
        },
        "实时写入队列": {
          "关键结构": "terminalLiveIngestQueue",
          "关键字段": ["pending", "closed", "done"],
          "可作为真相": [
            "等待更新当前 screen 的实时输出批次"
          ],
          "不能作为真相": [
            "终端生命周期",
            "历史完整性"
          ]
        }
      }
    },
    "daemon存储与事件": {
      "归属方": "core daemon",
      "主要文件": [
        "core/storage.go",
        "core/events.go",
        "core/workbench.go"
      ],
      "状态分组": {
        "透明存储": {
          "关键结构": "storageStore",
          "关键字段": ["entries: map[app_id, scope, owner_id, key]StorageEntry"],
          "可作为真相": [
            "app 作用域 opaque value bytes",
            "storage 版本",
            "updated_at",
            "CAS conflict boundary"
          ],
          "不能作为真相": [
            "终端生命周期",
            "input channel 有效性",
            "历史真相"
          ]
        },
        "事件分发": {
          "关键结构": "eventBroker",
          "关键字段": ["subscribers", "filters", "buffer", "nextID", "closed"],
          "可作为真相": [
            "哪些订阅者应该收到 core events"
          ],
          "不能作为真相": [
            "持久化业务状态"
          ]
        },
        "旧workbench存储": {
          "关键结构": "workbenchStore",
          "关键字段": ["snapshot", "nextID"],
          "状态": "legacy/migration debt",
          "可作为真相": [
            "旧 workbench API 被调用时的旧 snapshot 状态"
          ],
          "不能作为真相": [
            "新的 tui-v3 终端生命周期",
            "新的输入路由"
          ]
        }
      }
    }
  },
  "TUI_v3持有状态": {
    "reducer状态根": {
      "归属方": "当前 TUI 进程",
      "主要文件": ["tui/state/root.go"],
      "状态分组": {
        "状态根": {
          "关键结构": "state.Root",
          "关键字段": [
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
          "可作为真相": [
            "当前 TUI 进程的 UI 状态",
            "当前 reducer 持有的缓存和投影"
          ],
          "不能作为真相": [
            "core 终端生命周期",
            "core 历史真相",
            "daemon attachment registry"
          ]
        }
      }
    },
    "Shell与workbench界面状态": {
      "归属方": "当前 TUI 进程；部分可作为 opaque workbench storage 持久化",
      "主要文件": [
        "tui/state/shell.go",
        "tui/state/workbench_storage.go"
      ],
      "状态分组": {
        "Shell": {
          "关键结构": "ShellStore",
          "关键字段": [
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
          "可作为真相": [
            "当前 TUI 里哪个 pane/floating active",
            "workspace/tab/pane/floating 布局",
            "overlay 和 prompt 状态",
            "UI CTA selection",
            "当前 interaction mode"
          ],
          "不能作为真相": [
            "终端 running/exited",
            "input channel",
            "历史来源"
          ]
        },
        "普通面板": {
          "关键结构": "PaneState",
          "关键字段": ["id", "title", "kind", "terminalID", "active"],
          "可作为真相": [
            "panel slot identity",
            "展示层 terminal 连接意图"
          ],
          "不能作为真相": [
            "终端生命周期",
            "attachment channel"
          ],
          "允许的当前kind": ["empty", "terminal-live"]
        },
        "浮动面板": {
          "关键结构": "FloatingPaneState",
          "关键字段": ["id", "title", "pane", "rect", "z", "active", "collapsed", "fitMode", "autoFit"],
          "可作为真相": [
            "floating panel 本地布局和展示状态"
          ],
          "不能作为真相": [
            "终端生命周期"
          ]
        },
        "workbench存储快照": {
          "关键结构": "WorkbenchStorageSnapshot",
          "关键字段": [
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
          "可作为真相": [
            "持久化/共享的 TUI workbench 布局",
            "持久化/共享的连接意图"
          ],
          "不能作为真相": [
            "当前终端生命周期",
            "runtime channel 是否新鲜",
            "copy mode selection",
            "实时光标"
          ]
        },
        "workbench同步状态": {
          "关键结构": "WorkbenchSyncStore",
          "关键字段": [
            "ref",
            "lastSavedVersion",
            "lastAppliedVersion",
            "lastEventVersion",
            "baseVersion",
            "conflictVersion",
            "conflict"
          ],
          "可作为真相": [
            "当前 TUI 与 core opaque storage 的同步 bookkeeping"
          ],
          "不能作为真相": [
            "workbench schema 本身",
            "终端生命周期"
          ]
        }
      }
    },
    "TerminalView与输入绑定状态": {
      "归属方": "当前 TUI 进程",
      "主要文件": ["tui/state/terminal_view.go"],
      "状态分组": {
        "TerminalView索引": {
          "关键结构": "TerminalViewStore",
          "关键字段": [
            "views: map[view_id]TerminalViewBinding",
            "paneViews: map[pane_id]view_id",
            "floatingViews: map[floating_id]view_id"
          ],
          "可作为真相": [
            "哪个 TerminalView binding 属于哪个 panel",
            "active panel 输入目标查找依据"
          ],
          "不能作为真相": [
            "core 终端生命周期",
            "全局 fallback 输入目标"
          ]
        },
        "TerminalView绑定": {
          "关键结构": "TerminalViewBinding",
          "关键字段": [
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
          "可作为真相": [
            "当前 TUI 的 view-scoped 连接意图和最新已知 channel",
            "当前 TUI 对这个 view 的 resize projection",
            "active pane/floating 解析完成后的输入路由目标"
          ],
          "不能作为真相": [
            "终端进程是否 running",
            "stale channel 是否仍被 core 接受",
            "sibling panel channel"
          ]
        },
        "TerminalView本地布局": {
          "关键结构": "TerminalViewLayout",
          "关键字段": ["sizeLocked", "mode", "panX", "panY", "alignX", "alignY"],
          "可作为真相": [
            "view-local 展示和布局偏好"
          ],
          "不能作为真相": [
            "terminal PTY size lock 真相",
            "core resize owner 权威状态"
          ]
        }
      }
    },
    "实时投影与session缓存": {
      "归属方": "当前 TUI 进程",
      "主要文件": ["tui/state/live.go"],
      "状态分组": {
        "实时表面缓存": {
          "关键结构": "TerminalSurfaceStore",
          "关键字段": [
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
          "可作为真相": [
            "当前 TUI 缓存的实时表面投影",
            "当前 TUI 缓存的来自 core list/surface/event 的生命周期投影"
          ],
          "不能作为真相": [
            "权威历史",
            "主终端生命周期归属方",
            "active view binding 不一致时的输入目标"
          ]
        },
        "实时快照消息": {
          "关键结构": "LiveSurfaceSnapshot",
          "关键字段": [
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
          "可作为真相": [
            "service/event result 进入 reducer 前的实时投影载体"
          ],
          "不能作为真相": [
            "TUI storage 状态"
          ]
        },
        "session缓存": {
          "关键结构": "TerminalSessionStore",
          "关键字段": [
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
          "可作为真相": [
            "当前 TUI 的 attach/session 缓存",
            "active session 的兼容投影"
          ],
          "不能作为真相": [
            "multi-view 输入目标",
            "终端生命周期权威来源"
          ]
        },
        "终端模式投影": {
          "关键结构": "LiveTerminalModes",
          "关键字段": [
            "mouseTracking",
            "mouseX10",
            "mouseNormal",
            "mouseButton",
            "mouseAny",
            "mouseSGR",
            "bracketedPaste"
          ],
          "可作为真相": [
            "宿主鼠标/按键序列是否应该透传给 terminal"
          ],
          "不能作为真相": [
            "哪个 terminal 接收普通键盘输入"
          ]
        }
      }
    },
    "history与copy交互状态": {
      "归属方": "当前 TUI 进程",
      "主要文件": ["tui/state/history.go"],
      "状态分组": {
        "history缓存": {
          "关键结构": "HistoryStore",
          "关键字段": [
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
          "可作为真相": [
            "当前 TUI 已接纳的权威 history window/cache",
            "pending request guard"
          ],
          "不能作为真相": [
            "已提交历史存储",
            "实时表面真相"
          ]
        },
        "copy模式": {
          "关键结构": "CopyModeStore",
          "关键字段": [
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
          "可作为真相": [
            "copy/history 用户交互状态",
            "当前权威 window 上的 selection/cursor/search 状态"
          ],
          "不能作为真相": [
            "history payload 真相",
            "终端生命周期"
          ]
        },
        "history窗口DTO": {
          "关键结构": "state.HistoryWindow",
          "关键字段": [
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
          "可作为真相": [
            "从 core 权威 response 转换后的 TUI DTO"
          ],
          "不能作为真相": [
            "第二份历史模型"
          ]
        }
      }
    },
    "终端池投影状态": {
      "归属方": "当前 TUI 进程",
      "主要文件": ["tui/state/terminal_pool.go"],
      "状态分组": {
        "终端池缓存": {
          "关键结构": "TerminalPoolStore",
          "关键字段": [
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
          "可作为真相": [
            "当前 TUI 缓存的 core terminal list/action response",
            "terminal list 过期响应保护"
          ],
          "不能作为真相": [
            "core terminal registry",
            "输入路由目标"
          ]
        },
        "终端池条目": {
          "关键结构": "TerminalPoolItem",
          "关键字段": [
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
          "可作为真相": [
            "用于 picker/list/render 的 core terminal info 投影"
          ],
          "不能作为真相": [
            "TUI storage 真相",
            "channel 是否新鲜"
          ]
        }
      }
    },
    "剪贴板与宿主状态": {
      "归属方": "当前 TUI 进程；部分可通过 core opaque storage 同步",
      "主要文件": [
        "tui/state/clipboard.go",
        "tui/state/viewport.go",
        "tui/state/host_theme.go"
      ],
      "状态分组": {
        "剪贴板历史": {
          "关键结构": "ClipboardStore",
          "关键字段": [
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
          "可作为真相": [
            "当前 TUI 剪贴板历史投影",
            "storage 同步账本"
          ],
          "不能作为真相": [
            "系统剪贴板状态"
          ]
        },
        "宿主viewport": {
          "关键结构": "ViewportStore",
          "关键字段": ["cols", "rows", "valid"],
          "可作为真相": [
            "host TTY 可绘制画布尺寸"
          ],
          "不能作为真相": [
            "terminal PTY 尺寸",
            "未显式请求时的 history 投影列数"
          ]
        },
        "宿主主题": {
          "关键结构": "HostThemeStore",
          "关键字段": ["defaultFG", "defaultBG", "palette", "probed"],
          "可作为真相": [
            "host terminal theme/palette 探测结果"
          ],
          "不能作为真相": [
            "terminal 内容 SGR 颜色"
          ]
        }
      }
    },
    "runtime宿主与render临时状态": {
      "归属方": "当前 TUI 进程 runtime/host/render 输出边界",
      "主要文件": [
        "tui/app/runtime.go",
        "tui/terminalhost/host.go",
        "tui/terminalhost/input_parser.go",
        "tui/terminalhost/frame_sink.go",
        "tui/terminalhost/latest_frame_sink.go",
        "tui/render/types.go",
        "tui/render/vm.go"
      ],
      "状态分组": {
        "应用运行时": {
          "关键结构": "AppRuntime",
          "关键字段": [
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
          "可作为真相": [
            "事件循环调度与当前进程交互缓存",
            "鼠标路由使用的上一次渲染 hit regions"
          ],
          "不能作为真相": [
            "终端生命周期",
            "历史真相",
            "持久化 workbench 状态"
          ]
        },
        "宿主终端": {
          "关键结构": "terminalhost.Host",
          "关键字段": [
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
          "可作为真相": [
            "host TTY mode 与事件流状态"
          ],
          "不能作为真相": [
            "core 终端进程状态",
            "TUI 业务状态"
          ]
        },
        "宿主输入解析": {
          "关键结构": "InputParser",
          "关键字段": ["pending"],
          "可作为真相": [
            "尚未完整解析的宿主输入转义序列"
          ],
          "不能作为真相": [
            "terminal 输入路由"
          ]
        },
        "帧输出缓存": {
          "关键结构": "FrameSink",
          "关键字段": ["lastLines", "lastWidth", "lastHeight", "lastCursor", "hasLastFrame"],
          "可作为真相": [
            "宿主输出差异缓存"
          ],
          "不能作为真相": [
            "渲染视图模型",
            "terminal 内容"
          ]
        },
        "latest帧背压": {
          "关键结构": "LatestFrameSink",
          "关键字段": ["pending", "patches", "closed", "highWaterMark"],
          "可作为真相": [
            "输出背压队列"
          ],
          "不能作为真相": [
            "UI 状态",
            "terminal 状态"
          ]
        },
        "渲染帧": {
          "关键结构": "render.Frame",
          "关键字段": [
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
          "可作为真相": [
            "一次渲染轮次派生出的输出"
          ],
          "不能作为真相": [
            "持久化状态",
            "reducer 持有状态"
          ]
        }
      }
    }
  }
}
```

## 归属总表

| 归属方 | 主要结构 | 持有的数据 | 能否判断终端生命周期 | 能否路由普通键盘输入 | 能否决定历史真相 |
| --- | --- | --- | --- | --- | --- |
| core 终端注册表 | `terminalRegistry`, `TerminalInfo` | 终端 id/name/command/tags/size/state/exit metadata | 能 | 不能 | 不能 |
| core 终端运行时 | `Terminal` | process handle、live surface、history pipeline、queues | 能 | 只能在 protocol/session 校验 channel 后写入 | 通过 `HistoryTrack` 拥有 |
| core 协议会话 | `protocolSession`, `protocolAttachment` | 每个连接内的 channel attachment、resize owner、size lock、history pins | 不能，只读取 terminal info | 能，负责校验 channel/view/terminal | pin frozen snapshots，但不是基础真相 |
| core 历史 | `HistoryTrack`, `LogicalLineStore`, `CommittedHistoryIndex`, `MutableFrontier` | logical lines、committed order、mutable frontier、screen ownership | 不能 | 不能 | 能 |
| core 实时表面 | `live.SurfaceTrack` | current screen/cursor/modes/pending escape | 不能 | 只能提供 mouse/bracketed-paste mode 投影 | 不能 |
| core 透明存储 | `storageStore`, `StorageEntry` | app 作用域 bytes/version/update time | 不能 | 不能 | 不能 |
| TUI shell | `ShellStore`, `PaneState`, `FloatingPaneState` | active pane/floating、layout、overlay、CTA、interaction mode | 不能 | 只能先选出 active panel，再交给 `TerminalViewStore` | 不能 |
| TUI terminal views | `TerminalViewStore`, `TerminalViewBinding` | panel -> view -> terminal/channel binding | 不能 | 能，是 TUI 侧唯一输入目标来源 | 不能 |
| TUI live/session projection | `TerminalSurfaceStore`, `TerminalSessionStore` | live surface/event 投影、session channel 投影 | 不能，只能显示刚回投的 core lifecycle 消息 | 不能作为全局 fallback | 不能 |
| TUI history/copy | `HistoryStore`, `CopyModeStore` | accepted history windows、copy cursor/selection/search | 不能 | copy mode key 先消费，未消费才进入 terminal routing | 不能 |
| TUI terminal pool | `TerminalPoolStore` | 最近一次 list/action response | 不能，restart 等动作必须重新查询 core | 不能 | 不能 |
| TUI workbench storage projection | `WorkbenchStorageSnapshot`, `WorkbenchSyncStore` | persisted layout 和 connection intent | 不能 | 不能 | 不能 |
| TUI runtime/host | `AppRuntime`, `Host`, `InputParser`, `FrameSink` | event queue、hit regions、mouse drag、raw mode、output diff cache | 不能 | 只生成 input events 和 hit regions | 不能 |
| render output | `RenderVM`, `RenderResult`, `Frame` | derived view model/frame/hit regions | 不能 | hit regions 只能辅助 mouse focus/action | 不能 |

## TUI reducer 持有状态

| 状态 | 文件 | 关键字段 | 归属语义 | 边界说明 |
| --- | --- | --- | --- | --- |
| `Root` | `state/root.go` | `Generation`, `History`, `CopyMode`, `Clipboard`, `Surface`, `Session`, `TerminalViews`, `TerminalPool`, `Viewport`, `Shell`, `HostTheme`, `WorkbenchSync` | 当前 TUI 进程唯一 reducer 状态根 | 这里没有 core terminal lifecycle 真相；字段只能保存 UI 交互态、连接意图或刚收到的投影。 |
| `ShellStore` | `state/shell.go` | workspace、workspaces、floatings、active ids、`InteractionMode`、overlay、CTA、toasts | Workbench UI 结构和当前焦点 | `PaneState.Kind` 当前只能是 `empty` 或 `terminal-live`；exited/copy-history 不是当前 pane 状态。 |
| `TerminalViewStore` | `state/terminal_view.go` | `Views`, `PaneViews`, `FloatingViews` | 当前进程 view binding map | 普通 terminal input 必须通过 active pane/floating -> binding 解析。 |
| `TerminalViewBinding` | `state/terminal_view.go` | `ViewID`, `SurfaceID`, `TerminalID`, `Channel`, `ResizeRole`, desired size、pane/floating ids、`Attached`、resize projection | 当前 TUI 的连接意图和最新已知 attachment 投影 | Channel 可能 stale；send 失败时只能 reattach 当前 view。 |
| `TerminalSurfaceStore` | `state/live.go` | 当前 terminal projection 和 `Surfaces` map | live surface/event 展示投影 | 只显示 live surface/event 已回投的画面和退出提示；不能回答“现在是否应该 restart”，也不能从 terminal pool/list 推导 lifecycle。 |
| `TerminalSessionStore` | `state/live.go` | `TerminalID`, `Channel`, `InputChannels`, attach status、desired resize、last error | attach/live path 的 session projection | 多 view 输入不能用它当 global fallback；不持有 core terminal lifecycle truth，旧退出展示态只能被 core lifecycle 消息覆盖。 |
| `TerminalPoolStore` | `state/terminal_pool.go` | list status、items、request/applied seq、last action ids | 最近一次 core terminal list/action response | 用于 picker/pool 展示；restart、running/exited 判定必须重新查询 core，不得写回 pane live lifecycle，也不是 input routing truth。 |
| `HistoryStore` | `state/history.go` | accepted `SourceLines`, `Rows`, token/generation/cursor/boundary、pending/exhausted | 当前 authoritative history window cache | payload 来自 core；TUI 只缓存和本地 reflow 已接纳窗口。 |
| `CopyModeStore` | `state/history.go` | active/entering、pane/view/terminal ids、cursor、mark、selection、query、matches、bound token/cols | copy/history 交互状态 | 永远不是 history source，只在 `HistoryStore` 上选择和搜索。 |
| `ClipboardStore` | `state/clipboard.go` | entries、storage versions、conflict/dirty flags | 当前 TUI clipboard-history projection | system clipboard IO 和 core storage 是另外的边界。 |
| `ViewportStore` | `state/viewport.go` | host cols/rows/valid | host TTY drawable size | 不是任何 terminal 的 PTY size。 |
| `HostThemeStore` | `state/host_theme.go` | default fg/bg、palette、probed | host terminal theme probe | 不改写 terminal content colors。 |
| `WorkbenchSyncStore` | `state/root.go` | storage ref、saved/applied/event/base/conflict versions | core opaque workbench storage 的同步 bookkeeping | 只是版本状态，不是 lifecycle 或 input truth。 |

## Core 持有状态

| 区域 | 文件 | 主要结构 | 持有的数据 | 边界说明 |
| --- | --- | --- | --- | --- |
| Server 运行时 | `server.go` | `Server`, `serverConfig` | config、registry、storage、legacy workbench store、terminal map、event broker、listeners/transports、lifecycle/closed flags | 拥有 daemon process runtime，不拥有 TUI focus。 |
| 终端注册表 | `registry.go`, `types.go` | `terminalRegistry`, `TerminalInfo` | terminal identity、metadata、size、state、create/exit metadata | running/exited 的最简单来源。 |
| 终端运行时 | `terminal.go` | `Terminal` | `TerminalInfo`、process、live surface、history pipeline、ingest queues、event broker callback | restart 改 process/lifecycle，但按设计保留 terminal identity/history/live tail。 |
| 协议附件 | `protocol_service.go` | `protocolSession`, `protocolAttachment` | channel registry、per-view surface/view id、resize owner、size lock、event subscriptions、history pins | channel validity 在这里；storage snapshot 不能验证当前 channel。 |
| 进程 | `process.go` | `ProcessSpec`, `TerminalProcess`, `ProcessExit` | command、size、PTY/process handle、exit result | TUI 只能看到 terminal info 和 events 的投影。 |
| 实时表面 | `live/types.go` | `SurfaceTrack`, `SurfaceSnapshot` | VTerm screen、cursor、modes、pending escape、preserve-alt option | live surface 不是 history truth。 |
| 历史解析器/管线 | `terminal_history_pipeline.go`, `history_ingest.go` | `terminalHistoryPipeline`, `historyANSIParser` | parser state、screen size、alt capture、`HistoryTrack` | 把 PTY stream 转成 history events，不读取 TUI layout。 |
| 历史真相 | `history/*.go` | `HistoryTrack`, `LogicalLineStore`, `CommittedHistoryIndex`, `MutableFrontier` | logical line payloads、committed index、mutable frontier、generation、primary screen ownership | 唯一 committed history truth。 |
| 历史窗口/快照 | `history/window.go`, `history/snapshot.go` | `HistoryWindow`, `FrozenSnapshot` | requested projection 和 frozen copy session pin | 从 history truth 派生的输出 contract。 |
| Core 透明存储 | `storage.go` | `storageStore`, `StorageEntry` | `app_id/scope/owner_id/key -> bytes/version/updated_at` | daemon 只存 bytes 和 version，不理解 TUI pane lifecycle。 |
| 事件 | `events.go` | `eventBroker`, `Event`, `EventFilter` | event subscriptions and filters | 通知边界，不是 durable truth。 |
| 旧 workbench API | `workbench.go` | `workbenchStore` | old protocol workbench snapshot | 迁移债；不要作为新的 TUI lifecycle/input source。 |

## TUI schema 拥有的 workbench 存储内容

这份内容存在 core opaque storage 里，但 schema 和语义由 `tui` 解释。

| 字段 | 是否存储 | 含义 | 绝不能表示 |
| --- | --- | --- | --- |
| `Workspace`, `Workspaces` | 是 | 共享的 TUI workspace/tab/pane tree | core workbench 领域真相 |
| `Floatings`, `ActiveFloatingID` | 是 | 共享的 TUI floating layout/focus intent | core attachment 真相 |
| `PanelPresentation`, `ZoomedPaneID`, header/footer flags | 是 | 展示偏好 | 终端生命周期 |
| `ActivePaneID` | 是 | restore focus intent | runtime 激活且 binding 存在前的当前进程 input route |
| `TerminalViews` | 是 | connection intent：view/pane/floating -> terminal 以及 last known view metadata | 当前 channel 有效性或生命周期 |
| legacy `"exited"` / `"copy-history"` pane kind | 旧 snapshot 可能存在 | restore compatibility input | 当前 pane state；必须在 restore 边界 scrub 成 `terminal-live` intent |

## 状态使用规则

1. 终端生命周期只能由当次 core terminal info 查询或 core lifecycle live event/surface 消息决定。TUI storage、pane kind、copy mode、session/surface 投影、render output 都不能决定 lifecycle；需要判断时重新查询 core。
2. 普通键盘输入目标只能由当前 active pane/floating 加 `TerminalViewStore` binding 解析。`TerminalSessionStore`、storage snapshot、terminal pool selection、sibling binding、global fallback 都不能选择目标。
3. Channel validity 由 core protocol attachment registry 决定。TUI 可以在 `TerminalViewBinding` 缓存 channel，但 stale-channel error 只能 reattach 同一个 view，并且只 replay 这次 input。
4. 历史真相只在 core `HistoryTrack` logical lines。TUI `HistoryStore` 和 `CopyModeStore` 只缓存 authoritative windows 和交互状态，不能从 live rows 合成 committed history。
5. Workbench storage 只保存布局和连接意图。它可以恢复“某 panel 连接 terminal X”的意图，但不能表示 terminal X 是 exited/running。
6. Live surface 表达当前 screen/cursor/modes。core lifecycle live event/surface 消息可以用于更新当前 UI 展示，但不是可持久化 truth，也不能用于重建 committed history 或代替下一次 lifecycle 查询。
7. Render `Frame` 和 hit regions 都是派生输出。Hit regions 可以触发 focus 或 action，但 render output 不能成为 lifecycle、input route 或 history 的状态 owner。
8. Runtime/host caches 都是当前进程内的性能和 IO 状态。queue coalescing、last frame、input parser pending bytes、mouse drag state 都不能持久化，也不能当共享 truth。

## 常见问题归因表

| 现象 | 第一优先检查的归属方 | 不要用这些解释 |
| --- | --- | --- |
| restart 成功后重进 TUI 仍显示 restart | core terminal list state、lifecycle-known surface/event、`TerminalSurfaceStore` projection | workbench pane kind |
| active panel 边框移动了但键盘输入没进 terminal | `ShellStore` active ids -> `TerminalViewStore` binding -> protocol attachment channel | `TerminalSessionStore.Channel` global fallback |
| reattach 当前 panel 后 sibling panel 失效 | `TerminalViewStore` update scope 和 protocol attachment detach/restart 行为 | terminal pool selected item |
| copy/history 内容不对 | core `HistoryTrack` window response 和 TUI `HistoryStore` stale guard | live surface scrollback |
| restart 后 cursor 错位 | core `SurfaceTrack` snapshot cursor 和 TUI live cursor projection | text tail synthesis |
| 鼠标有效但键盘无效 | host input parser 和 runtime input routing 在 `InputMsg` 之后的分流 | storage snapshot |
