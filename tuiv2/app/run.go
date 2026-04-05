package app

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
)

// Run creates a new Model with the given Config and starts the bubbletea
// program. stdin/stdout are wired via the provided readers/writers so that
// tests can inject fakes without touching os.Stdin / os.Stdout.
func Run(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
	return RunWithClient(cfg, nil, stdin, stdout)
}

func RunWithClient(cfg shared.Config, client bridge.Client, stdin io.Reader, stdout io.Writer) error {
	return runWithClientOptions(cfg, client, stdin, stdout)
}

func runWithClientOptions(cfg shared.Config, client bridge.Client, stdin io.Reader, stdout io.Writer, extraOpts ...tea.ProgramOption) error {
	model := New(cfg, nil, runtime.New(client))
	opts := []tea.ProgramOption{
		tea.WithInput(nil),
		tea.WithOutput(stdout),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}
	opts = append(opts, extraOpts...)
	p := tea.NewProgram(model, opts...)
	model.SetSendFunc(p.Send)
	stopCursorBlink := startCursorBlinkForwarder(p, model.render)
	defer stopCursorBlink()
	stopSessionEvents := startSessionEventsForwarder(p.Send, cfg, client)
	defer stopSessionEvents()

	stopInput, restoreInput, err := startInputForwarder(p, stdin)
	if err != nil {
		return err
	}
	defer func() { _ = restoreInput() }()
	defer stopInput()

	if stdout != nil {
		_, _ = io.WriteString(stdout, xansi.RequestForegroundColor+xansi.RequestBackgroundColor+requestTerminalPaletteQueries())
	}

	_, err = p.Run()
	return err
}
