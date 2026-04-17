package shared

import (
	"strings"
	"sync"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/displaywidth"
)

var (
	eastAsianWidthNarrow = displaywidth.Options{EastAsianWidth: false}
	eastAsianWidthWide   = displaywidth.Options{EastAsianWidth: true}

	eastAsianAmbiguousWidthCache sync.Map
)

func IsEastAsianAmbiguousWidthCluster(content string) bool {
	if content == "" || strings.IndexByte(content, '\x1b') >= 0 {
		return false
	}
	if cached, ok := eastAsianAmbiguousWidthCache.Load(content); ok {
		return cached.(bool)
	}
	narrow := eastAsianWidthNarrow.String(content)
	wide := eastAsianWidthWide.String(content)
	ambiguous := narrow > 0 && wide > 0 && narrow != wide
	eastAsianAmbiguousWidthCache.Store(content, ambiguous)
	return ambiguous
}

func IsStableNarrowTerminalSymbol(content string) bool {
	switch content {
	case "─", "│", "┌", "┐", "└", "┘", "├", "┤", "┬", "┴", "┼", "●", "◆":
		return true
	default:
		return false
	}
}

func IsPrintableZeroWidthCluster(content string) bool {
	if content == "" || strings.IndexByte(content, '\x1b') >= 0 {
		return false
	}
	return xansi.StringWidth(content) == 0
}

func IsHostWidthAmbiguousCluster(content string, width int) bool {
	return IsAmbiguousEmojiVariationSelectorCluster(content, width) ||
		IsEastAsianAmbiguousWidthCluster(content) ||
		IsPrintableZeroWidthCluster(content)
}
