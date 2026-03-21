# Obsidian Clarity 落地指引

本文档不是新的产品总设计，而是基于外部提案 `Obsidian Clarity` 的吸收版落地说明：

- 保留它正确的视觉方向
- 修正它与 termx 当前模型冲突的地方
- 给后续代码实现提供明确线稿和约束

适用目标：

- 后续继续优化 `tui/render.go`
- 重构 picker / modal / status bar
- 改善 floating 的层次和焦点表达

## 设计定位

termx 不是“更好看的 tmux”，而是：

- `terminal` 是后台实体
- `pane` 是 terminal 的显示入口
- 同一个 terminal 可以被多个 pane 同时 attach
- `workspace / tab / floating / view` 都是客户端视图层

因此 UI 必须始终服务这个模型，而不是把 `pane` 假装成 terminal 本体。

## 这份方案真正采用的原则

### 1. 色彩断层

- 激活的 tiled pane：主高亮色
- 激活的 floating pane：浮窗强调色
- 非焦点 pane / 底层内容：降权但仍可辨认

目标不是“隐藏非焦点”，而是“让用户一眼知道自己当前操作哪一层”。

### 2. 不透明弹层

所有 modal / picker / help / command line panel 都必须：

- 先清空目标矩形
- 用实色背景填满
- 再绘制边框和文字

不能让底层字符透出来。

### 3. 零随机 ID 暴露

UI 默认不展示随机 terminal ID / pane ID。

优先级应为：

1. terminal name
2. friendly command label
3. 稳定短名（如 `shell-2`、`htop-1`）

只有 debug / log / command 输出里才允许出现底层 ID。

### 4. 左提示、右状态

底栏分成两侧：

- 左侧：当前 mode 的短按键提示
- 右侧：当前上下文状态

禁止把所有信息堆成一长串。

## 明确修正：不采用外部提案的地方

### 1. 不把 metadata 编辑放进 View mode

不采用 `Ctrl-v -> e`。

原因：

- `View mode` 只处理 pane 的显示行为
- terminal `name/tags` 属于 terminal 级别动作

后续统一使用：

- `Ctrl-f` picker 中按 `Ctrl-e` 编辑选中 terminal
- `Ctrl-g :` 输入 `edit-terminal` 编辑当前 terminal

### 2. 不使用“红色编辑态”

红色保留给：

- error
- kill / destructive
- invalid input

普通 metadata 编辑使用中性高亮，不制造“危险操作”错觉。

### 3. emoji 不作为主布局元素

主布局只使用：

- 单宽字符
- 边框字符
- ANSI 颜色
- 简单 badge 文本

这样可以避免终端宽度错位。

### 4. 不做全屏统一降权

不采用“进入某个 mode 后全屏一起灰掉”的方案。

后续只做局部降权：

- 非焦点 pane 边框变暗
- 非焦点 floating 标题栏变暗
- 背景内容轻微降权

避免闪烁和上下文丢失。

## 颜色与层次约定

建议继续沿用当前 termx 的冷色主基调，但收紧语义：

- Primary：激活 tiled pane、激活 tab 指示
- Accent：激活 floating pane、floating title emphasis
- Muted：非焦点 pane / 非焦点浮窗 / 背景信息
- Inverted：picker 选中行、mode badge
- Danger：kill / error / invalid input
- Success：短暂成功通知

注意：

- 颜色永远只是增强，不是唯一语义
- 同时必须有标题、badge、边框或文本说明

## 线稿

以下线稿是后续实现时优先对齐的目标态。

### 1. 启动空状态：terminal pool 为空

说明：

- 没有任何 terminal 时，不展示大而空的帮助页
- 给一个极简居中启动卡片
- 默认动作仍然是“直接开始”

