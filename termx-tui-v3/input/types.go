package input

// EventKind 分类宿主输入，不依赖 Bubble Tea key/mouse 类型。
type EventKind string

const (
	EventKindKey       EventKind = "key"
	EventKindMouse     EventKind = "mouse"
	EventKindResize    EventKind = "resize"
	EventKindHostTheme EventKind = "host-theme"
)

type Key string

const (
	KeyPageUp    Key = "page-up"
	KeyPageDn    Key = "page-down"
	KeyUp        Key = "up"
	KeyDown      Key = "down"
	KeyLeft      Key = "left"
	KeyRight     Key = "right"
	KeyHome      Key = "home"
	KeyEnd       Key = "end"
	KeyDelete    Key = "delete"
	KeyInsert    Key = "insert"
	KeyBackspace Key = "backspace"
	KeyTab       Key = "tab"
	KeyShiftTab  Key = "shift-tab"
	KeyEsc       Key = "esc"
	KeyEnter     Key = "enter"
	KeyF1        Key = "f1"
	KeyF2        Key = "f2"
	KeyF3        Key = "f3"
	KeyF4        Key = "f4"
	KeyF5        Key = "f5"
	KeyF6        Key = "f6"
	KeyF7        Key = "f7"
	KeyF8        Key = "f8"
	KeyF9        Key = "f9"
	KeyF10       Key = "f10"
	KeyF11       Key = "f11"
	KeyF12       Key = "f12"
	KeyChar      Key = "char"
	KeyUnknown   Key = "unknown"
)

type MouseButton string

const (
	MouseWheelUp    MouseButton = "wheel-up"
	MouseWheelDown  MouseButton = "wheel-down"
	MouseLeft       MouseButton = "left"
	MouseLeftDrag   MouseButton = "left-drag"
	MouseLeftUp     MouseButton = "left-up"
	MouseMiddle     MouseButton = "middle"
	MouseMiddleDrag MouseButton = "middle-drag"
	MouseMiddleUp   MouseButton = "middle-up"
	MouseRight      MouseButton = "right"
	MouseRightDrag  MouseButton = "right-drag"
	MouseRightUp    MouseButton = "right-up"
	MouseMove       MouseButton = "move"
	MouseRelease    MouseButton = "release"
)

// InputEvent 是 TerminalHost 拥有的宿主输入边界。
type InputEvent struct {
	Kind   EventKind
	Key    Key
	Char   string
	Mouse  MouseButton
	Row    int
	Col    int
	Cols   int
	Rows   int
	Theme  HostThemeEvent
	Alt    bool
	Ctrl   bool
	Shift  bool
	RawSeq string
}

type HostThemeEvent struct {
	DefaultFG    string
	DefaultBG    string
	PaletteIndex int
	PaletteColor string
}

type IntentKind string

const (
	IntentNone               IntentKind = "none"
	IntentOpenTerminalPicker IntentKind = "open-terminal-picker"
	IntentEnterCopyMode      IntentKind = "enter-copy-mode"
	IntentRequestOlder       IntentKind = "request-older"
	IntentExitCopyMode       IntentKind = "exit-copy-mode"
	IntentTerminalInput      IntentKind = "terminal-input"
	IntentMouseSelect        IntentKind = "mouse-select"
	IntentSetInteractionMode IntentKind = "set-interaction-mode"
	IntentExitInteraction    IntentKind = "exit-interaction"
	IntentShellAction        IntentKind = "shell-action"
	IntentPaneCommand        IntentKind = "pane-command"
	IntentWorkbenchCommand   IntentKind = "workbench-command"
)

type InteractionMode string

const (
	InteractionModeNormal    InteractionMode = ""
	InteractionModePane      InteractionMode = "pane"
	InteractionModeResize    InteractionMode = "resize"
	InteractionModeGlobal    InteractionMode = "global"
	InteractionModeFloating  InteractionMode = "floating"
	InteractionModeTab       InteractionMode = "tab"
	InteractionModeWorkspace InteractionMode = "workspace"
)

type ShellAction string

const (
	ShellActionToggleHeader ShellAction = "shell.toggle-header"
	ShellActionToggleFooter ShellAction = "shell.toggle-footer"
	ShellActionClearToasts  ShellAction = "shell.clear-toasts"
	ShellActionCloseToast   ShellAction = "shell.close-toast"
	ShellActionFloatingCtrl ShellAction = "shell.floating-control"
	ShellActionFloatingNew  ShellAction = "shell.floating-new"
	ShellActionFloatingMove ShellAction = "shell.floating-move"
	ShellActionFloatingSize ShellAction = "shell.floating-size"
	ShellActionOpenPool     ShellAction = "shell.open-terminal-pool"
	ShellActionOpenTree     ShellAction = "shell.open-workbench-tree"
	ShellActionOpenPrompt   ShellAction = "shell.open-prompt"
	ShellActionOpenHelp     ShellAction = "shell.open-help"
	ShellActionQuit         ShellAction = "shell.quit"
)

// Intent 是 input router 输出的 semantic intent，不直接修改 state。
type Intent struct {
	Kind     IntentKind
	Event    InputEvent
	Bytes    []byte
	Reason   string
	Command  string
	Mode     InteractionMode
	Action   ShellAction
	RawMouse bool
}

type RouteOptions struct {
	Mode                     InteractionMode
	CopyModeActive           bool
	TerminalMousePassthrough bool
}

func Route(event InputEvent, copyModeActive bool) Intent {
	return RouteWithOptions(event, RouteOptions{CopyModeActive: copyModeActive})
}

func RouteWithMode(event InputEvent, copyModeActive bool, mode InteractionMode) Intent {
	return RouteWithOptions(event, RouteOptions{Mode: mode, CopyModeActive: copyModeActive})
}

func RouteWithOptions(event InputEvent, options RouteOptions) Intent {
	switch event.Kind {
	case EventKindKey:
		return routeKey(event, options)
	case EventKindMouse:
		return routeMouse(event, options)
	case EventKindHostTheme:
		return Intent{Kind: IntentNone, Event: event, Reason: "host theme capability event"}
	default:
		return Intent{Kind: IntentNone, Event: event, Reason: "unknown input kind"}
	}
}
