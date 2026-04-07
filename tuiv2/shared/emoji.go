package shared

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
	// AmbiguousEmojiVariationSelectorAdvance keeps the original grapheme visible
	// and emits a one-column cursor advance after it. This compensates for hosts
	// that render the grapheme but only advance one column.
	AmbiguousEmojiVariationSelectorAdvance AmbiguousEmojiVariationSelectorMode = "advance"
	// AmbiguousEmojiVariationSelectorStrip falls back to the text presentation
	// plus a visible padding cell. It is the safest fallback when the host
	// terminal behavior is unknown.
	AmbiguousEmojiVariationSelectorStrip AmbiguousEmojiVariationSelectorMode = "strip"
)
