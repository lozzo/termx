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

type TUIChromeConfig struct {
	Header            bool
	Footer            bool
	PanelPresentation string
	TabCreateIcon     string
	TabTemplate       string
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
