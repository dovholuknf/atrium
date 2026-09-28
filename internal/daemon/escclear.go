package daemon

import (
	"strings"
	"time"
)

// escAgainWithin is how soon a second lone Esc has to follow the first to clear
// a prompt. See noteOperatorTyped.
const escAgainWithin = 2 * time.Second

// clearsOnEsc is a runner whose prompt a second Esc clears.
func clearsOnEsc(runner string) bool {
	return strings.EqualFold(strings.TrimSpace(runner), "claude")
}
