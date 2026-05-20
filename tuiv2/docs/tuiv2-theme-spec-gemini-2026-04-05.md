# termx tuiv2 视觉与配色规范文档

日期：2026-04-05  
来源：Gemini 设计方案

## 1. 整体视觉方向 (Design Goal)

- 现代克制风 (Modern-Minimal) & 宿主融合 (Host-Native)。
- 界面不应打破用户自己精心配置的终端主题。所有的颜色变体必须从用户宿主终端的调色板（Palette）和默认背景/前景色中推导。
- 弱化框架和边框的存在感，依靠不同灰度/亮度的前景色（Foreground）来区分信息层级，保持整体界面的“通透感”（避免大面积突兀的背景色块）。

## 2. 全局语义色体系 (Global Semantic Color System)

本系统不定义具体色相，而是定义“语义 Token”。工程师需从宿主终端的 ANSI 色板中提取对应槽位的颜色：

### 2.1 基础表面 (Surfaces)

- App / Chrome BG：完全透明或严格等同于宿主终端默认背景色。
- Panel BG (普通窗格)：同宿主默认背景色。
- Overlay BG (悬浮卡片)：在宿主背景色基础上进行极轻微的提亮（或使用终端调色板中的最暗灰），建立轻微的悬浮 Z 轴层级。

### 2.2 文本与边框 (Text & Borders)

- Normal Text：宿主默认前景色。
- Muted Text (弱化文本)：宿主默认前景色降低亮度/透明度（如 40%-50% 的不透明度）。用于次要信息、非激活态文本。
- Muted Border：极低亮度的前景色，仅用于物理分割，不抢视线。

### 2.3 强调与语义 (Accents)

- Primary Accent (主强调色)：提取宿主调色板中最亮眼的特征色（通常映射到 ANSI Bright Blue / Bright Magenta 等高光位）。用于核心焦点、当前选中项。
- Semantic Success：映射宿主调色板的成功/正常色（ANSI Green / Bright Green）。
- Semantic Warning：映射宿主调色板的警告色（ANSI Yellow / Bright Yellow）。
- Semantic Danger：映射宿主调色板的危险/错误色（ANSI Red / Bright Red）。
- Semantic Info：映射宿主调色板的信息色（ANSI Cyan / Bright Cyan）。

## 3. 交互层级定义 (Interaction Hierarchy)

界面元素的层级完全通过相对亮度和排版符号来区分。

- Current / Active (当前焦点)：整个界面中最亮的部分。必须使用 Primary Accent 配合宿主默认前景色。
- Primary Action (主操作)：必须明确指引用户点击。放弃大面积背景色块填充，改为：Primary Accent 前景色 + 粗体 (Bold) + 强视觉指示符包裹（例如 `► Attach ◄` 或 `[ Attach ]` 辅以高亮边框）。
- Secondary Action (次操作)：Normal Text 或微弱提亮的 Muted Text，表现为普通纯文本或图标。
- Danger Action (危险操作)：必须使用 Semantic Danger 色。常态下使用较暗的 Danger 色，选中/激活时使用最亮的 Danger 色。
- Passive Status (被动状态)：不可交互的状态信息（如：运行中）。只使用 Semantic 颜色文本，绝对不要看起来像按钮（不加括号、不加底色、无悬停高亮）。

## 4. 分区视觉规则 (Component Rules)

### 4.1 顶栏 (Top Bar)

- Workspace 标签：低调处理。使用 Muted Text，不需要高亮底色，仅作为左上角的文本锚点。
- Tab 标签 (Tabs)：
  - Active Tab：Normal Text 亮度最高，保留左侧 Primary Accent 颜色的竖线 `▎` 作为核心焦点标识。
  - Inactive Tab：Muted Text 弱化，无背景色区分，与顶栏融为一体。
- 新建按钮 (Create Tab +)：层级高于普通次操作，使用 Primary Accent 或 Semantic Success 的前景色，确保易于发现。
- 操作图标 (Action Tokens)：常态使用 Muted Text，激活/选中时提亮至 Normal Text。

### 4.2 状态栏 (Status Bar)

- 视觉基调：安静的底层状态条，类似现代化的 Vim/Tmux 底部栏。
- Mode Badge：唯一允许使用反白（前景色反转为背景色）的小色块，用于极致突出当前模式（如 Normal / Insert）。
- 快捷键提示 (Footer / Hint)：必须拆分层级。
  - Key (按键)：使用 Primary Accent 或 Normal Text，带方括号 `[Ctrl+N]`。
  - Text (说明)：紧跟其后，严格使用 Muted Text 弱化。
