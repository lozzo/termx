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
2. `04-connect-dialog.md`
3. `05-terminal-pool-overview.md`
4. `07-shared-terminal-owner-follower.md`
5. `09-floating-single.md`

再读状态与异常：

1. `02-pane-unconnected.md`
2. `03-pane-exited.md`
3. `08-remove-terminal-shared.md`
4. `14-remote-remove-notice.md`
5. `15-tab-last-pane-closes.md`

最后读补充专题：

1. `11-help-overlay.md`
2. `12-viewport-crop.md`
3. `13-theme-sync.md`
4. `16-metadata-tags-edit.md`

## 场景清单

### 主路径

- [01-workbench-default.md](/home/lozzow/workdir/termx/docs/tui-wireframes/01-workbench-default.md)
- [04-connect-dialog.md](/home/lozzow/workdir/termx/docs/tui-wireframes/04-connect-dialog.md)
- [05-terminal-pool-overview.md](/home/lozzow/workdir/termx/docs/tui-wireframes/05-terminal-pool-overview.md)
- [06-terminal-pool-actions.md](/home/lozzow/workdir/termx/docs/tui-wireframes/06-terminal-pool-actions.md)

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

### 补充专题

- [11-help-overlay.md](/home/lozzow/workdir/termx/docs/tui-wireframes/11-help-overlay.md)
- [12-viewport-crop.md](/home/lozzow/workdir/termx/docs/tui-wireframes/12-viewport-crop.md)
- [13-theme-sync.md](/home/lozzow/workdir/termx/docs/tui-wireframes/13-theme-sync.md)
- [16-metadata-tags-edit.md](/home/lozzow/workdir/termx/docs/tui-wireframes/16-metadata-tags-edit.md)

## 流转文件

- [flows/01-workbench-primary-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/01-workbench-primary-flow.md)
- [flows/02-terminal-lifecycle-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/02-terminal-lifecycle-flow.md)
- [flows/03-shared-terminal-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/03-shared-terminal-flow.md)
- [flows/04-floating-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/04-floating-flow.md)
- [flows/05-overlay-flow.md](/home/lozzow/workdir/termx/docs/tui-wireframes/flows/05-overlay-flow.md)

## 下一轮细化重点

- workbench 的多 pane 版本与 tab 切换版本
- connect dialog 的更多来源场景，例如 `new tab / new float`
- Terminal Pool 的更多动作前后对照图，例如 `kill` 后与 `remove` 后
- exited pane / unconnected pane 的远端事件版本
- floating pane 拖出主视口后的极限锚点场景
- viewport 裁切下宽字符、emoji、powerline 边界场景
