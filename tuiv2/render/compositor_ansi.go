package render

import (
	"strconv"
	"strings"
	"sync"
)

var styleANSICache sync.Map

func styleDiffANSI(from, to drawStyle) string {
	if from == to {
		return ""
	}
	return styleANSI(to)
}

func writeCHAANSI(out *strings.Builder, col int) {
	writeSimpleCSI(out, 'G', col)
}

func writeECHANSI(out *strings.Builder, count int) {
	writeSimpleCSI(out, 'X', count)
}

func writeSimpleCSI(out *strings.Builder, final byte, value int) {
	if out == nil {
		return
	}
	out.WriteByte('\x1b')
	out.WriteByte('[')
	writeANSIInt(out, value)
	out.WriteByte(final)
}

func writeANSIInt(out *strings.Builder, value int) {
	if out == nil {
		return
	}
	var scratch [24]byte
	buf := strconv.AppendInt(scratch[:0], int64(value), 10)
	_, _ = out.Write(buf)
}

func styleANSI(s drawStyle) string {
	if cached, ok := styleANSICache.Load(s); ok {
		return cached.(string)
	}
	var b strings.Builder
	b.WriteString("\x1b[0")
	if s == (drawStyle{}) {
		b.WriteByte('m')
		ansi := b.String()
		styleANSICache.Store(s, ansi)
		return ansi
	}
	if s.FG != "" {
		writeFGColor(&b, s.FG)
	}
	if s.BG != "" {
		writeBGColor(&b, s.BG)
	}
	if s.Bold {
		b.WriteString(";1")
	}
	if s.Italic {
		b.WriteString(";3")
	}
	if s.Underline {
		b.WriteString(";4")
	}
	if s.Reverse {
		b.WriteString(";7")
	}
	b.WriteByte('m')
	ansi := b.String()
	styleANSICache.Store(s, ansi)
	return ansi
}

// writeFGColor appends the ANSI foreground color sequence for the given color
// string. Supported formats: "ansi:N" (basic palette 0-15), "idx:N" (256-color
// index), "#rrggbb" (24-bit RGB).
func writeFGColor(b *strings.Builder, c string) {
	if n, ok := parseAnsiColor(c); ok {
		if n <= 7 {
			b.WriteString(";3")
			b.WriteString(itoa(n))
		} else {
			b.WriteString(";9")
			b.WriteString(itoa(n - 8))
		}
		return
	}
	if n, ok := parseIdxColor(c); ok {
		b.WriteString(";38;5;")
		b.WriteString(itoa(n))
		return
	}
	if rgb, ok := hexToRGB(c); ok {
		b.WriteString(";38;2;")
		b.WriteString(itoa(rgb[0]))
		b.WriteByte(';')
		b.WriteString(itoa(rgb[1]))
		b.WriteByte(';')
		b.WriteString(itoa(rgb[2]))
	}
}

// writeBGColor appends the ANSI background color sequence.
func writeBGColor(b *strings.Builder, c string) {
	if n, ok := parseAnsiColor(c); ok {
		if n <= 7 {
			b.WriteString(";4")
			b.WriteString(itoa(n))
		} else {
			b.WriteString(";10")
			b.WriteString(itoa(n - 8))
		}
		return
	}
	if n, ok := parseIdxColor(c); ok {
		b.WriteString(";48;5;")
		b.WriteString(itoa(n))
		return
	}
	if rgb, ok := hexToRGB(c); ok {
		b.WriteString(";48;2;")
		b.WriteString(itoa(rgb[0]))
		b.WriteByte(';')
		b.WriteString(itoa(rgb[1]))
		b.WriteByte(';')
		b.WriteString(itoa(rgb[2]))
	}
}

func parseAnsiColor(c string) (int, bool) {
	if !strings.HasPrefix(c, "ansi:") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(c, "ansi:"))
	if err != nil || n < 0 || n > 15 {
		return 0, false
	}
	return n, true
}

func parseIdxColor(c string) (int, bool) {
	if !strings.HasPrefix(c, "idx:") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(c, "idx:"))
	if err != nil || n < 0 || n > 255 {
		return 0, false
	}
	return n, true
}

func hexToRGB(hex string) ([3]int, bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return [3]int{}, false
	}
	r := hexByte(hex[0])<<4 | hexByte(hex[1])
	g := hexByte(hex[2])<<4 | hexByte(hex[3])
	b := hexByte(hex[4])<<4 | hexByte(hex[5])
	return [3]int{r, g, b}, true
}

func hexByte(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return 0
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
