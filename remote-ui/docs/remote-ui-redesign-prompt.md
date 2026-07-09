# AI Studio Prompt: TermX Remote UI Redesign

请基于下面的产品功能和交互要求，重新设计 TermX Remote UI。目标不是给现有界面换皮，而是重新梳理信息架构、工作区布局、交互优先级和响应式方案，尤其要补齐桌面端、平板端、大屏端的体验。

## 产品背景

TermX Remote UI 是一个 Web / embedded Web 远程控制应用，用于连接和操作远程机器。用户可以通过 Web Control 登录账号同步机器，也可以扫描本地机器二维码进行本地配对。连接建立后，所有终端、文件、预览、上传下载、诊断等运行时数据都通过已建立的 WebRTC 通道传输。

核心用户是开发者、运维、需要在手机/平板/桌面浏览器里远程操作机器的人。应用应该像一个高效的远程开发控制台，而不是营销页面。

## 设计目标

1. 重新设计完整产品 UI，而不是只做移动端列表和弹窗。
2. 桌面端和大屏优先考虑多栏工作台，减少全屏跳转和层层弹窗。
3. 平板端要兼顾触控和信息密度，可以用双栏、可收起面板、底部/侧边工具栏。
4. 手机端保留高效单手操作，但要把关键流程变得清晰：机器 -> 终端/文件 -> 工具。
5. 终端是核心工作区，必须最大化可用空间，工具和状态不能遮挡终端内容。
6. 文件管理、传输中心、连接诊断、终端管理都应作为工作台的一等功能，而不是隐藏在混乱的 sheet 里。
7. 不要做 landing page。首屏就是可用的机器控制台。

## 当前功能总结

### 1. 机器入口 / 首页

首页展示可连接机器列表，机器来源包括：

- Cloud：登录 Web Control 后同步的云端机器。
- Local：本地扫码配对保存的机器。
- Manual：手动保存的本地记录。

每台机器需要展示：

- 机器名称。
- hostname 或 machine id。
- 在线状态：online / offline / stale / connecting。
- 来源：Cloud / Local / Manual。
- 是否已经被当前设备授权：Ready / Scan QR / Re-authorize。
- last seen。
- 可能的连接路径信息：local / public P2P / managed relay。
- 终端数量。

首页主要操作：

- 打开机器工作区。
- 机器未授权时进入配对/重新授权流程。
- 扫描二维码添加本地机器。
- 刷新机器列表。
- 打开全局传输中心。
- 打开设置。
- 登录 / 退出 Web Control。

空状态需要支持：

- 无机器时引导扫码添加。
- 未登录时提示登录可同步云端机器。
- 已登录但没有机器时提示添加/配对。

### 2. 登录与设置

设置页包含账号、连接、终端偏好和诊断。

账号功能：

- Web Control 登录：账号/邮箱 + 密码。
- 显示当前登录用户。
- 刷新云端机器。
- 退出登录。

连接信息：

- 显示 Web Control URL，可能来自环境变量或内置配置。

终端偏好：

- 字体大小。
- 字体选择，带字体预览。
- 主题选择，带深色/浅色主题预览。
- 渲染器：Auto / WebGL / Canvas / DOM。
- 移动键盘模式：Auto / Resize / Shift up。
- scrollback 行数。
- scrollback prefetch threshold。
- cursor blink 开关。

诊断：

- 导出 debug logs。

设计建议：

- 桌面端设置可以是独立页面或右侧 drawer。
- 移动端可以是全屏设置页。
- 不要让登录表单和终端偏好混在一个很长的无结构列表里，应有明确分组和导航。

### 3. 配对 / 授权机器

配对场景有两类：

- Add Local Device：添加本地机器。
- Re-authorize Device：云端机器已经存在，但当前手机/浏览器没有本地 session token，需要重新授权。

配对方式：

