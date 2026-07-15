package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/lozzow/termx/proto/wire"
	"github.com/lozzow/termx/shared/connection"
	tuiconfig "github.com/lozzow/termx/tui/config"
)

func resolveV3Socket(path string) string {
	if path != "" {
		return path
	}
	name := fmt.Sprintf("termx-v2-wire%d.sock", wire.Version)
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir + "/" + name
	}
	return fmt.Sprintf("%s/termx-v2-wire%d-%d.sock", os.TempDir(), wire.Version, os.Getuid())
}

func resolveV3SocketAuto(path string) string {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(path) == "auto" {
		return resolveV3Socket("")
	}
	return resolveV3Socket(path)
}

func loadV3ConnectionRegistry() (connection.Registry, error) {
	return connection.Load("")
}

func resolveV3SocketForConnectionRegistry(path string, registry connection.Registry) (string, error) {
	if strings.TrimSpace(path) != "" {
		return resolveV3Socket(path), nil
	}
	normalized, err := registry.Normalize()
	if err != nil {
		return "", fmt.Errorf("normalize endpoint registry for local socket: %w", err)
	}
	registry = normalized
	endpoint, ok := registry.Endpoints[registry.Default]
	if !ok || !endpoint.Enabled {
		return "", fmt.Errorf("default endpoint %q is unavailable", registry.Default)
	}
	route, err := endpoint.ResolveCurrentRoute("")
	if err != nil {
		return "", fmt.Errorf("resolve default endpoint %q route: %w", endpoint.ID, err)
	}
	if route.Kind != connection.RouteLocalUnix {
		return "", fmt.Errorf("default endpoint %q route %q is %q, not %q", endpoint.ID, route.ID, route.Kind, connection.RouteLocalUnix)
	}
	socket := strings.TrimSpace(route.Socket)
	if socket != "" && socket != "auto" {
		return resolveV3Socket(socket), nil
	}
	return resolveV3Socket(""), nil
}

func resolveV3LogFilePath(path string) string {
	return resolveLogFilePath(path)
}

func v3ConfigPathPolicy() string {
	return tuiconfig.DefaultPath()
}

func v3StatePathPolicy() string {
	return "unused"
}

func resolveV3HistoryStorageDir() string {
	return resolveStateFilePath("history-v2")
}
