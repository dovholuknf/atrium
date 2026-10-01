package daemon

import "strings"

// bracketedPaste wraps text in bracketed paste markers, and is the only place that does.
//
// Text that holds the closing marker ends its own paste, and whatever follows arrives as keystrokes: `ESC[Z` is
// Shift+Tab, which cycles the permission mode. So both markers are removed from the text first. A lone trailing ESC
// goes too, since it would join the closer and form a different sequence.
func bracketedPaste(text string) string {
	return pasteOpen + stripPasteMarkers(text) + pasteClose
}

const (
	pasteOpen  = "\x1b[200~"
	pasteClose = "\x1b[201~"
)

// stripPasteMarkers removes both markers until none remain, because removing one can splice its neighbours into a
// new one (`ESC[20` + marker + `1~`).
func stripPasteMarkers(text string) string {
	for {
		out := strings.ReplaceAll(strings.ReplaceAll(text, pasteOpen, ""), pasteClose, "")
		if out == text {
			break
		}
		text = out
	}
	return strings.TrimSuffix(text, "\x1b")
}