- Meta (右侧信息)：全局最弱，使用 Muted Text。

### 4.3 主体窗格 (Pane Grid & Chrome)

- Active Pane (激活窗格)：
  - 边框：Primary Accent。
  - 标题 (Title)：Normal Text (最亮) + 粗体。
  - 状态与元数据：Muted Text。
  - 操作图标：提亮至 Normal Text。
- Inactive Pane (非激活窗格)：
  - 边框：Muted Border (极暗)。
  - 顶边所有元素（标题、状态、图标）：全部统一降级为 Muted Text。

### 4.4 空窗格 (Empty Pane)

作为按钮最集中的区域，严格按前景色和符号建立层级，保持界面通透：

- Headline (标题)：Normal Text，正常陈述。
- Attach existing (主操作)：Primary Accent 前景色 + 强包裹符 (如 `► Attach ◄`)。
- Create new (次操作/成功语境)：Semantic Success 前景色 + 普通包裹符 (如 `[ Create ]`)。
- Open manager (次操作)：Normal Text + 普通包裹符。
- Close pane (危险操作)：Semantic Danger 前景色 + 普通包裹符。

### 4.5 弹层与卡片 (Overlays)

- 视觉分离：除了 Overlay BG 的轻微提亮外，增加一圈高对比度的细边框（使用 Normal Text 或 Primary Accent 降低不透明度），将卡片从底层 Pane 中“拔”出来。
- 输入/搜索行：激活时的输入光标或前导符（如 `>` `?`）使用 Primary Accent。
- Overlay Footer：排版和配色完全复用状态栏快捷键规范（Key 亮，Text 暗）。

## 5. 按钮、图标与状态统一语言 (Token & Icon Language)

全面拥抱 Nerd Fonts，替代现有的基础 ASCII 字符，提升现代感与专业度。

### 5.1 窗格控制图标 (Pane Controls)

统一使用 Nerd Font 窗口控制类图标，常态 Muted，Hover/Active 时高亮：

- Zoom：替换 `⛶` 为 `󰍉` (nf-md-magnify) 或 `󰊓` (nf-md-window_maximize)。
- Split V/H：替换 `│` / `─` 为 `󰤽` / `󰤼` (nf-md-dock_*) 等切分图标。
- Close / Kill：替换 `×` 为 `󰖯` (nf-md-close_box) 或 `󱎘`。只要是关闭/销毁动作，Hover/Active 时必须映射为 Semantic Danger 色。
- Floating Center：替换 `◎` 为 `󰆚` (nf-md-image_filter_center_focus)。

### 5.2 状态符号 (Status Symbols)

保持极简，仅使用前置 Nerd Font 圆点或小图标，无背景色，无包裹符：

- Running (运行)：`` (nf-fa-circle) + Semantic Success。
- Waiting (等待)：`󰔟` (nf-md-clock_outline) 或 `` + Semantic Warning。
- Killed/Exited (终止)：`` (nf-fa-times) + Semantic Danger。

### 5.3 动作包裹语言 (Action Wrappers)

对于非图标类的文字操作（如 Footer 里的提交、Empty pane 的按钮），建立统一的字符包裹规范：

- 主操作 (Primary)：使用特殊的指向性符号，如 `► Text ◄`。
- 次操作/普通按钮 (Secondary/Danger)：统一使用方括号 `[ Text ]`，依靠内部文本颜色（Success / Danger / Normal）区分语义。

## 6. 给工程师的落地映射指南 (Implementation Notes)

工程师在修改 `styles.go` 和各渲染模块时，请参考以下映射：

1. 取消所有硬编码颜色：将 `uiThemeFromHostColors` 中的强调色直接映射为宿主的 ANSI 色值，不再指定具体的 HEX。
2. 重构 Empty Pane 按钮：修改 `emptyPaneActionDrawStyle`，移除背景色块逻辑，改为前景色提亮 + 前后追加 `► ◄` 或 `[ ]` 字符串。
3. 替换 Icon 常量：在 `paneChromeActionTokensForFrame` 等文件中，将旧的单字符替换为上述推荐的 Nerd Font Unicode 字符。
4. 统一 Footer 与 Hint：确保 `overlayFooterKeyStyle` 和 `statusHintKeyStyle` 指向同一个样式定义（亮前景色），`overlayFooterTextStyle` 和 `statusHintTextStyle` 指向同一个样式定义（Muted 前景色）。
5. 对齐危险色：全局检索 Close, Kill, Delete 的动作 Token，确保它们在被选中或 Hover 时，调用的都是基于 `hostPalette` 提取的 Danger 样式。
