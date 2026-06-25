package history

import (
	"strconv"
	"strings"
)

const (
	ColorTokenDefaultFG = "default:fg"
	ColorTokenDefaultBG = "default:bg"
)

// HistoryTheme 是查看历史时的颜色解析输入；它不是 history payload truth。
type HistoryTheme struct {
	DefaultFG string
	DefaultBG string
	ANSI      [16]string
	Indexed   [256]string
}

// ResolvedCellStyle 是 view-time projection。默认色在这里按当前主题解析；
// ANSI/256 token 只在调用方提供查看调色板时解析，否则保留原 terminal token。
type ResolvedCellStyle struct {
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

func ResolveCellStyle(style CellStyle, theme HistoryTheme) ResolvedCellStyle {
	return ResolvedCellStyle{
		FG:            ResolveHistoryColor(style.FG, theme, true),
		BG:            ResolveHistoryColor(style.BG, theme, false),
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

func ResolveHistoryColor(token string, theme HistoryTheme, foreground bool) string {
	switch token {
	case "":
		if foreground {
			return theme.DefaultFG
		}
		return theme.DefaultBG
	case ColorTokenDefaultFG:
		return theme.DefaultFG
	case ColorTokenDefaultBG:
		return theme.DefaultBG
	}
	if index, ok := historyColorTokenIndex(token, "ansi:", len(theme.ANSI)); ok {
		if theme.ANSI[index] != "" {
			return theme.ANSI[index]
		}
		return token
	}
	if index, ok := historyColorTokenIndex(token, "idx:", len(theme.Indexed)); ok {
		if theme.Indexed[index] != "" {
			return theme.Indexed[index]
		}
		return token
	}
	return token
}

func historyColorTokenIndex(token string, prefix string, limit int) (int, bool) {
	if !strings.HasPrefix(token, prefix) {
		return 0, false
	}
	index, err := strconv.Atoi(strings.TrimPrefix(token, prefix))
	if err != nil || index < 0 || index >= limit {
		return 0, false
	}
	return index, true
}
