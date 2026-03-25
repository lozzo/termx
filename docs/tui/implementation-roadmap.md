# termx TUI 编码与交付计划

状态：Draft v2
日期：2026-03-25

---

## 1. 总体策略

当前编码计划的核心不是“把 UI 壳继续做漂亮”，而是把 TUI 做回真正可用的工作台。

推进原则固定为：

1. 先工作台主体，后辅助层
2. 先 pane surface，后 chrome polish
3. 先 E2E 主路径，后字段级补洞
4. 先复用旧版已验证的工作台思路，后做新视觉语言
5. 先大块收口，后细节优化

当前明确不做：

- 继续扩展 `card / deck / rail` 主工作台
- 继续把 modal 当成主界面重点
- 在 pane surface 未完成前，长时间围绕 chrome 文案和 renderer 细节打转

---

## 2. 里程碑

### M0 文档主线纠偏

目标：

- 把文档主线统一到 `terminal-first workbench`
- 把偏线描述清理掉

交付：

- `docs/tui/current-status.md`
- `docs/tui/README.md`
- `docs/tui/wireframes.md`
- `docs/tui/architecture.md`
- `docs/tui/testing-strategy.md`
- `docs/tui/implementation-roadmap.md`

验收：

- 后续编码不会再被文档带回 `modal / deck / rail` 主线

### M1 tiled workbench 主体

目标：

- 恢复真正可工作的单 pane / split pane 工作台

范围：

- 默认 workspace
- 默认 shell pane
- terminal surface 真实渲染
- split layout projection
- active/inactive pane frame
- 最小 header/footer

验收：

- 启动即进入可输入 pane
- split 后两个 pane 都是可工作的 terminal surface
- 主工作台不依赖说明面板才能理解当前状态

### M2 floating compositor

目标：

- 让 floating 成为真正叠放在工作台上的窗口层

范围：

- floating create
- move / resize
- z-order
- clipping / overlap
- center / recall
- tiled / floating focus 切换

验收：

- floating E2E 主路径通过
- overlapping / z-order / drag / resize 主路径通过

### M3 overlay 盖板层

目标：

- 让 overlay 回到辅助层，而不是替代主体

范围：

- help
- terminal picker
- terminal manager
- workspace picker
- prompt
- layout resolve
- mask / backdrop / shadow / focus return

验收：

- overlay 打开时底下 workbench 仍可辨认
- overlay 关闭后无残影
- picker / manager / prompt 都能回到真实工作台

### M4 terminal connect / manager / exited slot 主线

目标：

- 把 terminal 资源管理重新接回真实工作台

范围：

- connect existing terminal
- create terminal
- metadata edit
- terminal manager connect-here / new-tab / floating
- unconnected pane
- terminal program exited pane

验收：

- create / connect / restart / manager 主路径 E2E 通过
- 相关动作不再依赖摘要卡 UI

### M5 shared terminal / restore / layout

目标：

- 把复杂能力接到已可用的工作台上

范围：

- owner / follower
- acquire / auto-acquire
- workspace restore
- startup layout
- waiting slot / layout resolve

验收：

- shared terminal 回归池通过
- restore / layout 主路径通过

### M6 视觉与稳定性收尾

目标：

- 功能完整后，再处理颜色、性能和稳定性

范围：

- ANSI 颜色层次
- render cache
- dirty region
- backpressure
- 残影 / 串屏 / overlay 清理

验收：

- 常用场景 E2E 稳定
- benchmark 基线建立

---

## 3. 每阶段代码任务

### 3.1 M1 tiled workbench 主体

1. 建立 `layout projection -> pane rect` 主路径
2. 建立单 pane terminal surface renderer
3. 建立 split compositor
4. 建立 active/inactive pane frame
5. 建立最小 header/footer
6. 移除或绕开当前 summary-first 工作台表现

### 3.2 M2 floating compositor

1. 建立 floating rect 投影
2. 建立 z-order 排序
3. 建立 clipping / overlap 合成
4. 建立 floating move / resize / raise / lower / center
5. 建立 tiled/floating 焦点切换

### 3.3 M3 overlay 盖板层

1. 建立 overlay mask/backdrop
2. 建立 modal 布局层
3. 让 manager/picker/prompt/layout resolve 全部覆盖到 workbench 上
4. 收口 focus return 与关闭清理

### 3.4 M4-M5 业务回接

1. terminal picker
2. terminal manager
3. metadata prompt
4. unconnected / exited pane
5. shared terminal
6. restore / layout

### 3.5 M6 收尾

1. ANSI 颜色与主题
2. render cache / dirty region
3. benchmark
4. real-program E2E

---

## 4. 每阶段测试安排

### 4.1 M1

- 启动 E2E
- 单 pane 输入 E2E
- split E2E
- active pane 可见性 E2E

### 4.2 M2

- floating create E2E
- floating overlap / z-order E2E
- floating move / resize / center E2E

### 4.3 M3

- overlay open/close E2E
- overlay focus return E2E
- overlay 无残影 E2E

### 4.4 M4

- picker E2E
- manager E2E
- metadata E2E
- exited/unconnected pane E2E

### 4.5 M5

- shared terminal E2E
- restore/layout E2E

### 4.6 M6

- benchmark
- real-program E2E
- 渲染稳定性回归

---

## 5. 当前工作节奏要求

后续每轮默认按一个完整工作块推进：

- 功能代码
- 对应 E2E
- 文档状态更新
- 提交

不再接受下面这种推进方式：

- 连续多轮只补 renderer 文案断言
- 连续多轮只补 modal/chrome 微调
- 一个很小的 UI 点拆成很多轮单独推进

每轮都应该让产品更接近“能直接在 TUI 里工作”。
