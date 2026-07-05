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
			TabTemplate:       "",
		},
		Footer: state.TUIFooterConfig{
			Templates: state.TUIFooterTemplatesConfig{
				ModeBadge: "{{mode_icon}} {{mode_label}}",
				Action:    "{{key}} {{icon}} {{label}}",
				Separator: " │ ",
				KeylockOn: "󰌾 KEYLOCK",
			},
		},
		Interaction: state.TUIInteractionConfig{
			Mouse:                         true,
			StickyPrefixTimeoutMS:         3000,
			ShortcutPassthroughIntervalMS: 1000,
			ConfirmDestructive:            true,
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
			},
			CopyMode: state.TUICopyKeymapConfig{
				Entry:            "ctrl-v",
				ClipboardHistory: "h",
				PasteLatest:      "p",
				PasteSystem:      "shift-p",
			},
			TabMode: state.TUITabKeymapConfig{
				Entry:    "ctrl-t",
				Create:   "c",
				Close:    "x",
				Rename:   "r",
				Next:     "n",
				Previous: "p",
			},
			WorkspaceMode: state.TUIWorkspaceKeymapConfig{
				Entry:     "ctrl-w",
				Navigator: "w",
				Create:    "c",
				Delete:    "x",
				Rename:    "r",
			},
			FloatingMode: state.TUIModeEntryKeymapConfig{Entry: "ctrl-o"},
			PaneMode:     state.TUIModeEntryKeymapConfig{Entry: "ctrl-p"},
			ResizeMode:   state.TUIModeEntryKeymapConfig{Entry: "ctrl-r"},
			GlobalMode:   state.TUIModeEntryKeymapConfig{Entry: "ctrl-g"},
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
			parsedValue, err := parseScalar(value)
			if err != nil {
				return state.TUIConfigStore{}, fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			handled, err := setFooterDynamicScalar(&cfg, path, parsedValue)
			if err != nil {
				return state.TUIConfigStore{}, fmt.Errorf("line %d: %s: %w", lineNo+1, path, err)
			}
			if !handled {
				return state.TUIConfigStore{}, fmt.Errorf("line %d: unknown field %q", lineNo+1, path)
			}
			continue
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
		"tui.footer",
		"tui.footer.templates",
		"tui.footer.modes",
		"tui.footer.actions",
		"tui.interaction",
		"tui.interaction.clipboard_history",
		"tui.interaction.picker",
		"tui.keymap",
		"tui.keymap.root",
		"tui.keymap.copy_mode",
		"tui.keymap.tab_mode",
		"tui.keymap.workspace_mode",
		"tui.keymap.floating_mode",
		"tui.keymap.pane_mode",
		"tui.keymap.resize_mode",
		"tui.keymap.global_mode":
		return true
	default:
		return knownFooterDynamicSection(path)
	}
}

func knownFooterDynamicSection(path string) bool {
	for _, prefix := range []string{"tui.footer.modes.", "tui.footer.actions."} {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		if rest == "" || strings.Contains(rest, ".") {
			return false
		}
		return validFooterConfigName(rest)
	}
	return false
}

type scalarSetter func(*state.TUIConfigStore, string) error

