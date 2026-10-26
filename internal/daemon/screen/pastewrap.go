package screen

import "strings"

// BracketedPaste wraps text in bracketed paste markers, and is the only place that does.
//
// Text that holds the closing marker ends its own paste, and whatever follows arrives as keystrokes: `ESC[Z` is
// Shift+Tab, which cycles the permission mode. So both markers are removed from the text first. A lone trailing ESC
// goes too, since it would join the closer and form a different sequence.
func BracketedPaste(text string) string {
	return PasteOpen + StripPasteMarkers(text) + PasteClose
}

const (
	PasteOpen  = "\x1b[200~"
	PasteClose = "\x1b[201~"
)

// StripPasteMarkers removes both markers until none remain, because removing one can splice its neighbours into a
// new one (`ESC[20` + marker + `1~`).
func StripPasteMarkers(text string) string {
	for {
		out := strings.ReplaceAll(strings.ReplaceAll(text, PasteOpen, ""), PasteClose, "")
		if out == text {
			break
		}
		text = out
	}
	return strings.TrimSuffix(text, "\x1b")
}
