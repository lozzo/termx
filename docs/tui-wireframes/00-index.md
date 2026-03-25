# TUI 线框图索引

## 目标

给 termx TUI 第一阶段建立一套可持续扩展的线框图索引，明确：

- 哪些是主路径
- 哪些是状态/异常路径
- 哪些是共享 terminal、floating、overlay 等复杂场景
- 场景之间如何流转

## 恢复入口

如果会话上下文被压缩，先读：

- [README.md](/home/lozzow/workdir/termx/docs/tui-wireframes/README.md)
- [99-session-handoff.md](/home/lozzow/workdir/termx/docs/tui-wireframes/99-session-handoff.md)

## 阅读顺序

建议先读：

1. `01-workbench-default.md`
2. `17-workbench-multipane.md`
3. `18-workbench-tab-switch.md`
4. `04-connect-dialog.md`
5. `19-connect-dialog-entry-variants.md`
6. `05-terminal-pool-overview.md`
7. `20-terminal-pool-kill-result.md`
8. `21-terminal-pool-remove-result.md`
9. `07-shared-terminal-owner-follower.md`
10. `09-floating-single.md`

再读状态与异常：

1. `02-pane-unconnected.md`
2. `03-pane-exited.md`
3. `08-remove-terminal-shared.md`
4. `14-remote-remove-notice.md`
5. `15-tab-last-pane-closes.md`

最后读补充专题：

1. `11-help-overlay.md`
2. `12-viewport-crop.md`
3. `23-viewport-wide-char-edge.md`
4. `13-theme-sync.md`
5. `16-metadata-tags-edit.md`

## 场景清单

### 主路径

- [01-workbench-default.md](/home/lozzow/workdir/termx/docs/tui-wireframes/01-workbench-default.md)
- [17-workbench-multipane.md](/home/lozzow/workdir/termx/docs/tui-wireframes/17-workbench-multipane.md)
- [18-workbench-tab-switch.md](/home/lozzow/workdir/termx/docs/tui-wireframes/18-workbench-tab-switch.md)
- [04-connect-dialog.md](/home/lozzow/workdir/termx/docs/tui-wireframes/04-connect-dialog.md)
- [19-connect-dialog-entry-variants.md](/home/lozzow/workdir/termx/docs/tui-wireframes/19-connect-dialog-entry-variants.md)
- [05-terminal-pool-overview.md](/home/lozzow/workdir/termx/docs/tui-wireframes/05-terminal-pool-overview.md)
- [06-terminal-pool-actions.md](/home/lozzow/workdir/termx/docs/tui-wireframes/06-terminal-pool-actions.md)
- [20-terminal-pool-kill-result.md](/home/lozzow/workdir/termx/docs/tui-wireframes/20-terminal-pool-kill-result.md)
- [21-terminal-pool-remove-result.md](/home/lozzow/workdir/termx/docs/tui-wireframes/21-terminal-pool-remove-result.md)

### pane 状态

- [02-pane-unconnected.md](/home/lozzow/workdir/termx/docs/tui-wireframes/02-pane-unconnected.md)
- [03-pane-exited.md](/home/lozzow/workdir/termx/docs/tui-wireframes/03-pane-exited.md)
- [15-tab-last-pane-closes.md](/home/lozzow/workdir/termx/docs/tui-wireframes/15-tab-last-pane-closes.md)

### shared terminal

- [07-shared-terminal-owner-follower.md](/home/lozzow/workdir/termx/docs/tui-wireframes/07-shared-terminal-owner-follower.md)
- [08-remove-terminal-shared.md](/home/lozzow/workdir/termx/docs/tui-wireframes/08-remove-terminal-shared.md)
- [14-remote-remove-notice.md](/home/lozzow/workdir/termx/docs/tui-wireframes/14-remote-remove-notice.md)

### floating

- [09-floating-single.md](/home/lozzow/workdir/termx/docs/tui-wireframes/09-floating-single.md)
- [10-floating-overlap.md](/home/lozzow/workdir/termx/docs/tui-wireframes/10-floating-overlap.md)
- [22-floating-anchor-limit.md](/home/lozzow/workdir/termx/docs/tui-wireframes/22-floating-anchor-limit.md)

### 补充专题

- [11-help-overlay.md](/home/lozzow/workdir/termx/docs/tui-wireframes/11-help-overlay.md)
- [12-viewport-crop.md](/home/lozzow/workdir/termx/docs/tui-wireframes/12-viewport-crop.md)
- [23-viewport-wide-char-edge.md](/home/lozzow/workdir/termx/docs/tui-wireframes/23-viewport-wide-char-edge.md)
- [13-theme-sync.md](/home/lozzow/workdir/termx/docs/tui-wireframes/13-theme-sync.md)
- [16-metadata-tags-edit.md](/home/lozzow/workdir/termx/docs/tui-wireframes/16-metadata-tags-edit.md)

## 流转文件

- [flows/01-workbench-primary-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/01-workbench-primary-flow.md)
- [flows/02-terminal-lifecycle-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/02-terminal-lifecycle-flow.md)
- [flows/03-shared-terminal-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/03-shared-terminal-flow.md)
- [flows/04-floating-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/04-floating-flow.md)
- [flows/05-overlay-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/05-overlay-flow.md)
- [flows/06-terminal-pool-action-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/06-terminal-pool-action-flow.md)

## 下一轮细化重点

- Workbench 下 workspace 切换与恢复场景
- Terminal Pool 三栏页在数据更密时的排版版本
- shared terminal 在 `no owner` 空窗期的显式表达
- 更多 overlay 叠层场景，例如 prompt 压在 help 或 connect 上层
- alt-screen 程序在 Workbench 和 Terminal Pool 中的对照图
