package wire

// CardRoomKey carries the room a card's own id resolved to, on a request's context.
type CardRoomKey struct{}

// TaggedKey carries the room when it came from a `room~id` in the path or in a launch's `task_id`, the cases where the
// answer is tagged on the way back out. A board that named its room in a header asked that room directly and wants the
// room's own ids. See `retagCard`.
type TaggedKey struct{}

// HostFor is the synthetic host a room's requests are addressed to.
//
// It exists to key the connection pool, and nothing ever resolves it. Lowercase because a host is case-insensitive and
// `Alpha` and `alpha` are one room, and because two spellings would be two pools to the same place.
func HostFor(room string) string {
	if room == "" {
		return "room.atrium.internal"
	}
	return KeyOf(room) + ".room.atrium.internal"
}
