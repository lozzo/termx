# termx TUI 交互规格

状态：Draft v1
日期：2026-03-23

---

## 1. 布局模型

### 1.1 workspace

- 一个 workspace 包含多个 tab
- workspace 有活动 tab、名称、恢复元数据

### 1.2 tab

- 一个 tab 同时拥有：
  - tiled pane tree
  - floating pane list
- tiled pane 由布局树组织
- floating pane 由矩形和 z-order 组织

### 1.3 pane

- pane 是终端工作位，而不是终端本体
- pane 可以连接 terminal，也可以暂时处于空槽位或恢复中状态
- pane 默认展示 terminal 名称，而不是 pane 自定义名字

---

## 2. 焦点模型

任一时刻，焦点只能落在以下之一：

1. prompt
2. overlay
3. floating layer
4. tiled layer
5. terminal input

其中 overlay 包括：

- terminal picker
- terminal manager
- workspace picker
- help
- prompt

规则：

- 高层打开时，低层不接收键盘输入
- `Esc` 是所有临时层统一退出键
- 关闭高层后，焦点回到最近合法 pane
- floating 层激活时，底栏和标题状态必须清楚表明当前不在 tiled 层

---

## 3. 默认工作流

### 3.1 启动

- 直接创建或恢复 workspace
- 默认进入一个已连接 terminal 的 pane
- 当前 pane 可立即输入 shell

### 3.2 split

split 当前 pane 时，系统提供两个入口：

1. `+ new terminal`
2. `connect existing terminal`

split 完成后：

- 新 pane 获得焦点
- 如果是新 terminal，立即连接并可工作
- 如果是复用 terminal，遵循 owner/follower 默认规则

### 3.3 tab

- 新建 tab 时可直接创建 terminal，也可先落到等待态
- 切换 tab 时焦点回到该 tab 最近合法 pane
- tab 支持普通工作页，不应该成为“功能面板页”的泛滥容器

### 3.4 floating

- floating pane 在 tab 内与 tiled pane 共存
- active floating pane 总是最高层
- 支持移动、缩放、切换 z-order、隐藏/恢复、居中呼回

---

## 4. 槽位状态交互

说明：

- 这部分不是对外主概念
- 只是 pane 在没有正常连接 terminal 时的槽位表现
- 用户真正要理解的主体仍然是 terminal

### 4.1 空槽位

正文区显示稳定的下一步动作：

- start new terminal
- connect existing terminal
- open terminal manager
- close pane

说明：

- 这个状态本质上就是“未连接 terminal 的 pane”
- 不再使用 `saved pane` 作为主线称呼

### 4.2 程序已退出 pane

正文区显示：

- terminal 中程序已退出的信息
- restart 入口
- connect another terminal 入口
- close pane 入口

说明：

- 这个状态本质上就是“terminal 中程序已经退出的 pane”
- 不再使用 `exited pane` 作为主线称呼

### 4.3 waiting slot

- 用于 layout / restore 中的未决槽位
- 不应表现得像错误状态
- 应提示这是一个待解析的预留位置

---

## 5. picker / manager / prompt 职责

### 5.1 terminal picker

职责：

- 以最快路径 connect existing terminal
- 从当前工作流发起 create terminal
- 支持 name / tags / command / location 搜索

适合：

- 当前用户正在工作，需要短路径 connect

### 5.2 terminal manager

职责：

- 浏览 terminal pool
- 查看 terminal 的当前可见性和位置
- 直接将当前 pane connect 到选中的 terminal
- 执行 here / new tab / floating / edit / stop

适合：

- 用户明确在做资源管理，而不是只做一次 connect

### 5.3 workspace picker

职责：

- 创建 workspace
- 搜索和切换 workspace
- 以树形结构展示 `workspace -> tab -> pane`
- 支持直接跳到某一个 pane

### 5.4 metadata prompt

职责：

- 编辑 terminal 的 `name / tags`
- 明确提示当前操作对象是 terminal，不是 pane

---

## 6. 快捷键策略

### 6.1 原则

- 快捷键是工具，不是产品本体
- normal 状态下应足够工作
- mode 必须短驻留、可退出、可忽略非法键
- 用户可通过帮助界面快速恢复记忆

### 6.2 推荐主入口

- `Ctrl-p` pane
- `Ctrl-r` resize
- `Ctrl-t` tab
- `Ctrl-w` workspace
- `Ctrl-o` floating
- `Ctrl-v` viewport / display
- `Ctrl-f` picker
- `Ctrl-g` global

### 6.3 模式行为

- 非 sticky 模式执行一次动作后自动退出
- sticky 模式在有效连续动作后续期
- 非法输入直接忽略，不进入异常状态
- `Esc` 统一清空临时模式

### 6.4 鼠标策略

- 鼠标只服务直觉操作
  - 点击聚焦
  - 拖动 floating
  - resize floating
- 鼠标不是唯一完成路径
- 所有鼠标交互都应有键盘回退路径

---

## 7. 共享 terminal 行为规格

### 7.1 connect 规则

- connect 到已存在 terminal 时，新 pane 默认 follower
- 如果该 terminal 当前没有 owner，则系统按规则选出 owner

### 7.2 resize 规则

- pane 几何变化不自动改写 terminal size
- 只有 owner 的显式 resize 才会提交到 terminal
- follower 不能隐式改 terminal 控制参数

### 7.3 owner 迁移

- owner pane 被关闭、解绑、切走后，系统要重新选 owner
- 迁移过程不能造成 terminal 消失、串屏或 resize 混乱

### 7.4 owner 获取

- 任意一个 pane 或客户端都可以主动请求 owner
- owner 获取必须稳定、可见、可预测
- 获取 owner 后才能执行 terminal 控制面动作
  - resize
  - metadata 更新

---

## 8. 恢复和降级

### 8.1 restore 原则

- 恢复失败不应闪退
- 恢复失败不应破坏已有 terminal
- 允许降级成 waiting slot、空槽位或 picker

### 8.2 layout resolve

当 layout 无法直接匹配 terminal 时，允许：

- connect existing
- create new
- skip

### 8.3 远端事件

- remote terminal removed / exited / metadata updated 时，当前 UI 应同步反映
- close 本地 pane 或 detach 本地 TUI 不应误广播成 terminal removed

---

## 9. 帮助系统

help 必须回答下面 4 件事：

1. 我现在在哪个层级
2. 当前有哪些主入口
3. 共享 terminal 时 owner/follower 是什么
4. close pane、stop terminal、detach 的区别是什么

help 不应该只是线性快捷键列表。

---

## 10. 交互验收标准

交互层验收通过，至少满足：

1. 焦点始终明确
2. overlay 关闭后无残影
3. 非法输入不锁死模式
4. split / connect / floating / workspace switch 主流程不需要用户猜测下一步
5. 共享 terminal 的 ownership 规则在 UI 上可见、在行为上可预测