var scalarSetters = map[string]scalarSetter{
	"version":                                          setInt(func(cfg *state.TUIConfigStore, value int) { cfg.Version = value }),
	"tui.profile":                                      setString(func(cfg *state.TUIConfigStore, value string) { cfg.Profile = value }),
	"tui.theme.mode":                                   setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Mode = value }),
	"tui.theme.palette":                                setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Palette = value }),
	"tui.theme.primary":                                setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Primary = value }),
	"tui.theme.secondary":                              setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Secondary = value }),
	"tui.theme.foreground":                             setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Foreground = value }),
	"tui.theme.background":                             setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Background = value }),
	"tui.theme.muted":                                  setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Muted = value }),
	"tui.theme.success":                                setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Success = value }),
	"tui.theme.warning":                                setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Warning = value }),
	"tui.theme.danger":                                 setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Danger = value }),
	"tui.theme.info":                                   setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Info = value }),
	"tui.theme.border.panel":                           setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Border.Panel = value }),
	"tui.theme.border.active":                          setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Border.Active = value }),
	"tui.theme.border.inactive":                        setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Border.Inactive = value }),
	"tui.theme.border.muted":                           setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Border.Muted = value }),
	"tui.theme.surface.chrome_bg":                      setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Surface.ChromeBG = value }),
	"tui.theme.surface.status_bg":                      setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Surface.StatusBG = value }),
	"tui.theme.surface.overlay_bg":                     setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Surface.OverlayBG = value }),
	"tui.theme.surface.toast_bg":                       setString(func(cfg *state.TUIConfigStore, value string) { cfg.Theme.Surface.ToastBG = value }),
	"tui.chrome.header":                                setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Chrome.Header = value }),
	"tui.chrome.footer":                                setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Chrome.Footer = value }),
	"tui.chrome.panel_presentation":                    setString(func(cfg *state.TUIConfigStore, value string) { cfg.Chrome.PanelPresentation = value }),
	"tui.chrome.tab_create_icon":                       setString(func(cfg *state.TUIConfigStore, value string) { cfg.Chrome.TabCreateIcon = value }),
	"tui.chrome.tab_template":                          setString(func(cfg *state.TUIConfigStore, value string) { cfg.Chrome.TabTemplate = value }),
	"tui.footer.templates.mode_badge":                  setString(func(cfg *state.TUIConfigStore, value string) { cfg.Footer.Templates.ModeBadge = value }),
	"tui.footer.templates.action":                      setString(func(cfg *state.TUIConfigStore, value string) { cfg.Footer.Templates.Action = value }),
	"tui.footer.templates.separator":                   setString(func(cfg *state.TUIConfigStore, value string) { cfg.Footer.Templates.Separator = value }),
	"tui.footer.templates.keylock_on":                  setString(func(cfg *state.TUIConfigStore, value string) { cfg.Footer.Templates.KeylockOn = value }),
	"tui.footer.templates.keylock_off":                 setString(func(cfg *state.TUIConfigStore, value string) { cfg.Footer.Templates.KeylockOff = value }),
	"tui.interaction.mouse":                            setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Interaction.Mouse = value }),
	"tui.interaction.sticky_prefix_timeout_ms":         setInt(func(cfg *state.TUIConfigStore, value int) { cfg.Interaction.StickyPrefixTimeoutMS = value }),
	"tui.interaction.shortcut_passthrough_interval_ms": setInt(func(cfg *state.TUIConfigStore, value int) { cfg.Interaction.ShortcutPassthroughIntervalMS = value }),
	"tui.interaction.confirm_destructive":              setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Interaction.ConfirmDestructive = value }),
	"tui.interaction.clipboard_history.max_items":      setInt(func(cfg *state.TUIConfigStore, value int) { cfg.Interaction.ClipboardHistory.MaxItems = value }),
	"tui.interaction.clipboard_history.name_width": setInt(func(cfg *state.TUIConfigStore, value int) {
		cfg.Interaction.ClipboardHistory.NameWidth = value
	}),
	"tui.interaction.clipboard_history.preview_width_ratio": setFloat(func(cfg *state.TUIConfigStore, value float64) {
		cfg.Interaction.ClipboardHistory.PreviewWidthRatio = value
	}),
	"tui.interaction.picker.fuzzy_match":       setString(func(cfg *state.TUIConfigStore, value string) { cfg.Interaction.Picker.FuzzyMatch = value }),
	"tui.interaction.picker.highlight_matches": setBool(func(cfg *state.TUIConfigStore, value bool) { cfg.Interaction.Picker.HighlightMatches = value }),
	"tui.keymap.root.terminal_picker":          setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.Root.TerminalPicker = value }),
	"tui.keymap.copy_mode.entry":               setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.CopyMode.Entry = value }),
	"tui.keymap.copy_mode.clipboard_history":   setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.CopyMode.ClipboardHistory = value }),
	"tui.keymap.copy_mode.paste_latest":        setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.CopyMode.PasteLatest = value }),
	"tui.keymap.copy_mode.paste_system":        setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.CopyMode.PasteSystem = value }),
	"tui.keymap.tab_mode.entry":                setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.TabMode.Entry = value }),
	"tui.keymap.tab_mode.create":               setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.TabMode.Create = value }),
	"tui.keymap.tab_mode.close":                setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.TabMode.Close = value }),
	"tui.keymap.tab_mode.rename":               setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.TabMode.Rename = value }),
	"tui.keymap.tab_mode.next":                 setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.TabMode.Next = value }),
	"tui.keymap.tab_mode.previous":             setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.TabMode.Previous = value }),
	"tui.keymap.workspace_mode.entry":          setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.WorkspaceMode.Entry = value }),
	"tui.keymap.workspace_mode.navigator":      setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.WorkspaceMode.Navigator = value }),
	"tui.keymap.workspace_mode.create":         setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.WorkspaceMode.Create = value }),
	"tui.keymap.workspace_mode.delete":         setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.WorkspaceMode.Delete = value }),
	"tui.keymap.workspace_mode.rename":         setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.WorkspaceMode.Rename = value }),
	"tui.keymap.floating_mode.entry":           setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.FloatingMode.Entry = value }),
	"tui.keymap.pane_mode.entry":               setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.PaneMode.Entry = value }),
	"tui.keymap.resize_mode.entry":             setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.ResizeMode.Entry = value }),
	"tui.keymap.global_mode.entry":             setString(func(cfg *state.TUIConfigStore, value string) { cfg.Keymap.GlobalMode.Entry = value }),
}

