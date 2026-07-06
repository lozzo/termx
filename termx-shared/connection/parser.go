package connection

import (
	"fmt"
	"strconv"
	"strings"
)

func parseRegistry(data []byte) (Registry, error) {
	if strings.TrimSpace(string(data)) == "" {
		return DefaultRegistry(), nil
	}
	registry := Registry{Version: 1, Connections: map[EndpointID]Config{}}
	stack := map[int]string{}
	for lineNo, raw := range strings.Split(string(data), "\n") {
		raw = strings.TrimRight(raw, "\r")
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent%2 != 0 {
			return Registry{}, fmt.Errorf("line %d: indentation must use two-space levels", lineNo+1)
		}
		level := indent / 2
		line = stripInlineComment(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") {
			return Registry{}, fmt.Errorf("line %d: list values are not supported in connections.yaml", lineNo+1)
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Registry{}, fmt.Errorf("line %d: expected key: value", lineNo+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return Registry{}, fmt.Errorf("line %d: empty key", lineNo+1)
		}
		if value == "" {
			if err := pushSection(stack, level, key); err != nil {
				return Registry{}, fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			continue
		}
		parsedValue, err := parseScalar(value)
		if err != nil {
			return Registry{}, fmt.Errorf("line %d: %w", lineNo+1, err)
		}
		if err := setScalar(&registry, stack, level, key, parsedValue); err != nil {
			return Registry{}, fmt.Errorf("line %d: %w", lineNo+1, err)
		}
	}
	return registry.Normalize()
}

func pushSection(stack map[int]string, level int, key string) error {
	switch level {
	case 0:
		if key != "connections" {
			return fmt.Errorf("unknown section %q", key)
		}
	case 1:
		if stack[0] != "connections" {
			return fmt.Errorf("unknown section %q", joinPath(stack, level, key))
		}
		if err := validateEndpointID(EndpointID(key)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown section %q", joinPath(stack, level, key))
	}
	stack[level] = key
	for existing := range stack {
		if existing > level {
			delete(stack, existing)
		}
	}
	return nil
}

func setScalar(registry *Registry, stack map[int]string, level int, key string, value string) error {
	switch level {
	case 0:
		switch key {
		case "version":
			version, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("version must be an integer")
			}
			registry.Version = version
			return nil
		case "default":
			registry.Default = EndpointID(value)
			return nil
		default:
			return fmt.Errorf("unknown field %q", key)
		}
	case 2:
		if stack[0] != "connections" || stack[1] == "" {
			return fmt.Errorf("unknown field %q", joinPath(stack, level, key))
		}
		id := EndpointID(stack[1])
		cfg := registry.Connections[id]
		if cfg.ID == "" {
			cfg = Config{ID: id, Enabled: true}
		}
		if err := setConnectionScalar(&cfg, key, value); err != nil {
			return err
		}
		registry.Connections[id] = cfg
		return nil
	default:
		return fmt.Errorf("unknown field %q", joinPath(stack, level, key))
	}
}

func setConnectionScalar(cfg *Config, key string, value string) error {
	switch key {
	case "label":
		cfg.Label = value
	case "enabled":
		enabled, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("enabled: %w", err)
		}
		cfg.Enabled = enabled
	case "transport":
		cfg.Transport = TransportKind(value)
	case "connect_mode":
		cfg.ConnectMode = ConnectMode(value)
	case "address":
		cfg.Address = value
	case "auth_ref":
		cfg.AuthRef = value
	case "socket":
		cfg.Socket = value
	case "remote_socket":
		cfg.RemoteSocket = value
	default:
		return fmt.Errorf("unknown field %q", key)
	}
	return nil
}

func parseBool(value string) (bool, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("must be true or false")
	}
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
	return value, nil
}
