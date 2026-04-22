package shared

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultConfigPathUsesXDGConfigHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	got := DefaultConfigPath()
	want := filepath.Join(base, "termx", "termx.yaml")
	if got != want {
		t.Fatalf("expected config path %q, got %q", want, got)
	}
}

func TestLoadConfigReturnsDefaultsWhenFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	want := DefaultConfig()
	want.ConfigPath = path
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected default config:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestEnsureDefaultConfigFileCreatesCommentedTermxYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx", "termx.yaml")
	if err := EnsureDefaultConfigFile(path); err != nil {
		t.Fatalf("EnsureDefaultConfigFile returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "# termx 用户配置文件") || !strings.Contains(text, "chrome:") || !strings.Contains(text, "theme:") {
		t.Fatalf("expected commented default config, got:\n%s", text)
	}
}

func TestLoadConfigParsesChromeAndThemeSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.yaml")
	content := `# test config
chrome:
  paneTop: [pane.title, pane.actions]
  statusLeft: [status.hints]
  statusRight: []
  tabLeft: [tab.workspace, tab.tabs]

theme:
  accent: "#8b5cf6"
  panelBorder: "#4b5563"
  tabActiveBG: "#111827"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	want := DefaultConfig()
	want.ConfigPath = path
	want.Chrome = ChromeConfig{
		PaneTop:     []string{"pane.title", "pane.actions"},
		StatusLeft:  []string{"status.hints"},
		StatusRight: []string{},
		TabLeft:     []string{"tab.workspace", "tab.tabs"},
	}
	want.Theme = ThemeConfig{
		Accent:      "#8b5cf6",
		PanelBorder: "#4b5563",
		TabActiveBG: "#111827",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected parsed config:\n got=%#v\nwant=%#v", got, want)
	}
}
