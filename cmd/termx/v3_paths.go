package main

import (
	"fmt"
	"os"
	"strings"

	endpointdomain "github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/wire"
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

func loadV3ConnectionRegistry() (endpointdomain.Registry, error) {
	return endpointdomain.Load("")
}

func resolveV3SocketForConnectionRegistry(path string, registry endpointdomain.Registry) (string, error) {
	return resolveV3SocketWithClientRuntime(path, registry)
}

func resolveV3SocketWithClientRuntime(path string, registry endpointdomain.Registry) (string, error) {
	if strings.TrimSpace(path) != "" {
		return resolveV3Socket(path), nil
	}
	endpoint, ok := registry.Endpoints[registry.Default]
	if !ok || !endpoint.Enabled {
		return "", fmt.Errorf("default endpoint %q is not available", registry.Default)
	}
	route, ok := endpoint.Route(endpointdomain.DefaultLocalRouteID)
	if !ok || !route.Enabled || route.Kind != endpointdomain.RouteLocalUnix {
		return "", fmt.Errorf("default endpoint %q is not a local unix endpoint", registry.Default)
	}
	if strings.TrimSpace(route.Socket) == "" || route.Socket == "auto" {
		return resolveV3Socket(""), nil
	}
	return route.Socket, nil
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
