package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

const DefaultFileName = "tui-v3.yaml"

func DefaultPath() string {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "termx", DefaultFileName)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "termx", DefaultFileName)
	}
	return filepath.Join(os.TempDir(), "termx-config", DefaultFileName)
}

func Default() state.TUIConfigStore {
	return state.TUIConfigStore{
		Version: 1,
		Profile: "default",
		Theme: state.TUIThemeConfig{
			Mode:    "dark",
			Palette: "host",
		},
		Chrome: state.TUIChromeConfig{
			Header:            true,
			Footer:            true,
			PanelPresentation: "split-line",
			TabCreateIcon:     "󰐕",
		},
		Interaction: state.TUIInteractionConfig{
			Mouse:                 true,
			StickyPrefixTimeoutMS: 3000,
			ConfirmDestructive:    true,
			ClipboardHistory: state.TUIClipboardHistoryConfig{
				MaxItems:          200,
				NameWidth:         34,
				PreviewWidthRatio: 0.68,
			},
			Picker: state.TUIPickerConfig{
				FuzzyMatch:       "subsequence",
				HighlightMatches: true,
			},
		},
		Keymap: state.TUIKeymapConfig{
			Root: state.TUIRootKeymapConfig{
				TerminalPicker: "ctrl-f",
				CopyMode:       "ctrl-v",
				TabMode:        "ctrl-t",
				WorkspaceMode:  "ctrl-w",
				FloatingMode:   "ctrl-o",
				PaneMode:       "ctrl-p",
				ResizeMode:     "ctrl-r",
				GlobalMode:     "ctrl-g",
			},
			Copy: state.TUICopyKeymapConfig{
				ClipboardHistory: "h",
				PasteLatest:      "p",
				PasteSystem:      "shift-p",
			},
			Tab: state.TUITabKeymapConfig{
				Create:   "c",
				Close:    "x",
				Rename:   "r",
				Next:     "n",
				Previous: "p",
			},
			Workspace: state.TUIWorkspaceKeymapConfig{
				Navigator: "w",
				Create:    "c",
				Delete:    "x",
				Rename:    "r",
			},
		},
	}
}

func Load(path string, lookupEnv func(string) string) (state.TUIConfigStore, error) {
	cfg := Default()
	explicit := strings.TrimSpace(path) != ""
	if !explicit {
		path = DefaultPath()
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) || explicit {
				return state.TUIConfigStore{}, err
			}
		} else {
			parsed, err := Parse(data)
			if err != nil {
				return state.TUIConfigStore{}, fmt.Errorf("parse tui-v3 config %q: %w", path, err)
			}
			cfg = parsed
		}
	}
	if lookupEnv != nil {
		if err := applyEnv(&cfg, lookupEnv); err != nil {
			return state.TUIConfigStore{}, err
		}
	}
	if err := Validate(cfg); err != nil {
		return state.TUIConfigStore{}, err
	}
	return cfg, nil
}

