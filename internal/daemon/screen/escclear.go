package screen

import (
	"strings"
	"time"
)

// EscAgainWithin is how soon a second lone Esc has to follow the first to clear
// a prompt. See noteOperatorTyped.
const EscAgainWithin = 2 * time.Second

// ClearsOnEsc is a runner whose prompt a second Esc clears.
func ClearsOnEsc(runner string) bool {
	return strings.EqualFold(strings.TrimSpace(runner), "claude")
}
