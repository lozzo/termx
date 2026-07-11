package main

import (
	"os"

	tuiconfig "github.com/lozzow/termx/tui/config"
	"github.com/lozzow/termx/tui/state"
)

func loadV3TUIConfig() (state.TUIConfigStore, error) {
	return tuiconfig.Load("", os.Getenv)
}
