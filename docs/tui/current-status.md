# termx TUI 当前状态

状态：Direction Reset
日期：2026-03-25

---

## 1. 当前结论

termx TUI 当前已经确认不继续沿“过渡 ASCII renderer”主线推进。

新的统一口径是：

1. 保留现有数据驱动层
2. 保留 runtime 接线层
3. 删除/替换当前过渡 renderer
4. 参考 `deprecated/tui-legacy/` 重建真正的 tiled / floating / overlay renderer

当前终端里能看到的纯文本工作台，只是临时过渡壳，不是最终产品 UI。

---

## 2. 已确认保留的部分

下面这些是新主线地基：

- `workspace / tab / pane / terminal / connection / overlay` 主概念
- `owner / follower`
- `connect` 连接语义
- `intent -> reducer -> effect -> runtime` 这条数据流
- startup / restore / session bootstrap / runtime update 主线
- 现有 Bubble Tea 壳

---

## 3. 已确认淘汰的部分

下面这些不再作为主线资产继续扩展：

- 当前过渡 renderer 的 `screen_shell / section_* / chrome_*` 文本结构
- 以旧 renderer 文本快照为核心的测试基线
- 把 summary/card/rail 当成主工作台的呈现方式

---

## 4. 当前阶段定义

当前阶段不是：

- modern shell 收口期

当前阶段是：

- renderer reset
- workbench rebuild

目标是把工作台重新做成：

- terminal-first
- pane-first
- tiled/floating 可工作
- overlay 只是辅助层

---

## 5. 这轮之前已经完成的重置动作

### 第 219 轮

已经完成：

1. 删除 `tui/runtime_renderer_test.go`
2. 把 `tui/runtime_test.go` 缩成运行时 smoke
3. 提取最小测试 helper
4. 清空旧 renderer 文本快照基线

结果：

- 旧 renderer 已不再是产品验收基线
- 数据层和 runtime smoke 还在

---

## 6. 第 220 轮

这一轮完成的是“架构口径收口”，不是继续给旧 renderer 打补丁。

已完成：

1. 重写 `docs/tui/architecture.md`
2. 明确把当前仓库已成型的数据驱动层写进架构图
3. 明确把当前 renderer 标为待替换层
4. 把 legacy renderer 的可复用骨架写清楚：
   - `renderTabComposite`
   - `paneRenderEntries`
   - `composedCanvas`
   - `damage redraw`
   - `dirty rows / dirty cols`
   - `fixed viewport direct render`

结果：

- 后续实现不需要再争论“哪些层保留、哪些层重写”
- 新 renderer 可以直接按 `projection -> surface -> canvas -> compositor -> HUD` 展开

---

## 7. 下一阶段工作块

下一阶段应直接进入 renderer 重建，不再围绕旧过渡 renderer 做小修小补。

顺序固定为：

1. 删除/替换当前过渡 renderer 实现
2. 建立 tiled workbench projection
3. 建立 pane surface renderer
4. 建立 floating compositor
5. 建立 overlay compositor
6. 回补新的 E2E

---

## 8. 当前测试口径

现在的测试口径是：

- 保留非渲染数据层和 runtime smoke
- 暂时不维护旧 renderer 的文本断言
- 等新 renderer 稳定后，再按新工作台体验重建 E2E

---

## 9. 后续禁止事项

后续实现明确禁止：

- 继续把当前 ASCII renderer 当产品主线
- 再走 summary/card/rail-first 路线
- 为了守住旧测试继续维护错误的 renderer 结构
- 把旧版大一统 `Model` 整体搬回新仓库

---

## 10. 当前一句话状态

termx TUI 当前已经完成“保数据层、换 renderer”的方向收口，下一步应该直接进入真实工作台渲染层重建。