```text
  [main]  < 1:default >


                ┌ termx workspace ─────────────────────────────┐
                │                                              │
                │   Enter      start new terminal              │
                │   Ctrl-f     attach existing terminal        │
                │   Ctrl-w     workspace picker                │
                │   Ctrl-g ?   help                            │
                │                                              │
                └────────────────────────────────────── v1 ────┘


  [NORMAL] pane  tab  workspace  float  picker                ws:main
```

### 2. 启动空状态：terminal pool 非空

说明：

- 如果已有 terminal，直接进入居中 picker
- 用户第一眼就看到“复用还是新建”

```text
  [main]  < 1:default >

  ┌ shell-1 (muted) ─────────────────────────────────────────────────────────┐
  │                                                                          │
  │                    ┌ Choose Terminal ────────────────────────────────┐    │
  │                    │ search: _                                      │    │
  │                    │                                                │    │
  │                    │   + new terminal                               │    │
  │                    │ > api-shell            running   tags:role=api │    │
  │                    │   worker-shell         running   tags:role=job │    │
  │                    │   logs-tail            exited    tags:type=log │    │
  │                    │                                                │    │
  │                    │ Enter attach  Tab split  Ctrl-e edit  Esc close│    │
  │                    └────────────────────────────────────────────────┘    │
  │                                                                          │
  └──────────────────────────────────────────────────────────────────────────┘

  [PICKER] filter / attach / edit                                     ws:main
```

### 3. 正常平铺界面

说明：

- 标题栏只显示“名字 + 状态 + 少量 tag”
- 不展示随机 ID
- 激活 pane 边框最亮，其他 pane 可见但降权

```text
  [project-x]  1:dev  [2:logs]  3:build

  ┌ api-shell  running  role=api ─────────────────┬ build-watch  running ─────┐
  │ $ npm run dev                                 │ $ go test ./...           │
  │ server listening on :3000                     │ ok   ./protocol           │
  │                                               │                           │
  ├ redis-cli  running  role=data ────────────────┴───────────────────────────┤
  │ 127.0.0.1:6379>                                                        │
  │ OK                                                                     │
  └──────────────────────────────────────────────────────────────────────────┘

  [NORMAL] Ctrl-p pane  Ctrl-t tab  Ctrl-w ws  Ctrl-o float     pane:api-shell
```

### 4. 平铺 + 浮窗混合界面

说明：

- tiled pane 进入背景层后只降权，不消失
- 当前激活 floating 用 Accent
- 非焦点 floating 仍显示标题栏，便于发现层级

```text
  [project-x]  [2:dev]

  ┌ api-shell (muted) ────────────────────────────┬ build-watch (muted) ──────┐
  │                                               │                           │
  │        ┌ float:htop  z:1 ───────────────────┐ │                           │
  │        │ cpu  mem  load                      │ │                           │
  │   ┌────┴ float:quick-shell  z:2  ACTIVE ────┴───────────────┐            │
  │   │ $ git status                                             │            │
  │   │                                                          │            │
  │   └──────────────────────────────────────────────────────────┘            │
  └───────────────────────────────────────────────────────────────────────────┘

  [FLOAT] n new  Tab focus  [] z-order  hjkl move  HJKL size      focus:float
```

### 5. Terminal Picker

说明：

- 居中
- 选中行必须整行反色
- 列表只展示真正有决策价值的字段

```text
  ┌ Choose Terminal ──────────────────────────────────────────────────────────┐
  │ search: api_                                                             │
  │                                                                          │
  │   + new terminal                                                         │
  │ > api-shell            running        tags:role=api team=infra           │
  │   api-log              exited         tags:type=log service=api          │
  │   worker-shell         running        tags:role=worker                   │
  │                                                                          │
  │ Enter attach   Tab split   Ctrl-e edit   Ctrl-k kill   Esc close         │
  └──────────────────────────────────────────────────────────────────────────┘
```

### 6. Terminal Metadata 编辑

说明：

- 必须同时编辑 `name` 和 `tags`
- 明确告诉用户“会影响所有 attach 到这个 terminal 的 pane 标题与 tags 展示”

