package app

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

var systemClipboardWriter = writeSystemClipboard
var systemClipboardReader = readSystemClipboard

const clipboardCommandTimeout = 1500 * time.Millisecond

func writeSystemClipboard(text string) error {
	if text == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardCommandTimeout)
	defer cancel()
	for _, spec := range clipboardWriteCommands() {
		cmd := exec.CommandContext(ctx, spec.name, spec.args...)
		cmd.Stdin = bytes.NewBufferString(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no system clipboard command available")
}

func readSystemClipboard() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardCommandTimeout)
	defer cancel()
	for _, spec := range clipboardReadCommands() {
		cmd := exec.CommandContext(ctx, spec.name, spec.args...)
		out, err := cmd.Output()
		if err == nil {
			return string(out), nil
		}
	}
	return "", fmt.Errorf("no system clipboard command available")
}

type clipboardCommandSpec struct {
	name string
	args []string
}

func clipboardWriteCommands() []clipboardCommandSpec {
	return []clipboardCommandSpec{
		{name: "wl-copy"},
		{name: "xclip", args: []string{"-selection", "clipboard", "-in"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
		{name: "pbcopy"},
	}
}

func clipboardReadCommands() []clipboardCommandSpec {
	return []clipboardCommandSpec{
		{name: "wl-paste"},
		{name: "xclip", args: []string{"-selection", "clipboard", "-out"}},
		{name: "xsel", args: []string{"--clipboard", "--output"}},
		{name: "pbpaste"},
	}
}
