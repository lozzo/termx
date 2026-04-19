package app

import (
	"strconv"
	"strings"
	"testing"
)

type cursorWriterBenchmarkSink struct {
	bytes int
}

var benchmarkCursorWriterOutputSink string

func (s *cursorWriterBenchmarkSink) Write(p []byte) (int, error) {
	s.bytes += len(p)
	return len(p), nil
}

func (s *cursorWriterBenchmarkSink) Reset() {
	s.bytes = 0
}

func BenchmarkOutputCursorWriterWriteFrameFixedDamageByTerminalWidth(b *testing.B) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	cases := []struct {
		name  string
		width int
	}{
		{name: "width_120", width: 120},
		{name: "width_220", width: 220},
		{name: "width_320", width: 320},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			sink := &cursorWriterBenchmarkSink{}
			writer := newOutputCursorWriter(sink)
			frameA := benchmarkCursorWriterFrame(tc.width, 36, 18, 7, 54, 16, false)
			frameB := benchmarkCursorWriterFrame(tc.width, 36, 19, 7, 54, 16, false)

			if err := writer.WriteFrame(frameA, ""); err != nil {
				b.Fatalf("prime frame: %v", err)
			}
			sink.Reset()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				frame := frameB
				if i%2 != 0 {
					frame = frameA
				}
				if err := writer.WriteFrame(frame, ""); err != nil {
					b.Fatalf("write diff frame: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(sink.bytes)/float64(maxInt(1, b.N)), "bytes/op")
		})
	}
}

func BenchmarkOutputCursorWriterWriteFrameFixedDamageContentComplexity(b *testing.B) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	cases := []struct {
		name   string
		styled bool
	}{
		{name: "plain_shell", styled: false},
		{name: "styled_codex", styled: true},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			sink := &cursorWriterBenchmarkSink{}
			writer := newOutputCursorWriter(sink)
			frameA := benchmarkCursorWriterFrame(220, 36, 18, 7, 54, 16, tc.styled)
			frameB := benchmarkCursorWriterFrame(220, 36, 19, 7, 54, 16, tc.styled)

			if err := writer.WriteFrame(frameA, ""); err != nil {
				b.Fatalf("prime frame: %v", err)
			}
			sink.Reset()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				frame := frameB
				if i%2 != 0 {
					frame = frameA
				}
				if err := writer.WriteFrame(frame, ""); err != nil {
					b.Fatalf("write diff frame: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(sink.bytes)/float64(maxInt(1, b.N)), "bytes/op")
		})
	}
}

func BenchmarkOutputCursorWriterWriteFrameLinesDamageProfile(b *testing.B) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	cases := []struct {
		name string
		next []string
	}{
		{
			name: "partial_damage",
			next: strings.Split(benchmarkCursorWriterFrame(220, 36, 19, 7, 54, 16, true), "\n"),
		},
		{
			name: "full_damage",
			next: strings.Split(benchmarkCursorWriterFrame(220, 36, 0, 0, 220, 36, true), "\n"),
		},
	}

	base := strings.Split(benchmarkCursorWriterFrame(220, 36, 18, 7, 54, 16, true), "\n")
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			sink := &cursorWriterBenchmarkSink{}
			writer := newOutputCursorWriter(sink)

			if err := writer.WriteFrameLines(base, ""); err != nil {
				b.Fatalf("prime lines frame: %v", err)
			}
			sink.Reset()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				lines := tc.next
				if i%2 != 0 {
					lines = base
				}
				if err := writer.WriteFrameLines(lines, ""); err != nil {
					b.Fatalf("write lines frame: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(sink.bytes)/float64(maxInt(1, b.N)), "bytes/op")
		})
	}
}

func BenchmarkFramePresenterPresentLinesDamageProfile(b *testing.B) {
	cases := []struct {
		name string
		next []string
	}{
		{
			name: "partial_damage",
			next: stripTrailingEraseLineRight(strings.Split(benchmarkCursorWriterFrame(220, 36, 19, 7, 54, 16, true), "\n")),
		},
		{
			name: "full_damage",
			next: stripTrailingEraseLineRight(strings.Split(benchmarkCursorWriterFrame(220, 36, 0, 0, 220, 36, true), "\n")),
		},
	}

	base := stripTrailingEraseLineRight(strings.Split(benchmarkCursorWriterFrame(220, 36, 18, 7, 54, 16, true), "\n"))
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var presenter framePresenter
			presenter.fullWidthLines = true
			benchmarkCursorWriterOutputSink = presenter.PresentLines(base)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				lines := tc.next
				if i%2 != 0 {
					lines = base
				}
				benchmarkCursorWriterOutputSink = presenter.PresentLines(lines)
			}
		})
	}
}

