package store

import (
	"errors"
	"strings"
	"unicode"
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

// lookalikes are letters of "atrium" written in another script, folded to the ASCII they pass for.
var lookalikes = map[rune]rune{
	'а': 'a', 'ɑ': 'a', 'α': 'a', 'і': 'i', 'ı': 'i', 'ι': 'i', 'ᴛ': 't', 'т': 't', 'г': 'r', 'ꭇ': 'r',
	'υ': 'u', 'ᴜ': 'u', 'м': 'm', 'ᴍ': 'm',
}

// skeleton is a name as a reader sees it: marks and invisible characters dropped, full-width and
// look-alike letters folded, anything after an `@` ignored (`atrium@x` reads as atrium's voice on x).
func skeleton(name string) string {
	var b strings.Builder
	for _, r := range LocalName(strings.TrimSpace(name)) {
		switch {
		case r == '@':
			return strings.ToLower(b.String())
		case unicode.Is(unicode.Cf, r), unicode.Is(unicode.Mn, r), unicode.IsSpace(r) && b.Len() == 0:
			continue
		case r >= 0xFF01 && r <= 0xFF5E:
			r -= 0xFEE0
		}
		if f, ok := lookalikes[unicode.ToLower(r)]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return strings.ToLower(strings.TrimSpace(b.String()))
}

// IsReserved says whether a bare or machine-qualified name is, or reads as, the reserved handle.
func IsReserved(name string) bool { return skeleton(name) == ReservedHandle }

func isReserved(name string) bool { return IsReserved(name) }
