package main

import (
	"sync/atomic"

	clientendpoint "github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/internal/protocol"
)

var nextLocalApplicationGeneration atomic.Uint64

func newLocalApplicationSession(client *protocol.Client) (*clientruntime.ApplicationSession, error) {
	return clientruntime.NewApplicationSession(clientruntime.EndpointSessionStamp{
		EndpointID: clientendpoint.DefaultEndpointID,
		RouteID:    clientendpoint.DefaultLocalRouteID,
		Generation: clientruntime.SessionGeneration(nextLocalApplicationGeneration.Add(1)),
	}, client)
}