func BenchmarkOutputCursorWriterWriteFrameLinesScrollBytesByMode(b *testing.B) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous, next := benchmarkCursorWriterScrollFrames(120, 40)
	cases := []struct {
		name string
		mode verticalScrollMode
	}{
		{name: "scroll_disabled", mode: verticalScrollModeNone},
		{name: "scroll_rows_enabled", mode: verticalScrollModeRowsAndRects},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			sink := &cursorWriterBenchmarkSink{}
			writer := newOutputCursorWriter(sink)
			writer.SetVerticalScrollMode(tc.mode)

			if err := writer.WriteFrameLines(previous, ""); err != nil {
				b.Fatalf("prime lines frame: %v", err)
			}
			sink.Reset()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				lines := next
				if i%2 != 0 {
					lines = previous
				}
				if err := writer.WriteFrameLines(lines, ""); err != nil {
					b.Fatalf("write scrolled frame: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(sink.bytes)/float64(maxInt(1, b.N)), "bytes/op")
		})
	}
}

func BenchmarkOutputCursorWriterWriteFrameLinesRectScrollBytesByGate(b *testing.B) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous, next := benchmarkCursorWriterRectScrollFrames()
	cases := []struct {
		name          string
		lrScrollEnv   string
		remoteLatency string
	}{
		{name: "lr_scroll_disabled", lrScrollEnv: "0", remoteLatency: "0"},
		{name: "lr_scroll_remote_auto", lrScrollEnv: "", remoteLatency: "1"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.Setenv("TERMX_EXPERIMENTAL_LR_SCROLL", tc.lrScrollEnv)
			b.Setenv("TERMX_REMOTE_LATENCY", tc.remoteLatency)

			sink := &cursorWriterBenchmarkSink{}
			writer := newOutputCursorWriter(sink)
			writer.SetVerticalScrollMode(verticalScrollModeRectsOnly)

			if err := writer.WriteFrameLines(previous, ""); err != nil {
				b.Fatalf("prime lines frame: %v", err)
			}
			sink.Reset()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				lines := next
				if i%2 != 0 {
					lines = previous
				}
				if err := writer.WriteFrameLines(lines, ""); err != nil {
					b.Fatalf("write rect scrolled frame: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(sink.bytes)/float64(maxInt(1, b.N)), "bytes/op")
		})
	}
}

func BenchmarkOutputCursorWriterWriteFrameLinesScrollBytesWithCursorMotion(b *testing.B) {
	originalDelay := directFrameBatchDelay
	directFrameBatchDelay = 0
	defer func() { directFrameBatchDelay = originalDelay }()

	previous, next := benchmarkCursorWriterScrollFrames(120, 40)
	cursorA := "\x1b[10;20H"
	cursorB := "\x1b[11;20H"
	cases := []struct {
		name string
		mode verticalScrollMode
	}{
		{name: "scroll_disabled", mode: verticalScrollModeNone},
		{name: "scroll_rows_enabled", mode: verticalScrollModeRowsAndRects},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			sink := &cursorWriterBenchmarkSink{}
			writer := newOutputCursorWriter(sink)
			writer.SetVerticalScrollMode(tc.mode)

			if err := writer.WriteFrameLines(previous, cursorA); err != nil {
				b.Fatalf("prime lines frame: %v", err)
			}
			sink.Reset()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				lines := next
				cursor := cursorB
				if i%2 != 0 {
					lines = previous
					cursor = cursorA
				}
				if err := writer.WriteFrameLines(lines, cursor); err != nil {
					b.Fatalf("write scrolled frame: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(sink.bytes)/float64(maxInt(1, b.N)), "bytes/op")
		})
	}
}

func benchmarkCursorWriterFrame(width, height, paneX, paneY, paneW, paneH int, styled bool) string {
	var frame strings.Builder
	for y := 0; y < height; y++ {
		if y > 0 {
			frame.WriteByte('\n')
		}
		frame.WriteString(benchmarkCursorWriterRow(width, paneX, paneY, paneW, paneH, y, styled))
	}
	return frame.String()
}

func benchmarkCursorWriterRow(width, paneX, paneY, paneW, paneH, row int, styled bool) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	out.Grow(width + 32)
	out.WriteString("\x1b[G")
	if row < paneY || row >= paneY+paneH {
		out.WriteString(strings.Repeat(".", width))
		out.WriteString("\x1b[0m\x1b[K")
		return out.String()
	}

	left := clampInt(paneX, 0, width)
	rightStart := clampInt(paneX+paneW, 0, width)
	if left > 0 {
		out.WriteString(strings.Repeat(".", left))
	}
	out.WriteString(benchmarkCursorWriterPaneRow(rightStart-left, row-paneY, styled))
	if rightStart < width {
		out.WriteString(strings.Repeat(".", width-rightStart))
	}
	out.WriteString("\x1b[0m\x1b[K")
	return out.String()
}

