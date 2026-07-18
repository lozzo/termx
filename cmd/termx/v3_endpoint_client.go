package main

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	protocoladapter "github.com/lozzow/termx/client/adapter/protocol"
	endpointdomain "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
)

var nextCLIEndpointGeneration atomic.Uint64

func openEndpointProtocolClient(ctx context.Context, endpoint endpointdomain.Endpoint, socketOverride, logFile string) (*protocoladapter.ApplicationClient, func(), error) {
	route, err := selectCLIEndpointRoute(endpoint, "")
	if err != nil {
		return nil, func() {}, err
	}
	return openEndpointRouteProtocolClient(ctx, endpoint, route, socketOverride, logFile)
}

func probeEndpointProtocolClient(ctx context.Context, endpoint endpointdomain.Endpoint, requestedRoute endpointdomain.RouteID, socketOverride, logFile string) (endpointdomain.RouteID, string, string, func(), error) {
	route, err := selectCLIEndpointRoute(endpoint, requestedRoute)
	if err != nil {
		return "", "", "", func() {}, err
	}
	_, closeClient, err := openEndpointRouteProtocolClient(ctx, endpoint, route, socketOverride, logFile)
	if err != nil {
		return "", "", "", func() {}, err
	}
	return route.ID, "", "only_viable", func() { closeClient() }, nil
}

func openEndpointRouteProtocolClient(ctx context.Context, endpoint endpointdomain.Endpoint, route endpointdomain.AccessRoute, socketOverride, logFile string) (*protocoladapter.ApplicationClient, func(), error) {
	if route.Kind != endpointdomain.RouteLocalUnix {
		return nil, func() {}, fmt.Errorf("endpoint %s route %s (%s) requires shared client runtime integration", endpoint.ID, route.ID, route.Kind)
	}
	socketPath := strings.TrimSpace(route.Socket)
	if strings.TrimSpace(socketOverride) != "" {
		socketPath = strings.TrimSpace(socketOverride)
	}
	if socketPath == "" || socketPath == "auto" {
		socketPath = resolveV3Socket("")
	}
	client, err := dialOrStartV3ClientContext(ctx, socketPath, resolveV3LogFilePath(logFile), nil)
	if err != nil {
		return nil, func() {}, err
	}
	application, err := protocoladapter.NewApplicationClient(client, clientruntime.EndpointSessionStamp{
		EndpointID: endpoint.ID, RouteID: route.ID, Generation: clientruntime.SessionGeneration(nextCLIEndpointGeneration.Add(1)),
	})
	if err != nil {
		_ = client.Close()
		return nil, func() {}, err
	}
	return application, func() { _ = client.Close() }, nil
}

func selectCLIEndpointRoute(endpoint endpointdomain.Endpoint, requested endpointdomain.RouteID) (endpointdomain.AccessRoute, error) {
	if requested != "" {
		route, ok := endpoint.Route(requested)
		if !ok || !route.Enabled {
			return endpointdomain.AccessRoute{}, fmt.Errorf("endpoint %s route %s is unavailable", endpoint.ID, requested)
		}
		return route, nil
	}
	eligible := make([]endpointdomain.AccessRoute, 0, len(endpoint.Routes))
	for _, route := range endpoint.RouteList() {
		if route.Enabled && !route.ManualOnly {
			eligible = append(eligible, route)
		}
	}
	if len(eligible) == 0 {
		return endpointdomain.AccessRoute{}, fmt.Errorf("endpoint %s has no eligible route", endpoint.ID)
	}
	if len(eligible) != 1 {
		return endpointdomain.AccessRoute{}, fmt.Errorf("endpoint %s requires explicit route selection", endpoint.ID)
	}
	return eligible[0], nil
}
