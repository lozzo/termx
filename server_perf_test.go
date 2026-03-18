package termx

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	"github.com/lozzow/termx/protocol"
)

func BenchmarkEventBusPublish64Subscribers(b *testing.B) {
	bus := NewEventBus(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 64; i++ {
		_ = bus.Subscribe(ctx)
	}

	evt := Event{Type: EventTerminalCreated, TerminalID: "bench"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(evt)
	}
}

func BenchmarkServerHandleRequestList(b *testing.B) {
	srv := NewServer()
	srv.terminals = make(map[string]*Terminal, 1000)
	for i := 0; i < 1000; i++ {
		id := strconv.Itoa(i)
		srv.terminals[id] = &Terminal{
			id:      id,
			name:    id,
			command: []string{"bash"},
			tags:    map[string]string{"group": "bench"},
			size:    Size{Cols: 80, Rows: 24},
			state:   StateRunning,
		}
	}

	req := protocol.Request{
		ID:     1,
		Method: "list",
		Params: json.RawMessage(`{}`),
	}
	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := srv.handleRequest(context.Background(), "bench", allocator, attachments, &attachmentsMu, req, sendFrame); err != nil {
			b.Fatalf("handle request failed: %v", err)
		}
	}
}

func BenchmarkServerListParallel(b *testing.B) {
	srv := NewServer()
	srv.terminals = make(map[string]*Terminal, 1000)
	for i := 0; i < 1000; i++ {
		id := strconv.Itoa(i)
		srv.terminals[id] = &Terminal{
			id:      id,
			name:    id,
			command: []string{"bash"},
			tags:    map[string]string{"group": "bench"},
			size:    Size{Cols: 80, Rows: 24},
			state:   StateRunning,
		}
	}

	ctx := context.Background()
	opts := ListOptions{Tags: map[string]string{"group": "bench"}}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			infos, err := srv.List(ctx, opts)
			if err != nil {
				b.Fatalf("list failed: %v", err)
			}
			if len(infos) != 1000 {
				b.Fatalf("unexpected list size: %d", len(infos))
			}
		}
	})
}
