package termx

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkServerList(b *testing.B) {
	srv := NewServer()
	srv.terminals = make(map[string]*Terminal, 1000)

	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("bench-%04d", i)
		srv.terminals[id] = &Terminal{
			id:        id,
			name:      id,
			command:   []string{"bash"},
			tags:      map[string]string{"group": "bench"},
			size:      Size{Cols: 80, Rows: 24},
			state:     StateRunning,
			createdAt: time.Unix(0, 0).UTC(),
		}
	}

	ctx := context.Background()
	opts := ListOptions{Tags: map[string]string{"group": "bench"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		infos, err := srv.List(ctx, opts)
		if err != nil {
			b.Fatalf("list failed: %v", err)
		}
		if len(infos) != 1000 {
			b.Fatalf("unexpected list size: %d", len(infos))
		}
	}
}

func BenchmarkServerGet(b *testing.B) {
	srv := NewServer()
	srv.terminals = map[string]*Terminal{
		"bench-get": {
			id:        "bench-get",
			name:      "bench-get",
			command:   []string{"bash"},
			tags:      map[string]string{"group": "bench"},
			size:      Size{Cols: 80, Rows: 24},
			state:     StateRunning,
			createdAt: time.Unix(0, 0).UTC(),
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		info, err := srv.Get(ctx, "bench-get")
		if err != nil {
			b.Fatalf("get failed: %v", err)
		}
		if info.ID != "bench-get" {
			b.Fatalf("unexpected terminal id: %s", info.ID)
		}
	}
}