- 扫描 TermX QR。
- 手动粘贴 QR 内容，例如 `termx://pair?payload=...`。
- 对于机器工作区内的本地授权，也支持输入 Pair ID 和 Pair Secret。

配对状态：

- 扫描中。
- 配对中。
- 成功，保存 session token 后进入机器工作区。
- 错误：二维码属于其他机器、需要先登录、配对信息不匹配、网络/授权失败。

设计建议：

- 配对应是清晰的 wizard 或 modal，不要让用户分不清“添加本地机器”和“重新授权当前机器”。
- 手动输入应作为扫描失败/无摄像头时的 fallback。
- 对云端设备扫码时，如果未登录，需要明确引导先登录。

### 4. 机器工作区

进入某台机器后，目前有两个主页面状态：

- Terminal List：该机器的终端列表。
- Terminal Workspace：打开某个终端后的终端工作区。

机器工作区需要支持：

- 返回机器列表。
- 查看机器名称、machine id、在线状态。
- 查看连接状态：connecting / probing / connected / waiting network / reconnecting / failed。
- 连接失败、网络等待、重连时显示明确状态，但不要长期遮挡主要内容。
- 打开连接诊断。
- 打开文件管理器。
- 创建终端。
- 管理终端。
- 对未授权机器显示 Verify This Device gate。

### 5. 终端列表

终端列表展示某台机器上的多个 terminal session。

每个终端展示：

- title 或 command。
- cwd。
- environment。
- state：running / exited / unknown。
- terminal size：cols x rows。
- size lock 状态：resizable / warn / locked。

终端操作：

- 打开终端。
- 创建终端。
- 编辑终端：name、cwd、environment、size lock mode。
- 新建终端：name、command、cwd、environment、size lock mode。
- 删除终端。
- 长按或更多菜单进入管理。

设计建议：

- 桌面端：终端列表应常驻左侧栏或可收起侧栏。
- 平板端：可用左侧栏 + 主工作区，窄屏时转为 sheet。
- 手机端：终端列表和终端工作区可以是页面切换，但切换入口必须明显。

### 6. 终端工作区

终端是最重要的页面。它需要支持：

