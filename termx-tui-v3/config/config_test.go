package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestParseExampleConfigMatchesDefaults(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "tui-v3.example.yaml"))
	if err != nil {
		t.Fatalf("read example config: %v", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("parse example config: %v", err)
	}
	want := Default()
	if cfg.Theme.Primary != "" || cfg.Theme.Secondary != "" {
		t.Fatalf("example config should keep primary/secondary host-aware by default, got %#v", cfg.Theme)
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("example config should parse to defaults\n got=%#v\nwant=%#v", cfg, want)
	}
}

func TestParseDocumentedConfigExample(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "config.example.yaml"))
	if err != nil {
		t.Fatalf("read documented example config: %v", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("parse documented example config: %v", err)
	}
	if !strings.Contains(cfg.Chrome.WorkspaceTemplate, "{{workspace | truncate 18}}") ||
		!strings.Contains(cfg.Chrome.TabTemplate, "{{if active}}") ||
		!strings.Contains(cfg.Chrome.TabTemplate, "header-active-edge") ||
		cfg.Chrome.TabCreateIcon == "" {
		t.Fatalf("documented example should carry text/template tab format and create icon, got %#v", cfg.Chrome)
	}
}

func TestParseConfigOverridesScalars(t *testing.T) {
	cfg, err := Parse([]byte(`
version: 1
tui:
  profile: work
  theme:
    palette: builtin
    primary: "#d65cff"
    secondary: "#66e3ff"
    border:
      active: "#ff00aa"
  chrome:
    header: false
    panel_presentation: card
    tab_create_icon: "+"
    tab_create_template: "[fg:#040a0d;bg:#8ffcff] {{create_icon}} [/]"
    workspace_template: "[style:header-workspace] {{workspace | truncate 8}} [/style]"
    tab_template: "{{tab_id}} {{if active}}▎{{else}}|{{end}} {{title | truncate 8}} [action:tab.close]{{close_icon}}[/action]"
    pane_title_template: "{{terminal}}@{{endpoint}}"
    pane_glyphs:
      action_left: ""
      action_right: " "
      action_separator: ""
      action_group_left: "[fg:#8ffcff]"
      action_group_right: "[reset]"
      owner_left: " "
      owner_right: " "
      owner: ""
      owner_pending: "owner?"
      take_owner: "follow"
      zoom: "󰁌"
      unzoom: "󰁄"
      split_vertical: "□"
      split_horizontal: "▭"
      close: "⤫"
      size_lock: "🔒"
      size_unlock: "○"
      running: "●"
      waiting: "○"
      exited: "×"
      overflow_left: "‹"
      overflow_right: "›"
      overflow_top: "˄"
      overflow_bottom: "˅"
      overflow_style: "#c7c7c7"
      extent_placeholder: "·"
      extent_placeholder_style: "#a8a8a8"
  footer:
    templates:
      mode_badge: "{{mode_icon}} {{mode_label}}"
      key: ""
      action: "{{key}} {{icon}} {{label}}"
      separator: " · "
      workspace_summary: "󰙅 {{workspace}}"
      floating_summary: "󰹙 {{count}}"
      floating_collapsed_summary: "󰃄 {{count}}"
      terminals_summary: " {{count}}"
      keylock_on: "󰌾 KEYLOCK"
    modes:
      live:
        icon: "󰆍"
        label: "TERM"
        style: footer-accent
        actions: "pane,copy,global"
    actions:
      pane:
        id: footer.mode-pane
        key: "^P"
        icon: ""
        label: "pane"
        style: footer-key-pane
  interaction:
    sticky_prefix_timeout_ms: 5000
    shortcut_passthrough_interval_ms: 750
    clipboard_history:
      max_items: 500
      preview_width_ratio: 0.72
  keymap:
    root:
      terminal_picker: ctrl-space
    copy_mode:
      entry: ctrl-y
      clipboard_history: y
    tab_mode:
      entry: alt-t
`))
	if err != nil {
		t.Fatalf("parse override config: %v", err)
	}
	if cfg.Profile != "work" || cfg.Theme.Palette != "builtin" || cfg.Theme.Primary != "#d65cff" || cfg.Theme.Secondary != "#66e3ff" {
		t.Fatalf("theme/profile overrides not applied: %#v", cfg)
	}
	if cfg.Theme.Border.Active != "#ff00aa" || cfg.Chrome.Header || cfg.Chrome.PanelPresentation != "card" || cfg.Chrome.TabCreateIcon != "+" || !strings.Contains(cfg.Chrome.TabCreateTemplate, "{{create_icon}}") || !strings.Contains(cfg.Chrome.WorkspaceTemplate, "{{workspace | truncate 8}}") || !strings.Contains(cfg.Chrome.TabTemplate, "{{tab_id}}") || cfg.Chrome.PaneTitleTemplate != "{{terminal}}@{{endpoint}}" {
		t.Fatalf("chrome/border overrides not applied: %#v", cfg)
	}
	if cfg.Chrome.PaneGlyphs.Zoom != "󰁌" ||
		cfg.Chrome.PaneGlyphs.Unzoom != "󰁄" ||
		!cfg.Chrome.PaneGlyphs.ActionLeftSet ||
		cfg.Chrome.PaneGlyphs.ActionLeft != "" ||
		!cfg.Chrome.PaneGlyphs.ActionRightSet ||
		cfg.Chrome.PaneGlyphs.ActionRight != " " ||
		!cfg.Chrome.PaneGlyphs.ActionSeparatorSet ||
		cfg.Chrome.PaneGlyphs.ActionSeparator != "" ||
		cfg.Chrome.PaneGlyphs.ActionGroupLeft != "[fg:#8ffcff]" ||
		cfg.Chrome.PaneGlyphs.ActionGroupRight != "[reset]" ||
		cfg.Chrome.PaneGlyphs.OwnerLeft != " " ||
		cfg.Chrome.PaneGlyphs.OwnerRight != " " ||
		!cfg.Chrome.PaneGlyphs.OwnerSet ||
		cfg.Chrome.PaneGlyphs.Owner != "" ||
		!cfg.Chrome.PaneGlyphs.OwnerPendingSet ||
		cfg.Chrome.PaneGlyphs.OwnerPending != "owner?" ||
		!cfg.Chrome.PaneGlyphs.TakeOwnerSet ||
		cfg.Chrome.PaneGlyphs.TakeOwner != "follow" ||
		cfg.Chrome.PaneGlyphs.SplitVertical != "□" ||
		cfg.Chrome.PaneGlyphs.SplitHorizontal != "▭" ||
		cfg.Chrome.PaneGlyphs.Close != "⤫" ||
		cfg.Chrome.PaneGlyphs.SizeLock != "🔒" ||
		cfg.Chrome.PaneGlyphs.Running != "●" ||
		!cfg.Chrome.PaneGlyphs.OverflowLeftSet ||
		cfg.Chrome.PaneGlyphs.OverflowLeft != "‹" ||
		!cfg.Chrome.PaneGlyphs.OverflowRightSet ||
		cfg.Chrome.PaneGlyphs.OverflowRight != "›" ||
		!cfg.Chrome.PaneGlyphs.OverflowTopSet ||
		cfg.Chrome.PaneGlyphs.OverflowTop != "˄" ||
		!cfg.Chrome.PaneGlyphs.OverflowBottomSet ||
		cfg.Chrome.PaneGlyphs.OverflowBottom != "˅" ||
		!cfg.Chrome.PaneGlyphs.OverflowStyleSet ||
		cfg.Chrome.PaneGlyphs.OverflowStyle != "#c7c7c7" ||
		!cfg.Chrome.PaneGlyphs.ExtentPlaceholderSet ||
		cfg.Chrome.PaneGlyphs.ExtentPlaceholder != "·" ||
		!cfg.Chrome.PaneGlyphs.ExtentPlaceholderStyleSet ||
		cfg.Chrome.PaneGlyphs.ExtentPlaceholderStyle != "#a8a8a8" {
		t.Fatalf("pane chrome glyph overrides not applied: %#v", cfg.Chrome.PaneGlyphs)
	}
	if cfg.Footer.Templates.Separator != " · " ||
		cfg.Footer.Templates.Key != "" ||
		cfg.Footer.Templates.WorkspaceSummary != "󰙅 {{workspace}}" ||
		cfg.Footer.Templates.FloatingSummary != "󰹙 {{count}}" ||
		cfg.Footer.Templates.TerminalsSummary != " {{count}}" ||
		cfg.Footer.Modes["live"].Icon != "󰆍" ||
		cfg.Footer.Modes["live"].Actions != "pane,copy,global" ||
		cfg.Footer.Actions["pane"].ID != "footer.mode-pane" ||
		cfg.Footer.Actions["pane"].Icon != "" {
		t.Fatalf("footer overrides not applied: %#v", cfg.Footer)
	}
	if cfg.Interaction.StickyPrefixTimeoutMS != 5000 ||
		cfg.Interaction.ShortcutPassthroughIntervalMS != 750 ||
		cfg.Interaction.ClipboardHistory.MaxItems != 500 ||
		cfg.Interaction.ClipboardHistory.PreviewWidthRatio != 0.72 {
		t.Fatalf("interaction overrides not applied: %#v", cfg.Interaction)
	}
	if cfg.Keymap.Root.TerminalPicker != "ctrl-space" ||
		cfg.Keymap.CopyMode.Entry != "ctrl-y" ||
		cfg.Keymap.CopyMode.ClipboardHistory != "y" ||
		cfg.Keymap.TabMode.Entry != "alt-t" ||
		cfg.Keymap.TabMode.Create != "c" {
		t.Fatalf("keymap override/default merge wrong: %#v", cfg.Keymap)
	}
}

