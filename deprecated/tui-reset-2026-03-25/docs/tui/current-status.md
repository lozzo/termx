# termx TUI 当前状态

状态：Direction Reset
日期：2026-03-25

## 当前结论

当前 TUI 主线已经确定：

1. 保留现有数据驱动层
2. 保留 runtime 接线层
3. 删除并替换当前过渡 renderer
4. 参考 `deprecated/tui-legacy/` 重建真实工作台渲染层

当前屏幕里的纯文本工作台只是过渡壳，不是最终产品 UI。

## 当前保留的部分

- `workspace / tab / pane / terminal / connection / overlay`
- `owner / follower`
- `connect` 语义
- `intent -> reducer -> effect -> runtime`
- startup / restore / session bootstrap / terminal store
- Bubble Tea 外壳

## 当前淘汰的部分

- 旧 `screen_shell / chrome_* / section_*` 文本结构
- 旧 renderer 文本快照测试基线
- summary/card/rail-first 的主工作台方向

## 当前阶段

当前阶段不是“UI polish”，而是：

- renderer reset
- workbench rebuild

目标是先把下面三件事做对：

1. tiled pane surface
2. floating compositor
3. overlay 盖板层

## 下一阶段工作

1. 删除/替换当前过渡 renderer 实现
2. 建立 `projection -> pane surface -> canvas compositor`
3. 恢复 floating 的 overlap / clipping / z-order
4. 恢复 overlay 的 backdrop / modal / close-cleanup
5. 按新 renderer 重建 E2E

## 当前禁止事项

- 继续扩展当前 ASCII renderer
- 继续围绕旧 renderer 文本断言补测试
- 再把 summary/card/rail 做成主工作台
- 把 legacy 的大一统 `Model` 整体搬回新仓库
