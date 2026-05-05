package remote

import (
	"testing"

	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
)

func TestLocalStatusDisabledByDefault(t *testing.T) {
	service := NewService(remoteprotocol.Config{}, nil)

	status := service.LocalStatus()
	if status.Enabled {
		t.Fatalf("LocalStatus enabled by default: %+v", status)
	}
}
