package input

import (
	"sort"
	"strconv"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/shortcut"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// Binding 是 shortcut catalog 编译后的运行时路由项。
// 它由内置默认 shortcuts 或用户 `tui.shortcuts` 生成；输入路由只查 Binding，
// 不再直接读取旧 keymap 或分散硬编码键位。
type Binding struct {
	ID         string
	ActionID   string
	Label      string
	Mode       InteractionMode
	Key        Key
	Char       string
	Ctrl       bool
	Alt        bool
	Shift      bool
	Invocation shortcut.ActionInvocation
}

type shortcutDefault struct {
	Scene  string
	Key    string
	Action string
}

// ShortcutEntry 是 shortcut catalog 面向提示层和 overlay 输入层的只读条目。
// Scene/Key/ActionID 来自内置默认或 `tui.shortcuts`；Label 只来自配置覆盖，不生成执行语义。
type ShortcutEntry struct {
	Scene    string
	Key      string
	KeyLabel string
	ActionID string
	Label    string
	Show     *bool
}

var builtinShortcutDefaults = []shortcutDefault{
	{Scene: "global", Key: "ctrl-p", Action: "menu.panel"},
	{Scene: "global", Key: "ctrl-r", Action: "menu.resize"},
	{Scene: "global", Key: "ctrl-g", Action: "menu.system"},
	{Scene: "global", Key: "ctrl-o", Action: "menu.floating"},
	{Scene: "global", Key: "ctrl-t", Action: "menu.tab"},
	{Scene: "global", Key: "ctrl-w", Action: "menu.workspace"},
	{Scene: "global", Key: "ctrl-f", Action: "terminal_picker.open"},
	{Scene: "global", Key: "ctrl-v", Action: "copy.enter"},
	{Scene: "global", Key: "page-up", Action: "copy.enter"},

	{Scene: "panel", Key: "x", Action: "panel.close"},
	{Scene: "panel", Key: "w", Action: "panel.close"},
	{Scene: "panel", Key: "d", Action: "panel.detach"},
	{Scene: "panel", Key: "r", Action: "panel.reconnect"},
	{Scene: "panel", Key: "R", Action: "panel.restart"},
	{Scene: "panel", Key: "a", Action: "panel.take_owner"},
	{Scene: "panel", Key: "s", Action: "panel.size_lock"},
	{Scene: "panel", Key: "%", Action: "panel.split_right"},
	{Scene: "panel", Key: "ctrl-d", Action: "panel.split_right"},
	{Scene: "panel", Key: "\"", Action: "panel.split_down"},
	{Scene: "panel", Key: "ctrl-e", Action: "panel.split_down"},
	{Scene: "panel", Key: "X", Action: "panel.kill"},
	{Scene: "panel", Key: "z", Action: "panel.toggle_zoom"},
	{Scene: "panel", Key: "b", Action: "panel.balance"},
	{Scene: "panel", Key: "c", Action: "panel.presentation_card"},
	{Scene: "panel", Key: "p", Action: "panel.presentation_split_line"},
	{Scene: "panel", Key: "n", Action: "panel.focus_next"},
	{Scene: "panel", Key: "N", Action: "panel.focus_prev"},
	{Scene: "panel", Key: "h", Action: "panel.focus_prev"},
	{Scene: "panel", Key: "k", Action: "panel.focus_prev"},
	{Scene: "panel", Key: "l", Action: "panel.focus_next"},
	{Scene: "panel", Key: "j", Action: "panel.focus_next"},
	{Scene: "panel", Key: "left", Action: "panel.focus_prev"},
	{Scene: "panel", Key: "up", Action: "panel.focus_prev"},
	{Scene: "panel", Key: "right", Action: "panel.focus_next"},
	{Scene: "panel", Key: "down", Action: "panel.focus_next"},

	{Scene: "resize", Key: "left", Action: "resize.left"},
	{Scene: "resize", Key: "right", Action: "resize.right"},
	{Scene: "resize", Key: "up", Action: "resize.up"},
	{Scene: "resize", Key: "down", Action: "resize.down"},
	{Scene: "resize", Key: "h", Action: "resize.left"},
	{Scene: "resize", Key: "l", Action: "resize.right"},
	{Scene: "resize", Key: "k", Action: "resize.up"},
	{Scene: "resize", Key: "j", Action: "resize.down"},
	{Scene: "resize", Key: "a", Action: "panel.take_owner"},
	{Scene: "resize", Key: "s", Action: "panel.size_lock"},
	{Scene: "resize", Key: "space", Action: "resize.layout_toggle"},
	{Scene: "resize", Key: "A", Action: "resize.pan_left"},
	{Scene: "resize", Key: "S", Action: "resize.pan_down"},
	{Scene: "resize", Key: "W", Action: "resize.pan_up"},
	{Scene: "resize", Key: "D", Action: "resize.pan_right"},
	{Scene: "resize", Key: "shift-left", Action: "resize.pan_left"},
	{Scene: "resize", Key: "shift-down", Action: "resize.pan_down"},
	{Scene: "resize", Key: "shift-up", Action: "resize.pan_up"},
	{Scene: "resize", Key: "shift-right", Action: "resize.pan_right"},
	{Scene: "resize", Key: "0", Action: "resize.align_left"},
	{Scene: "resize", Key: "$", Action: "resize.align_right"},
	{Scene: "resize", Key: "^", Action: "resize.align_top"},
	{Scene: "resize", Key: "B", Action: "resize.align_bottom"},
	{Scene: "resize", Key: "m", Action: "resize.center"},
	{Scene: "resize", Key: "|", Action: "resize.center_x"},
	{Scene: "resize", Key: "_", Action: "resize.center_y"},
	{Scene: "resize", Key: "r", Action: "resize.layout_reset"},
	{Scene: "resize", Key: "H", Action: "resize.left_large"},
	{Scene: "resize", Key: "L", Action: "resize.right_large"},
	{Scene: "resize", Key: "K", Action: "resize.up_large"},
	{Scene: "resize", Key: "J", Action: "resize.down_large"},
	{Scene: "resize", Key: "b", Action: "panel.balance"},
	{Scene: "resize", Key: "=", Action: "panel.balance"},

	{Scene: "system", Key: "h", Action: "system.toggle_header"},
	{Scene: "system", Key: "f", Action: "system.toggle_footer"},
	{Scene: "system", Key: "c", Action: "system.clear_toasts"},
	{Scene: "system", Key: "T", Action: "system.close_toast"},
	{Scene: "system", Key: "p", Action: "system.open_terminal_pool"},
	{Scene: "system", Key: "m", Action: "system.open_terminal_pool"},
	{Scene: "system", Key: "t", Action: "system.open_terminal_pool"},
	{Scene: "system", Key: "w", Action: "system.open_workbench_tree"},
	{Scene: "system", Key: "l", Action: "system.toggle_shortcut_lock"},
	{Scene: "system", Key: ":", Action: "system.open_prompt"},
	{Scene: "system", Key: "?", Action: "system.open_help"},
	{Scene: "system", Key: "q", Action: "system.quit"},

	{Scene: "floating", Key: "n", Action: "floating.new"},
	{Scene: "floating", Key: "o", Action: "floating.overview"},
	{Scene: "floating", Key: "1", Action: "floating.summon.1"},
	{Scene: "floating", Key: "2", Action: "floating.summon.2"},
	{Scene: "floating", Key: "3", Action: "floating.summon.3"},
	{Scene: "floating", Key: "4", Action: "floating.summon.4"},
	{Scene: "floating", Key: "5", Action: "floating.summon.5"},
	{Scene: "floating", Key: "6", Action: "floating.summon.6"},
	{Scene: "floating", Key: "7", Action: "floating.summon.7"},
	{Scene: "floating", Key: "8", Action: "floating.summon.8"},
	{Scene: "floating", Key: "9", Action: "floating.summon.9"},
	{Scene: "floating", Key: "f", Action: "system.open_terminal_picker"},
	{Scene: "floating", Key: "a", Action: "floating.take_owner"},
	{Scene: "floating", Key: "x", Action: "floating.close"},
	{Scene: "floating", Key: "z", Action: "floating.collapse"},
	{Scene: "floating", Key: "m", Action: "floating.collapse"},
	{Scene: "floating", Key: "c", Action: "floating.center"},
	{Scene: "floating", Key: "v", Action: "floating.toggle_all"},
	{Scene: "floating", Key: "=", Action: "floating.fit"},
	{Scene: "floating", Key: "s", Action: "floating.auto_fit"},
	{Scene: "floating", Key: "h", Action: "floating.move_left"},
	{Scene: "floating", Key: "l", Action: "floating.move_right"},
	{Scene: "floating", Key: "k", Action: "floating.move_up"},
	{Scene: "floating", Key: "j", Action: "floating.move_down"},
	{Scene: "floating", Key: "left", Action: "floating.move_left"},
	{Scene: "floating", Key: "right", Action: "floating.move_right"},
	{Scene: "floating", Key: "up", Action: "floating.move_up"},
	{Scene: "floating", Key: "down", Action: "floating.move_down"},
	{Scene: "floating", Key: "H", Action: "floating.narrow"},
	{Scene: "floating", Key: "L", Action: "floating.wide"},
	{Scene: "floating", Key: "K", Action: "floating.short"},
	{Scene: "floating", Key: "J", Action: "floating.tall"},

	{Scene: "tab", Key: "c", Action: "tab.create"},
	{Scene: "tab", Key: "n", Action: "tab.next"},
	{Scene: "tab", Key: "l", Action: "tab.next"},
	{Scene: "tab", Key: "]", Action: "tab.next"},
	{Scene: "tab", Key: "p", Action: "tab.previous"},
	{Scene: "tab", Key: "h", Action: "tab.previous"},
	{Scene: "tab", Key: "[", Action: "tab.previous"},
	{Scene: "tab", Key: "1", Action: "tab.jump.1"},
	{Scene: "tab", Key: "2", Action: "tab.jump.2"},
	{Scene: "tab", Key: "3", Action: "tab.jump.3"},
	{Scene: "tab", Key: "4", Action: "tab.jump.4"},
	{Scene: "tab", Key: "5", Action: "tab.jump.5"},
	{Scene: "tab", Key: "6", Action: "tab.jump.6"},
	{Scene: "tab", Key: "7", Action: "tab.jump.7"},
	{Scene: "tab", Key: "8", Action: "tab.jump.8"},
	{Scene: "tab", Key: "9", Action: "tab.jump.9"},
	{Scene: "tab", Key: "r", Action: "tab.rename"},
	{Scene: "tab", Key: "x", Action: "tab.close"},
	{Scene: "tab", Key: "X", Action: "tab.kill"},

	{Scene: "workspace", Key: "c", Action: "workspace.create"},
	{Scene: "workspace", Key: "n", Action: "workspace.next"},
	{Scene: "workspace", Key: "l", Action: "workspace.next"},
	{Scene: "workspace", Key: "]", Action: "workspace.next"},
	{Scene: "workspace", Key: "p", Action: "workspace.previous"},
	{Scene: "workspace", Key: "h", Action: "workspace.previous"},
	{Scene: "workspace", Key: "[", Action: "workspace.previous"},
	{Scene: "workspace", Key: "r", Action: "workspace.rename"},
	{Scene: "workspace", Key: "x", Action: "workspace.delete"},
	{Scene: "workspace", Key: "t", Action: "system.open_workbench_tree"},
	{Scene: "workspace", Key: "f", Action: "system.open_workbench_tree"},
	{Scene: "workspace", Key: "s", Action: "system.open_workbench_tree"},

	{Scene: "copy", Key: "page-up", Action: "copy.request_older"},
	{Scene: "copy", Key: "page-down", Action: "copy.request_newer"},
	{Scene: "copy", Key: "home", Action: "copy.line_start"},
	{Scene: "copy", Key: "end", Action: "copy.line_end"},
	{Scene: "copy", Key: "left", Action: "copy.cursor_left"},
	{Scene: "copy", Key: "right", Action: "copy.cursor_right"},
	{Scene: "copy", Key: "down", Action: "copy.cursor_down"},
	{Scene: "copy", Key: "up", Action: "copy.cursor_up"},
	{Scene: "copy", Key: "enter", Action: "copy.accept"},
	{Scene: "copy", Key: "h", Action: "copy.cursor_left"},
	{Scene: "copy", Key: "l", Action: "copy.cursor_right"},
	{Scene: "copy", Key: "j", Action: "copy.cursor_down"},
	{Scene: "copy", Key: "k", Action: "copy.cursor_up"},
	{Scene: "copy", Key: "g", Action: "copy.oldest"},
	{Scene: "copy", Key: "G", Action: "copy.newest"},
	{Scene: "copy", Key: "u", Action: "copy.half_page_older"},
	{Scene: "copy", Key: "d", Action: "copy.half_page_newer"},
	{Scene: "copy", Key: "space", Action: "copy.mark"},
	{Scene: "copy", Key: "y", Action: "copy.copy_selection"},
	{Scene: "copy", Key: "/", Action: "copy.search_start"},
	{Scene: "copy", Key: "H", Action: "copy.open_clipboard_history"},
	{Scene: "copy", Key: "p", Action: "copy.paste_latest"},
	{Scene: "copy", Key: "P", Action: "copy.paste_system"},

	{Scene: "terminal_picker", Key: "enter", Action: "terminal_picker.attach"},
	{Scene: "terminal_picker", Key: "tab", Action: "terminal_picker.split"},
	{Scene: "terminal_picker", Key: "ctrl-e", Action: "terminal_picker.edit"},
	{Scene: "terminal_picker", Key: "ctrl-k", Action: "terminal_picker.kill"},
	{Scene: "terminal_picker", Key: "ctrl-x", Action: "terminal_picker.delete"},

	{Scene: "terminal_pool", Key: "enter", Action: "terminal_pool.attach"},
	{Scene: "terminal_pool", Key: "ctrl-t", Action: "terminal_pool.attach_tab"},
	{Scene: "terminal_pool", Key: "ctrl-o", Action: "terminal_pool.attach_float"},
	{Scene: "terminal_pool", Key: "ctrl-r", Action: "terminal_pool.restart"},
	{Scene: "terminal_pool", Key: "ctrl-e", Action: "terminal_pool.edit"},
	{Scene: "terminal_pool", Key: "ctrl-k", Action: "terminal_pool.kill"},
	{Scene: "terminal_pool", Key: "ctrl-x", Action: "terminal_pool.delete"},

	{Scene: "workbench_tree", Key: "enter", Action: "workbench_tree.open"},
	{Scene: "workbench_tree", Key: "ctrl-n", Action: "workbench_tree.new"},
	{Scene: "workbench_tree", Key: "ctrl-r", Action: "workbench_tree.rename"},
	{Scene: "workbench_tree", Key: "ctrl-x", Action: "workbench_tree.delete"},
	{Scene: "workbench_tree", Key: "ctrl-d", Action: "workbench_tree.detach"},
	{Scene: "workbench_tree", Key: "ctrl-z", Action: "workbench_tree.zoom"},

	{Scene: "clipboard_history", Key: "enter", Action: "clipboard_history.paste"},
	{Scene: "clipboard_history", Key: "ctrl-n", Action: "clipboard_history.new"},
	{Scene: "clipboard_history", Key: "ctrl-e", Action: "clipboard_history.edit"},
	{Scene: "clipboard_history", Key: "ctrl-x", Action: "clipboard_history.delete"},

	{Scene: "floating_overview", Key: "enter", Action: "floating_overview.open"},
	{Scene: "floating_overview", Key: "1", Action: "floating.summon.1"},
	{Scene: "floating_overview", Key: "2", Action: "floating.summon.2"},
	{Scene: "floating_overview", Key: "3", Action: "floating.summon.3"},
	{Scene: "floating_overview", Key: "4", Action: "floating.summon.4"},
	{Scene: "floating_overview", Key: "5", Action: "floating.summon.5"},
	{Scene: "floating_overview", Key: "6", Action: "floating.summon.6"},
	{Scene: "floating_overview", Key: "7", Action: "floating.summon.7"},
	{Scene: "floating_overview", Key: "8", Action: "floating.summon.8"},
	{Scene: "floating_overview", Key: "9", Action: "floating.summon.9"},
	{Scene: "floating_overview", Key: "s", Action: "floating_overview.show_all"},
	{Scene: "floating_overview", Key: "c", Action: "floating_overview.collapse_all"},
	{Scene: "floating_overview", Key: "x", Action: "floating.close"},

	{Scene: "prompt", Key: "enter", Action: "prompt.submit"},

	{Scene: "help", Key: "enter", Action: "help.close"},
}

type shortcutCatalog struct {
	bindings []Binding
}

func (catalog shortcutCatalog) bindingsCopy() []Binding {
	out := make([]Binding, len(catalog.bindings))
	copy(out, catalog.bindings)
	return out
}

// BindingCatalog 返回内置默认 shortcuts 编译出的路由项。
// 它只用于测试和后续提示投影读取默认目录；运行时路由会按当前 config 重新生成 catalog。
func BindingCatalog() []Binding {
	return shortcutCatalogForConfig(state.TUIShortcutConfig{}).bindingsCopy()
}

// ShortcutEntriesForConfig 返回当前配置下的完整 shortcut catalog 条目。
// 该函数是 footer/help/overlay 提示层的快捷键真值；route 只消费其中可路由的 scene。
func ShortcutEntriesForConfig(shortcuts state.TUIShortcutConfig) []ShortcutEntry {
	if shortcutConfigUsesUserCatalog(shortcuts) {
		return shortcutEntriesFromConfig(shortcuts)
	}
	entries := make([]ShortcutEntry, 0, len(builtinShortcutDefaults))
	for _, item := range builtinShortcutDefaults {
		entries = append(entries, shortcutEntryFromParts(item.Scene, item.Key, item.Action, ""))
	}
	return entries
}

// ShortcutEntriesForScene 返回某个 scene 下的快捷键条目，保持 catalog 中的定义顺序。
func ShortcutEntriesForScene(shortcuts state.TUIShortcutConfig, sceneName string) []ShortcutEntry {
	entries := ShortcutEntriesForConfig(shortcuts)
	sceneKey := shortcutSceneCatalogKey(sceneName)
	out := make([]ShortcutEntry, 0, len(entries))
	for _, entry := range entries {
		if shortcutSceneCatalogKey(entry.Scene) == sceneKey {
			out = append(out, entry)
		}
	}
	return out
}

// ShortcutEntryForEvent 按 route 同源的 key matcher 查找某个 scene 下的 action。
// overlay reducer 用它把提示 catalog 直接作为键盘动作表，避免 overlay 另写一套快捷键。
func ShortcutEntryForEvent(shortcuts state.TUIShortcutConfig, sceneName string, event InputEvent) (ShortcutEntry, bool) {
	if event.Kind != EventKindKey {
		return ShortcutEntry{}, false
	}
	for _, entry := range ShortcutEntriesForScene(shortcuts, sceneName) {
		key, ok := parseShortcutKeyToken(entry.Key)
		if !ok {
			continue
		}
		binding := Binding{Key: key.Key, Char: key.Char, Ctrl: key.Ctrl, Alt: key.Alt, Shift: key.Shift}
		if bindingMatches(binding, event) {
			return entry, true
		}
	}
	return ShortcutEntry{}, false
}

func shortcutCatalogForConfig(shortcuts state.TUIShortcutConfig) shortcutCatalog {
	if shortcutConfigUsesUserCatalog(shortcuts) {
		return shortcutCatalogFromConfig(shortcuts)
	}
	catalog := shortcutCatalog{bindings: make([]Binding, 0, len(builtinShortcutDefaults))}
	for _, item := range builtinShortcutDefaults {
		catalog.add(item.Scene, item.Key, item.Action, "")
	}
	return catalog
}

func shortcutConfigUsesUserCatalog(shortcuts state.TUIShortcutConfig) bool {
	// 只有显式 scene 才声明用户 binding catalog；actions 只覆盖文案，必须继续复用默认 bindings。
	return len(shortcuts.Scenes) > 0
}

func shortcutCatalogFromConfig(shortcuts state.TUIShortcutConfig) shortcutCatalog {
	catalog := shortcutCatalog{}
	for sceneName, scene := range shortcuts.Scenes {
		for key, binding := range scene.Bindings {
			catalog.add(sceneName, key, binding.Action, binding.Label)
		}
	}
	return catalog
}

func shortcutEntriesFromConfig(shortcuts state.TUIShortcutConfig) []ShortcutEntry {
	sceneNames := make([]string, 0, len(shortcuts.Scenes))
	for sceneName := range shortcuts.Scenes {
		sceneNames = append(sceneNames, sceneName)
	}
	sort.Strings(sceneNames)
	entries := []ShortcutEntry{}
	for _, sceneName := range sceneNames {
		scene := shortcuts.Scenes[sceneName]
		keys := make([]string, 0, len(scene.Bindings))
		for key := range scene.Bindings {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			binding := scene.Bindings[key]
			entry := shortcutEntryFromParts(sceneName, key, binding.Action, binding.Label)
			entry.Show = binding.Show
			entries = append(entries, entry)
		}
	}
	return entries
}

func shortcutEntryFromParts(sceneName string, keyToken string, actionID string, label string) ShortcutEntry {
	return ShortcutEntry{
		Scene:    sceneName,
		Key:      keyToken,
		KeyLabel: ShortcutKeyDisplay(keyToken),
		ActionID: actionID,
		Label:    label,
	}
}

func shortcutSceneCatalogKey(sceneName string) string {
	sceneName = strings.ReplaceAll(strings.TrimSpace(sceneName), "-", "_")
	if sceneName == "pane" {
		return "panel"
	}
	return sceneName
}

func (catalog *shortcutCatalog) add(sceneName string, keyToken string, actionID string, label string) {
	mode, ok := shortcutSceneMode(sceneName)
	if !ok {
		return
	}
	key, ok := parseShortcutKeyToken(keyToken)
	if !ok {
		return
	}
	invocation, _, err := shortcut.ParseInvocation(actionID)
	if err != nil {
		return
	}
	definition := Binding{ActionID: actionID, Invocation: invocation}
	definition.ID = sceneName + ":" + keyToken + ":" + actionID
	definition.Mode = mode
	definition.Key = key.Key
	definition.Char = key.Char
	definition.Ctrl = key.Ctrl
	definition.Alt = key.Alt
	definition.Shift = key.Shift
	definition.Label = label
	catalog.bindings = append(catalog.bindings, definition)
}

type shortcutKey struct {
	Key   Key
	Char  string
	Ctrl  bool
	Alt   bool
	Shift bool
}

func parseShortcutKeyToken(token string) (shortcutKey, bool) {
	rest := strings.TrimSpace(token)
	if rest == "" {
		return shortcutKey{}, false
	}
	key := shortcutKey{}
	for {
		lower := strings.ToLower(rest)
		switch {
		case strings.HasPrefix(lower, "ctrl-"):
			key.Ctrl = true
			rest = rest[len("ctrl-"):]
		case strings.HasPrefix(lower, "alt-"):
			key.Alt = true
			rest = rest[len("alt-"):]
		case strings.HasPrefix(lower, "shift-"):
			key.Shift = true
			rest = rest[len("shift-"):]
		default:
			return parseShortcutKeyTokenBase(rest, key)
		}
		if rest == "" {
			return shortcutKey{}, false
		}
	}
}

func parseShortcutKeyTokenBase(token string, key shortcutKey) (shortcutKey, bool) {
	switch strings.ToLower(token) {
	case "space":
		key.Key = KeyChar
		key.Char = " "
	case "page-up", "pgup":
		key.Key = KeyPageUp
	case "page-down", "pgdn":
		key.Key = KeyPageDn
	case "up":
		key.Key = KeyUp
	case "down":
		key.Key = KeyDown
	case "left":
		key.Key = KeyLeft
	case "right":
		key.Key = KeyRight
	case "home":
		key.Key = KeyHome
	case "end":
		key.Key = KeyEnd
	case "delete":
		key.Key = KeyDelete
	case "insert":
		key.Key = KeyInsert
	case "backspace":
		key.Key = KeyBackspace
	case "tab":
		if key.Shift {
			key.Key = KeyShiftTab
		} else {
			key.Key = KeyTab
		}
	case "esc", "escape":
		key.Key = KeyEsc
	case "enter", "return":
		key.Key = KeyEnter
	case "f1":
		key.Key = KeyF1
	case "f2":
		key.Key = KeyF2
	case "f3":
		key.Key = KeyF3
	case "f4":
		key.Key = KeyF4
	case "f5":
		key.Key = KeyF5
	case "f6":
		key.Key = KeyF6
	case "f7":
		key.Key = KeyF7
	case "f8":
		key.Key = KeyF8
	case "f9":
		key.Key = KeyF9
	case "f10":
		key.Key = KeyF10
	case "f11":
		key.Key = KeyF11
	case "f12":
		key.Key = KeyF12
	default:
		if len([]rune(token)) != 1 {
			return shortcutKey{}, false
		}
		key.Key = KeyChar
		key.Char = token
	}
	return key, true
}

// ShortcutKeyDisplay 将配置里的 key token 转成 footer/help 使用的稳定展示文本。
// 展示文本由同一解析器生成，避免配置能按但提示显示另一套拼写。
func ShortcutKeyDisplay(token string) string {
	key, ok := parseShortcutKeyToken(token)
	if !ok {
		return strings.TrimSpace(token)
	}
	base := shortcutKeyBaseDisplay(key)
	if key.Ctrl && key.Key == KeyChar && base != "" {
		base = "^" + strings.ToUpper(base)
	} else {
		prefixes := []string{}
		if key.Ctrl {
			prefixes = append(prefixes, "Ctrl")
		}
		if key.Alt {
			prefixes = append(prefixes, "Alt")
		}
		if key.Shift {
			prefixes = append(prefixes, "Shift")
		}
		if len(prefixes) > 0 {
			base = strings.Join(append(prefixes, base), "+")
		}
	}
	if base == "" {
		return strings.TrimSpace(token)
	}
	return base
}

// ShortcutKeyRequiresEnhancedKeyboard 判断某个 binding 是否依赖传统 TTY 无法区分的修饰键编码。
// capability availability 由宿主 TerminalHost 探测；该函数只复用 canonical key parser 判定键位语义。
func ShortcutKeyRequiresEnhancedKeyboard(token string) bool {
	key, ok := parseShortcutKeyToken(token)
	if !ok || key.Key != KeyChar || !key.Ctrl {
		return false
	}
	if _, ok := ctrlCharBytes(key.Char); ok {
		return false
	}
	return true
}

func shortcutKeyBaseDisplay(key shortcutKey) string {
	switch key.Key {
	case KeyChar:
		if key.Char == " " {
			return "space"
		}
		return key.Char
	case KeyPageUp:
		return "PgUp"
	case KeyPageDn:
		return "PgDn"
	case KeyUp:
		return "↑"
	case KeyDown:
		return "↓"
	case KeyLeft:
		return "←"
	case KeyRight:
		return "→"
	case KeyHome:
		return "home"
	case KeyEnd:
		return "end"
	case KeyDelete:
		return "delete"
	case KeyInsert:
		return "insert"
	case KeyBackspace:
		return "backspace"
	case KeyTab:
		return "tab"
	case KeyShiftTab:
		return "shift-tab"
	case KeyEsc:
		return "Esc"
	case KeyEnter:
		return "enter"
	default:
		return string(key.Key)
	}
}

func shortcutSceneMode(sceneName string) (InteractionMode, bool) {
	switch sceneName {
	case "global":
		return InteractionModeNormal, true
	case "system":
		return InteractionModeGlobal, true
	case "panel", "pane":
		return InteractionModePane, true
	case "resize":
		return InteractionModeResize, true
	case "floating":
		return InteractionModeFloating, true
	case "tab":
		return InteractionModeTab, true
	case "workspace":
		return InteractionModeWorkspace, true
	case "copy":
		return InteractionModeCopy, true
	default:
		return InteractionModeNormal, false
	}
}

// ShortcutBindingSignature 返回 scene/key 在配置层输入模型里的唯一签名。
// routed scene 按 InteractionMode 合并 panel/pane 等别名，overlay scene 按 canonical scene 隔离；
// 它只归一协议无关的 token 别名；实际宿主 capability 与 binding available 状态由 TerminalHost 阶段决定。
func ShortcutBindingSignature(sceneName string, keyToken string) (string, bool) {
	key, ok := parseShortcutKeyToken(keyToken)
	if !ok {
		return "", false
	}
	key.Char = canonicalShortcutChar(key.Key, key.Char, key.Ctrl, key.Shift)
	sceneKey := shortcutSceneCatalogKey(sceneName)
	if mode, routed := shortcutSceneMode(sceneName); routed {
		sceneKey = "route:" + string(mode)
	} else {
		sceneKey = "overlay:" + sceneKey
	}
	parts := []string{
		sceneKey,
		string(key.Key),
		strconv.Quote(key.Char),
		strconv.FormatBool(key.Ctrl),
		strconv.FormatBool(key.Alt),
		strconv.FormatBool(key.Shift),
	}
	return strings.Join(parts, "\x00"), true
}

// ShortcutKeyIsGlobalEscape 判断配置 key 是否占用了 TUI 保留的全局返回键。
// 未修饰的 esc/escape 由 app back-navigation 统一处理，不进入用户 shortcut catalog。
func ShortcutKeyIsGlobalEscape(keyToken string) bool {
	key, ok := parseShortcutKeyToken(keyToken)
	return ok && key.Key == KeyEsc && !key.Ctrl && !key.Alt && !key.Shift
}

// KnownShortcutActionID 判断 action id 是否属于 shortcut domain registry。
// 配置加载期用它拒绝拼写错误；运行时 input 只生成 canonical invocation。
func KnownShortcutActionID(actionID string) bool {
	_, _, err := shortcut.ParseInvocation(actionID)
	return err == nil
}

// RoutableShortcutActionID 判断 action 是否能由主 input route 直接产出 reducer intent。
// overlay 专用 action 只能出现在 overlay scene，不能绑定到 global/panel 等可路由 scene 后静默吞键。
func RoutableShortcutActionID(actionID string) bool {
	_, spec, err := shortcut.ParseInvocation(actionID)
	return err == nil && spec.Routable
}

func routeKey(event InputEvent, options RouteOptions) Intent {
	if options.ForceTerminalPassthrough {
		if data := terminalBytes(event); len(data) > 0 {
			return Intent{Kind: IntentTerminalInput, Event: event, Bytes: data}
		}
		return Intent{Kind: IntentNone, Event: event, Reason: "forced passthrough without bytes"}
	}
	catalog := shortcutCatalogForConfig(options.Shortcuts)
	if options.Mode != InteractionModeNormal {
		if binding, ok := lookupBinding(options.Mode, event, catalog); ok {
			return intentFromBinding(event, binding)
		}
		// 中文说明：sticky 场景下 root/global 菜单键仍可用，但这个顺序来自同一个 catalog；
		// 用户删掉 global shortcut 后不会回落到旧硬编码 route。
		if binding, ok := lookupBinding(InteractionModeNormal, event, catalog); ok {
			return intentFromBinding(event, binding)
		}
		return Intent{Kind: IntentNone, Event: event}
	}
	if options.CopyModeActive {
		if binding, ok := lookupBinding(InteractionModeCopy, event, catalog); ok {
			return intentFromBinding(event, binding)
		}
	}
	if binding, ok := lookupBinding(InteractionModeNormal, event, catalog); ok {
		return intentFromBinding(event, binding)
	}
	if options.CopyModeActive {
		return Intent{Kind: IntentNone, Event: event}
	}
	if data := terminalBytes(event); len(data) > 0 {
		return Intent{Kind: IntentTerminalInput, Event: event, Bytes: data}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func routeMouse(event InputEvent, options RouteOptions) Intent {
	// 中文说明：copy/history 尚未接管时，前台 terminal 的 mouse tracking 优先于滚轮进历史；
	// 一旦 copy/history 已经激活，滚轮仍属于 TermX history。
	if !options.CopyModeActive && options.TerminalMousePassthrough && event.RawSeq != "" {
		return Intent{Kind: IntentTerminalInput, Event: event, Bytes: []byte(event.RawSeq), RawMouse: true}
	}
	switch event.Mouse {
	case MouseWheelUp:
		if options.CopyModeActive {
			return Intent{Kind: IntentRequestOlder, Event: event}
		}
		return Intent{Kind: IntentEnterCopyMode, Event: event}
	case MouseWheelDown:
		if options.CopyModeActive {
			return Intent{Kind: IntentRequestNewer, Event: event}
		}
	case MouseLeft, MouseLeftDrag:
		if options.CopyModeActive {
			return Intent{Kind: IntentMouseSelect, Event: event}
		}
	}
	return Intent{Kind: IntentNone, Event: event}
}

func lookupBinding(mode InteractionMode, event InputEvent, catalog shortcutCatalog) (Binding, bool) {
	for _, binding := range catalog.bindings {
		if binding.Mode == mode && bindingMatches(binding, event) {
			return binding, true
		}
	}
	return Binding{}, false
}

func bindingMatches(binding Binding, event InputEvent) bool {
	if binding.Key != event.Key {
		return false
	}
	if !bindingCharMatches(binding, event) {
		return false
	}
	return binding.Ctrl == event.Ctrl && binding.Alt == event.Alt && binding.Shift == event.Shift
}

func bindingCharMatches(binding Binding, event InputEvent) bool {
	if binding.Char == event.Char {
		return true
	}
	if binding.Key != KeyChar || !binding.Ctrl || !event.Ctrl {
		return false
	}
	if canonicalShortcutChar(binding.Key, binding.Char, binding.Ctrl, binding.Shift) == canonicalShortcutChar(event.Key, event.Char, event.Ctrl, event.Shift) {
		return true
	}
	data, ok := ctrlCharBytes(binding.Char)
	return ok && string(data) == event.Char
}

func canonicalShortcutChar(key Key, char string, ctrl bool, shift bool) string {
	if key != KeyChar || !ctrl || shift {
		return char
	}
	if char == " " || char == "@" || char == "\x00" {
		return "\x00"
	}
	if len(char) == 1 && char[0] >= 'A' && char[0] <= 'Z' {
		// Ctrl-A 与 Ctrl-a 是配置拼写别名；显式 Shift 必须写成 ctrl-shift-a。
		return strings.ToLower(char)
	}
	return char
}

func intentFromBinding(event InputEvent, binding Binding) Intent {
	return Intent{
		Kind:       IntentShortcutAction,
		Event:      event,
		Invocation: binding.Invocation,
	}
}

// LockableRootShortcutIntent 返回会被 shortcut lock 透传的 root 快捷键。
// global/system mode 入口保留为控制面逃生键；开启 lock 后仍可用 Ctrl-G, l 解除锁定。
func LockableRootShortcutIntent(event InputEvent) (Intent, bool) {
	return LockableRootShortcutIntentWithShortcuts(event, state.TUIShortcutConfig{})
}

// LockableRootShortcutIntentWithShortcuts 使用当前 shortcuts 判断会被 shortcut lock 透传的 root 快捷键。
func LockableRootShortcutIntentWithShortcuts(event InputEvent, shortcuts state.TUIShortcutConfig) (Intent, bool) {
	intent, ok := rootShortcutIntent(event, shortcuts)
	if !ok {
		return Intent{}, false
	}
	if intent.Invocation.ID == "menu.system" {
		return Intent{}, false
	}
	return intent, true
}

// StickyModeEntryShortcutIntent 判断当前按键是否是某个 sticky mode 的入口键。
// UI reducer 用它实现双击前缀透传，例如 Ctrl-W Ctrl-W 将第二个 Ctrl-W 发给 terminal。
func StickyModeEntryShortcutIntent(event InputEvent, mode InteractionMode) (Intent, bool) {
	return StickyModeEntryShortcutIntentWithShortcuts(event, mode, state.TUIShortcutConfig{})
}

// StickyModeEntryShortcutIntentWithShortcuts 使用当前 shortcuts 判断 sticky mode 入口键。
func StickyModeEntryShortcutIntentWithShortcuts(event InputEvent, mode InteractionMode, shortcuts state.TUIShortcutConfig) (Intent, bool) {
	if mode == InteractionModeNormal {
		return Intent{}, false
	}
	intent, ok := rootShortcutIntent(event, shortcuts)
	if !ok || shortcutInvocationMode(intent.Invocation) != mode {
		return Intent{}, false
	}
	return intent, true
}

// CopyModeEntryShortcutIntent 判断当前按键是否是 copy/history 的 root 入口键。
// copy mode 本身不是 sticky interaction mode，但第二次按入口键也应显式透传给 terminal。
func CopyModeEntryShortcutIntent(event InputEvent) (Intent, bool) {
	return CopyModeEntryShortcutIntentWithShortcuts(event, state.TUIShortcutConfig{})
}

// CopyModeEntryShortcutIntentWithShortcuts 使用当前 shortcuts 判断 copy/history 入口键。
func CopyModeEntryShortcutIntentWithShortcuts(event InputEvent, shortcuts state.TUIShortcutConfig) (Intent, bool) {
	intent, ok := rootShortcutIntent(event, shortcuts)
	if !ok || (intent.Invocation.ID != "copy.enter" && intent.Invocation.ID != "menu.copy") {
		return Intent{}, false
	}
	if event.Key != KeyChar || !event.Ctrl {
		return Intent{}, false
	}
	return intent, true
}

func rootShortcutIntent(event InputEvent, shortcuts state.TUIShortcutConfig) (Intent, bool) {
	catalog := shortcutCatalogForConfig(shortcuts)
	binding, ok := lookupBinding(InteractionModeNormal, event, catalog)
	if !ok {
		return Intent{}, false
	}
	return intentFromBinding(event, binding), true
}

func shortcutInvocationMode(invocation shortcut.ActionInvocation) InteractionMode {
	switch invocation.ID {
	case "menu.panel":
		return InteractionModePane
	case "menu.resize":
		return InteractionModeResize
	case "menu.system":
		return InteractionModeGlobal
	case "menu.floating":
		return InteractionModeFloating
	case "menu.tab":
		return InteractionModeTab
	case "menu.workspace":
		return InteractionModeWorkspace
	default:
		return InteractionModeNormal
	}
}

func terminalBytes(event InputEvent) []byte {
	if event.RawSeq != "" && event.KeyboardProtocol == "" {
		return []byte(event.RawSeq)
	}
	var data []byte
	switch event.Key {
	case KeyEsc:
		data = []byte{'\x1b'}
	case KeyEnter:
		data = []byte{'\r'}
	case KeyBackspace:
		data = []byte{0x7f}
	case KeyTab:
		data = []byte{'\t'}
	case KeyChar:
		if event.Char != "" {
			if event.Ctrl {
				if data, ok := ctrlCharBytes(event.Char); ok {
					if event.Alt {
						return append([]byte{'\x1b'}, data...)
					}
					return data
				}
				return nil
			}
			if event.Alt {
				return append([]byte{'\x1b'}, []byte(event.Char)...)
			}
			return []byte(event.Char)
		}
	}
	if len(data) > 0 {
		if event.Alt {
			return append([]byte{'\x1b'}, data...)
		}
		return data
	}
	return nil
}

func ctrlCharBytes(char string) ([]byte, bool) {
	if len(char) == 0 {
		return nil, false
	}
	if len(char) == 1 && char[0] < 0x20 {
		return []byte{char[0]}, true
	}
	if len(char) != 1 {
		return nil, false
	}
	c := char[0]
	if c >= 'a' && c <= 'z' {
		return []byte{c - 'a' + 1}, true
	}
	if c >= 'A' && c <= 'Z' {
		return []byte{c - 'A' + 1}, true
	}
	switch c {
	case ' ':
		return []byte{0x00}, true
	case '[':
		return []byte{0x1b}, true
	case '\\':
		return []byte{0x1c}, true
	case ']':
		return []byte{0x1d}, true
	case '^':
		return []byte{0x1e}, true
	case '_':
		return []byte{0x1f}, true
	case '?':
		return []byte{0x7f}, true
	}
	return nil, false
}
