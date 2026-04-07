package app

import (
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
)

// hostEmojiVariationProbeSequence measures how the host terminal advances the
// cursor for U+267B U+FE0F ("♻️"). We temporarily print it at the origin,
// request a cursor-position report, erase the cells, and restore the cursor.
//
// If the host reports X=1 (zero-based), it rendered the grapheme but only
// advanced one column; render switches to the "advance" strategy for later
// frames. If it reports X=2, raw output is already safe.
const hostEmojiVariationProbeSequence = xansi.SaveCursor +
	xansi.CursorOrigin +
	"♻️" +
	xansi.RequestExtendedCursorPositionReport +
	xansi.CursorOrigin +
	"  " +
	xansi.RestoreCursor

var (
	// Queue the first probe immediately after Bubble Tea paints its first frame,
	// then keep retrying on a timer if the host doesn't answer.
	hostEmojiProbeRetryDelay  = 120 * time.Millisecond
	hostEmojiProbeMaxAttempts = 6
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
	output := stdout
	probeSupported := false
	if stdout != nil {
		writer := newOutputCursorWriter(stdout)
		model.SetCursorWriter(writer)
		output = writer
		probeSupported = writer != nil && writer.tty != nil
	}
	if model.runtime != nil {
		if probeSupported {
			// Default to the conservative fallback until the host terminal reports
			// how far it actually advances for an ambiguous FE0F emoji cluster.
			model.runtime.SetHostAmbiguousEmojiVariationSelectorMode(shared.AmbiguousEmojiVariationSelectorStrip)
			model.hostEmojiProbePending = true
		} else {
			model.runtime.SetHostAmbiguousEmojiVariationSelectorMode(shared.AmbiguousEmojiVariationSelectorRaw)
			model.hostEmojiProbePending = false
		}
		if writer := model.cursorOut; writer != nil && model.hostEmojiProbePending {
			writer.QueueControlSequenceAfterWrite(hostEmojiVariationProbeSequence)
		}
	}
	opts := []tea.ProgramOption{
		tea.WithInput(nil),
		tea.WithOutput(output),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}
	opts = append(opts, extraOpts...)
	p := tea.NewProgram(model, opts...)
	model.SetSendFunc(p.Send)
	stopCursorBlink := startCursorBlinkForwarder(p, model.render)
	defer stopCursorBlink()
	stopTerminalEvents := startTerminalEventsForwarder(p.Send, cfg, client)
	defer stopTerminalEvents()
	stopSessionEvents := startSessionEventsForwarder(p.Send, cfg, client)
	defer stopSessionEvents()

	stopInput, restoreInput, err := startInputForwarder(p, stdin)
	if err != nil {
		return err
	}
	defer func() { _ = restoreInput() }()
	defer stopInput()

	if output != nil {
		_, _ = io.WriteString(output, xansi.RequestForegroundColor+xansi.RequestBackgroundColor+requestTerminalPaletteQueries())
	}

	_, err = p.Run()
	return err
}
