package runtime

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
)

func TestTerminalServiceDelegatesCreateAndAttach(t *testing.T) {
	client := &stubClient{}
	service := NewTerminalService(client)

	created, err := service.Create(context.Background(), []string{"/bin/sh"}, "shell", protocol.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.TerminalID != "term-1" {
		t.Fatalf("expected term-1, got %#v", created)
	}

	attached, err := service.Attach(context.Background(), "term-1", "rw")
	if err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}
	if attached.Channel != 7 {
		t.Fatalf("expected channel 7, got %#v", attached)
	}
}
