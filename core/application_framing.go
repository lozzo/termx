package core

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/internal/protocol"
)

func (session *protocolSession) dispatchApplicationPayload(ctx context.Context, payload []byte) ([]byte, bool, int, error) {
	if session.application == nil {
		return nil, false, protocolErrorUnavailable, fmt.Errorf("application executor is unavailable")
	}
	command, err := protocol.DecodeApplicationCommand(payload)
	if err != nil {
		return nil, false, protocolErrorBadRequest, err
	}
	result := session.application.Execute(ctx, command)
	payload, err = protocol.EncodeApplicationResult(result)
	if err != nil {
		return nil, false, protocolErrorInternal, err
	}
	return payload, false, 0, nil
}