func benchmarkCursorWriterPaneRow(width, row int, styled bool) string {
	if width <= 0 {
		return ""
	}
	if width == 1 {
		return "#"
	}
	if row == 0 || row == 15 {
		return "+" + strings.Repeat("-", width-2) + "+"
	}
	innerWidth := width - 2
	if !styled {
		return "|" + benchmarkCursorWriterPattern(innerWidth, row, "") + "|"
	}
	return "|" + benchmarkCursorWriterStyledPattern(innerWidth, row) + "|"
}

func benchmarkCursorWriterScrollFrames(width, height int) ([]string, []string) {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	if height < 6 {
		height = 6
	}
	previous := make([]string, height)
	next := make([]string, height)
	previous[0] = benchmarkCursorWriterFixedRow(width, "HEADER")
	next[0] = previous[0]
	previous[height-1] = benchmarkCursorWriterFixedRow(width, "FOOTER")
	next[height-1] = previous[height-1]
	for y := 1; y < height-1; y++ {
		previous[y] = benchmarkCursorWriterScrollRow(width, y-1)
	}
	for y := 1; y < height-2; y++ {
		next[y] = previous[y+1]
	}
	next[height-2] = benchmarkCursorWriterScrollRow(width, height-2)
	return previous, next
}

func benchmarkCursorWriterScrollRow(width, row int) string {
	label := "row-" + strconv.Itoa(row) + " "
	if len(label) >= width {
		return label[:width]
	}
	return label + strings.Repeat(string(byte('a'+row%26)), width-len(label))
}

func benchmarkCursorWriterFixedRow(width int, label string) string {
	if len(label) >= width {
		return label[:width]
	}
	return label + strings.Repeat(".", width-len(label))
}

func benchmarkCursorWriterRectScrollFrames() ([]string, []string) {
	const totalWidth = 120
	const leftWidth = 58
	const rightWidth = 58
	build := func(left, right string) string {
		return padRight(left, leftWidth) + "||" + padRight(right, rightWidth) + "\x1b[0m\x1b[K"
	}
	previous := []string{
		build("header", "right-pane-header"),
		build("left-row-aaaa "+strings.Repeat("a", 32), "right-pane-row-01 "+strings.Repeat("x", 38)),
		build("left-row-bbbb "+strings.Repeat("b", 32), "right-pane-row-02 "+strings.Repeat("x", 38)),
		build("left-row-cccc "+strings.Repeat("c", 32), "right-pane-row-03 "+strings.Repeat("x", 38)),
		build("left-row-dddd "+strings.Repeat("d", 32), "right-pane-row-04 "+strings.Repeat("x", 38)),
		build("left-row-eeee "+strings.Repeat("e", 32), "right-pane-row-05 "+strings.Repeat("x", 38)),
		build("left-row-ffff "+strings.Repeat("f", 32), "right-pane-row-06 "+strings.Repeat("x", 38)),
		build("footer", "right-pane-footer"),
	}
	next := []string{
		build("header", "right-pane-header"),
		build("left-row-bbbb "+strings.Repeat("b", 32), "right-pane-row-01 "+strings.Repeat("x", 38)),
		build("left-row-cccc "+strings.Repeat("c", 32), "right-pane-row-02 "+strings.Repeat("x", 38)),
		build("left-row-dddd "+strings.Repeat("d", 32), "right-pane-row-03 "+strings.Repeat("x", 38)),
		build("left-row-eeee "+strings.Repeat("e", 32), "right-pane-row-04 "+strings.Repeat("x", 38)),
		build("left-row-ffff "+strings.Repeat("f", 32), "right-pane-row-05 "+strings.Repeat("x", 38)),
		build("left-row-gggg "+strings.Repeat("g", 32), "right-pane-row-06 "+strings.Repeat("x", 38)),
		build("footer", "right-pane-footer"),
	}
	_ = totalWidth
	return previous, next
}

func padRight(text string, width int) string {
	if len(text) >= width {
		return text[:width]
	}
	return text + strings.Repeat(" ", width-len(text))
}

func benchmarkCursorWriterPattern(width, row int, style string) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	if style != "" {
		out.WriteString(style)
	}
	for x := 0; x < width; x++ {
		out.WriteByte(byte('a' + (x+row)%26))
	}
	if style != "" {
		out.WriteString("\x1b[0m")
	}
	return out.String()
}

func benchmarkCursorWriterStyledPattern(width, row int) string {
	if width <= 0 {
		return ""
	}
	palette := []string{
		"\x1b[0;97;44m",
		"\x1b[0;30;103m",
		"\x1b[0;96;40m",
		"\x1b[0;1;37;100m",
	}
	var out strings.Builder
	for x := 0; x < width; {
		style := palette[(x/6+row)%len(palette)]
		run := minInt(width-x, 6)
		out.WriteString(benchmarkCursorWriterPattern(run, row+x, style))
		x += run
	}
	return out.String()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
