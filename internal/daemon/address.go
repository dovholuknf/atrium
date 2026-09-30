package daemon

import (
	"fmt"
	"strings"
)

// How a session names a card on another room. See docs/fabric/cross-room-say-design.md.
//
// `name` and `@name` stay in the sender's own room. `name@room` and
// `@name@room` reach `name` on `room`, split on the LAST `@` so a handle that
// holds one still parses when the room is given. `room~id` is the aggregate
// board's tagged id and means `id@room`.
//
// A COPY OF internal/link/address.go, because neither package imports the
// other. Both carry the same table test, so the two cannot drift apart
// without one of them failing.

// SplitAddress pulls a room off an address. An empty room is the sender's own.
// It refuses an address with an empty name or an empty room part, because a
// half-typed address sent to the wrong place is worse than a sentence.
func SplitAddress(addr string) (name, room string, err error) {
	s := strings.TrimSpace(addr)
	s = strings.TrimPrefix(s, "@")
	if s == "" {
		return "", "", fmt.Errorf("say who to")
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		name, room = strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
		if name == "" || room == "" {
			return "", "", fmt.Errorf("%q is not an address. use name for this room, or name@room for another", addr)
		}
		return strings.TrimPrefix(name, "@"), room, nil
	}
	if i := strings.Index(s, "~"); i > 0 {
		if s[i+1:] == "" {
			return "", "", fmt.Errorf("%q is not an address. use name for this room, or name@room for another", addr)
		}
		return s[i+1:], s[:i], nil
	}
	return s, "", nil
}

// otherRoom is the room part when it names a room other than this one, and
// empty when it names none or names this one, which is the same as none.
// Case does not matter, the way the hub folds room names.
func (d *Daemon) otherRoom(room string) string {
	room = strings.TrimSpace(room)
	if room == "" || strings.EqualFold(room, strings.TrimSpace(d.opts.Room)) {
		return ""
	}
	return room
}
