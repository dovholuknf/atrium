package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// How much of a session you can scroll back through.
//
// TWO NUMBERS, because there are two buffers and the smaller one wins.
//
// The daemon keeps the last N bytes of every supervised runner's output and
// writes the whole lot down the socket when a browser attaches. Nothing older
// than that buffer can ever reach the page, whatever the page is willing to
// hold, so this is the one that decides whether scrolling up finds anything.
//
// The browser then keeps the last N LINES of what it was sent. Past that xterm
// drops from the top, so a generous daemon with a small browser buffer gives a
// short scrollback that cost a lot to produce.
//
// Sized in bytes on one side and lines on the other because that is what each
// end actually counts, and converting between them means guessing a line
// length. A progress bar redrawing itself is one line and can be megabytes.
const (
	SettingScrollbackMB    = "scrollback_mb"
	SettingScrollbackLines = "scrollback_lines"
)

// The defaults, and the ceilings.
//
// The ceilings are about somebody typing an extra digit into a box rather than
// about what is safe: the megabytes are resident per RUNNING runner, and the
// lines are resident per open terminal in the browser. Anyone who wants 512 and
// means it can have it.
const (
	defaultScrollbackMB    = 16
	maxScrollbackMB        = 512
	defaultScrollbackLines = 50000
	maxScrollbackLines     = 1000000
)

// ScrollbackBytes is what the supervisor sizes a runner's ring buffer to.
//
// Read at spawn, so a change applies to the next runner started rather than to
// one already running. Resizing a live ring means either dropping the
// scrollback it holds or copying it under the lock every write takes, and
// neither is worth it for a number changed twice a year.
func ScrollbackBytes(st *store.Store) int {
	return scrollbackMB(st) << 20
}

func scrollbackMB(st *store.Store) int {
	return readNum(st, SettingScrollbackMB, defaultScrollbackMB, maxScrollbackMB)
}

func scrollbackLines(st *store.Store) int {
	return readNum(st, SettingScrollbackLines, defaultScrollbackLines, maxScrollbackLines)
}

// readNum answers with the default for anything it cannot use.
//
// Unreadable, empty, not a number and out of range all mean the same thing
// here: nobody has usefully set this. Falling back is right because the
// alternative is a terminal with no scrollback at all because of a typo.
func readNum(st *store.Store, key string, def, max int) int {
	v, err := st.Setting(key)
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 || n > max {
		return def
	}
	return n
}

// checkNum validates what somebody typed, and says what the limit is rather
// than only refusing.
func checkNum(what, value string, max int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		// Cleared means back to the default. Stored as empty rather than as
		// today's number, so moving the default later moves this with it.
		return "", nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("%s takes a whole number above zero, not %q", what, value)
	}
	if n > max {
		return "", fmt.Errorf("%s is capped at %d, and %d is more than that", what, max, n)
	}
	return strconv.Itoa(n), nil
}
