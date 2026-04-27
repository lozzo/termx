package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultConfigFileName = "termx.yaml"

// StateDir returns the writable state directory used by termx.
func StateDir() string {
	if stateDir := os.Getenv("XDG_STATE_HOME"); stateDir != "" {
		return filepath.Join(stateDir, "termx")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "state", "termx")
	}
	return filepath.Join(os.TempDir(), "termx-state")
}

// ConfigDir returns the user configuration directory used by termx.
func ConfigDir() string {
	if configDir := os.Getenv("XDG_CONFIG_HOME"); configDir != "" {
		return filepath.Join(configDir, "termx")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "termx")
	}
	return filepath.Join(os.TempDir(), "termx-config")
}

// DefaultConfigPath returns the default termx.yaml path.
func DefaultConfigPath() string {
	return filepath.Join(ConfigDir(), defaultConfigFileName)
}

// DefaultChromeConfig returns the built-in UI chrome configuration.
//
// 说明：这里定义项目默认展示方案；如果用户配置文件不存在，termx 会直接使用这份默认值。
func DefaultChromeConfig() ChromeConfig {
	return ChromeConfig{
		// Pane 顶栏默认展示：标题、状态、共享数、角色、copy 时间/行号、动作按钮。
		PaneTop: []string{
			"pane.title",
			"pane.state",
			"pane.share",
			"pane.role",
			"pane.copy_time",
			"pane.copy_row",
			"pane.actions",
		},
		// Status 左侧默认展示：模式 badge + hint。
		StatusLeft: []string{
			"status.mode",
			"status.hints",
		},
		// Status 右侧默认展示：右侧 token 集合。
		StatusRight: []string{
			"status.tokens",
		},
		// Tab 左侧默认展示：workspace、tabs、create、actions。
		TabLeft: []string{
			"tab.workspace",
			"tab.tabs",
			"tab.create",
			"tab.actions",
		},
	}
}

// DefaultThemeConfig returns the built-in theme override configuration.
//
// 说明：默认 theme 不强行覆盖渲染层的 host-aware 推导逻辑；字段留空时表示沿用 render 内部默认主题算法。
func DefaultThemeConfig() ThemeConfig {
	return ThemeConfig{}
}

// DefaultAuthConfig returns the built-in auth configuration defaults.
func DefaultAuthConfig() AuthConfig {
	return AuthConfig{}
}

// DefaultConfig returns the built-in termx config.
func DefaultConfig() Config {
	return Config{
		Chrome: DefaultChromeConfig(),
		Theme:  DefaultThemeConfig(),
		Auth:   DefaultAuthConfig(),
	}
}

// LoadConfig reads termx.yaml.
//
// 规则：
// 1. 文件不存在：返回默认配置，不报错。
// 2. 文件存在但解析失败：返回错误，避免静默吃掉坏配置。
// 3. 当前解析 chrome / theme / auth 三段；后续扩展其它段时保持同一个 termx.yaml 即可。
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		path = DefaultConfigPath()
	}
	cfg.ConfigPath = path
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}
	parsed, err := parseYAMLConfig(string(data))
	if err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if parsed.Chrome.PaneTop != nil {
		cfg.Chrome.PaneTop = parsed.Chrome.PaneTop
	}
	if parsed.Chrome.StatusLeft != nil {
		cfg.Chrome.StatusLeft = parsed.Chrome.StatusLeft
	}
	if parsed.Chrome.StatusRight != nil {
		cfg.Chrome.StatusRight = parsed.Chrome.StatusRight
	}
	if parsed.Chrome.TabLeft != nil {
		cfg.Chrome.TabLeft = parsed.Chrome.TabLeft
	}
	if parsed.Theme != (ThemeConfig{}) {
		cfg.Theme = parsed.Theme
	}
	if parsed.Auth != (AuthConfig{}) {
		cfg.Auth = parsed.Auth
	}
	return cfg, nil
}

