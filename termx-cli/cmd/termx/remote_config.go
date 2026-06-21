package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
)

const (
	defaultRemoteLocalWebAddr = "127.0.0.1:18888"
	defaultRemoteLocalICEAddr = "127.0.0.1:18889"
)

type remoteConfigPathContextKey struct{}

func remoteConfigPathValue(path *string) string {
	if path == nil {
		return ""
	}
	return strings.TrimSpace(*path)
}

func resolveRemoteConfigPath(path string) string {
	return resolveConfigFilePath(strings.TrimSpace(path))
}

func remoteContextWithConfigPath(ctx context.Context, configPath *string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	path := remoteConfigPathValue(configPath)
	if path == "" {
		return ctx
	}
	// 中文说明：remote 命令的 --config 只作为请求上下文传给 core-v2
	// daemon auto-start；已运行 daemon 不会被 CLI 侧隐式重配。
	return context.WithValue(ctx, remoteConfigPathContextKey{}, path)
}

func remoteConfigPathFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	path, _ := ctx.Value(remoteConfigPathContextKey{}).(string)
	return strings.TrimSpace(path)
}

func remoteConfigFromEnv() remoteprotocol.Config {
	enabled, enabledSet, _ := envBoolValue("TERMX_REMOTE_ENABLE")
	allowLAN, _, _ := envBoolValue("TERMX_REMOTE_ALLOW_LAN")
	hubURLs := csvList(os.Getenv("TERMX_REMOTE_HUB_URLS"))
	hubURL := strings.TrimSpace(os.Getenv("TERMX_REMOTE_HUB_URL"))
	if hubURL == "" && len(hubURLs) > 0 {
		hubURL = hubURLs[0]
	}
	if hubURL != "" && len(hubURLs) == 0 {
		hubURLs = []string{hubURL}
	}
	cfg := remoteprotocol.Config{
		Enabled:    enabled,
		ControlURL: strings.TrimSpace(os.Getenv("TERMX_REMOTE_CONTROL_URL")),
		HubURL:     hubURL,
		HubURLs:    hubURLs,
		AccessToken: firstNonEmpty(
			strings.TrimSpace(os.Getenv("TERMX_REMOTE_CONNECTION_KEY")),
			strings.TrimSpace(os.Getenv("TERMX_REMOTE_TOKEN")),
			strings.TrimSpace(os.Getenv("TERMX_REMOTE_ACCESS_TOKEN")),
		),
		DataDir:      strings.TrimSpace(os.Getenv("TERMX_REMOTE_DATA_DIR")),
		DeviceName:   strings.TrimSpace(os.Getenv("TERMX_REMOTE_DEVICE_NAME")),
		Region:       strings.TrimSpace(os.Getenv("TERMX_REMOTE_REGION")),
		Mode:         strings.TrimSpace(os.Getenv("TERMX_REMOTE_MODE")),
		LocalWebAddr: remoteLocalWebAddrFromEnv(),
		ICETCPAddr:   remoteLocalICETCPAddrFromEnv(),
		AllowLAN:     allowLAN,
		LANIPs:       splitTrimmed(os.Getenv("TERMX_REMOTE_LAN_IPS")),
	}
	if tokenTTL := durationSecondsFromString(os.Getenv("TERMX_REMOTE_TOKEN_TTL")); tokenTTL > 0 {
		cfg.TokenTTLSeconds = tokenTTL
	}
	if !enabledSet && !cfg.Enabled && remoteConfigHasFields(cfg) {
		cfg.Enabled = true
	}
	return cfg
}