func Parse(data []byte) (state.TUIConfigStore, error) {
	cfg := Default()
	if strings.TrimSpace(string(data)) == "" {
		return cfg, nil
	}
	stack := map[int]string{}
	for lineNo, raw := range strings.Split(string(data), "\n") {
		raw = strings.TrimRight(raw, "\r")
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent%2 != 0 {
			return state.TUIConfigStore{}, fmt.Errorf("line %d: indentation must use two-space levels", lineNo+1)
		}
		level := indent / 2
		line = stripInlineComment(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") {
			return state.TUIConfigStore{}, fmt.Errorf("line %d: list values are not supported in tui-v3 config", lineNo+1)
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return state.TUIConfigStore{}, fmt.Errorf("line %d: expected key: value", lineNo+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return state.TUIConfigStore{}, fmt.Errorf("line %d: empty key", lineNo+1)
		}
		path := joinPath(stack, level, key)
		if value == "" {
			if !knownSection(path) {
				return state.TUIConfigStore{}, fmt.Errorf("line %d: unknown section %q", lineNo+1, path)
			}
			stack[level] = key
			for existing := range stack {
				if existing > level {
					delete(stack, existing)
				}
			}
			continue
		}
		setter, ok := scalarSetters[path]
		if !ok {
			return state.TUIConfigStore{}, fmt.Errorf("line %d: unknown field %q", lineNo+1, path)
		}
		parsedValue, err := parseScalar(value)
		if err != nil {
			return state.TUIConfigStore{}, fmt.Errorf("line %d: %w", lineNo+1, err)
		}
		if err := setter(&cfg, parsedValue); err != nil {
			return state.TUIConfigStore{}, fmt.Errorf("line %d: %s: %w", lineNo+1, path, err)
		}
	}
	if err := Validate(cfg); err != nil {
		return state.TUIConfigStore{}, err
	}
	return cfg, nil
}

func joinPath(stack map[int]string, level int, key string) string {
	parts := make([]string, 0, level+1)
	for i := 0; i < level; i++ {
		if value := stack[i]; value != "" {
			parts = append(parts, value)
		}
	}
	parts = append(parts, key)
	return strings.Join(parts, ".")
}

func stripInlineComment(value string) string {
	var quote rune
	escaped := false
	for index, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		if r == '#' {
			return strings.TrimSpace(value[:index])
		}
	}
	return strings.TrimSpace(value)
}

func parseScalar(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "\"") {
		out, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid quoted string %q", value)
		}
		return out, nil
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), nil
	}
	return strings.TrimSpace(value), nil
}

func knownSection(path string) bool {
	switch path {
	case "tui",
		"tui.theme",
		"tui.theme.border",
		"tui.theme.surface",
		"tui.chrome",
		"tui.interaction",
		"tui.interaction.clipboard_history",
		"tui.interaction.picker",
		"tui.keymap",
		"tui.keymap.root",
		"tui.keymap.copy",
		"tui.keymap.tab",
		"tui.keymap.workspace":
		return true
	default:
		return false
	}
}

type scalarSetter func(*state.TUIConfigStore, string) error