func parseYAMLConfig(content string) (Config, error) {
	var cfg Config
	var section string
	for lineNo, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "-") {
			section = strings.TrimSuffix(line, ":")
			continue
		}
		if strings.HasPrefix(line, "-") {
			return Config{}, fmt.Errorf("line %d: list item without key", lineNo+1)
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return Config{}, fmt.Errorf("line %d: invalid mapping", lineNo+1)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch section {
		case "chrome":
			items, err := parseYAMLInlineList(value)
			if err != nil {
				return Config{}, fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			switch key {
			case "paneTop":
				cfg.Chrome.PaneTop = items
			case "statusLeft":
				cfg.Chrome.StatusLeft = items
			case "statusRight":
				cfg.Chrome.StatusRight = items
			case "tabLeft":
				cfg.Chrome.TabLeft = items
			}
		case "theme":
			value = strings.Trim(value, `"'`)
			switch key {
			case "accent":
				cfg.Theme.Accent = value
			case "success":
				cfg.Theme.Success = value
			case "warning":
				cfg.Theme.Warning = value
			case "danger":
				cfg.Theme.Danger = value
			case "info":
				cfg.Theme.Info = value
			case "panelBorder":
				cfg.Theme.PanelBorder = value
			case "panelBorder2":
				cfg.Theme.PanelBorder2 = value
			case "tabActiveBG":
				cfg.Theme.TabActiveBG = value
			case "tabActiveFG":
				cfg.Theme.TabActiveFG = value
			case "tabInactiveBG":
				cfg.Theme.TabInactiveBG = value
			case "tabInactiveFG":
				cfg.Theme.TabInactiveFG = value
			case "tabCreateBG":
				cfg.Theme.TabCreateBG = value
			case "tabCreateFG":
				cfg.Theme.TabCreateFG = value
			}
		case "auth":
			value = strings.Trim(value, `"'`)
			switch key {
			case "serverURL":
				cfg.Auth.ServerURL = value
			case "accessToken":
				cfg.Auth.AccessToken = value
			case "refreshToken":
				cfg.Auth.RefreshToken = value
			case "userID":
				cfg.Auth.UserID = value
			case "username":
				cfg.Auth.Username = value
			}
		}
	}
	return cfg, nil
}

