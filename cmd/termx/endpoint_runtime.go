package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/shared/connection"
)

var (
	dialCLIEndpointSSH   = dialV3SSHEndpointClient
	dialCLIEndpointCloud = dialV3ManagedEndpointClient
	dialCLIEndpointLocal = dialOrStartV3Client
)

type resolvedTerminalRef struct {
	EndpointID connection.EndpointID
	TerminalID string
}

func (ref resolvedTerminalRef) String() string { return string(ref.EndpointID) + ":" + ref.TerminalID }

func loadNormalizedConnectionRegistry() (connection.Registry, error) {
	registry, err := connection.Load("")
	if err != nil {
		return connection.Registry{}, &cliError{code: 2, message: err.Error(), cause: err}
	}
	registry, err = registry.Normalize()
	if err != nil {
		return connection.Registry{}, &cliError{code: 2, message: err.Error(), cause: err}
	}
	return registry, nil
}

func resolveTerminalRef(target, requestedEndpoint string, registry connection.Registry) (resolvedTerminalRef, error) {
	target = strings.TrimSpace(target)
	requestedEndpoint = strings.TrimSpace(requestedEndpoint)
	if target == "" {
		return resolvedTerminalRef{}, usageCLIError("terminal target cannot be empty")
	}
	endpointID := connection.EndpointID(requestedEndpoint)
	terminalID := target
	if endpoint, id, found := strings.Cut(target, ":"); found {
		if endpoint == "" {
			return resolvedTerminalRef{}, usageCLIError("terminal target must be ENDPOINT_ID:TERMINAL_ID")
		}
		if requestedEndpoint != "" && endpoint != requestedEndpoint {
			return resolvedTerminalRef{}, usageCLIError("TARGET endpoint conflicts with --endpoint")
		}
		endpointID = connection.EndpointID(endpoint)
		terminalID = id
	}
	if endpointID == "" {
		endpointID = registry.Default
	}
	if terminalID == "" || strings.Contains(terminalID, ":") {
		return resolvedTerminalRef{}, usageCLIError("terminal target must be ENDPOINT_ID:TERMINAL_ID")
	}
	cfg, ok := registry.Endpoints[endpointID]
	if !ok {
		return resolvedTerminalRef{}, &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", endpointID)}
	}
	if !cfg.Enabled {
		return resolvedTerminalRef{}, &cliError{code: 4, message: fmt.Sprintf("endpoint %s is disabled", endpointID)}
	}
	return resolvedTerminalRef{EndpointID: endpointID, TerminalID: terminalID}, nil
}

func resolveEndpointConfig(requested string, registry connection.Registry) (connection.Endpoint, error) {
	id := connection.EndpointID(strings.TrimSpace(requested))
	if id == "" {
		id = registry.Default
	}
	cfg, ok := registry.Endpoints[id]
	if !ok {
		return connection.Endpoint{}, &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
	}
	if !cfg.Enabled {
		return connection.Endpoint{}, &cliError{code: 4, message: fmt.Sprintf("endpoint %s is disabled", id)}
	}
	return cfg, nil
}

func openEndpointProtocolClient(ctx context.Context, endpoint connection.Endpoint, socketOverride, logFile string) (*protocol.Client, func(), error) {
	route, err := endpoint.ResolveCurrentRoute("")
	if err != nil {
		return nil, func() {}, err
	}
	return openEndpointRouteProtocolClient(ctx, endpoint, route, socketOverride, logFile)
}

func openEndpointRouteProtocolClient(ctx context.Context, endpoint connection.Endpoint, route connection.AccessRoute, socketOverride, logFile string) (*protocol.Client, func(), error) {
	// Route 由调用方显式选择或经唯一 eligible route 解析；失败直接返回，不能尝试其它 route/endpoint。
	switch route.Kind {
	case connection.RouteLocalUnix:
		socketPath := strings.TrimSpace(route.Socket)
		if strings.TrimSpace(socketOverride) != "" {
			socketPath = socketOverride
		}
		if socketPath == "" || socketPath == "auto" {
			socketPath = resolveV3Socket("")
		}
		logger, closeLogger, resolvedLog, err := openLogFileLogger(logFile)
		if err != nil {
			return nil, func() {}, err
		}
		client, err := dialCLIEndpointLocal(socketPath, resolvedLog, logger)
		if err != nil {
			closeLogger()
			return nil, func() {}, err
		}
		return client, func() { _ = client.Close(); closeLogger() }, nil
	case connection.RouteSSHStdio:
		client, err := dialCLIEndpointSSH(ctx, ctx, endpoint, route)
		if err != nil {
			return nil, func() {}, err
		}
		return client, func() { _ = client.Close() }, nil
	case connection.RouteManagedWebRTC:
		client, _, err := dialCLIEndpointCloud(ctx, endpoint, route)
		if err != nil {
			return nil, func() {}, err
		}
		return client, func() { _ = client.Close() }, nil
	default:
		return nil, func() {}, &cliError{code: 2, message: fmt.Sprintf("endpoint %s route %s has unsupported kind %s", endpoint.ID, route.ID, route.Kind)}
	}
}

func probeEndpointProtocolClient(ctx context.Context, endpoint connection.Endpoint, requestedRoute connection.RouteID, socketOverride, logFile string) (connection.RouteID, string, string, func(), error) {
	route, err := endpoint.ResolveCurrentRoute(requestedRoute)
	if err != nil {
		return "", "", "", func() {}, err
	}
	if route.Kind == connection.RouteManagedWebRTC {
		client, session, err := dialCLIEndpointCloud(ctx, endpoint, route)
		if err != nil {
			return "", "", "", func() {}, err
		}
		return route.ID, string(session.ObservedPath), string(session.RouteSelectionReason), func() { _ = client.Close() }, nil
	}
	_, closeClient, err := openEndpointRouteProtocolClient(ctx, endpoint, route, socketOverride, logFile)
	return route.ID, "", "", closeClient, err
}