var scalarSetters = map[string]scalarSetter{
	"version":                                     setInt(func(cfg *state.TUIConfigStore, value int) { cfg.Version = value }),
	"tui.profile":                                 setString(func(cfg *state.TUIConfigStore, value string) { cfg.Profile = value }),
	"tui.theme.mode":                              setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Mode = value }),
	"tui.theme.palette":                           setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Palette = value }),
	"tui.theme.primary":                           setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Primary = value }),
	"tui.theme.secondary":                         setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Secondary = value }),
	"tui.theme.foreground":                        setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Foreground = value }),
	"tui.theme.background":                        setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Background = value }),
	"tui.theme.muted":                             setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Muted = value }),
	"tui.theme.success":                           setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Success = value }),
	"tui.theme.warning":                           setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Warning = value }),
	"tui.theme.danger":                            setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Danger = value }),
	"tui.theme.info":                              setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Info = value }),
	"tui.theme.border.panel":                      setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Border.Panel = value }),
	"tui.theme.border.active":                     setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Border.Active = value }),
	"tui.theme.border.inactive":                   setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Border.Inactive = value }),
	"tui.theme.border.muted":                      setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Border.Muted = value }),
	"tui.theme.surface.chrome_bg":                 setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Surface.ChromeBG = value }),
	"tui.theme.surface.status_bg":                 setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Surface.StatusBG = value }),
	"tui.theme.surface.overlay_bg":                setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Surface.OverlayBG = value }),
	"tui.theme.surface.toast_bg":                  setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Surface.ToastBG = value }),
	"tui.chrome.header":                           setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Chrome.Header = value }),
	"tui.chrome.footer":                           setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Chrome.Footer = value }),
	"tui.chrome.panel_presentation":               setString(func(cfg *state.TUIConfigStore, value string) { cfg.Chrome.PanelPresentation = value }),
	"tui.chrome.tab_create_icon":                  setString(func(cfg *state.TUIConfigStore, value string) { cfg.Chrome.TabCreateIcon = value }),
	"tui.interaction.mouse":                       setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Interaction.Mouse = value }),
	"tui.interaction.sticky_prefix_timeout_ms":    setInt(func(cfg *state.TUIConfigStore, value int) { cfg.Interaction.StickyPrefixTimeoutMS = value }),
	"tui.interaction.confirm_destructive":         setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Interaction.ConfirmDestructive = value }),
	"tui.interaction.clipboard_history.max_items": setInt(func(cfg *state.TUIConfigStore, value int) { cfg.Interaction.ClipboardHistory.MaxItems = value }),
	"tui.interaction.clipboard_history.name_width": setInt(func(cfg *state.TUIConfigStore, value int) {
		cfg.Interaction.ClipboardHistory.NameWidth = value
	}),
	"tui.interaction.clipboard_history.preview_width_ratio": setFloat(func(cfg *state.TUIConfigStore, value float64) {
		cfg.Interaction.ClipboardHistory.PreviewWidthRatio = value
	}),
	"tui.interaction.picker.fuzzy_match":       setString(func(cfg *state.TUIConfigStore, value string) { cfg.Interaction.Picker.FuzzyMatch = value }),
	"tui.interaction.picker.highlight_matches": setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Interaction.Picker.HighlightMatches = value }),
	"tui.keymap.root.terminal_picker":          setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Root.TerminalPicker = value }),
	"tui.keymap.root.copy_mode":                setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Root.CopyMode = value }),
	"tui.keymap.root.tab_mode":                 setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Root.TabMode = value }),
	"tui.keymap.root.workspace_mode":           setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Root.WorkspaceMode = value }),
	"tui.keymap.root.floating_mode":            setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Root.FloatingMode = value }),
	"tui.keymap.root.pane_mode":                setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Root.PaneMode = value }),
	"tui.keymap.root.resize_mode":              setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Root.ResizeMode = value }),
	"tui.keymap.root.global_mode":              setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Root.GlobalMode = value }),
	"tui.keymap.copy.clipboard_history":        setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Copy.ClipboardHistory = value }),
	"tui.keymap.copy.paste_latest":             setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Copy.PasteLatest = value }),
	"tui.keymap.copy.paste_system":             setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Copy.PasteSystem = value }),
	"tui.keymap.tab.create":                    setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Tab.Create = value }),
	"tui.keymap.tab.close":                     setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Tab.Close = value }),
	"tui.keymap.tab.rename":                    setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Tab.Rename = value }),
	"tui.keymap.tab.next":                      setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Tab.Next = value }),
	"tui.keymap.tab.previous":                  setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Tab.Previous = value }),
	"tui.keymap.workspace.navigator":           setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Workspace.Navigator = value }),
	"tui.keymap.workspace.create":              setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Workspace.Create = value }),
	"tui.keymap.workspace.delete":              setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Workspace.Delete = value }),
	"tui.keymap.workspace.rename":              setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Workspace.Rename = value }),
}

func setString(assign func(*state.TUIConfigStore, string)) scalarSetter {
	return func(cfg *state.TUIConfigStore, value string) error {
		assign(cfg, value)
		return nil
	}
}

func setBool(assign func(*state.TUIConfigStore, bool)) scalarSetter {
	return func(cfg *state.TUIConfigStore, value string) error {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true":
			assign(cfg, true)
			return nil
		case "false":
			assign(cfg, false)
			return nil
		default:
			return fmt.Errorf("expected boolean true or false, got %q", value)
		}
	}
}

func setInt(assign func(*state.TUIConfigStore, int)) scalarSetter {
	return func(cfg *state.TUIConfigStore, value string) error {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("expected integer, got %q", value)
		}
		assign(cfg, parsed)
		return nil
	}
}

func setFloat(assign func(*state.TUIConfigStore, float64)) scalarSetter {
	return func(cfg *state.TUIConfigStore, value string) error {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return fmt.Errorf("expected number, got %q", value)
		}
		assign(cfg, parsed)
		return nil
	}
}

