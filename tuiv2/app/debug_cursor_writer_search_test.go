package app

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	localvterm "github.com/lozzow/termx/termx-core/vterm"
)

func TestDebugSearchCursorWriterStyledRoundTrip(t *testing.T) {
	if os.Getenv("TERMX_SEARCH_CURSOR_WRITER_STYLE") != "1" {
		t.Skip("set TERMX_SEARCH_CURSOR_WRITER_STYLE=1 to search for styled frame presenter divergences")
	}
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	type styleCell struct {
		ch string
		fg string
		bg string
	}

	encodeRow := func(row []styleCell) string {
		var b strings.Builder
		curFG, curBG := "", ""
		for _, cell := range row {
			if cell.fg != curFG || cell.bg != curBG {
				b.WriteString("\x1b[0m")
				if cell.fg != "" || cell.bg != "" {
					b.WriteString("\x1b[")
					first := true
					if cell.fg != "" {
						r, g, bl := rgbHex(cell.fg)
						fmt.Fprintf(&b, "38;2;%d;%d;%d", r, g, bl)
						first = false
					}
					if cell.bg != "" {
						r, g, bl := rgbHex(cell.bg)
						if !first {
							b.WriteByte(';')
						}
						fmt.Fprintf(&b, "48;2;%d;%d;%d", r, g, bl)
					}
					b.WriteByte('m')
				}
				curFG, curBG = cell.fg, cell.bg
			}
			b.WriteString(cell.ch)
		}
		b.WriteString("\x1b[0m")
		return b.String()
	}

	buildFrame := func(width, height, seed int) []string {
		rng := rand.New(rand.NewSource(int64(seed)))
		lines := make([]string, height)
		paletteFG := []string{"#f9fafb", "#fde68a", "#93c5fd", "#86efac"}
		paletteBG := []string{"#111827", "#1f2937", "#374151", "#4b5563"}
		for y := 0; y < height; y++ {
			row := make([]styleCell, width)
			block := rng.Intn(7) + 3
			for x := 0; x < width; x++ {
				fg := paletteFG[(x/block+y+seed)%len(paletteFG)]
				bg := paletteBG[(x/(block+1)+seed+y)%len(paletteBG)]
				ch := " "
				if x%11 == 0 {
					ch = string(rune('a' + (x+y+seed)%26))
				}
				row[x] = styleCell{ch: ch, fg: fg, bg: bg}
			}
			lines[y] = encodeRow(row)
		}
		return lines
	}

	replay := func(frames [][]string) localvterm.ScreenData {
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		for _, frame := range frames {
			if err := writer.WriteFrameLines(frame, ""); err != nil {
				t.Fatalf("write frame lines: %v", err)
			}
		}
		sink.mu.Lock()
		stream := strings.Join(sink.writes, "")
		sink.mu.Unlock()
		vt := localvterm.New(48, 10, 0, nil)
		if _, err := vt.Write([]byte(stream)); err != nil {
			t.Fatalf("replay stream: %v", err)
		}
		return vt.ScreenContent()
	}

	for seq := 0; seq < 2000; seq++ {
		frames := [][]string{
			buildFrame(48, 10, seq),
			buildFrame(48, 10, seq+1),
			buildFrame(48, 10, seq+2),
		}
		got := replay(frames)
		want := replay(frames[len(frames)-1:])
		if err := screenDiffError(got, want); err != nil {
			t.Fatalf("found divergence at sequence seed %d: %v", seq, err)
		}
	}
}

func rgbHex(hex string) (int, int, int) {
	if len(hex) == 7 && hex[0] == '#' {
		var r, g, b int
		fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
		return r, g, b
	}
	return 255, 255, 255
}

func TestDebugSearchCursorWriterResizeRaceStyledRoundTrip(t *testing.T) {
	if os.Getenv("TERMX_SEARCH_CURSOR_WRITER_RESIZE_RACE") != "1" {
		t.Skip("set TERMX_SEARCH_CURSOR_WRITER_RESIZE_RACE=1 to search resize-race divergences")
	}
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	buildModel := func(input string) *Model {
		t.Helper()
		model := setupModel(t, modelOpts{width: 120, height: 36})
		base := model.runtime.Registry().GetOrCreate("term-1")
		base.Snapshot = cursorWriterCodexInputSnapshot("term-1", 118, 30, input)
		return model
	}

	for seq := 0; seq < 100; seq++ {
		model := buildModel("")
		sink := &cursorWriterProbeTTY{}
		writer := newOutputCursorWriter(sink)
		model.SetFrameWriter(writer)
		model.SetCursorWriter(writer)

		model.render.Invalidate()
		_ = model.View()
		terminal := model.runtime.Registry().Get("term-1")
		for i := 0; i < 8; i++ {
			terminal.Snapshot = cursorWriterCodexInputSnapshot("term-1", 118, 30, strings.Repeat(string(rune('a'+(seq+i)%26)), i+1))
			touchRuntimeVisibleStateForTest(model.runtime, uint8(i+1))
			if i == 3 {
				_, cmd := model.Update(tea.WindowSizeMsg{Width: 96, Height: 28})
				drainCmd(t, model, cmd, 20)
			}
			model.render.Invalidate()
			_ = model.View()
		}
	}
}
