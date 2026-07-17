package core

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
)

func (session *protocolSession) dispatchApplicationPayload(ctx context.Context, payload []byte) ([]byte, bool, int, error) {
	if session.application == nil {
		return nil, false, protocolErrorUnavailable, fmt.Errorf("application executor is unavailable")
	}
	decoded, err := protocol.DecodeMethodParams("api.execute", payload)
	if err != nil {
		return nil, false, protocolErrorBadRequest, err
	}
	command, ok := decoded.(*apipb.CommandEnvelope)
	if !ok || command == nil {
		return nil, false, protocolErrorBadRequest, fmt.Errorf("api.execute decoded unexpected payload %T", decoded)
	}
	result := session.application.Execute(ctx, command)
	payload, err = protocol.EncodeMethodResult("api.execute", result)
	if err != nil {
		return nil, false, protocolErrorInternal, err
	}
	return payload, false, 0, nil
}
