package main

import (
	"os"

	tuiconfig "github.com/lozzow/termx/termx-tui-v3/config"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func loadV3TUIConfig() (state.TUIConfigStore, error) {
	return tuiconfig.Load("", os.Getenv)
}
