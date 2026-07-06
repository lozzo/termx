package state

type TUIConfigStore struct {
	Version     int
	Profile     string
	Theme       TUIThemeConfig
	Chrome      TUIChromeConfig
	Footer      TUIFooterConfig
	Interaction TUIInteractionConfig
	Keymap      TUIKeymapConfig
}

type TUIThemeConfig struct {
	Mode       string
	Palette    string
	Primary    string
	Secondary  string
	Foreground string
	Background string
	Muted      string
	Success    string
	Warning    string
	Danger     string
	Info       string
	Border     TUIThemeBorderConfig
	Surface    TUIThemeSurfaceConfig
}

type TUIThemeBorderConfig struct {
	Panel    string
	Active   string
	Inactive string
	Muted    string
}

type TUIThemeSurfaceConfig struct {
	ChromeBG  string
	StatusBG  string
	OverlayBG string
	ToastBG   string
}

// TUIChromeConfig 描述 TUI 外层 chrome 的本地展示偏好。
// 它只控制 header/footer、pane 呈现和可见 action 字形；pane/tab/workspace 的真实状态仍由 ShellStore 持有。
type TUIChromeConfig struct {
	Header            bool
	Footer            bool
	PanelPresentation string
	TabCreateIcon     string
	// TabCreateTemplate 是顶部创建 tab 按钮的展示模板。
	// 它只改变 tab.create action 的可见片段和 hit region 样式，真实新建 tab 消息仍由 app reducer 处理。
	TabCreateTemplate string
	// WorkspaceTemplate 是顶部 workspace 槽位的展示模板。
	// 它只消费当前 ShellStore workspace 名称并绑定打开 Workbench Navigator 的既有 action，
	// 不保存 workspace truth，也不能改写 workspace/tab/pane 的 reducer-owned 状态。
	WorkspaceTemplate string
	TabTemplate       string
	// PaneTitleTemplate 是 pane/floating terminal chrome 标题模板。
	// 它只消费 reducer-owned terminal pool 与 endpoint 展示投影，不改变 pane title、terminal identity 或输入路由。
	PaneTitleTemplate string
	PaneGlyphs        TUIPaneChromeGlyphsConfig
}

// TUIPaneChromeGlyphsConfig 描述 pane/floating chrome 可点击动作、状态标记和左右装饰模板。
// 这些字段只改变 renderer 展示文字；真实 action id、鼠标 hit region、resize owner
// 消息链路仍由 reducer/app 持有，配置不能改写这些交互语义。
type TUIPaneChromeGlyphsConfig struct {
	ActionLeft          string
	ActionLeftSet       bool
	ActionRight         string
	ActionRightSet      bool
	ActionSeparator     string
	ActionSeparatorSet  bool
	ActionGroupLeft     string
	ActionGroupLeftSet  bool
	ActionGroupRight    string
	ActionGroupRightSet bool
	OwnerLeft           string
	OwnerLeftSet        bool
	OwnerRight          string
	OwnerRightSet       bool
	Owner               string
	OwnerSet            bool
	OwnerPending        string
	OwnerPendingSet     bool
	TakeOwner           string
	TakeOwnerSet        bool
	Zoom                string
	// Unzoom 是 zoom 状态下 pane.zoom toggle action 的展示 glyph；配置只影响 renderer。
	Unzoom           string
	SplitVertical    string
	SplitHorizontal  string
	Close            string
	SizeLock         string
	SizeUnlock       string
	CenterFloating   string
	CollapseFloating string
	Running          string
	Waiting          string
	Exited           string
	Killed           string
	// Overflow* 只配置内容裁切提示 glyph；是否裁切仍由 renderer ContentOverflow 计算。
	OverflowLeft      string
	OverflowLeftSet   bool
	OverflowRight     string
	OverflowRightSet  bool
	OverflowTop       string
	OverflowTopSet    bool
	OverflowBottom    string
	OverflowBottomSet bool
	// OverflowStyle 可写内置 style token 或 #RRGGBB，作用域只限 pane/floating chrome marker。
	OverflowStyle    string
	OverflowStyleSet bool
	// ExtentPlaceholder* 配置 live surface 尺寸差异区域的占位点展示，不改变 terminal 内容。
	ExtentPlaceholder         string
	ExtentPlaceholderSet      bool
	ExtentPlaceholderStyle    string
	ExtentPlaceholderStyleSet bool
}

