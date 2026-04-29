package bridge

import (
	"github.com/lozzow/termx/termx-core/clientapi"
	"github.com/lozzow/termx/termx-core/protocol"
)

type Client = clientapi.Client
type ProtocolClient = clientapi.ProtocolClient

func NewProtocolClient(inner *protocol.Client) *ProtocolClient {
	return clientapi.NewProtocolClient(inner)
}
