# tmux 黑盒 harness artifact 规范

tmux harness 只作为测试宿主存在，不进入 termx 产品运行时依赖。缺少 tmux 时，可选 smoke 目标必须 skip。

## 可选目标

- `make test-cli-v3-tmux-smoke`：验证 tmux 可启动、可发送键盘、可抓 ANSI/plain 输出。
- `make test-cli-v3-tmux-terminal-smoke`：验证真实 core-v2 daemon/protocol/PTY、tui-v3 attach 和键盘输入回环。
- `make test-cli-v3-tmux-resize-smoke`：验证 tmux 宿主 resize 进入 tui-v3 layout，并按 active content rect 触达 PTY。
- `make test-cli-v3-tmux-ansi-smoke`：验证 live surface ANSI 16 色、256 色、truecolor、CR 和 alt-screen 证据。
- `make test-cli-v3-tmux-stability-smoke`：短轮次串行执行 terminal、resize、ANSI 三类黑盒 smoke。

## artifact

每个子 smoke 都保留独立临时目录，至少包含：

- `capture.ansi`：`tmux capture-pane -e -p` 输出，用于检查真实 SGR。
- `capture.txt`：plain capture，用于检查 live marker、frame 文本和错位。
- `daemon.log`：core-v2 daemon 日志。
- `termx-core-v2.sock`：本轮隔离 socket path。
- `terminal.id`：本轮创建的 terminal id。
- `tmux-*.sh`：tmux pane 内执行的脚本。
- `timeline.txt`：daemon、create、attach、input、resize、capture 和 cleanup 时间线。

stability smoke 还会生成汇总目录：

- `timeline.txt`：每轮子 smoke 开始、成功和 artifact path。
- `artifacts.txt`：子 smoke artifact 目录列表。

## 定位顺序

1. `timeline.txt`：先确认失败发生在 daemon ready、terminal create、attach render、send-keys、resize、ANSI capture 还是 cleanup。
2. `capture.txt`：确认 live marker 是否出现，frame 是否错位，pane chrome 是否被 terminal 内容破坏。
3. `capture.ansi`：确认 ANSI SGR 是否保留，尤其是 live 内容 palette 是否仍为宿主 ANSI/256/truecolor。
4. `daemon.log`：确认 protocol create/input/resize/remove 是否到达 core-v2。
5. `tmux-*.sh`：确认本轮输入脚本和 PTY 探针逻辑。

测试路径中的 Go tests 会清理 artifact；Makefile 和 CLI smoke 默认保留 artifact 供人工复核。
