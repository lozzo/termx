# 快照（Snapshot）

快照用于客户端重连时恢复终端屏幕状态。当客户端断开又重新连接时，它需要知道终端当前显示的内容，这正是快照的作用。

## 快照内容

一个完整的快照包含恢复终端显示所需的所有信息：

```go
type Snapshot struct {
    TerminalID string        // 所属 Terminal
    Size       Size          // 终端尺寸（cols × rows）
    Screen     ScreenData    // 当前屏幕内容
    Scrollback [][]Cell      // 滚动回看缓冲区
    Cursor     CursorState   // 光标位置和状态
    Modes      TerminalModes // 终端模式
    Timestamp  time.Time     // 快照时间
}

type ScreenData struct {
    Cells [][]Cell // rows × cols 网格
    // 使用备用屏幕时，包含备用屏幕内容
    IsAlternateScreen bool
}
```

## 获取快照

```go
snap, err := srv.Snapshot(ctx, terminalID)
```

- 快照是某一时刻的**一致性快照**：获取时短暂持有 VTerm 读锁，确保屏幕 + 光标 + 模式的一致性
- 快照数据是深拷贝，获取后释放锁，后续序列化不阻塞 VTerm 写入
- 对已退出的 Terminal 也可以获取快照（最终屏幕状态）

## 序列化格式

初版使用 **JSON** 编码。理由：

- 快照传输频率低（仅重连和显式请求时），不是性能瓶颈
- 典型终端屏幕（80×24，半满）JSON 压缩后约 5-15 KB，完全可接受
- JSON 天然可调试、可读、跨语言解码零成本
- 避免自定义二进制格式带来的维护成本和 schema evolution 问题

### JSON 结构

```json
{
    "terminal_id": "a7k2m9x1",
    "size": {"cols": 80, "rows": 24},
    "screen": {
        "is_alternate": false,
        "rows": [
            {
                "cells": [
                    {"r": "h", "s": {"fg": "#ffffff"}},
                    {"r": "e", "s": {"fg": "#ffffff"}},
                    {"r": "l"},
                    {"r": "l"},
                    {"r": "o"}
                ]
            }
        ]
    },
    "scrollback_rows": 142,
    "scrollback": [
        {"cells": [...]},
    ],
    "cursor": {
        "row": 5,
        "col": 12,
        "visible": true,
        "shape": "block"
    },
    "modes": {
        "alternate_screen": false,
        "mouse_tracking": false,
        "bracketed_paste": true,
        "application_cursor": false,
        "auto_wrap": true
    },
    "timestamp": "2026-03-18T10:30:00Z"
}
```

### Cell 编码优化

为减小 JSON 体积，Cell 使用缩写字段名，并省略默认值：

```json
// 完整 Cell
{"r": "A", "w": 1, "s": {"fg": "#ff0000", "bg": "#000000", "b": true, "i": false}}

// 省略默认值后（宽度 1、无样式）
{"r": "A"}

// 宽字符（中文等）
{"r": "你", "w": 2}

// 空白 Cell 省略（行尾空白不编码，用 Cell 数量 < cols 隐式表达）
```

字段说明：
- `r` — rune（字符）
- `w` — width（字符宽度，默认 1，省略）
- `s` — style（样式，无样式时省略整个字段）
  - `fg` / `bg` — 前景/背景色（默认色省略）
  - `b` — bold
  - `i` — italic
  - `u` — underline
  - `k` — blink
  - `rv` — reverse
  - `st` — strikethrough

### 行尾空白省略

每行只编码到最后一个非空白 Cell。客户端解码时，不足 cols 的部分用默认空白 Cell 填充。典型终端屏幕的大部分行都是半满的，这个优化可以减少 50-70% 的数据量。

## Scrollback 传输策略

Scrollback 可能很大（10000 行），但客户端通常只需要最近的几百行。传输策略：

1. **快照响应分两部分**：Screen + Cursor + Modes 立即返回，Scrollback 可选请求
2. **Scrollback 分页**：客户端通过 `snapshot` 请求指定 `scrollback_limit`（默认 500 行）
3. **按需加载**：客户端需要更多 scrollback 时，发送额外请求

```json
// 请求快照（限制 scrollback 200 行）
{"id": 1, "method": "snapshot", "params": {"terminal_id": "a7k2m9x1", "scrollback_limit": 200}}

// 请求更多 scrollback（从第 200 行开始，再取 500 行）
{"id": 2, "method": "snapshot", "params": {"terminal_id": "a7k2m9x1", "scrollback_offset": 200, "scrollback_limit": 500}}
```

这确保了首屏恢复的速度（只传 Screen），同时支持按需加载完整 scrollback。

## 未来优化

以下优化在初版不实现：

- **二进制格式**：如果 JSON 被证明是瓶颈，可以切换到自定义二进制格式或 protobuf
- **增量快照**：客户端维护序列号，服务端只传变化的行
- **压缩**：对 JSON 做 gzip 压缩（在 transport 层透明处理）

## 相关文档

- [虚拟终端](spec-vterm.md) — 快照数据的来源
- [线协议](spec-protocol.md) — 快照在协议中的位置
- [传输层](spec-transport.md) — 快照的传输方式
