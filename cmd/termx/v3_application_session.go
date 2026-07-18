package main

import (
	"fmt"

	localadapter "github.com/lozzow/termx/client/adapter/local"
	protocoladapter "github.com/lozzow/termx/client/adapter/protocol"
	clientendpoint "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
)

func newLocalApplicationSession(client any) (*clientruntime.ApplicationSession, error) {
	switch value := client.(type) {
	case *protocoladapter.ApplicationClient:
		if value == nil || value.ApplicationSession == nil {
			return nil, fmt.Errorf("owned local application client is required")
		}
		return value.ApplicationSession, nil
	case *localadapter.ProtocolClient:
		owned, err := adoptCLIProtocolClient(value, clientendpoint.DefaultEndpointID)
		if err != nil {
			return nil, err
		}
		return owned.ApplicationSession, nil
	default:
		return nil, fmt.Errorf("unsupported local application client %T", client)
	}
}

func adoptCLIProtocolClient(client *localadapter.ProtocolClient, endpointID clientendpoint.EndpointID) (*protocoladapter.ApplicationClient, error) {
	owner := clientruntime.NewSessionOwner()
	registry := clientendpoint.DefaultRegistry()
	target, _ := registry.DefaultEndpoint()
	if endpointID != "" {
		target.ID = endpointID
	}
	attempt, err := owner.BeginRouteAttempt(target, clientendpoint.DefaultLocalRouteID, clientruntime.ConnectIntentInteractive)
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	ready, err := protocoladapter.NewApplicationClient(client, attempt.Stamp())
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	lease, err := owner.AdoptReadySession(attempt, ready)
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	owned, err := owner.ApplicationSession(lease)
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	return protocoladapter.NewOwnedApplicationClient(client, owned)
}
