package main

import (
	"os"

	tuiconfig "github.com/lozzow/termx/tui/config"
	"github.com/lozzow/termx/tui/state"
)

func loadV3TUIConfig(path string) (state.TUIConfigStore, error) {
	return tuiconfig.Load(path, os.Getenv)
}
