package store

import (
	"errors"
	"strings"
)

// ReservedHandle is the sender atrium itself uses: the line that lists a launcher's owed items
// after its context clears, the arbiter notices and every other word atrium says in its own voice.
// No card may be called it, or answer to it as an alias, or a worker could pass itself off as
// atrium to a launcher that trusts what atrium says. A name a hook DERIVED from a folder called
// `atrium` (this repository's own) is not refused: it is kept, as ReservedDerived. See
// docs/rnd/long-turn-checkin-design.md section 11 and NameFromDir.
const ReservedHandle = "atrium"

// ReservedDerived is what a folder-derived `atrium` becomes. The same every time, so the
// session finds its own card again.
const ReservedDerived = "atrium-dir"

// ErrReservedName is a told name or an alias that is the reserved handle.
var ErrReservedName = errors.New(`"atrium" is atrium's own name and cannot be a card's handle or alias`)

// isReserved says whether a bare or machine-qualified name is the reserved handle.
func isReserved(name string) bool {
	return strings.EqualFold(strings.TrimSpace(LocalName(name)), ReservedHandle)
}
