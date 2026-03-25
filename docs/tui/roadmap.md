# termx TUI 工作计划

状态：Draft v3
日期：2026-03-25

## 当前阶段顺序

### R1 renderer reset

- 删除/替换当前过渡 renderer
- 建立新的 render 子分层骨架

### R2 tiled workbench

- single pane
- split pane
- active/inactive pane frame
- terminal surface 贴合

### R3 floating compositor

- floating rect
- move / resize
- z-order
- clipping / overlap

### R4 overlay compositor

- backdrop
- centered modal
- focus return
- cleanup without residue

### R5 business flows reconnect

- terminal picker
- terminal manager
- metadata prompt
- exited/waiting/empty pane
- restore / layout resolve

### R6 polish

- ANSI colors
- damage redraw
- dirty rows / cols
- benchmark
- 残影 / 串屏 / backpressure

## 当前工作要求

- 每一轮尽量按一个完整工作块推进
- 不要再围绕旧 renderer 做碎片化小修小补
- 新 renderer 稳住后再补完整 E2E
