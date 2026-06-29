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

type TUIKeymapConfig struct {
	Root      TUIRootKeymapConfig
	Copy      TUICopyKeymapConfig
	Tab       TUITabKeymapConfig
	Workspace TUIWorkspaceKeymapConfig
}

type TUIRootKeymapConfig struct {
	TerminalPicker string
	TabMode        string
	WorkspaceMode  string
	FloatingMode   string
	PaneMode       string
	ResizeMode     string
	GlobalMode     string
}

type TUICopyKeymapConfig struct {
	ClipboardHistory string
	PasteLatest      string
	PasteSystem      string
}

type TUITabKeymapConfig struct {
	Create   string
	Close    string
	Rename   string
	Next     string
	Previous string
}

type TUIWorkspaceKeymapConfig struct {
	Navigator string
	Create    string
	Delete    string
	Rename    string
}