```text
  ┌ Edit Terminal ────────────────────────────────────────────────────────────┐
  │ name:  [ api-shell_______________________________________________ ]      │
  │ tags:  [ role=api team=infra____________________________________ ]      │
  │                                                                          │
  │ updates all panes attached to this terminal                              │
  │ Enter save   Esc cancel                                                  │
  └──────────────────────────────────────────────────────────────────────────┘
```

### 7. Help 面板

说明：

- 先讲概念，再讲快捷键
- 不要一上来就是一大堆键位表

```text
  ┌ Help / Shortcut Map ──────────────────────────────────────────────────────┐
  │ workspace = a whole working set                                          │
  │ tab       = one page inside a workspace                                  │
  │ pane      = one visible viewport bound to a terminal                     │
  │ view      = fit/fixed/readonly/pin display state                         │
  │ floating  = a pane shown above the tiled layout                          │
  │                                                                          │
  │ modes: Ctrl-p pane  Ctrl-r resize  Ctrl-t tab  Ctrl-w workspace          │
  │        Ctrl-o float Ctrl-v view   Ctrl-f picker Ctrl-g global            │
  │                                                                          │
  │ Esc always leaves the current mode                                       │
  └──────────────────────────────────────────────────────────────────────────┘
```

## 关键交互约束

### 1. metadata 变更语义

这是后续代码必须严格遵守的规则：

- terminal `name/tags` 是可变元数据
- 已绑定 pane 继续按 `terminal_id` 稳定附着
- 修改后：
  - 所有 attach 到该 terminal 的 pane 立即刷新标题和 tags
  - 当前 workspace 不自动重排
  - 其他 workspace 里已存在的 pane 也同步刷新 metadata
- 新 tags 只影响未来：
  - picker
  - search
  - layout resolve
  - load layout

### 2. floating 焦点流转

后续实现必须保证：

- `Esc`：floating -> tiled
- `Tab`：在浮窗间循环焦点
- `Ctrl-o`：进入 floating mode，但不强制抢焦点到浮窗
- 当存在 active floating 时，底栏右侧明确显示 `focus:float`

### 3. picker 行信息上限

一行最多展示：

- terminal label
- running/exited
- 简短 tags

不要再把：

- 原始 ID
- 冗长 command
- 无意义附加字段

都塞进一行。

### 4. 底栏裁剪规则

窄屏时遵守以下优先级：

1. mode badge
2. 当前 mode 的 3~5 个核心按键
3. 当前 pane / focus 状态
4. workspace / tab 状态
5. 其他次级信息

不要为了“信息全”牺牲可读性。

## 后续代码实现指导

### 第一批建议落点

- `tui/render.go`
  - 统一 pane / floating / modal 的层次绘制
- `tui/picker.go`
  - picker 居中、整行反色、字段瘦身
- `tui/model.go`
  - status bar 左右分区
  - metadata 编辑弹层语义
  - focus / mode 状态汇总

### 实施顺序建议

1. 先统一 modal / picker 的不透明绘制和居中布局
2. 再做底栏左右分区与信息裁剪
3. 再做 pane / floating 标题栏和边框层次
4. 最后细修空状态和 help 面板

### 回归检查点

每次改 UI 都至少验证：

- 没 terminal 时启动是否合理
- 有 terminal 时 picker 是否居中且不透底
- split / new-tab / floating 创建流是否还能正常完成
- metadata 修改后，跨 workspace attach pane 是否都刷新
- 浮窗多层叠加时，焦点和 z-order 是否一眼可辨

## 最终落地结论

后续 termx UI 优化应以这份文档为准，而不是直接照搬外部提案原文。

可继承的是：

- 视觉层次思路
- picker / modal 的呈现方式
- 零 ID 展示方向

必须坚持 termx 自己的则是：

- terminal 与 pane 解耦
- metadata 的稳定语义
- v3 direct mode 结构
- 可复用 terminal 的核心产品模型
