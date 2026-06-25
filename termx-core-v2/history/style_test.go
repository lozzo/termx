package history

import "testing"

func TestResolveCellStyleKeepsDefaultColorsThemeResolved(t *testing.T) {
	theme := HistoryTheme{
		DefaultFG: "#eeeeee",
		DefaultBG: "#111111",
	}

	resolved := ResolveCellStyle(CellStyle{Bold: true}, theme)

	if resolved.FG != "#eeeeee" || resolved.BG != "#111111" || !resolved.Bold {
		t.Fatalf("default style should resolve from current theme, got %#v", resolved)
	}
}

func TestResolveCellStyleDistinguishesContentColorTokens(t *testing.T) {
	theme := HistoryTheme{
		DefaultFG: "#eeeeee",
		DefaultBG: "#111111",
	}
	theme.ANSI[1] = "#cc0000"
	theme.Indexed[24] = "#005f87"

	resolved := ResolveCellStyle(CellStyle{
		FG:        "ansi:1",
		BG:        "idx:24",
		Underline: true,
	}, theme)

	if resolved.FG != "#cc0000" || resolved.BG != "#005f87" || !resolved.Underline {
		t.Fatalf("ansi/index tokens should resolve through viewing palette, got %#v", resolved)
	}
	truecolor := ResolveCellStyle(CellStyle{FG: "#010203", BG: "#040506"}, theme)
	if truecolor.FG != "#010203" || truecolor.BG != "#040506" {
		t.Fatalf("explicit RGB belongs to content and must not be theme-replaced, got %#v", truecolor)
	}
	passthrough := ResolveCellStyle(CellStyle{FG: "ansi:2", BG: "idx:25"}, HistoryTheme{})
	if passthrough.FG != "ansi:2" || passthrough.BG != "idx:25" {
		t.Fatalf("missing viewing palette should keep terminal SGR tokens, got %#v", passthrough)
	}
}

func TestResolveCellStyleDoesNotMutateHistoryPayload(t *testing.T) {
	style := CellStyle{FG: "ansi:2", BG: "", Reverse: true}
	theme := HistoryTheme{DefaultFG: "#eeeeee", DefaultBG: "#111111"}
	theme.ANSI[2] = "#00aa00"

	_ = ResolveCellStyle(style, theme)

	if style.FG != "ansi:2" || style.BG != "" || !style.Reverse {
		t.Fatalf("view-time color resolution must not rewrite payload style, got %#v", style)
	}
}
