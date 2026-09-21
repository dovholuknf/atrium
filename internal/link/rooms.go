package link

import (
	"strings"
)

// Which room a request is for, and how a card is addressed across several.
//
// ── the two modes ────────────────────────────────────────
//
// SCOPED. The board names a room and the hub is a byte pipe to it. Nothing is
// parsed, nothing is merged, and the room's answers arrive untouched. This is
// the mode everything was built for and it stays the cheap one.
//
// AGGREGATE. No room is named, so the board is asking about all of them. The
// hub fans out, merges, and hands back one answer. It has to understand the
// payloads to do that, which is the thing `docs/federation-design-v2.md` warned
// against, and the operator overrode it deliberately: a hub that cannot show
// four machines at once is not the thing they asked for.
//
// ── how the board says which ─────────────────────────────
//
// A header on every JSON call and a query parameter on the websocket, because
// a websocket cannot carry a header. Both are read here so the rest of the hub
// asks one function.
//
// NOT A PATH PREFIX. Every URL the board builds is relative, so a prefix would
// mean rewriting them all and the board's JavaScript is meant to stay as it is.

// RoomHeader is how a scoped board names its room.
const RoomHeader = "X-Atrium-Room"

// RoomParam is the same thing for a websocket, which cannot set a header.
const RoomParam = "atrium_room"

// idJoin separates a room from a card id in aggregate mode.
//
// A TILDE, because it survives a URL path segment unescaped and appears in no
// ULID, no directory name and no room name this can produce. A slash would be
// read as a path separator by every router in the chain, and a colon is taken
// by a Windows drive letter in the places these ids end up beside paths.
const idJoin = "~"

// tagFor is a card ID as the aggregate view addresses it.
//
// THE HUB REWRITES IDS ON THE WAY OUT AND UNDOES IT ON THE WAY IN, which is
// what makes the aggregate board work with a board that was never told about
// rooms. `/v1/tasks` comes back with `sg4~01a0...`, so every per-card URL the
// board builds from that id carries the room with it, and the hub can route it
// without the board knowing why.
//
// Two rooms can mint the same card id, so without this a click in the aggregate
// view would reach whichever room answered first.
func tagFor(room, id string) string {
	if room == "" || id == "" {
		return id
	}
	return room + idJoin + id
}

// splitTag pulls a room back off an id. Answers an empty room for an id that
// carries none, which is every id in scoped mode.
func splitTag(id string) (room, bare string) {
	i := strings.Index(id, idJoin)
	if i <= 0 {
		return "", id
	}
	return id[:i], id[i+1:]
}
