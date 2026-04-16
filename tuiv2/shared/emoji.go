package shared

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// AmbiguousEmojiVariationSelectorMode controls how termx serializes grapheme
// clusters like U+267B U+FE0F ("♻️") when drawing to the host terminal.
//
// These clusters are width-ambiguous across terminal emulators: some hosts
// advance one column while others advance two. termx keeps the internal pane
// model width-aware at two columns and chooses an output strategy that keeps
// the host cursor in sync with that model.
type AmbiguousEmojiVariationSelectorMode string

const (
	// AmbiguousEmojiVariationSelectorRaw keeps the original grapheme untouched.
	// Use this when the host terminal already advances the cursor by two cells.
	AmbiguousEmojiVariationSelectorRaw AmbiguousEmojiVariationSelectorMode = "raw"
	// 中文说明：advance 只表示“宿主把这个 emoji 画出来了，但光标只前进 1 列”。
	// 由于标准行渲染不能安全夹带行内光标移动，最终会按 strip 的方式输出。
	// AmbiguousEmojiVariationSelectorAdvance classifies hosts that render the
	// grapheme but only advance one column. The renderer treats this the same as
	// strip mode because the standard line renderer cannot safely embed mid-line
	// cursor movement in a frame string.
	AmbiguousEmojiVariationSelectorAdvance AmbiguousEmojiVariationSelectorMode = "advance"
	// AmbiguousEmojiVariationSelectorStrip falls back to the text presentation
	// plus a visible padding cell. It is the safest fallback when the host
	// terminal behavior is unknown.
	AmbiguousEmojiVariationSelectorStrip AmbiguousEmojiVariationSelectorMode = "strip"
)

func IsAmbiguousEmojiVariationSelectorCluster(content string, width int) bool {
	if width != 2 || !strings.ContainsRune(content, '\uFE0F') {
		return false
	}
	if strings.ContainsRune(content, '\u200D') || strings.ContainsRune(content, '\u20E3') {
		return false
	}
	stripped := strings.ReplaceAll(content, "\uFE0F", "")
	return stripped != "" && xansi.StringWidth(stripped) > 0 && xansi.StringWidth(stripped) <= width
}
