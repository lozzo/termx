package state

type TUIConfigStore struct {
	Version     int
	Profile     string
	Theme       TUIThemeConfig
	Chrome      TUIChromeConfig
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

type TUIInteractionConfig struct {
	Mouse                 bool
	StickyPrefixTimeoutMS int
	ConfirmDestructive    bool
	ClipboardHistory      TUIClipboardHistoryConfig
	Picker                TUIPickerConfig
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
