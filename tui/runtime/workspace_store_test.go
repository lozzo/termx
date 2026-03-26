package runtime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lozzow/termx/tui/app"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

func TestWorkspaceStoreSaveAndLoadRoundTrip(t *testing.T) {
	store := NewWorkspaceStore(filepath.Join(t.TempDir(), "workspace.json"))
	model := app.NewModel("main")
	model.Screen = app.ScreenTerminalPool
	model.Workbench.BindActivePane(coreterminal.Metadata{
		ID:    types.TerminalID("term-1"),
		Name:  "shell",
		State: coreterminal.StateRunning,
	})

	if err := store.Save(context.Background(), model); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Screen != app.ScreenTerminalPool {
		t.Fatalf("expected terminal pool screen, got %q", loaded.Screen)
	}
	if _, ok := loaded.Workbench.Terminals[types.TerminalID("term-1")]; !ok {
		t.Fatal("expected persisted terminal metadata")
	}
	if len(loaded.Workbench.Sessions) != 0 {
		t.Fatalf("expected sessions to stay out of persistence, got %#v", loaded.Workbench.Sessions)
	}
}
