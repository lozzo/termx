package main

import (
	"fmt"

	protocoladapter "github.com/lozzow/termx/client/adapter/protocol"
	clientruntime "github.com/lozzow/termx/client/runtime"
)

func newLocalApplicationSession(client *protocoladapter.ApplicationClient) (*clientruntime.ApplicationSession, error) {
	if client == nil || client.ApplicationSession == nil {
		return nil, fmt.Errorf("owned local application client is required")
	}
	return client.ApplicationSession, nil
}
