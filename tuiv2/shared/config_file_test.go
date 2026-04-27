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
	if !strings.Contains(text, "# termx 用户配置文件") || !strings.Contains(text, "chrome:") || !strings.Contains(text, "theme:") || !strings.Contains(text, "auth:") {
		t.Fatalf("expected commented default config, got:\n%s", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected config file permissions 0600, got %#o", got)
	}
}

func TestLoadConfigParsesChromeThemeAndAuthSections(t *testing.T) {
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

auth:
  serverURL: "https://termx.example"
  accessToken: "access-1"
  refreshToken: "refresh-1"
  userID: "user-1"
  username: "alice"
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
	want.Auth = AuthConfig{
		ServerURL:    "https://termx.example",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		UserID:       "user-1",
		Username:     "alice",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected parsed config:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestSaveConfigRoundTripsChromeThemeAndAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.yaml")
	cfg := DefaultConfig()
	cfg.ConfigPath = path
	cfg.Chrome = ChromeConfig{
		PaneTop:     []string{"pane.title", "pane.actions"},
		StatusLeft:  []string{"status.mode"},
		StatusRight: []string{},
		TabLeft:     []string{"tab.workspace", "tab.tabs"},
	}
	cfg.Theme = ThemeConfig{
		Accent:        "#336699",
		PanelBorder:   "#112233",
		TabInactiveFG: "#eeeeee",
	}
	cfg.Auth = AuthConfig{
		ServerURL:    "https://termx.example",
		AccessToken:  "access-2",
		RefreshToken: "refresh-2",
		UserID:       "user-2",
		Username:     "bob",
	}

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	want := cfg
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected round-tripped config:\n got=%#v\nwant=%#v", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if gotPerm := info.Mode().Perm(); gotPerm != 0o600 {
		t.Fatalf("expected saved config permissions 0600, got %#o", gotPerm)
	}
}

func TestSaveConfigPreservesUnknownSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.yaml")
	content := `# termx custom file
chrome:
  paneTop: [pane.title, pane.actions]

plugins:
  enabled: "demo"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	cfg.Auth = AuthConfig{
		ServerURL:   "https://termx.example",
		AccessToken: "access-1",
	}
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "# termx custom file") {
		t.Fatalf("expected header comment to survive, got:\n%s", text)
	}
	if !strings.Contains(text, "plugins:") || !strings.Contains(text, `enabled: "demo"`) {
		t.Fatalf("expected unknown section to survive, got:\n%s", text)
	}
	if !strings.Contains(text, `serverURL: "https://termx.example"`) {
		t.Fatalf("expected auth section to be written, got:\n%s", text)
	}
}
