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

func TestChangedRowsPatchStatsMatchesRenderedPatch(t *testing.T) {
	previous := []string{
		"same",
		"old-1",
		"old-2",
		"same",
		"old\ninner",
	}
	next := []string{
		"same",
		"new-1",
		"new-2",
		"same",
		"new\ninner",
	}

	payload, changed := renderChangedRows(previous, next)
	statsChanged, statsBytes := changedRowsPatchStats(previous, next)
	if statsChanged != changed {
		t.Fatalf("changedRowsPatchStats changed=%d want %d", statsChanged, changed)
	}
	if statsBytes != normalizedFrameLen(payload) {
		t.Fatalf("changedRowsPatchStats bytes=%d want %d payload=%q", statsBytes, normalizedFrameLen(payload), payload)
	}
}