func setFooterDynamicScalar(cfg *state.TUIConfigStore, path string, value string) (bool, error) {
	if strings.HasPrefix(path, "tui.footer.modes.") {
		rest := strings.TrimPrefix(path, "tui.footer.modes.")
		name, field, ok := strings.Cut(rest, ".")
		if !ok || !validFooterConfigName(name) {
			return false, nil
		}
		mode := cfg.Footer.Modes[name]
		switch field {
		case "icon":
			mode.Icon = value
		case "label":
			mode.Label = value
		case "style":
			mode.Style = value
		case "actions":
			mode.Actions = value
		default:
			return false, nil
		}
		if cfg.Footer.Modes == nil {
			cfg.Footer.Modes = map[string]state.TUIFooterModeConfig{}
		}
		cfg.Footer.Modes[name] = mode
		return true, nil
	}
	if strings.HasPrefix(path, "tui.footer.actions.") {
		rest := strings.TrimPrefix(path, "tui.footer.actions.")
		name, field, ok := strings.Cut(rest, ".")
		if !ok || !validFooterConfigName(name) {
			return false, nil
		}
		action := cfg.Footer.Actions[name]
		switch field {
		case "id":
			action.ID = value
		case "key":
			action.Key = value
		case "label":
			action.Label = value
		case "icon":
			action.Icon = value
		case "style":
			action.Style = value
		default:
			return false, nil
		}
		if cfg.Footer.Actions == nil {
			cfg.Footer.Actions = map[string]state.TUIFooterActionConfig{}
		}
		cfg.Footer.Actions[name] = action
		return true, nil
	}
	return false, nil
}

