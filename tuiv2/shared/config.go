package shared

type Config struct {
	Workspace          string
	SessionID          string
	ViewID             string
	AttachID           string
	SocketPath         string
	LogFilePath        string
	WorkspaceStatePath string
	ConfigPath         string
	Chrome             ChromeConfig
	Theme              ThemeConfig
	Auth               AuthConfig
}

type ChromeConfig struct {
	PaneTop     []string
	StatusLeft  []string
	StatusRight []string
	TabLeft     []string
}

type ThemeConfig struct {
	Accent        string
	Success       string
	Warning       string
	Danger        string
	Info          string
	PanelBorder   string
	PanelBorder2  string
	TabActiveBG   string
	TabActiveFG   string
	TabInactiveBG string
	TabInactiveFG string
	TabCreateBG   string
	TabCreateFG   string
}

type AuthConfig struct {
	ServerURL    string
	AccessToken  string
	RefreshToken string
	UserID       string
	Username     string
}
