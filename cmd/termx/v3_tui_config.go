package main

import (
	"os"

	tuiconfig "github.com/muxvia/muxvia/tui/config"
	"github.com/muxvia/muxvia/tui/state"
)

func loadV3TUIConfig(path string) (state.TUIConfigStore, error) {
	return tuiconfig.Load(path, os.Getenv)
}