func validFooterConfigName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
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
	"TERMX_TUI_THEME_MODE":                       "tui.theme.mode",
	"TERMX_TUI_THEME_PALETTE":                    "tui.theme.palette",
	"TERMX_TUI_THEME_PRIMARY":                    "tui.theme.primary",
	"TERMX_TUI_THEME_SECONDARY":                  "tui.theme.secondary",
	"TERMX_TUI_THEME_FOREGROUND":                 "tui.theme.foreground",
	"TERMX_TUI_THEME_BACKGROUND":                 "tui.theme.background",
	"TERMX_TUI_THEME_MUTED":                      "tui.theme.muted",
	"TERMX_TUI_THEME_SUCCESS":                    "tui.theme.success",
	"TERMX_TUI_THEME_WARNING":                    "tui.theme.warning",
	"TERMX_TUI_THEME_DANGER":                     "tui.theme.danger",
	"TERMX_TUI_THEME_INFO":                       "tui.theme.info",
	"TERMX_TUI_CHROME_HEADER":                    "tui.chrome.header",
	"TERMX_TUI_CHROME_FOOTER":                    "tui.chrome.footer",
	"TERMX_TUI_CHROME_PANEL_PRESENTATION":        "tui.chrome.panel_presentation",
	"TERMX_TUI_INTERACTION_MOUSE":                "tui.interaction.mouse",
	"TERMX_TUI_STICKY_PREFIX_TIMEOUT_MS":         "tui.interaction.sticky_prefix_timeout_ms",
	"TERMX_TUI_SHORTCUT_PASSTHROUGH_INTERVAL_MS": "tui.interaction.shortcut_passthrough_interval_ms",
	"TERMX_TUI_CONFIRM_DESTRUCTIVE":              "tui.interaction.confirm_destructive",
	"TERMX_TUI_CLIPBOARD_HISTORY_MAX_ITEMS":      "tui.interaction.clipboard_history.max_items",
	"TERMX_TUI_CLIPBOARD_HISTORY_NAME_WIDTH":     "tui.interaction.clipboard_history.name_width",
	"TERMX_TUI_CLIPBOARD_HISTORY_PREVIEW_RATIO":  "tui.interaction.clipboard_history.preview_width_ratio",
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
	if strings.ContainsAny(cfg.Chrome.TabTemplate, "\r\n") {
		return fmt.Errorf("tui.chrome.tab_template must be a single-line template")
	}
	if err := validateFooterConfig(cfg.Footer); err != nil {
		return err
	}
	if cfg.Interaction.StickyPrefixTimeoutMS < 0 {
		return fmt.Errorf("tui.interaction.sticky_prefix_timeout_ms must be >= 0")
	}
	if cfg.Interaction.ShortcutPassthroughIntervalMS <= 0 {
		return fmt.Errorf("tui.interaction.shortcut_passthrough_interval_ms must be > 0")
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

func validateFooterConfig(footer state.TUIFooterConfig) error {
	for _, item := range []struct {
		path  string
		value string
	}{
		{"tui.footer.templates.mode_badge", footer.Templates.ModeBadge},
		{"tui.footer.templates.action", footer.Templates.Action},
		{"tui.footer.templates.separator", footer.Templates.Separator},
		{"tui.footer.templates.keylock_on", footer.Templates.KeylockOn},
		{"tui.footer.templates.keylock_off", footer.Templates.KeylockOff},
	} {
		if err := validateSingleLine(item.path, item.value); err != nil {
			return err
		}
	}
	for name, mode := range footer.Modes {
		if !validFooterConfigName(name) {
			return fmt.Errorf("tui.footer.modes.%s has invalid name", name)
		}
		for _, item := range []struct {
			path  string
			value string
		}{
			{"icon", mode.Icon},
			{"label", mode.Label},
			{"style", mode.Style},
			{"actions", mode.Actions},
		} {
			if err := validateSingleLine("tui.footer.modes."+name+"."+item.path, item.value); err != nil {
				return err
			}
		}
		if !validFooterStyleToken(mode.Style) {
			return fmt.Errorf("tui.footer.modes.%s.style has unknown style token %q", name, mode.Style)
		}
		for _, ref := range footerConfigRefs(mode.Actions) {
			if !validFooterActionRef(ref) {
				return fmt.Errorf("tui.footer.modes.%s.actions has invalid action ref %q", name, ref)
			}
		}
	}
	for name, action := range footer.Actions {
		if !validFooterConfigName(name) {
			return fmt.Errorf("tui.footer.actions.%s has invalid name", name)
		}
		for _, item := range []struct {
			path  string
			value string
		}{
			{"id", action.ID},
			{"key", action.Key},
			{"label", action.Label},
			{"icon", action.Icon},
			{"style", action.Style},
		} {
			if err := validateSingleLine("tui.footer.actions."+name+"."+item.path, item.value); err != nil {
				return err
			}
		}
		if action.ID != "" && !validFooterActionRef(action.ID) {
			return fmt.Errorf("tui.footer.actions.%s.id has invalid action id %q", name, action.ID)
		}
		if !validFooterStyleToken(action.Style) {
			return fmt.Errorf("tui.footer.actions.%s.style has unknown style token %q", name, action.Style)
		}
	}
	return nil
}

func validateSingleLine(path string, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be a single-line value", path)
	}
	return nil
}

