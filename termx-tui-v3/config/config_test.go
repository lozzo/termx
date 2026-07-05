package config

import (
	"os"
	"path/filepath"
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
	if cfg != want {
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
	if !strings.Contains(cfg.Chrome.TabTemplate, "{{if active}}") ||
		!strings.Contains(cfg.Chrome.TabTemplate, "font:bold") ||
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
    tab_template: "{{tab_id}} {{if active}}▎{{else}}|{{end}} {{title | truncate 8}} [action:tab.close]{{close_icon}}[/action]"
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
	if cfg.Theme.Border.Active != "#ff00aa" || cfg.Chrome.Header || cfg.Chrome.PanelPresentation != "card" || cfg.Chrome.TabCreateIcon != "+" || !strings.Contains(cfg.Chrome.TabTemplate, "{{tab_id}}") {
		t.Fatalf("chrome/border overrides not applied: %#v", cfg)
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
}

func TestLoadUsesMissingDefaultPathButFailsExplicitMissingPath(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatalf("missing default config should not fail: %v", err)
	}
	if cfg != Default() {
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
		cfg.Interaction.ShortcutPassthroughIntervalMS != 650 ||
		cfg.Interaction.ClipboardHistory.PreviewWidthRatio != 0.75 {
		t.Fatalf("env overrides not applied: %#v", cfg)
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
