package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/connection"
	tuiconfig "github.com/lozzow/termx/termx-tui-v3/config"
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

func loadV3ConnectionRegistry() (connection.Registry, error) {
	return connection.Load("")
}

func normalizeV3ConnectionRegistry(registry connection.Registry) connection.Registry {
	normalized, err := registry.Normalize()
	if err != nil {
		return connection.DefaultRegistry()
	}
	return normalized
}

func resolveV3SocketForConnectionRegistry(path string, registry connection.Registry) string {
	if strings.TrimSpace(path) != "" {
		return resolveV3Socket(path)
	}
	registry = normalizeV3ConnectionRegistry(registry)
	if cfg, ok := registry.Connections[connection.DefaultEndpointID]; ok && cfg.Transport == connection.TransportLocal {
		socket := strings.TrimSpace(cfg.Socket)
		if socket != "" && socket != "auto" {
			return resolveV3Socket(socket)
		}
	}
	return resolveV3Socket("")
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