// TUIFooterConfig 描述 footer 的用户可配置展示层。
// 它只影响 mode badge、action token 的顺序和文案；真实点击语义仍以 render ActionSpec/app reducer 为准。
type TUIFooterConfig struct {
	Templates TUIFooterTemplatesConfig
	Modes     map[string]TUIFooterModeConfig
	Actions   map[string]TUIFooterActionConfig
}

// TUIFooterTemplatesConfig 描述 footer token 的轻量模板。
// 当前只支持文本占位符替换，不执行脚本，也不改变 hit region/action owner。
type TUIFooterTemplatesConfig struct {
	ModeBadge                string
	Key                      string
	Action                   string
	Separator                string
	WorkspaceSummary         string
	FloatingSummary          string
	FloatingCollapsedSummary string
	TerminalsSummary         string
	TabsSummary              string
	PanesSummary             string
	KeylockOn                string
	KeylockOff               string
}

// TUIFooterModeConfig 描述某个 footer mode 的徽标和 action 引用列表。
// Actions 是逗号分隔的 action alias 或 render action id，避免配置解析层引入通用 YAML list。
type TUIFooterModeConfig struct {
	Icon    string
	Label   string
	Style   string
	Actions string
}

// TUIFooterActionConfig 描述一个可复用 footer action token。
// ID 为空时该 token 只是展示提示；ID 非空时必须落到现有 render action 语义边界。
type TUIFooterActionConfig struct {
	ID    string
	Key   string
	Label string
	Icon  string
	Style string
}

type TUIInteractionConfig struct {
	Mouse                         bool
	StickyPrefixTimeoutMS         int
	ShortcutPassthroughIntervalMS int
	ConfirmDestructive            bool
	ClipboardHistory              TUIClipboardHistoryConfig
	Picker                        TUIPickerConfig
}

type TUIClipboardHistoryConfig struct {
	MaxItems          int
	NameWidth         int
	PreviewWidthRatio float64
}

type TUIPickerConfig struct {
	FuzzyMatch       string
	HighlightMatches bool
}

// TUIKeymapConfig 描述当前 TUI 客户端的键盘入口与各 sticky mode 内部动作；配置只影响输入路由，不持有 reducer 状态。
type TUIKeymapConfig struct {
	Root          TUIRootKeymapConfig
	CopyMode      TUICopyKeymapConfig
	TabMode       TUITabKeymapConfig
	WorkspaceMode TUIWorkspaceKeymapConfig
	FloatingMode  TUIModeEntryKeymapConfig
	PaneMode      TUIModeEntryKeymapConfig
	ResizeMode    TUIModeEntryKeymapConfig
	GlobalMode    TUIModeEntryKeymapConfig
}

// TUIRootKeymapConfig 描述默认工作台输入态下不进入 sticky mode 的直接动作。
type TUIRootKeymapConfig struct {
	TerminalPicker string
}

// TUIModeEntryKeymapConfig 描述 sticky mode 从 root 输入态进入该 mode 的入口键。
type TUIModeEntryKeymapConfig struct {
	Entry string
}

// TUICopyKeymapConfig 描述 copy/history mode 的入口键与 mode 内部动作键。
type TUICopyKeymapConfig struct {
	Entry            string
	ClipboardHistory string
	PasteLatest      string
	PasteSystem      string
}

// TUITabKeymapConfig 描述 tab sticky mode 的入口键与 mode 内部动作键。
type TUITabKeymapConfig struct {
	Entry    string
	Create   string
	Close    string
	Rename   string
	Next     string
	Previous string
}

// TUIWorkspaceKeymapConfig 描述 workspace sticky mode 的入口键与 mode 内部动作键。
type TUIWorkspaceKeymapConfig struct {
	Entry     string
	Navigator string
	Create    string
	Delete    string
	Rename    string
}
