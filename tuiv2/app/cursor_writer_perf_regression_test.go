package app

import (
	"strings"
	"testing"
)

func TestNormalizedJoinedLinesWireLenMatchesNormalizedFrameLenJoinedLines(t *testing.T) {
	tests := [][]string{
		nil,
		{},
		{"abc"},
		{"ab", "cde", ""},
		{"", "", ""},
		{"\x1b[31mred\x1b[0m", "next"},
		{"a\nb"},
		{"left\nmiddle", "right\nend"},
	}

	for _, lines := range tests {
		frame := strings.Join(lines, "\n")
		if got, want := normalizedJoinedLinesWireLen(lines), normalizedFrameLen(frame); got != want {
			t.Fatalf("normalizedJoinedLinesWireLen(%q)=%d want %d", lines, got, want)
		}
	}
}