func applyEnv(cfg *state.TUIConfigStore, lookup func(string) string) error {
	for env, path := range envScalarPaths {
		value := strings.TrimSpace(lookup(env))
		if value == "" {
			continue
		}
		setter, ok := scalarSetters[path]
		if !ok {
			return fmt.Errorf("unknown env binding %s -> %s", env, path)
		}
		if err := setter(cfg, value); err != nil {
			return fmt.Errorf("%s: %w", env, err)
		}
	}
	return nil
}

var envScalarPaths = map[string]string{
	"TERMX_TUI_THEME_MODE":                      "tui.theme.mode",
	"TERMX_TUI_THEME_PALETTE":                   "tui.theme.palette",
	"TERMX_TUI_THEME_PRIMARY":                   "tui.theme.primary",
	"TERMX_TUI_THEME_SECONDARY":                 "tui.theme.secondary",
	"TERMX_TUI_THEME_FOREGROUND":                "tui.theme.foreground",
	"TERMX_TUI_THEME_BACKGROUND":                "tui.theme.background",
	"TERMX_TUI_THEME_MUTED":                     "tui.theme.muted",
	"TERMX_TUI_THEME_SUCCESS":                   "tui.theme.success",
	"TERMX_TUI_THEME_WARNING":                   "tui.theme.warning",
	"TERMX_TUI_THEME_DANGER":                    "tui.theme.danger",
	"TERMX_TUI_THEME_INFO":                      "tui.theme.info",
	"TERMX_TUI_CHROME_HEADER":                   "tui.chrome.header",
	"TERMX_TUI_CHROME_FOOTER":                   "tui.chrome.footer",
	"TERMX_TUI_CHROME_PANEL_PRESENTATION":       "tui.chrome.panel_presentation",
	"TERMX_TUI_INTERACTION_MOUSE":               "tui.interaction.mouse",
	"TERMX_TUI_STICKY_PREFIX_TIMEOUT_MS":        "tui.interaction.sticky_prefix_timeout_ms",
	"TERMX_TUI_CONFIRM_DESTRUCTIVE":             "tui.interaction.confirm_destructive",
	"TERMX_TUI_CLIPBOARD_HISTORY_MAX_ITEMS":     "tui.interaction.clipboard_history.max_items",
	"TERMX_TUI_CLIPBOARD_HISTORY_NAME_WIDTH":    "tui.interaction.clipboard_history.name_width",
	"TERMX_TUI_CLIPBOARD_HISTORY_PREVIEW_RATIO": "tui.interaction.clipboard_history.preview_width_ratio",
}