func TestParseRejectsUnknownFieldAndBadValues(t *testing.T) {
	_, err := Parse([]byte("tui:\n  theme:\n    primarry: \"#ffffff\"\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}

	_, err = Parse([]byte("version: 2\n"))
	if err == nil || !strings.Contains(err.Error(), "version must be 1") {
		t.Fatalf("expected version validation error, got %v", err)
	}

	_, err = Parse([]byte("tui:\n  theme:\n    primary: red\n"))
	if err == nil || !strings.Contains(err.Error(), "must be empty or #RRGGBB") {
		t.Fatalf("expected color validation error, got %v", err)
	}

	_, err = Parse([]byte("tui:\n  interaction:\n    shortcut_passthrough_interval_ms: 0\n"))
	if err == nil || !strings.Contains(err.Error(), "shortcut_passthrough_interval_ms must be > 0") {
		t.Fatalf("expected passthrough interval validation error, got %v", err)
	}

	_, err = Parse([]byte("tui:\n  footer:\n    actions:\n      pane:\n        style: nope\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown style token") {
		t.Fatalf("expected footer style validation error, got %v", err)
	}

	_, err = Parse([]byte("tui:\n  footer:\n    modes:\n      live:\n        actions: \"pane,$bad\"\n"))
	if err == nil || !strings.Contains(err.Error(), "invalid action ref") {
		t.Fatalf("expected footer action ref validation error, got %v", err)
	}
}

func TestLoadUsesMissingDefaultPathButFailsExplicitMissingPath(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatalf("missing default config should not fail: %v", err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("missing default config should return defaults, got %#v", cfg)
	}

	_, err = Load(filepath.Join(configHome, "missing.yaml"), nil)
	if err == nil {
		t.Fatalf("explicit missing config path should fail")
	}
}

func TestLoadAppliesEnvOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui-v3.yaml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	env := map[string]string{
		"TERMX_TUI_THEME_PRIMARY":                    "#010203",
		"TERMX_TUI_THEME_PALETTE":                    "builtin",
		"TERMX_TUI_CHROME_HEADER":                    "false",
		"TERMX_TUI_CHROME_TAB_CREATE_TEMPLATE":       "{{create_icon}}",
		"TERMX_TUI_CHROME_PANE_TITLE_TEMPLATE":       "{{terminal}}@{{endpoint}}",
		"TERMX_TUI_SHORTCUT_PASSTHROUGH_INTERVAL_MS": "650",
		"TERMX_TUI_CLIPBOARD_HISTORY_PREVIEW_RATIO":  "0.75",
	}
	cfg, err := Load(path, func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("load with env overrides: %v", err)
	}
	if cfg.Theme.Primary != "#010203" ||
		cfg.Theme.Palette != "builtin" ||
		cfg.Chrome.Header ||
		cfg.Chrome.TabCreateTemplate != "{{create_icon}}" ||
		cfg.Chrome.PaneTitleTemplate != "{{terminal}}@{{endpoint}}" ||
		cfg.Interaction.ShortcutPassthroughIntervalMS != 650 ||
		cfg.Interaction.ClipboardHistory.PreviewWidthRatio != 0.75 {
		t.Fatalf("env overrides not applied: %#v", cfg)
	}
}

func TestValidateRejectsMultiLineTabCreateTemplate(t *testing.T) {
	cfg := Default()
	cfg.Chrome.TabCreateTemplate = "{{create_icon}}\n+"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "tab_create_template") {
		t.Fatalf("expected multiline tab create template validation error, got %v", err)
	}
}

func TestValidateRejectsMultiLinePaneTitleTemplate(t *testing.T) {
	cfg := Default()
	cfg.Chrome.PaneTitleTemplate = "{{terminal}}\n{{endpoint}}"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "pane_title_template") {
		t.Fatalf("expected pane_title_template validation error, got %v", err)
	}
}

func TestValidateRejectsMultiLineWorkspaceTemplate(t *testing.T) {
	cfg := Default()
	cfg.Chrome.WorkspaceTemplate = "{{workspace}}\n{{title}}"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "workspace_template") {
		t.Fatalf("expected workspace_template validation error, got %v", err)
	}
}

func TestValidateRejectsDuplicateKeymapWithinMode(t *testing.T) {
	cfg := Default()
	cfg.Keymap.TabMode.Close = cfg.Keymap.TabMode.Create
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate keymap error, got %v", err)
	}
}

func TestValidateRejectsDuplicateKeymapEntriesInRootInput(t *testing.T) {
	cfg := Default()
	cfg.Keymap.CopyMode.Entry = cfg.Keymap.Root.TerminalPicker
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "tui.keymap.root has duplicate key") {
		t.Fatalf("expected duplicate root entry keymap error, got %v", err)
	}
}

func TestDefaultPathUsesXDGConfigHome(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	if got, want := DefaultPath(), filepath.Join(configHome, "termx", DefaultFileName); got != want {
		t.Fatalf("default path mismatch got=%q want=%q", got, want)
	}
}

var _ state.TUIConfigStore