func footerConfigRefs(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func validFooterActionRef(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func validFooterStyleToken(value string) bool {
	return oneOf(strings.TrimSpace(value),
		"",
		"accent",
		"foreground",
		"strong-foreground",
		"muted",
		"status",
		"status-accent",
		"status-muted",
		"status-warning",
		"footer-chrome",
		"footer-muted",
		"footer-accent",
		"footer-key-pane",
		"footer-key-resize",
		"footer-key-tab",
		"footer-key-workspace",
		"footer-key-float",
		"footer-key-copy",
		"footer-key-picker",
		"footer-key-global",
		"info",
		"success",
		"warning",
		"danger",
		"danger-strong",
	)
}

func validateKeymap(keymap state.TUIKeymapConfig) error {
	// entry 键虽然配置在各 mode 下，但输入归属是 root；冲突必须按 root 输入态统一判断。
	if err := validateKeymapMode("root", []keymapBinding{
		{Path: "tui.keymap.root.terminal_picker", Key: keymap.Root.TerminalPicker},
		{Path: "tui.keymap.copy_mode.entry", Key: keymap.CopyMode.Entry},
		{Path: "tui.keymap.tab_mode.entry", Key: keymap.TabMode.Entry},
		{Path: "tui.keymap.workspace_mode.entry", Key: keymap.WorkspaceMode.Entry},
		{Path: "tui.keymap.floating_mode.entry", Key: keymap.FloatingMode.Entry},
		{Path: "tui.keymap.pane_mode.entry", Key: keymap.PaneMode.Entry},
		{Path: "tui.keymap.resize_mode.entry", Key: keymap.ResizeMode.Entry},
		{Path: "tui.keymap.global_mode.entry", Key: keymap.GlobalMode.Entry},
	}); err != nil {
		return err
	}
	for _, mode := range []struct {
		name     string
		bindings []keymapBinding
	}{
		{name: "copy_mode", bindings: []keymapBinding{
			{Path: "tui.keymap.copy_mode.clipboard_history", Key: keymap.CopyMode.ClipboardHistory},
			{Path: "tui.keymap.copy_mode.paste_latest", Key: keymap.CopyMode.PasteLatest},
			{Path: "tui.keymap.copy_mode.paste_system", Key: keymap.CopyMode.PasteSystem},
		}},
		{name: "tab_mode", bindings: []keymapBinding{
			{Path: "tui.keymap.tab_mode.create", Key: keymap.TabMode.Create},
			{Path: "tui.keymap.tab_mode.close", Key: keymap.TabMode.Close},
			{Path: "tui.keymap.tab_mode.rename", Key: keymap.TabMode.Rename},
			{Path: "tui.keymap.tab_mode.next", Key: keymap.TabMode.Next},
			{Path: "tui.keymap.tab_mode.previous", Key: keymap.TabMode.Previous},
		}},
		{name: "workspace_mode", bindings: []keymapBinding{
			{Path: "tui.keymap.workspace_mode.navigator", Key: keymap.WorkspaceMode.Navigator},
			{Path: "tui.keymap.workspace_mode.create", Key: keymap.WorkspaceMode.Create},
			{Path: "tui.keymap.workspace_mode.delete", Key: keymap.WorkspaceMode.Delete},
			{Path: "tui.keymap.workspace_mode.rename", Key: keymap.WorkspaceMode.Rename},
		}},
	} {
		if err := validateKeymapMode(mode.name, mode.bindings); err != nil {
			return err
		}
	}
	return nil
}

type keymapBinding struct {
	Path string
	Key  string
}

func validateKeymapMode(mode string, bindings []keymapBinding) error {
	seen := map[string]string{}
	for _, binding := range bindings {
		key := strings.ToLower(strings.TrimSpace(binding.Key))
		if key == "" {
			return fmt.Errorf("%s must not be empty", binding.Path)
		}
		if previous := seen[key]; previous != "" {
			return fmt.Errorf("tui.keymap.%s has duplicate key %q for %s and %s", mode, key, previous, binding.Path)
		}
		seen[key] = binding.Path
	}
	return nil
}
