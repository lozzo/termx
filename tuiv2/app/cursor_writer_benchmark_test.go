package app

import (
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
