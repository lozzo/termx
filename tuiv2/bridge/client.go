package bridge

import (
	"github.com/lozzow/termx/internal/clientapi"
	"github.com/lozzow/termx/protocol"
)

type Client = clientapi.Client
type ProtocolClient = clientapi.ProtocolClient

func NewProtocolClient(inner *protocol.Client) *ProtocolClient {
	return clientapi.NewProtocolClient(inner)
}
