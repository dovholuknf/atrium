package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// The narrowest a runner's terminal may go.
//
// Claude Code wraps its own lines and reprints its whole conversation on every
// width change without clearing the scrollback, so every width a pty passes
// through leaves a copy of the conversation at that width for good. A narrow
// width leaves a narrow band nobody can read. The floor bounds the damage: the
// pty never goes under it, and a narrower window scrolls sideways instead.
//
// RUNNERS ONLY. A shell does not reprint, so narrow does no harm there.
const SettingTerminalMinCols = "terminal_min_cols"

const (
	defaultTerminalMinCols = 120
	minTerminalMinCols     = 40
	maxTerminalMinCols     = 400
)

// TerminalMinCols is the floor in force. Anything unusable reads as the
// default, the same rule `readNum` follows.
func TerminalMinCols(st *store.Store) int {
	v, err := st.Setting(SettingTerminalMinCols)
	if err != nil {
		return defaultTerminalMinCols
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < minTerminalMinCols || n > maxTerminalMinCols {
		return defaultTerminalMinCols
	}
	return n
}

// checkMinCols validates a typed floor. Out of range is refused rather than
// clamped, because a clamped value saves as something nobody typed.
func checkMinCols(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		// Cleared means back to the default, stored as empty so a later
		// default moves this with it.
		return "", nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < minTerminalMinCols || n > maxTerminalMinCols {
		return "", fmt.Errorf("the terminal width floor takes a whole number of columns from %d to %d, not %q",
			minTerminalMinCols, maxTerminalMinCols, value)
	}
	return strconv.Itoa(n), nil
}
