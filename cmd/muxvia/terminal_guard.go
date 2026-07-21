package main

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

var isInteractiveTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func nestedTUIBlocked() bool {
	return os.Getenv("MUXVIA") == "1" && os.Getenv("MUXVIA_ALLOW_NESTED") != "1"
}

func rejectNestedTUI() error {
	if !nestedTUIBlocked() {
		return nil
	}
	return fmt.Errorf("refusing to start muxvia TUI inside a muxvia remote terminal; use a normal shell, or set MUXVIA_ALLOW_NESTED=1 if you really want nesting")
}