func parseYAMLInlineList(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "[]" {
		return []string{}, nil
	}
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("expected inline list like [a, b]")
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if inner == "" {
		return []string{}, nil
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		item = strings.Trim(item, `"'`)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// DefaultConfigFileContent returns a commented default termx.yaml.
func DefaultConfigFileContent() string {
	return `# termx 用户配置文件
#
# 说明：
# - 这里只放“用户偏好配置”，例如 UI 展示槽位和主题色覆盖。
# - 删除某个槽位表示隐藏它；调整数组顺序表示重排。
# - theme 段里的字段是可选覆盖项；不填表示继续使用 termx 的默认 host-aware 主题推导。
# - 后续 bindings / plugins 等配置也继续扩在这个文件里。

chrome:
  # Pane 顶栏展示顺序。
  paneTop: [pane.title, pane.state, pane.share, pane.role, pane.copy_time, pane.copy_row, pane.actions]

  # Status 左侧展示顺序。
  statusLeft: [status.mode, status.hints]

  # Status 右侧展示顺序；设为 [] 表示隐藏右侧 token。
  statusRight: [status.tokens]

  # Tab 左侧展示顺序。
  tabLeft: [tab.workspace, tab.tabs, tab.create, tab.actions]

theme:
  # 下面这些字段都可以留空；只有你想显式覆盖默认主题时再填写。
  # accent: "#8b5cf6"
  # success: "#34d399"
  # warning: "#fbbf24"
  # danger: "#f87171"
  # info: "#60a5fa"
  # panelBorder: "#4b5563"
  # panelBorder2: "#6b7280"
  # tabActiveBG: "#1f2937"
  # tabActiveFG: "#f9fafb"
  # tabInactiveBG: "#111827"
  # tabInactiveFG: "#9ca3af"
  # tabCreateBG: "#1f2937"
  # tabCreateFG: "#f9fafb"

auth:
  # 远程控制面地址，例如 https://termx.example.com
  # serverURL: "https://termx.example.com"
  # accessToken: ""
  # refreshToken: ""
  # userID: ""
  # username: ""
`
}

// EnsureDefaultConfigFile creates termx.yaml when it does not exist.
func EnsureDefaultConfigFile(path string) error {
	if path == "" {
		path = DefaultConfigPath()
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(DefaultConfigFileContent()), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// SaveConfig writes the supported termx.yaml subset back to disk.
func SaveConfig(path string, cfg Config) error {
	if path == "" {
		if cfg.ConfigPath != "" {
			path = cfg.ConfigPath
		} else {
			path = DefaultConfigPath()
		}
	}
	cfg.ConfigPath = path
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content, err := renderMergedConfig(path, cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func renderMergedConfig(path string, cfg Config) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return renderConfig(cfg), nil
		}
		return "", err
	}
	return mergeManagedSections(string(content), cfg), nil
}

func renderConfig(cfg Config) string {
	effective := DefaultConfig()
	if cfg.Chrome.PaneTop != nil {
		effective.Chrome.PaneTop = append([]string{}, cfg.Chrome.PaneTop...)
	}
	if cfg.Chrome.StatusLeft != nil {
		effective.Chrome.StatusLeft = append([]string{}, cfg.Chrome.StatusLeft...)
	}
	if cfg.Chrome.StatusRight != nil {
		effective.Chrome.StatusRight = append([]string{}, cfg.Chrome.StatusRight...)
	}
	if cfg.Chrome.TabLeft != nil {
		effective.Chrome.TabLeft = append([]string{}, cfg.Chrome.TabLeft...)
	}
	if cfg.Theme != (ThemeConfig{}) {
		effective.Theme = cfg.Theme
	}
	if cfg.Auth != (AuthConfig{}) {
		effective.Auth = cfg.Auth
	}

	sections := []string{
		renderChromeSection(effective.Chrome),
		renderThemeSection(effective.Theme),
		renderAuthSection(effective.Auth),
	}
	return strings.Join(sections, "\n\n") + "\n"
}

func mergeManagedSections(existing string, cfg Config) string {
	blocks := splitTopLevelBlocks(existing)
	replacements := map[string]string{
		"chrome": renderChromeSection(cfg.Chrome),
		"theme":  renderThemeSection(cfg.Theme),
		"auth":   renderAuthSection(cfg.Auth),
	}
	seen := make(map[string]bool, len(replacements))
	rendered := make([]string, 0, len(blocks)+len(replacements))
	for _, block := range blocks {
		if block.name != "" {
			if replacement, ok := replacements[block.name]; ok {
				rendered = append(rendered, replacement)
				seen[block.name] = true
				continue
			}
		}
		rendered = append(rendered, block.content)
	}
	for _, name := range []string{"chrome", "theme", "auth"} {
		if seen[name] {
			continue
		}
		rendered = append(rendered, replacements[name])
	}
	result := strings.TrimRight(strings.Join(rendered, "\n\n"), "\n")
	if result == "" {
		return renderConfig(cfg)
	}
	return result + "\n"
}

type configBlock struct {
	name    string
	content string
}

func splitTopLevelBlocks(content string) []configBlock {
	lines := strings.Split(content, "\n")
	blocks := make([]configBlock, 0, 8)
	current := make([]string, 0, len(lines))
	currentName := ""
	hasCurrent := false

	flush := func() {
		if !hasCurrent {
			return
		}
		blocks = append(blocks, configBlock{
			name:    currentName,
			content: strings.TrimRight(strings.Join(current, "\n"), "\n"),
		})
		current = nil
		currentName = ""
		hasCurrent = false
	}

	for _, line := range lines {
		if isTopLevelSectionHeader(line) {
			flush()
			currentName = strings.TrimSuffix(strings.TrimSpace(line), ":")
			current = []string{line}
			hasCurrent = true
			continue
		}
		if !hasCurrent {
			current = append(current, line)
			hasCurrent = true
			continue
		}
		current = append(current, line)
	}
	flush()
	return blocks
}

func isTopLevelSectionHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed != "" &&
		trimmed == line &&
		!strings.HasPrefix(trimmed, "#") &&
		strings.HasSuffix(trimmed, ":")
}

func renderChromeSection(cfg ChromeConfig) string {
	effective := DefaultChromeConfig()
	if cfg.PaneTop != nil {
		effective.PaneTop = append([]string{}, cfg.PaneTop...)
	}
	if cfg.StatusLeft != nil {
		effective.StatusLeft = append([]string{}, cfg.StatusLeft...)
	}
	if cfg.StatusRight != nil {
		effective.StatusRight = append([]string{}, cfg.StatusRight...)
	}
	if cfg.TabLeft != nil {
		effective.TabLeft = append([]string{}, cfg.TabLeft...)
	}

	var b strings.Builder
	b.WriteString("chrome:\n")
	fmt.Fprintf(&b, "  paneTop: %s\n", renderInlineList(effective.PaneTop))
	fmt.Fprintf(&b, "  statusLeft: %s\n", renderInlineList(effective.StatusLeft))
	fmt.Fprintf(&b, "  statusRight: %s\n", renderInlineList(effective.StatusRight))
	fmt.Fprintf(&b, "  tabLeft: %s\n", renderInlineList(effective.TabLeft))
	return strings.TrimRight(b.String(), "\n")
}

func renderThemeSection(cfg ThemeConfig) string {
	var b strings.Builder
	b.WriteString("theme:\n")
	writeOptionalString(&b, "accent", cfg.Accent)
	writeOptionalString(&b, "success", cfg.Success)
	writeOptionalString(&b, "warning", cfg.Warning)
	writeOptionalString(&b, "danger", cfg.Danger)
	writeOptionalString(&b, "info", cfg.Info)
	writeOptionalString(&b, "panelBorder", cfg.PanelBorder)
	writeOptionalString(&b, "panelBorder2", cfg.PanelBorder2)
	writeOptionalString(&b, "tabActiveBG", cfg.TabActiveBG)
	writeOptionalString(&b, "tabActiveFG", cfg.TabActiveFG)
	writeOptionalString(&b, "tabInactiveBG", cfg.TabInactiveBG)
	writeOptionalString(&b, "tabInactiveFG", cfg.TabInactiveFG)
	writeOptionalString(&b, "tabCreateBG", cfg.TabCreateBG)
	writeOptionalString(&b, "tabCreateFG", cfg.TabCreateFG)
	return strings.TrimRight(b.String(), "\n")
}

func renderAuthSection(cfg AuthConfig) string {
	var b strings.Builder
	b.WriteString("auth:\n")
	writeOptionalString(&b, "serverURL", cfg.ServerURL)
	writeOptionalString(&b, "accessToken", cfg.AccessToken)
	writeOptionalString(&b, "refreshToken", cfg.RefreshToken)
	writeOptionalString(&b, "userID", cfg.UserID)
	writeOptionalString(&b, "username", cfg.Username)
	return strings.TrimRight(b.String(), "\n")
}

func renderInlineList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	trimmed := make([]string, 0, len(items))
	for _, item := range items {
		trimmed = append(trimmed, strings.TrimSpace(item))
	}
	return "[" + strings.Join(trimmed, ", ") + "]"
}

func writeOptionalString(b *strings.Builder, key, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, "  %s: %s\n", key, strconv.Quote(value))
}