- 渲染远程终端。
- 连接中状态。
- 无 active terminal 状态。
- 终端内容选择、复制、粘贴。
- 大段粘贴确认。
- 字体大小快速调整。
- 渲染器快速切换。
- 快捷短语 / Fn 面板。
- 移动端特殊按键栏。
- 系统键盘显示/隐藏/锁定。
- Ctrl / Alt modifier：单次、双击锁定、长按锁定。
- Esc、Tab、方向键、Home、End、Page Up、Page Down、`/`、`|`、`-`、`\` 等常用键。
- 终端 split view：同时打开两个终端。
- split input sync：同步输入到两个终端。
- 关闭 split。
- 切换当前 active split slot。
- terminal resize ownership：Acquire / Release resize owner。
- size locked 时显示解锁 resize 操作。

移动端当前工具包括：

- 顶部终端标题区：机器名、终端名、当前 cwd。
- split 按钮。
- sync split input 按钮。
- close split 按钮。
- resize owner 状态按钮，例如 OW / FL / LK。
- terminal tools 按钮。
- connection info 按钮。
- files 按钮。
- 底部 keybar。
- Fn panel。
- selection toolbar。

设计建议：

- 桌面端不应复用手机顶部小按钮堆叠。桌面端应有专业工具栏或侧栏，常用操作可见，次级操作放进 command menu / toolbar menu。
- split view 在桌面端可以做横向或纵向分屏，可拖拽分隔线。
- 终端区域要尽量占满主工作区，状态提示使用轻量 toast/banner。
- 复制粘贴、选择模式、快捷键面板不能遮挡太多终端内容。

### 7. 文件管理器

文件管理器在当前实现里是覆盖在终端上的全屏 overlay。新设计应把文件作为与终端同级的重要工作区。

文件管理功能：

- 显示当前路径 breadcrumb。
- 进入目录。
- 返回上级目录。
- 刷新当前目录。
- 新建目录。
- 显示/隐藏隐藏文件。
- 排序。
- 选择模式。
- 全选 / 取消全选。
- 单文件/多文件复制。
- 剪切。
- 粘贴。
- 复制路径。
- 重命名。
- 删除。
- 上传文件。
- 下载文件。
- 打开传输中心。
- 文件预览。

文件条目展示：

- 文件/目录图标。
- 文件名。
- metadata：大小、修改时间、类型等。
- 更多操作。
- 目录有进入箭头。
- 选择模式有 checkbox。

排序选项：

- Name A to Z。
- Name Z to A。
- Newest first。
- Oldest first。
- Largest first。
- Smallest first。
- Type A to Z。
- Type Z to A。
- Folders first。

文件预览支持：

- 文本。
- Markdown。
- 图片。
- 视频。
- 3D model。
- unsupported / too large 提示。
- loading / error 状态。

设计建议：

- 桌面端：文件管理可以是右侧 panel、底部 panel、或与终端并排 tab/workspace，而不是只能全屏覆盖。
- 平板端：建议 terminal/files 双标签或左右分栏。
- 手机端：可全屏文件页，但底部工具栏需要清晰分组，避免按钮过多。

### 8. 传输中心

传输中心用于管理上传/下载任务，可从首页全局打开，也可从某台机器工作区打开。

传输信息：

- 文件名。
- 上传/下载方向。
- 所属机器。
- 源/目标路径。
- 状态：pending / transferring / paused / missing / failed / completed。
- 已传输大小 / 总大小。
- 速度。
- 进度条。
- saved path。
- 错误信息。

传输操作：

- 暂停单个任务。
- 恢复/重试单个任务。
- 取消进行中的任务。
- 清除已完成/失败任务。
- 批量选择。
- Pause selected。
- Start selected。
- Resume all。
- Clear completed。
- Clear failed。

设计建议：

- 传输中心应像下载管理器一样明确，不要只是角落小浮层。
- 桌面端可以做右侧抽屉或底部任务面板。
- 有 active transfer 时，主导航/工具栏要有非打扰式进度提示。

### 9. 连接诊断

连接诊断用于查看当前机器连接路径与 WebRTC 信息。

展示字段：

- Mode：P2P direct / Relay / Unknown。
- Path。
- Local address。
- Remote address。
- Candidate types：local / remote。
- RTT。
- Connection ID。

操作：

- Refresh stats。
- Reconnect。
- Use relay / Try P2P。

设计建议：

- 连接状态应该出现在机器/终端工作区的状态区域。
- 诊断详情可放在 modal、drawer 或 inspector panel。
- 失败状态要提供下一步操作：重连、切换 relay、重新授权。

## 推荐信息架构

请设计以下概念页面/区域：

1. Machine Browser
   - 机器列表、搜索/筛选、状态分组、添加/扫码、登录入口、传输中心入口。

2. Machine Workspace
   - 针对单台机器的主工作区。
   - 包含终端、文件、连接状态、机器信息、传输任务。

3. Terminal Workspace
   - 默认主视图。
   - 支持终端列表侧栏、终端主画布、工具栏、split view、移动 keybar。

4. Files Workspace
   - 文件浏览、文件操作、预览、上传下载。
   - 可作为 tab、side panel 或 split panel。

5. Transfer Center
   - 全局和机器级任务视图。

6. Settings
   - Account、Connection、Terminal、Diagnostics 分组。

7. Pair / Authorize Flow
   - 扫码、手动输入、Pair ID/Secret、错误恢复。

8. Connection Inspector
   - 连接模式、candidate、RTT、重连/relay 控制。

## 响应式设计要求

### 手机竖屏

- 单列导航。
- 首页是机器列表。
- 进入机器后先看到终端列表。
- 打开终端后终端全屏优先。
- 顶部只放必要状态和少量图标，更多操作进入工具面板。
- 底部保留 terminal keybar。
- 文件管理和传输中心可以是全屏页面。
- 所有底部工具栏要避开 safe area。

### 手机横屏

- 终端应尽可能全屏。
- 顶部栏高度尽量小。
- keybar 可压缩或自动隐藏。
- 文件/工具面板优先用侧边抽屉。

### 平板

- 推荐双栏：左侧机器/终端导航，右侧工作区。
- 机器工作区内可切换 Terminal / Files / Transfers / Info。
- 文件管理可以与终端并排或作为右侧 panel。
- 弹窗减少，更多使用 side sheet。

### 桌面 / 大屏

- 推荐三栏工作台：
  - 左侧：机器导航，可搜索、筛选、分组。
  - 中间：终端工作区，支持 split view。
  - 右侧：可切换 inspector，例如 Files、Transfers、Connection、Terminal Details。
- 终端列表可以是机器工作区左侧的第二层侧栏，也可以合并到左侧导航中。
- 全局顶部栏显示当前机器、连接状态、主要命令。
- 大屏不要出现大量手机式底部 sheet。
- 支持高信息密度，但保持清晰层级。

## 视觉风格要求

- 风格：专业、安静、高效，接近远程开发控制台 / 运维工具。
- 不要做营销感 hero、插画、大卡片堆叠。
- 不要用过度圆角、渐变球、装饰背景。
- 终端区域应使用真正适合长时间工作的深色/浅色主题。
- 图标按钮要有明确 tooltip。
- 状态色要克制：
  - Connected / Online：绿色。
  - Connecting / Syncing：蓝色。
  - Relay / Warning：琥珀色。
  - Failed / Dangerous：红色。
  - Neutral / Offline：灰色。
- 机器、终端、文件、传输任务都需要可扫描的信息密度。
- 按钮和工具栏不要因为文字过长导致挤压或换行混乱。
- 桌面端应有 hover、focus、selected、disabled、loading、error 状态。

## 必须覆盖的关键流程

请至少输出这些流程的高保真界面：

1. 未登录且无机器：扫码添加 / 登录同步。
2. 已登录机器列表：云端和本地机器混合展示。
3. 点击未授权云端机器：进入 Re-authorize 设备流程。
4. 扫码添加本地机器成功后进入机器工作区。
5. 进入机器后查看终端列表并创建新终端。
6. 打开终端，显示连接中、连接成功、连接失败三种状态。
7. 终端工具：复制/粘贴、选择模式、Fn 快捷、字体大小、renderer、resize owner。
8. 打开 split terminal，并支持同步输入。
9. 从终端打开文件管理器，浏览 cwd。
10. 文件预览图片/文本/视频或 unsupported 状态。
11. 下载文件后进入传输中心，查看暂停/恢复/失败/完成。
12. 打开连接诊断并切换 Use relay / Try P2P。
13. 设置页调整终端主题、字体、renderer、键盘模式。

## 输出要求

请输出一套完整 UI 设计方案，包括：

- 信息架构图或页面地图。
- 手机、平板、桌面三个断点的布局说明。
- 关键页面高保真设计。
- 主要组件清单：机器行、终端行、终端工具栏、文件行、文件预览、传输任务、连接状态、配对弹窗、设置项。
- 每个页面的 loading / empty / error / disconnected / reconnecting 状态。
- 交互说明：点击、长按、右键、拖拽分屏、快捷工具、返回行为。
- 一套设计 token：颜色、字体、间距、圆角、阴影、状态色。
- 不改变产品边界：运行时数据仍然通过 WebRTC 通道，不要设计任何绕过 WebRTC 的直接文件/终端传输入口。

请用中文输出设计方案，但界面文案可以保留英文，因为当前产品已有大量英文 UI 文案。重点是让设计既能落地实现，又能明显改善当前移动优先、弹窗过多、桌面端利用不足的问题。