func Validate(cfg state.TUIConfigStore) error {
	if cfg.Version != 1 {
		return fmt.Errorf("version must be 1, got %d", cfg.Version)
	}
	if strings.TrimSpace(cfg.Profile) == "" {
		return fmt.Errorf("tui.profile must not be empty")
	}
	if !oneOf(cfg.Theme.Mode, "dark", "light", "system") {
		return fmt.Errorf("tui.theme.mode must be dark, light or system, got %q", cfg.Theme.Mode)
	}
	if !oneOf(cfg.Theme.Palette, "host", "builtin") {
		return fmt.Errorf("tui.theme.palette must be host or builtin, got %q", cfg.Theme.Palette)
	}
	for _, item := range []struct {
		name  string
		value string
	}{
		{"tui.theme.primary", cfg.Theme.Primary},
		{"tui.theme.secondary", cfg.Theme.Secondary},
		{"tui.theme.foreground", cfg.Theme.Foreground},
		{"tui.theme.background", cfg.Theme.Background},
		{"tui.theme.muted", cfg.Theme.Muted},
		{"tui.theme.success", cfg.Theme.Success},
		{"tui.theme.warning", cfg.Theme.Warning},
		{"tui.theme.danger", cfg.Theme.Danger},
		{"tui.theme.info", cfg.Theme.Info},
		{"tui.theme.border.panel", cfg.Theme.Border.Panel},
		{"tui.theme.border.active", cfg.Theme.Border.Active},
		{"tui.theme.border.inactive", cfg.Theme.Border.Inactive},
		{"tui.theme.border.muted", cfg.Theme.Border.Muted},
		{"tui.theme.surface.chrome_bg", cfg.Theme.Surface.ChromeBG},
		{"tui.theme.surface.status_bg", cfg.Theme.Surface.StatusBG},
		{"tui.theme.surface.overlay_bg", cfg.Theme.Surface.OverlayBG},
		{"tui.theme.surface.toast_bg", cfg.Theme.Surface.ToastBG},
	} {
		if !validOptionalHexColor(item.value) {
			return fmt.Errorf("%s must be empty or #RRGGBB, got %q", item.name, item.value)
		}
	}
	if !oneOf(cfg.Chrome.PanelPresentation, "split-line", "card") {
		return fmt.Errorf("tui.chrome.panel_presentation must be split-line or card, got %q", cfg.Chrome.PanelPresentation)
	}
	if strings.TrimSpace(cfg.Chrome.TabCreateIcon) == "" {
		return fmt.Errorf("tui.chrome.tab_create_icon must not be empty")
	}
	if cfg.Interaction.StickyPrefixTimeoutMS < 0 {
		return fmt.Errorf("tui.interaction.sticky_prefix_timeout_ms must be >= 0")
	}
	if cfg.Interaction.ClipboardHistory.MaxItems < 0 {
		return fmt.Errorf("tui.interaction.clipboard_history.max_items must be >= 0")
	}
	if cfg.Interaction.ClipboardHistory.NameWidth <= 0 {
		return fmt.Errorf("tui.interaction.clipboard_history.name_width must be > 0")
	}
	if cfg.Interaction.ClipboardHistory.PreviewWidthRatio <= 0 || cfg.Interaction.ClipboardHistory.PreviewWidthRatio >= 1 {
		return fmt.Errorf("tui.interaction.clipboard_history.preview_width_ratio must be > 0 and < 1")
	}
	if !oneOf(cfg.Interaction.Picker.FuzzyMatch, "subsequence") {
		return fmt.Errorf("tui.interaction.picker.fuzzy_match must be subsequence, got %q", cfg.Interaction.Picker.FuzzyMatch)
	}
	if err := validateKeymap(cfg.Keymap); err != nil {
		return err
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func validOptionalHexColor(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, r := range value[1:] {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func validateKeymap(keymap state.TUIKeymapConfig) error {
	modes := map[string]map[string]string{
		"root": {
			"terminal_picker": keymap.Root.TerminalPicker,
			"copy_mode":       keymap.Root.CopyMode,
			"tab_mode":        keymap.Root.TabMode,
			"workspace_mode":  keymap.Root.WorkspaceMode,
			"floating_mode":   keymap.Root.FloatingMode,
			"pane_mode":       keymap.Root.PaneMode,
			"resize_mode":     keymap.Root.ResizeMode,
			"global_mode":     keymap.Root.GlobalMode,
		},
		"copy": {
			"clipboard_history": keymap.Copy.ClipboardHistory,
			"paste_latest":      keymap.Copy.PasteLatest,
			"paste_system":      keymap.Copy.PasteSystem,
		},
		"tab": {
			"create":   keymap.Tab.Create,
			"close":    keymap.Tab.Close,
			"rename":   keymap.Tab.Rename,
			"next":     keymap.Tab.Next,
			"previous": keymap.Tab.Previous,
		},
		"workspace": {
			"navigator": keymap.Workspace.Navigator,
			"create":    keymap.Workspace.Create,
			"delete":    keymap.Workspace.Delete,
			"rename":    keymap.Workspace.Rename,
		},
	}
	for mode, bindings := range modes {
		seen := map[string]string{}
		for action, key := range bindings {
			key = strings.ToLower(strings.TrimSpace(key))
			if key == "" {
				return fmt.Errorf("tui.keymap.%s.%s must not be empty", mode, action)
			}
			if previous := seen[key]; previous != "" {
				return fmt.Errorf("tui.keymap.%s has duplicate key %q for %s and %s", mode, key, previous, action)
			}
			seen[key] = action
		}
	}
	return nil
}