func remoteConfigFromFileAndEnv(path string) (remoteprotocol.Config, error) {
	cfg, fileEnabledSet, err := loadRemoteConfigFromFile(path)
	if err != nil {
		return remoteprotocol.Config{}, err
	}
	envCfg := remoteConfigFromEnv()
	envEnabled, envEnabledSet, _ := envBoolValue("TERMX_REMOTE_ENABLE")
	envAllowLAN, envAllowLANSet, _ := envBoolValue("TERMX_REMOTE_ALLOW_LAN")
	if envEnabledSet {
		cfg.Enabled = envEnabled
	}
	if envCfg.ControlURL != "" {
		cfg.ControlURL = envCfg.ControlURL
	}
	if envCfg.HubURL != "" {
		cfg.HubURL = envCfg.HubURL
		if len(envCfg.HubURLs) > 0 {
			cfg.HubURLs = append([]string(nil), envCfg.HubURLs...)
		} else {
			cfg.HubURLs = []string{envCfg.HubURL}
		}
	}
	if envCfg.AccessToken != "" {
		cfg.AccessToken = envCfg.AccessToken
	}
	if envCfg.DataDir != "" {
		cfg.DataDir = envCfg.DataDir
	}
	if envCfg.DeviceName != "" {
		cfg.DeviceName = envCfg.DeviceName
	}
	if envCfg.Region != "" {
		cfg.Region = envCfg.Region
	}
	if envCfg.Mode != "" {
		cfg.Mode = envCfg.Mode
	}
	if envCfg.LocalWebAddr != "" {
		cfg.LocalWebAddr = envCfg.LocalWebAddr
	}
	if envCfg.ICETCPAddr != "" {
		cfg.ICETCPAddr = envCfg.ICETCPAddr
	}
	if envCfg.TokenTTLSeconds > 0 {
		cfg.TokenTTLSeconds = envCfg.TokenTTLSeconds
	}
	if envAllowLANSet {
		cfg.AllowLAN = envAllowLAN
	}
	if len(envCfg.LANIPs) > 0 {
		cfg.LANIPs = append([]string(nil), envCfg.LANIPs...)
	}
	if !envEnabledSet && !fileEnabledSet && !cfg.Enabled && remoteConfigHasFields(cfg) {
		cfg.Enabled = true
	}
	return cfg, nil
}

func remoteConfigFromFile(path string) (remoteprotocol.Config, error) {
	cfg, _, err := loadRemoteConfigFromFile(path)
	return cfg, err
}

func loadRemoteConfigFromFile(path string) (remoteprotocol.Config, bool, error) {
	path = resolveRemoteConfigPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return remoteprotocol.Config{}, false, nil
		}
		return remoteprotocol.Config{}, false, err
	}
	values, err := parseRemoteConfigSection(string(data))
	if err != nil {
		return remoteprotocol.Config{}, false, fmt.Errorf("remote config %q: %w", path, err)
	}
	enabled, enabledSet, validEnabled := configBoolValue(values["enabled"])
	if enabledSet && !validEnabled {
		return remoteprotocol.Config{}, false, fmt.Errorf("remote enabled has invalid boolean value %q", values["enabled"])
	}
	cfg := remoteprotocol.Config{
		Enabled:    enabled,
		ControlURL: remoteConfigValue(values, "controlURL", "control_url"),
		HubURL:     remoteConfigValue(values, "hubURL", "hub_url"),
		HubURLs:    splitTrimmed(remoteConfigValue(values, "hubURLs", "hub_urls")),
		DataDir:    remoteConfigValue(values, "dataDir", "data_dir"),
		DeviceName: remoteConfigValue(values, "deviceName", "device_name"),
		Region:     remoteConfigValue(values, "region"),
		Mode:       remoteConfigValue(values, "mode"),
		LocalWebAddr: remoteConfigValue(
			values,
			"localWebAddr",
			"local_web_addr",
			"localWebAddress",
			"local_web_address",
			"local_web",
		),
		ICETCPAddr: remoteConfigValue(values, "iceTCPAddr", "ice_tcp_addr", "iceTCPAddress", "ice_tcp_address"),
	}
	if tokenTTL := durationSecondsFromString(remoteConfigValue(values, "token_ttl", "tokenTTL")); tokenTTL > 0 {
		cfg.TokenTTLSeconds = tokenTTL
	}
	if rawAllowLAN := remoteConfigValue(values, "allow_lan", "allowLAN"); rawAllowLAN != "" {
		cfg.AllowLAN, _ = strconv.ParseBool(rawAllowLAN)
	}
	cfg.LANIPs = splitTrimmed(remoteConfigValue(values, "lan_ips", "lanIPs"))
	if cfg.HubURL == "" && len(cfg.HubURLs) > 0 {
		cfg.HubURL = cfg.HubURLs[0]
	}
	if cfg.HubURL != "" && len(cfg.HubURLs) == 0 {
		cfg.HubURLs = []string{cfg.HubURL}
	}
	if accessToken := remoteConfigValue(values, "access_token", "token"); accessToken != "" {
		cfg.AccessToken = accessToken
	}
	if authStore := remoteConfigValue(values, "authStore", "auth_store"); authStore != "" {
		record, err := loadRemoteAuthRecord(authStore)
		if err != nil {
			return remoteprotocol.Config{}, false, fmt.Errorf("load remote auth store: %w", err)
		}
		if cfg.ControlURL == "" {
			cfg.ControlURL = record.ControlURL
		}
		if cfg.HubURL == "" && cfg.ControlURL == "" {
			cfg.HubURL = record.HubURL
			if cfg.HubURL != "" {
				cfg.HubURLs = []string{cfg.HubURL}
			}
		}
		cfg.AccessToken = record.AccessToken
	}
	if keyEnv := firstNonEmpty(
		remoteConfigValue(values, "connectionKeyEnv", "connection_key_env"),
		remoteConfigValue(values, "accessTokenEnv", "access_token_env"),
	); keyEnv != "" {
		cfg.AccessToken = strings.TrimSpace(os.Getenv(keyEnv))
	}
	if !enabledSet && !cfg.Enabled && remoteConfigHasFields(cfg) {
		cfg.Enabled = true
	}
	return cfg, enabledSet, nil
}

func remoteConfigValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func remoteConfigHasFields(cfg remoteprotocol.Config) bool {
	return cfg.ControlURL != "" ||
		cfg.HubURL != "" ||
		len(cfg.HubURLs) > 0 ||
		cfg.AccessToken != "" ||
		cfg.DataDir != "" ||
		cfg.DeviceName != "" ||
		cfg.Region != "" ||
		cfg.Mode != "" ||
		cfg.LocalWebAddr != "" ||
		cfg.ICETCPAddr != "" ||
		cfg.TokenTTLSeconds > 0 ||
		cfg.AllowLAN ||
		len(cfg.LANIPs) > 0
}

func durationSecondsFromString(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds > 0 {
			return seconds
		}
		return 0
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return 0
	}
	return int(duration.Seconds())
}

func parseRemoteConfigSection(content string) (map[string]string, error) {
	values := make(map[string]string)
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
		if section != "remote" || strings.HasPrefix(line, "-") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: invalid remote mapping", lineNo+1)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		if key == "accessToken" {
			continue
		}
		values[key] = value
	}
	return values, nil
}

func splitTrimmed(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "[]")
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		field = strings.Trim(field, `"'`)
		if field == "" {
			continue
		}
		out = append(out, field)
	}
	return out
}

func parseConfigBool(value string) bool {
	parsed, _, _ := configBoolValue(value)
	return parsed
}

func configBoolValue(value string) (bool, bool, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return false, false, true
	}
	switch strings.ToLower(raw) {
	case "1", "t", "true", "yes", "y", "on":
		return true, true, true
	case "0", "f", "false", "no", "n", "off":
		return false, true, true
	default:
		return false, true, false
	}
}

func remoteLocalWebAddrFromEnv() string {
	if addr := strings.TrimSpace(os.Getenv("TERMX_REMOTE_LOCAL_WEB_ADDR")); addr != "" {
		return addr
	}
	if envBool("TERMX_REMOTE_LOCAL_WEB_ENABLE") {
		return defaultRemoteLocalWebAddr
	}
	return ""
}

func remoteLocalICETCPAddrFromEnv() string {
	if addr := strings.TrimSpace(os.Getenv("TERMX_REMOTE_LOCAL_ICE_TCP_ADDR")); addr != "" {
		return addr
	}
	if envBool("TERMX_REMOTE_LOCAL_ICE_TCP_ENABLE") {
		return defaultRemoteLocalICEAddr
	}
	return ""
}

func daemonLocalWebAddr(cfg remoteprotocol.Config) string {
	if addr := remoteLocalWebAddrFromEnv(); addr != "" {
		return addr
	}
	if !cfg.Enabled || !modeIncludesLocal(cfg.Mode) {
		return ""
	}
	if addr := strings.TrimSpace(cfg.LocalWebAddr); addr != "" {
		return addr
	}
	return defaultRemoteLocalWebAddr
}

func daemonLocalICETCPAddr(cfg remoteprotocol.Config) string {
	if addr := remoteLocalICETCPAddrFromEnv(); addr != "" {
		return addr
	}
	if !cfg.Enabled || !modeIncludesLocal(cfg.Mode) {
		return ""
	}
	if addr := strings.TrimSpace(cfg.ICETCPAddr); addr != "" {
		return addr
	}
	return defaultRemoteLocalICEAddr
}

func modeIncludesLocal(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "local", "both":
		return true
	default:
		return false
	}
}

func envBool(key string) bool {
	value, _, _ := envBoolValue(key)
	return value
}

func envBoolValue(key string) (bool, bool, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, false, true
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, true, true
	case "0", "false", "no", "off":
		return false, true, true
	default:
		return false, true, false
	}
}
